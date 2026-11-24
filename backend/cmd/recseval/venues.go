package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/config"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	recommendationpg "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/postgres/sqlcgen"
	recommendationdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
	recommendationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/usecase"
)

// Venue recommendations (AI-6.1, AI-6.2, NFR-6; slice 25, chunk 25.1).
//
// The development database holds real venues, so the labelled sample venues live only in the
// TEST database, made there by `seedsample -test-venues`. The sample projects and their
// title-and-abstract embeddings live only in development. calibrate-venues and eval-venues
// therefore read both, each through a read-only session (default_transaction_read_only), and
// rank each project's vector against the test database's venues with the endpoint's own
// query text. Neither writes to any database; eval-venues writes docs/evaluation.md.

// venueK is how many venues with a deadline a project is shown (AI-6.1).
const venueK = recommendationusecase.VenueLimit

// sampleVenue is one embedded sample venue of the test database and its ground truth.
type sampleVenue struct {
	id      string
	cluster string
	live    bool // its submission deadline is after now
}

// venueProject is a sample project with its title-and-abstract embedding (as pgvector text).
type venueProject struct {
	id      string
	cluster string
	split   string
	vector  string
}

// venueData is what the venue modes read.
type venueData struct {
	model     string
	venues    map[string]sampleVenue
	live      map[string]int // live sample venues per cluster
	projects  []venueProject
	perSplit  map[string]int // sample projects per split, with or without an abstract
	devTarget string
	tstTarget string
}

func (d venueData) split(name string) []venueProject {
	var out []venueProject
	for _, p := range d.projects {
		if p.split == name {
			out = append(out, p)
		}
	}
	return out
}

func (d venueData) liveCount() int {
	n := 0
	for _, c := range d.live {
		n += c
	}
	return n
}

// readOnlyPool opens a pool whose sessions refuse every write.
func readOnlyPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return nil, errors.New("database URL is not valid") // the error would repeat the password
	}
	q := u.Query()
	q.Set("default_transaction_read_only", "on")
	u.RawQuery = q.Encode()
	return postgres.NewPool(ctx, u.String())
}

// checkTestTarget accepts only a local database whose name ends in _test.
func checkTestTarget(databaseURL string) error {
	if databaseURL == "" {
		return errors.New("TEST_DATABASE_URL is not set")
	}
	u, err := url.Parse(databaseURL)
	if err != nil {
		return errors.New("TEST_DATABASE_URL is not a valid URL")
	}
	if name := strings.TrimPrefix(u.Path, "/"); !strings.HasSuffix(name, "_test") {
		return fmt.Errorf("refusing %q as the test database: its name must end in _test", name)
	}
	if host := u.Hostname(); host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return fmt.Errorf("refusing the test database on %s: only a local one", host)
	}
	return nil
}

// runVenueModes runs calibrate-venues or eval-venues.
func runVenueModes(ctx context.Context, o options, getenv func(string) string, out io.Writer) error {
	devURL, testURL := getenv("DATABASE_URL"), getenv("TEST_DATABASE_URL")
	if err := checkTarget(devURL, getenv("APP_ENV")); err != nil {
		return err
	}
	if err := checkTestTarget(testURL); err != nil {
		return err
	}
	writes := "none"
	if o.write != "" {
		writes = o.write
	}
	fmt.Fprintf(out, "Databases: reads %s (sample projects and their title-and-abstract embeddings) and %s (sample venues made by seedsample -test-venues), both in read-only sessions; writes no database. Writes: %s.\n",
		describeTarget(devURL), describeTarget(testURL), writes)
	dev, err := readOnlyPool(ctx, devURL)
	if err != nil {
		return err
	}
	defer dev.Close()
	tst, err := readOnlyPool(ctx, testURL)
	if err != nil {
		return err
	}
	defer tst.Close()
	d, err := loadVenueData(ctx, dev, tst)
	if err != nil {
		return err
	}
	d.devTarget, d.tstTarget = describeTarget(devURL), describeTarget(testURL)
	fmt.Fprintf(out, "Sample projects with a title-and-abstract embedding of %s: %d tuning, %d test (of %d and %d sample projects). Sample venues: %d, %d with a deadline still ahead, in %d clusters.\n",
		d.model, len(d.split("tuning")), len(d.split("test")), d.perSplit["tuning"], d.perSplit["test"], len(d.venues), d.liveCount(), len(d.live))

	if o.mode == "calibrate-venues" {
		projects, err := projectScoreMapFromEnv(getenv)
		if err != nil {
			return err
		}
		return calibrateVenues(ctx, tst, d, projects, out)
	}
	scores, err := venueScoreMapFromEnv(getenv)
	if err != nil {
		return err
	}
	var report strings.Builder
	if err := evaluateVenues(ctx, tst, d, scores, o.seed, time.Now(), &report); err != nil {
		return err
	}
	fmt.Fprint(out, report.String())
	if o.write == "" {
		return nil
	}
	if err := writeSection(o.write, report.String()); err != nil {
		return err
	}
	fmt.Fprintf(out, "\nWrote the results into %s.\n", o.write)
	return nil
}

