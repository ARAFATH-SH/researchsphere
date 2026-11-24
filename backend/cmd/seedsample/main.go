// Command seedsample loads synthetic sample profiles into the development database (slice 16),
// so recommendations and search have something to find, NFR-1 can be measured with 10,000
// profiles, and NFR-6 has labelled data for collaborator matching. `make sample-data`
// (default 1,000) and `make sample-data n=10000` run it; `make sample-purge` deletes the
// sample accounts again. docs/decisions.md, 2026-11-15 (sample data).
//
// The data is deterministic: profile number seq is made from (seed, seq) alone, so the same
// command makes the same data, and a second run tops up to n without duplicating. Every sample
// account is at @sample.researchsphere.local, is verified, has the password SAMPLE_PASSWORD
// and has a row in sample_profiles with its research clusters and its tuning or test split.
// It reads DATABASE_URL, APP_ENV, SAMPLE_PASSWORD and TEST_EMAIL_DOMAINS, and refuses anything
// but a local development database (see checkTarget). Never run it in production.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/argon2"
	authpg "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/postgres"
	authusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/logging"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
)

// defaultSeed is the generator seed unless -seed says otherwise. Changing it changes every
// profile, so it stays fixed.
const defaultSeed = 16

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "seedsample:", err)
		stop()
		os.Exit(1)
	}
}

type options struct {
	n     int
	seed  int64
	purge bool
	// purgeVenues deletes only the sample venues, keeping sample accounts and projects.
	purgeVenues bool
	wait        bool
	projects    bool
	venues      bool
	// testVenues makes the sample venues in the TEST database for the NFR-6 venue evaluation.
	testVenues bool
}

