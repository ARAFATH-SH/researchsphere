// Command recseval measures collaborator recommendations (slice 17) on the sample data of
// slice 16, in the development database:
//
//   - calibrate (make calibrate-recs) fits the AI-2.2 score map on the TUNING split only and
//     prints its constants, which go into config (RECS_SCORE_* defaults); then checks it on
//     profile-project pairs of the tuning split and fits the project map
//     (RECS_PROJECT_SCORE_*, slice 19);
//   - eval (make eval-recs) measures NFR-6 on the held-out TEST split: precision@10 and
//     recall@10 against "relevant = same main cluster", for people and then for project
//     matching in both directions (AI-3.1, AI-2.3);
//   - bench (make bench-recs) measures NFR-1: the endpoint's work for 200 random sample users,
//     p50, p95 and max, and EXPLAIN ANALYZE of the ranking query; then the same for both
//     project matching endpoints;
//   - eval-search (make eval-search) measures AI-5.2 for hybrid researcher search (slice 18):
//     precision@10 of plain-language queries that share no word with their cluster, among
//     TEST split profiles, hybrid against keyword;
//   - bench-search (make bench-search) measures NFR-1 for hybrid search: p50, p95 and max
//     with a cold and a cached query vector, and EXPLAIN ANALYZE of the meaning query.
//
// Definitions and results are in docs/evaluation.md. It reads DATABASE_URL and APP_ENV and
// refuses anything but a local development database (checkTarget). It only reads. The test
// split is never used to fit anything.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/config"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	recommendationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/usecase"
)

// defaultSeed draws the calibration queries and the benchmark users. Fixed, so a run can be
// repeated; slice 16 used 16 for the data.
const defaultSeed = 17

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "recseval:", err)
		stop()
		os.Exit(1)
	}
}

type options struct {
	mode    string
	seed    uint64
	queries int // calibrate: tuning queries; bench: measured users
}

func parseFlags(args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("recseval", flag.ContinueOnError)
	fs.Uint64Var(&o.seed, "seed", defaultSeed, "seed of the random draws (calibration queries, benchmark users)")
	fs.IntVar(&o.queries, "n", 0, "calibrate: tuning queries (default 2000); bench: users measured (default 200)")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if fs.NArg() != 1 {
		return o, errors.New("usage: recseval [-seed N] [-n N] calibrate|eval|bench|eval-search|bench-search")
	}
	o.mode = fs.Arg(0)
	switch o.mode {
	case "calibrate":
		if o.queries == 0 {
			o.queries = 2000
		}
	case "bench", "bench-search":
		if o.queries == 0 {
			o.queries = 200
		}
	case "eval":
		if o.queries != 0 {
			return o, errors.New("eval uses every test profile; -n does not apply")
		}
	case "eval-search":
		if o.queries != 0 {
			return o, errors.New("eval-search uses every evaluation query; -n does not apply")
		}
	default:
		return o, fmt.Errorf("unknown mode %q: use calibrate, eval, bench, eval-search or bench-search", o.mode)
	}
	if o.queries < 0 || o.queries > 10_000 {
		return o, errors.New("-n must be 1 to 10000")
	}
	return o, nil
}

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	o, err := parseFlags(args)
	if err != nil {
		return err
	}
	databaseURL := getenv("DATABASE_URL")
	if err := checkTarget(databaseURL, getenv("APP_ENV")); err != nil {
		return err
	}
	pool, err := postgres.NewPool(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	s, err := loadSamples(ctx, pool)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Sample data in %s: %d embedded sample profiles (%d tuning, %d test) in %d clusters, model %s.\n",
		describeTarget(databaseURL), len(s.all), len(s.ids("tuning")), len(s.ids("test")), len(s.clusterSize), s.model)

	switch o.mode {
	case "calibrate":
		if err := calibrate(ctx, pool, s, o, out); err != nil {
			return err
		}
		ps, err := loadProjectSamples(ctx, pool, s.model)
		if err != nil {
			return err
		}
		people, err := scoreMapFromEnv(getenv)
		if err != nil {
			return err
		}
		return calibrateProjects(ctx, pool, s, ps, people, o, out)
	case "eval-search", "bench-search":
		ai, err := newAI(getenv)
		if err != nil {
			return err
		}
		if o.mode == "eval-search" {
			return evaluateSearch(ctx, pool, s, ai, out)
		}
		return benchSearch(ctx, pool, s, ai, o, out)
	case "eval":
		cfg, err := scoreMapFromEnv(getenv)
		if err != nil {
			return err
		}
		if err := evaluate(ctx, pool, s, cfg, o.seed, out); err != nil {
			return err
		}
		pcfg, err := projectScoreMapFromEnv(getenv)
		if err != nil {
			return err
		}
		ps, err := loadProjectSamples(ctx, pool, s.model)
		if err != nil {
			return err
		}
		return evaluateProjects(ctx, pool, s, ps, pcfg, o.seed, out)
	default:
		cfg, err := scoreMapFromEnv(getenv)
		if err != nil {
			return err
		}
		if err := bench(ctx, pool, s, cfg, o, out); err != nil {
			return err
		}
		pcfg, err := projectScoreMapFromEnv(getenv)
		if err != nil {
			return err
		}
		ps, err := loadProjectSamples(ctx, pool, s.model)
		if err != nil {
			return err
		}
		return benchProjects(ctx, pool, s, ps, cfg, pcfg, o, out)
	}
}

// scoreMapFromEnv is the score map the API would use with these settings.
func scoreMapFromEnv(getenv func(string) string) (recommendationusecase.ScoreMap, error) {
	m := recommendationusecase.ScoreMap{Model: config.DefaultRecsScoreModel, Slope: config.DefaultRecsScoreSlope, Intercept: config.DefaultRecsScoreIntercept}
	if v := strings.TrimSpace(getenv("RECS_SCORE_MODEL")); v != "" {
		m.Model = v
	}
	for name, dst := range map[string]*float64{"RECS_SCORE_SLOPE": &m.Slope, "RECS_SCORE_INTERCEPT": &m.Intercept} {
		if v := strings.TrimSpace(getenv(name)); v != "" {
			if _, err := fmt.Sscan(v, dst); err != nil {
				return m, fmt.Errorf("%s must be a number", name)
			}
		}
	}
	return m, m.Validate()
}

func describeTarget(databaseURL string) string {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return "the database"
	}
	return fmt.Sprintf("database %s on %s", strings.TrimPrefix(u.Path, "/"), u.Host)
}

// checkTarget refuses anything but a local development database, as cmd/seedsample does:
// the sample data lives only there.
func checkTarget(databaseURL, appEnv string) error {
	if databaseURL == "" {
		return errors.New("DATABASE_URL is not set")
	}
	switch strings.ToLower(strings.TrimSpace(appEnv)) {
	case "development":
	case "production":
		return errors.New("refusing to run with APP_ENV=production")
	default:
		return errors.New("APP_ENV is not development: the measurements need the development sample data")
	}
	u, err := url.Parse(databaseURL)
	if err != nil {
		return errors.New("DATABASE_URL is not a valid URL") // the error would repeat the password
	}
	name := strings.TrimPrefix(u.Path, "/")
	if name == "" {
		return errors.New("DATABASE_URL names no database")
	}
	if strings.HasSuffix(name, "_test") {
		return fmt.Errorf("refusing the test database %s", name)
	}
	if host := u.Hostname(); host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return fmt.Errorf("refusing the database on %s: only a local database is measured", host)
	}
	return nil
}