// loadVenueData reads the sample venues of the test database and the sample projects of the
// development database whose title-and-abstract embedding is of the venues' model.
func loadVenueData(ctx context.Context, dev, tst *pgxpool.Pool) (venueData, error) {
	d := venueData{venues: map[string]sampleVenue{}, live: map[string]int{}, perSplit: map[string]int{}}
	rows, err := tst.Query(ctx, `SELECT sv.venue_id::text, sv.cluster, v.submission_deadline > now(), e.model
		FROM sample_venues sv
		JOIN venues v ON v.id = sv.venue_id AND v.removed_at IS NULL AND NOT v.rolling
		JOIN venue_embeddings e ON e.venue_id = sv.venue_id
		ORDER BY sv.seq`)
	if err != nil {
		return d, fmt.Errorf("read the test database's sample venues: %w", err)
	}
	models := map[string]bool{}
	for rows.Next() {
		var v sampleVenue
		var model string
		if err := rows.Scan(&v.id, &v.cluster, &v.live, &model); err != nil {
			return d, err
		}
		models[model] = true
		d.model = model
		d.venues[v.id] = v
		if v.live {
			d.live[v.cluster]++
		}
	}
	if err := rows.Err(); err != nil {
		return d, err
	}
	if len(d.venues) == 0 {
		return d, errors.New("no embedded sample venues in the test database: run make eval-venues (it runs seedsample -test-venues first)")
	}
	if len(models) != 1 {
		return d, fmt.Errorf("the sample venues are of %d models; run seedsample -test-venues again", len(models))
	}

	var tuning, test int
	if err := dev.QueryRow(ctx, `SELECT count(*) FILTER (WHERE split = 'tuning'), count(*) FILTER (WHERE split = 'test') FROM sample_projects`).
		Scan(&tuning, &test); err != nil {
		return d, fmt.Errorf("count sample projects: %w", err)
	}
	d.perSplit["tuning"], d.perSplit["test"] = tuning, test
	rows, err = dev.Query(ctx, `SELECT sp.project_id::text, sp.cluster, sp.split, a.embedding::text
		FROM sample_projects sp
		JOIN project_abstract_embeddings a ON a.project_id = sp.project_id
		WHERE a.model = $1
		ORDER BY sp.seq`, d.model)
	if err != nil {
		return d, fmt.Errorf("read the sample projects' title-and-abstract embeddings: %w", err)
	}
	for rows.Next() {
		var p venueProject
		if err := rows.Scan(&p.id, &p.cluster, &p.split, &p.vector); err != nil {
			return d, err
		}
		d.projects = append(d.projects, p)
	}
	if err := rows.Err(); err != nil {
		return d, err
	}
	if len(d.projects) == 0 {
		return d, fmt.Errorf("no sample project has a title-and-abstract embedding of %s: run make sample-data n=10000 projects=1 wait=1 and make embed-backfill with make api running", d.model)
	}
	return d, nil
}

// The me CTE of the endpoint's RankProjectVenues reads the project's stored vector; the
// evaluation passes the vector instead, since the projects are in another database. Every
// other line of the query is the endpoint's own (recommendationpg.QueryText), so a change to
// the product query changes the evaluation, or stops it.
const (
	venueMeAnchor = "SELECT embedding, model FROM project_abstract_embeddings WHERE project_id = @project_id::uuid"
	venueMeByArg  = "SELECT $1::vector AS embedding, $2::text AS model"
)

// rankVenuesSQL is RankProjectVenues with $1 the vector, $2 its model, $3 rolling, $4 the limit.
func rankVenuesSQL() (string, error) {
	q, err := recommendationpg.QueryText("RankProjectVenues")
	if err != nil {
		return "", err
	}
	if strings.Count(q, venueMeAnchor) != 1 {
		return "", errors.New("RankProjectVenues changed: its me CTE is not the one the evaluation replaces")
	}
	q = strings.Replace(q, venueMeAnchor, venueMeByArg, 1)
	return strings.NewReplacer("@rolling", "$3", "@lim", "$4").Replace(q), nil
}