func parseFlags(args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("seedsample", flag.ContinueOnError)
	fs.IntVar(&o.n, "n", 1000, "how many sample profiles there should be (tops up to n)")
	fs.Int64Var(&o.seed, "seed", defaultSeed, "generator seed; the same seed makes the same data")
	fs.BoolVar(&o.purge, "purge", false, "delete every sample account instead, through the account deletion")
	fs.BoolVar(&o.purgeVenues, "purge-venues", false, "delete only the sample venues (with their embeddings and jobs), keeping sample accounts and projects")
	fs.BoolVar(&o.wait, "wait", false, "after loading, wait until the running API has embedded every sample profile")
	fs.BoolVar(&o.projects, "projects", false, "also make one sample project per 10 sample profiles, in the owner's cluster")
	fs.BoolVar(&o.venues, "venues", false, "also make the fictional sample venues, 10 per research cluster")
	fs.BoolVar(&o.testVenues, "test-venues", false, "make and embed the sample venues in the TEST database (TEST_DATABASE_URL) only, for make eval-venues")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if fs.NArg() > 0 {
		return o, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if o.n < 1 || o.n > maxProfiles {
		return o, fmt.Errorf("-n must be 1 to %d", maxProfiles)
	}
	if o.seed < 0 {
		return o, errors.New("-seed must not be negative")
	}
	if o.purge && (o.wait || o.projects || o.venues) {
		return o, errors.New("use -purge alone, without -wait, -projects or -venues")
	}
	if o.purgeVenues && (o.purge || o.wait || o.projects || o.venues) {
		return o, errors.New("use -purge-venues alone")
	}
	if o.testVenues && (o.purge || o.purgeVenues || o.wait || o.projects || o.venues) {
		return o, errors.New("use -test-venues alone")
	}
	return o, nil
}

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	o, err := parseFlags(args)
	if err != nil {
		return err
	}
	if o.testVenues {
		return runTestVenues(ctx, getenv, o.seed, out)
	}
	databaseURL := getenv("DATABASE_URL")
	if err := checkTarget(databaseURL, getenv("APP_ENV")); err != nil {
		return err
	}
	password := getenv("SAMPLE_PASSWORD")
	if !o.purge && !o.purgeVenues {
		if err := checkPassword(password); err != nil {
			return err
		}
	}
	pool, err := postgres.NewPool(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	target := describeTarget(databaseURL)

	if o.purgeVenues {
		venues, err := purgeVenues(ctx, pool)
		if err != nil {
			return fmt.Errorf("delete the sample venues: %w", err)
		}
		fmt.Fprintf(out, "Deleted %d sample venues from %s; sample accounts and projects are kept.\n", venues, target)
		return nil
	}

	if o.purge {
		// The deletion needs only the auth store; the mailer, tokens and login counter are
		// for routes, which this command does not serve.
		logger := logging.New(os.Stderr, slog.LevelWarn)
		auth := authusecase.New(authpg.NewStore(pool), nil, nil, nil, nil, authusecase.Config{}, time.Now, logger)
		fmt.Fprintf(out, "Purging the sample accounts (@%s) from %s.\n", sampleDomain, target)
		started := time.Now()
		deleted, kept, err := purge(ctx, pool, auth, out)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Deleted %d sample accounts in %s; kept %d.\n", deleted, time.Since(started).Round(time.Millisecond), kept)
		venues, err := purgeVenues(ctx, pool)
		if err != nil {
			return fmt.Errorf("delete the sample venues: %w", err)
		}
		fmt.Fprintf(out, "Deleted %d sample venues.\n", venues)
		return nil
	}

	c, err := loadContent()
	if err != nil {
		return err
	}
	g := &generator{c: c, seed: o.seed}
	ids, err := existingSamples(ctx, pool, o.seed)
	if err != nil {
		return err
	}
	p := makePlan(ids, o.n)
	fmt.Fprintf(out, "Sample data in %s: %d sample profiles exist; adding %d to reach %d (seed %d, %d clusters, accounts @%s).\n",
		target, p.existing, len(p.missing), p.existing+len(p.missing), o.seed, len(c.clusters), sampleDomain)
	if !slices.Contains(splitDomains(getenv("TEST_EMAIL_DOMAINS")), sampleDomain) {
		fmt.Fprintf(out, "Warning: TEST_EMAIL_DOMAINS does not name %s; add it in .env (see .env.example).\n", sampleDomain)
	}
	if p.existing > o.n {
		fmt.Fprintf(out, "There are already more than %d; a smaller n removes none (make sample-purge removes them all).\n", o.n)
	}
	if len(p.missing) > 0 {
		fmt.Fprintf(out, "The running API embeds them in the background; at about %.0f per second on CPU that takes about %s.\n",
			profilesPerSecond, estimate(len(p.missing)))
		hash, err := argon2.New(argon2.DefaultParams).Hash(password)
		if err != nil {
			return err
		}
		started := time.Now()
		err = seed(ctx, pool, g, p, ids, hash, func(done int) {
			if done%2000 == 0 || done == len(p.missing) {
				fmt.Fprintf(out, "  %d of %d created\n", done, len(p.missing))
			}
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Created %d sample profiles in %s. The password is SAMPLE_PASSWORD from .env.\n",
			len(p.missing), time.Since(started).Round(time.Millisecond))
	}
	if o.projects {
		if err := runProjects(ctx, pool, g, ids, out); err != nil {
			return err
		}
	}
	if o.venues {
		if err := runVenues(ctx, pool, g, time.Now().UTC(), out); err != nil {
			return err
		}
	}
	if !o.wait {
		return nil
	}
	started := time.Now()
	q, err := waitForEmbeddings(ctx, pool, out, 10*time.Second, 2*time.Minute)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Every sample profile is embedded (%d of %d) after %s.", q.embedded, q.total, time.Since(started).Round(time.Second))
	if q.failed > 0 {
		fmt.Fprintf(out, " %d failed five times; see last_error in embedding_jobs.", q.failed)
	}
	fmt.Fprintln(out)
	return nil
}

// runProjects tops up the sample projects to one per profilesPerProject sample profiles.
func runProjects(ctx context.Context, pool *pgxpool.Pool, g *generator, profiles map[int]string, out io.Writer) error {
	have, err := existingSampleProjects(ctx, pool, g.seed)
	if err != nil {
		return err
	}
	missing := projectPlan(profiles, have)
	fmt.Fprintf(out, "Sample projects: %d exist; adding %d (one per %d sample profiles, in the owner's cluster).\n",
		len(have), len(missing), profilesPerProject)
	if len(missing) == 0 {
		return nil
	}
	started := time.Now()
	if err := seedProjects(ctx, pool, g, missing, profiles, nil); err != nil {
		return err
	}
	fmt.Fprintf(out, "Created %d sample projects in %s.\n", len(missing), time.Since(started).Round(time.Millisecond))
	return nil
}

func splitDomains(s string) []string {
	var out []string
	for d := range strings.SplitSeq(s, ",") {
		if d = strings.ToLower(strings.TrimSpace(d)); d != "" {
			out = append(out, d)
		}
	}
	return out
}

// describeTarget names the database and host, never the password.
func describeTarget(databaseURL string) string {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return "the database"
	}
	return fmt.Sprintf("database %s on %s", strings.TrimPrefix(u.Path, "/"), u.Host)
}

// checkPassword wants a password the API would accept at login.
func checkPassword(p string) error {
	if n := len([]rune(p)); n < authusecase.MinPasswordLength || n > authusecase.MaxPasswordLength {
		return fmt.Errorf("set SAMPLE_PASSWORD in .env, %d to %d characters", authusecase.MinPasswordLength, authusecase.MaxPasswordLength)
	}
	return nil
}

// checkTarget refuses every database but a local development one: APP_ENV must say
// development (unset is refused too), and it refuses a test database, which the tests empty,
// and any host other than this machine. The rest is the check of cmd/seeddev.
func checkTarget(databaseURL, appEnv string) error {
	if databaseURL == "" {
		return errors.New("DATABASE_URL is not set")
	}
	switch env := strings.ToLower(strings.TrimSpace(appEnv)); env {
	case "development":
	case "production":
		return errors.New("refusing to run with APP_ENV=production")
	case "":
		return errors.New("APP_ENV is not set: sample data needs APP_ENV=development")
	default:
		return errors.New("APP_ENV is not development: sample data needs APP_ENV=development")
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
		return fmt.Errorf("refusing the database on %s: only a local database can be seeded", host)
	}
	return nil
}