// rankVenuesFor ranks the test database's venues for one vector as the endpoint does, under
// the endpoint's settings.
func rankVenuesFor(ctx context.Context, tst *pgxpool.Pool, sql, vector, model string, k int) ([]neighbour, error) {
	var out []neighbour
	err := pgx.BeginFunc(ctx, tst, func(tx pgx.Tx) error {
		if err := recommendationpg.SetupRanking(ctx, sqlcgen.New(tx)); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, sql, vector, model, false, k)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, name, typ, link, kind string
			var short, tz *string
			var deadline *time.Time
			var cos float64
			if err := rows.Scan(&id, &cos, &name, &short, &typ, &tz, &deadline, &link, &kind); err != nil {
				return err
			}
			out = append(out, neighbour{id: id, cosine: cos})
		}
		return rows.Err()
	})
	return out, err
}

// venueCosines are the cosines of one vector to every sample venue, live or past.
func venueCosines(ctx context.Context, tst *pgxpool.Pool, vector string) (map[string]float64, error) {
	rows, err := tst.Query(ctx, `SELECT sv.venue_id::text, (1 - (e.embedding <=> $1::vector))::float8
		FROM sample_venues sv JOIN venue_embeddings e ON e.venue_id = sv.venue_id`, vector)
	if err != nil {
		return nil, err
	}
	out := map[string]float64{}
	for rows.Next() {
		var id string
		var c float64
		if err := rows.Scan(&id, &c); err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, rows.Err()
}

// calibrateVenues checks the project score map (19.2) on project -> venue pairs of the TUNING
// split only (every tuning project with every sample venue; "same" when the venue's cluster
// is the project's) and fits a map of their own, with the rule of calibrateProjects.
func calibrateVenues(ctx context.Context, tst *pgxpool.Pool, d venueData, projects recommendationusecase.ScoreMap, out io.Writer) error {
	tuning := d.split("tuning")
	if len(tuning) < 2 {
		return errors.New("too few tuning projects with an abstract to calibrate")
	}
	started := time.Now()
	var pairs []pair
	for _, p := range tuning {
		cs, err := venueCosines(ctx, tst, p.vector)
		if err != nil {
			return err
		}
		for id, c := range cs {
			pairs = append(pairs, pair{c, d.venues[id].cluster == p.cluster})
		}
	}
	slices.SortFunc(pairs, func(a, b pair) int { return cmpFloat(a.cosine, b.cosine) })
	slope, intercept, err := fitLogistic(pairs)
	if err != nil {
		return err
	}
	fitted := recommendationusecase.ScoreMap{Model: d.model, Slope: slope, Intercept: intercept}
	same := 0
	for _, p := range pairs {
		if p.same {
			same++
		}
	}
	fmt.Fprintf(out, "\n## Project -> venue pairs (AI-6.2), tuning split only\n\n%d tuning projects x %d sample venues = %d pairs (%.1f%% same cluster), read in %s.\n\n",
		len(tuning), len(d.venues), len(pairs), 100*float64(same)/float64(len(pairs)), time.Since(started).Round(time.Second))
	fmt.Fprintf(out, "| Map | Slope | Intercept | Cosine at 50 | Error on these pairs (points) |\n| --- | ---: | ---: | ---: | ---: |\n")
	fmt.Fprintf(out, "| Project map (19.2) | %.4f | %.4f | %.4f | %.1f |\n", projects.Slope, projects.Intercept, midpoint(projects), calibrationError(pairs, projects))
	fmt.Fprintf(out, "| Fitted | %.4f | %.4f | %.4f | %.1f |\n", slope, intercept, midpoint(fitted), calibrationError(pairs, fitted))
	errProject := calibrationError(pairs, projects)
	shift := math.Abs(midpoint(fitted) - midpoint(projects))
	fmt.Fprintf(out, "\nRule: keep the project map unless its error on these pairs is above %.0f points or the 50 point moves more than %.2f.\n", maxCalibrationError, maxMidpointShift)
	fmt.Fprintf(out, "Error %.1f points, 50 point moved %.4f: ", errProject, shift)
	if errProject <= maxCalibrationError && shift <= maxMidpointShift {
		fmt.Fprintf(out, "the project map holds; set the venue map to it:\n\nRECS_VENUE_SCORE_SLOPE=%.4f\nRECS_VENUE_SCORE_INTERCEPT=%.4f\n", projects.Slope, projects.Intercept)
	} else {
		fmt.Fprintf(out, "the distributions differ; use separate constants:\n\nRECS_VENUE_SCORE_SLOPE=%.4f\nRECS_VENUE_SCORE_INTERCEPT=%.4f\n", slope, intercept)
	}
	fmt.Fprintf(out, "\n(fitted for RECS_SCORE_MODEL=%s)\n\nCalibration table, scored with the fitted map:\n", d.model)
	printCalibrationTable(out, pairs, fitted)
	fmt.Fprintln(out, "\nThe same pairs, scored with the project map:")
	printCalibrationTable(out, pairs, projects)
	return nil
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// venueScoreMapFromEnv is the venue score map the API would use with these settings.
func venueScoreMapFromEnv(getenv func(string) string) (recommendationusecase.ScoreMap, error) {
	people, err := scoreMapFromEnv(getenv)
	if err != nil {
		return people, err
	}
	m := recommendationusecase.ScoreMap{Model: people.Model, Slope: config.DefaultRecsVenueScoreSlope, Intercept: config.DefaultRecsVenueScoreIntercept}
	for name, dst := range map[string]*float64{"RECS_VENUE_SCORE_SLOPE": &m.Slope, "RECS_VENUE_SCORE_INTERCEPT": &m.Intercept} {
		if v := strings.TrimSpace(getenv(name)); v != "" {
			if _, err := fmt.Sscan(v, dst); err != nil {
				return m, fmt.Errorf("%s must be a number", name)
			}
		}
	}
	return m, m.Validate()
}

// wilson is the 95% Wilson score interval of hits successes in n trials.
func wilson(hits, n int) (lo, hi float64) {
	if n == 0 {
		return 0, 0
	}
	const z = 1.959963984540054
	p, nf := float64(hits)/float64(n), float64(n)
	den := 1 + z*z/nf
	centre := (p + z*z/(2*nf)) / den
	half := z * math.Sqrt(p*(1-p)/nf+z*z/(4*nf*nf)) / den
	return max(centre-half, 0), min(centre+half, 1)
}

// chanceHit is the probability that k venues drawn at random from n live venues, of which
// r are relevant, include at least one relevant one: 1 - C(n-r, k) / C(n, k).
func chanceHit(n, r, k int) float64 {
	if r <= 0 {
		return 0
	}
	if n-r < k {
		return 1
	}
	miss := 1.0
	for i := range k {
		miss *= float64(n-r-i) / float64(n-i)
	}
	return 1 - miss
}

// evaluateVenues measures NFR-6 venue top-5 accuracy on the held-out TEST split: each test
// project's top 5 venues with a deadline still ahead, ranked as the endpoint ranks; a hit is
// a venue whose cluster is the project's. Nothing is fitted here.
func evaluateVenues(ctx context.Context, tst *pgxpool.Pool, d venueData, scores recommendationusecase.ScoreMap, seed uint64, now time.Time, out io.Writer) error {
	sql, err := rankVenuesSQL()
	if err != nil {
		return err
	}
	test := d.split("test")
	if len(test) == 0 {
		return errors.New("no test project with a title-and-abstract embedding")
	}
	var hits, queries, skipped, fewer int
	var precision, chance float64
	var topScores, randomScores []int
	r := rand.New(rand.NewPCG(seed, 25)) //nolint:gosec // reproducible draws of sample data, not a secret
	ids := make([]string, 0, len(d.venues))
	for id, v := range d.venues {
		if v.live {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	for _, p := range test {
		if d.live[p.cluster] == 0 {
			skipped++ // no venue of its cluster is open: nothing could be a hit
			continue
		}
		top, err := rankVenuesFor(ctx, tst, sql, p.vector, d.model, venueK)
		if err != nil {
			return fmt.Errorf("rank venues for %s: %w", short(p.id), err)
		}
		if len(top) < venueK {
			fewer++
		}
		rel := 0
		for _, n := range top {
			topScores = append(topScores, scores.Score(n.cosine))
			if d.venues[n.id].cluster == p.cluster {
				rel++
			}
		}
		queries++
		if rel > 0 {
			hits++
		}
		precision += float64(rel) / venueK
		chance += chanceHit(len(ids), d.live[p.cluster], venueK)
		cs, err := venueCosines(ctx, tst, p.vector)
		if err != nil {
			return err
		}
		for range 5 {
			randomScores = append(randomScores, scores.Score(cs[ids[r.IntN(len(ids))]]))
		}
	}
	if queries == 0 {
		return errors.New("no test project has an open venue of its cluster")
	}
	acc := float64(hits) / float64(queries)
	lo, hi := wilson(hits, queries)
	q := float64(queries)
	slices.Sort(topScores)
	slices.Sort(randomScores)

	fmt.Fprintf(out, "<!-- eval-venues:begin (written by make eval-venues; do not edit by hand) -->\n")
	fmt.Fprintf(out, "### Quality (`make eval-venues`, test split; nothing fitted)\n\n")
	fmt.Fprintf(out, "Run %s. Model `%s`. Score map: slope %.4f, intercept %.4f.\n\n", now.UTC().Format("2006-01-02 15:04 UTC"), d.model, scores.Slope, scores.Intercept)
	fmt.Fprintf(out, "Data: %d test projects with an abstract and its embedding (of %d test sample projects; about half the sample projects have an abstract), read from %s. %d sample venues in the test database (%s), %d of them with a deadline still ahead (%d per cluster on average), as `seedsample -test-venues` made them today, so about a quarter have passed. %d test projects skipped (no open venue of their cluster).\n\n",
		queries, d.perSplit["test"], d.devTarget, len(d.venues), d.tstTarget, len(ids), len(ids)/max(len(d.live), 1), skipped)
	fmt.Fprintf(out, "Relevant: a venue whose sample cluster is the project's cluster (the project owner's main cluster). Ranked with the endpoint's own `RankProjectVenues` (deadline after now, not removed, not rolling, same model, cosine order) and its HNSW settings.\n\n")
	fmt.Fprintf(out, "| Measure | Value |\n| --- | ---: |\n")
	fmt.Fprintf(out, "| Top-5 accuracy (at least one relevant venue in the top 5) | %.3f (%d of %d) |\n", acc, hits, queries)
	fmt.Fprintf(out, "| 95%% confidence interval (Wilson) | %.3f to %.3f |\n", lo, hi)
	fmt.Fprintf(out, "| Precision@5 | %.3f |\n", precision/q)
	fmt.Fprintf(out, "| Top-5 accuracy of 5 random open venues (expected) | %.3f |\n", chance/q)
	if fewer > 0 {
		fmt.Fprintf(out, "| Projects with fewer than 5 venues listed | %d |\n", fewer)
	}
	fmt.Fprintf(out, "\nScores (AI-6.2) of the listed venues and of 5 random open venues per project:\n\n")
	fmt.Fprintf(out, "| Score of | Pairs | Mean | p10 | p25 | p50 | p75 | p90 |\n| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	fmt.Fprintln(out, scoreRow("listed (top 5)", topScores))
	fmt.Fprintln(out, scoreRow("random open venue", randomScores))
	fmt.Fprintf(out, "\n**Optimism.** These numbers are higher than real use will see: the sample projects and venues are written from the same 20 cluster templates (`cmd/seedsample/content`), so a venue's scope shares vocabulary with the projects of its cluster, and each cluster has its own venues. Real projects straddle fields and real calls are broader. The test split keeps the projects unseen by the calibration, not the templates. The interval counts only the sampling error of %d test projects, not this template bias, so read the accuracy as a check that the ranking and the deadline filter work, not as an estimate for real projects and venues.\n", queries)
	fmt.Fprintf(out, "<!-- eval-venues:end -->\n")
	return nil
}

// writeSection replaces the eval-venues block of path with section, or adds it under the
// venue heading at the end when the file has none.
func writeSection(path, section string) error {
	b, err := os.ReadFile(path) //nolint:gosec // the -write path the developer named
	if err != nil {
		return err
	}
	text := string(b)
	const begin, end = "<!-- eval-venues:begin", "<!-- eval-venues:end -->\n"
	i, j := strings.Index(text, begin), strings.Index(text, end)
	switch {
	case i >= 0 && j > i:
		text = text[:i] + section + text[j+len(end):]
	case i < 0 && j < 0:
		text = strings.TrimRight(text, "\n") + "\n\n" + section
	default:
		return fmt.Errorf("%s has a broken eval-venues block; fix its markers", path)
	}
	return os.WriteFile(path, []byte(text), 0o644) //nolint:gosec // a document in the repository
}

// benchVenues measures NFR-1 for GET /projects/{id}/recommended-venues: the uncached work of
// the usecase (the project's abstract and stamp, both rankings, the fields) for o.queries
// random sample projects with a title-and-abstract embedding, against the venues of this
// (development) database, then EXPLAIN ANALYZE of the ranking query. The member check is not
// in it: the project module reads one row for it.
func benchVenues(ctx context.Context, pool *pgxpool.Pool, scores, venues recommendationusecase.ScoreMap, o options, out io.Writer) error {
	rows, err := pool.Query(ctx, `SELECT sp.project_id::text FROM sample_projects sp
		JOIN project_abstract_embeddings a ON a.project_id = sp.project_id ORDER BY sp.seq`)
	if err != nil {
		return fmt.Errorf("read sample projects with a title-and-abstract embedding: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("read sample projects with a title-and-abstract embedding: %w", err)
	}
	if len(ids) == 0 {
		fmt.Fprintln(out, "\n## Venue recommendations (AI-6)\n\nSkipped: no sample project has a title-and-abstract embedding (make embed-backfill with make api running).")
		return nil
	}
	var live, rolling int
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE NOT rolling AND submission_deadline > now()), count(*) FILTER (WHERE rolling)
		FROM venues v JOIN venue_embeddings e ON e.venue_id = v.id WHERE v.removed_at IS NULL`).Scan(&live, &rolling); err != nil {
		return err
	}
	logger := slog.New(slog.DiscardHandler)
	store := recommendationpg.NewStore(pool)
	svc := recommendationusecase.New(store, noCache{}, relations{}, scores, logger).
		WithVenues(recommendationusecase.VenueDeps{Store: store, Cache: noVenueCache{}, Access: anyone{}, Scores: venues, JournalMinScore: config.DefaultRecsJournalMinScore})
	r := rand.New(rand.NewPCG(o.seed, 26)) //nolint:gosec // reproducible draws of sample data, not a secret
	projs := make([]string, benchWarmup+o.queries)
	for i := range projs {
		projs[i] = ids[r.IntN(len(ids))]
	}
	fmt.Fprintf(out, "\n## Venue recommendations (AI-6)\n\n%d random sample projects (with repeats, seed %d) against the %d embedded venues with a deadline ahead and %d rolling journals of this database, after %d warm-up calls, nothing cached.\n",
		o.queries, o.seed, live, rolling, benchWarmup)
	times := make([]time.Duration, 0, o.queries)
	for i, p := range projs {
		start := time.Now()
		res, err := svc.ProjectVenues(ctx, "", p)
		if err != nil {
			return fmt.Errorf("venues for %s: %w", short(p), err)
		}
		if res.State != recommendationdomain.StateReady {
			return fmt.Errorf("venues for %s: state %s", short(p), res.State)
		}
		if i >= benchWarmup {
			times = append(times, time.Since(start))
		}
	}
	slices.Sort(times)
	fmt.Fprintf(out, "\n| Measure | Time |\n| --- | ---: |\n| Calls | %d |\n| p50 | %s |\n| p95 | %s |\n| max | %s |\n",
		len(times), percentile(times, 50), percentile(times, 95), times[len(times)-1].Round(10*time.Microsecond))
	fmt.Fprintf(out, "\nNFR-1 target: p95 under 2 s. %s\n", map[bool]string{true: "Met.", false: "NOT met."}[percentile(times, 95) < 2*time.Second])

	q, err := recommendationpg.RankProjectVenuesSQL()
	if err != nil {
		return err
	}
	var lines []string
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := recommendationpg.SetupRanking(ctx, sqlcgen.New(tx)); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+q, projs[benchWarmup], false, venueK)
		if err != nil {
			return err
		}
		lines, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		return fmt.Errorf("explain the venue ranking query: %w", err)
	}
	fmt.Fprintf(out, "\nEXPLAIN (ANALYZE, BUFFERS) of RankProjectVenues (deadline list) for %s:\n\n%s\n", short(projs[benchWarmup]), joinLines(lines))
	return nil
}

// noVenueCache never keeps anything.
type noVenueCache struct{}

func (noVenueCache) GetVenues(context.Context, string) (recommendationdomain.KeptVenues, bool, error) {
	return recommendationdomain.KeptVenues{}, false, nil
}

func (noVenueCache) SetVenues(context.Context, string, recommendationdomain.KeptVenues, time.Duration) error {
	return nil
}
func (noVenueCache) DeleteVenues(context.Context, ...string) error           { return nil }
func (noVenueCache) VenueGeneration(context.Context) (int64, error)          { return 0, nil }
func (noVenueCache) NextVenueGeneration(context.Context) error               { return nil }
func (anyone) CanSeeRecommendedVenues(context.Context, string, string) error { return nil }
