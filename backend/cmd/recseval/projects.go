package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/config"
	recommendationpg "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/postgres/sqlcgen"
	recommendationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/usecase"
)

// Project matching (AI-3.1, AI-2.3; slice 19, chunk 19.2) on the sample projects of
// `make sample-data projects=1`. Their labels are in sample_projects: the cluster is the
// owner's main cluster and the split is the owner's, so the tuning and test splits stay
// apart across profiles and projects.

// sampleProject is one embedded sample project and its ground truth.
type sampleProject struct {
	id      string
	cluster string
	split   string
	open    bool
	ownerID string
}

// projectSamples are the embedded sample projects of the profiles' model.
type projectSamples struct {
	all  []sampleProject
	byID map[string]sampleProject
}

func (p projectSamples) ids(split string) []sampleProject {
	var out []sampleProject
	for _, x := range p.all {
		if x.split == split {
			out = append(out, x)
		}
	}
	return out
}

// openCount is how many open sample projects are in each cluster, both splits: the
// relevant candidates of a profile query.
func (p projectSamples) openCount() map[string]int {
	out := map[string]int{}
	for _, x := range p.all {
		if x.open {
			out[x.cluster]++
		}
	}
	return out
}

// loadProjectSamples reads the labels of every sample project embedded by model. Only this
// command and cmd/seedsample read sample_projects (AGENTS.md).
func loadProjectSamples(ctx context.Context, pool *pgxpool.Pool, model string) (projectSamples, error) {
	rows, err := pool.Query(ctx, `SELECT sp.project_id::text, sp.cluster, sp.split, p.status = 'open', o.user_id::text
		FROM sample_projects sp
		JOIN projects p ON p.id = sp.project_id
		JOIN project_members o ON o.project_id = p.id AND o.role = 'owner'
		JOIN project_embeddings e ON e.project_id = sp.project_id
		WHERE e.model = $1
		ORDER BY sp.seq`, model)
	if err != nil {
		return projectSamples{}, fmt.Errorf("read sample projects: %w", err)
	}
	out := projectSamples{byID: map[string]sampleProject{}}
	for rows.Next() {
		var x sampleProject
		if err := rows.Scan(&x.id, &x.cluster, &x.split, &x.open, &x.ownerID); err != nil {
			return projectSamples{}, err
		}
		out.all = append(out.all, x)
		out.byID[x.id] = x
	}
	if err := rows.Err(); err != nil {
		return projectSamples{}, fmt.Errorf("read sample projects: %w", err)
	}
	if len(out.all) == 0 {
		return projectSamples{}, errors.New("no embedded sample projects: run make sample-data n=10000 projects=1 wait=1 first")
	}
	return out, nil
}

// rankProjectsSQL ranks the sample projects for one profile as the endpoint's RankProjects
// does (internal/recommendation/adapter/postgres/queries.sql): the same index, operator,
// model, status, owner, membership filters and order, under the same settings. Only sample
// projects are candidates ($3 narrows them to one split, or ” for both); join requests and
// blocks are not left out (the measurement is of matching, not the social graph).
const rankProjectsSQL = `
WITH me AS (
    SELECT embedding, model FROM profile_embeddings WHERE user_id = $1::uuid
),
cand AS MATERIALIZED (
    SELECT e.project_id, (e.embedding <=> (SELECT embedding FROM me))::float8 AS distance
    FROM project_embeddings e
    JOIN projects p ON p.id = e.project_id
    JOIN project_members o ON o.project_id = p.id AND o.role = 'owner'
    JOIN users u ON u.id = o.user_id
    JOIN sample_projects sp ON sp.project_id = e.project_id
    WHERE e.model = (SELECT model FROM me)
      AND p.status = 'open'
      AND u.email_verified_at IS NOT NULL
      AND (u.suspended_at IS NULL OR (u.suspended_until IS NOT NULL AND u.suspended_until <= now()))
      AND NOT EXISTS (SELECT 1 FROM project_members m WHERE m.project_id = p.id AND m.user_id = $1::uuid)
      AND ($3::text = '' OR sp.split = $3::text)
    ORDER BY e.embedding <=> (SELECT embedding FROM me)
    LIMIT $2
)
SELECT cand.project_id::text, (1 - cand.distance)::float8
FROM cand
ORDER BY cand.distance, cand.project_id`

// rankResearchersSQL ranks the sample profiles for one project as the endpoint's
// RankResearchers does, without the block filter; $3 narrows the candidates to one split.
const rankResearchersSQL = `
WITH pe AS (
    SELECT embedding, model FROM project_embeddings WHERE project_id = $1::uuid
),
cand AS MATERIALIZED (
    SELECT e.user_id, (e.embedding <=> (SELECT embedding FROM pe))::float8 AS distance
    FROM profile_embeddings e
    JOIN users u ON u.id = e.user_id
    JOIN profiles p ON p.user_id = e.user_id
    JOIN sample_profiles sp ON sp.user_id = e.user_id
    WHERE e.model = (SELECT model FROM pe)
      AND u.email_verified_at IS NOT NULL
      AND (u.suspended_at IS NULL OR (u.suspended_until IS NOT NULL AND u.suspended_until <= now()))
      AND NOT EXISTS (SELECT 1 FROM project_members m WHERE m.project_id = $1::uuid AND m.user_id = e.user_id)
      AND ($3::text = '' OR sp.split = $3::text)
    ORDER BY e.embedding <=> (SELECT embedding FROM pe)
    LIMIT $2
)
SELECT cand.user_id::text, (1 - cand.distance)::float8
FROM cand
ORDER BY cand.distance, cand.user_id`

// rankWith runs one of the ranking queries above under the endpoint's settings.
func rankWith(ctx context.Context, pool *pgxpool.Pool, sql, id string, k int, split string) ([]neighbour, error) {
	var out []neighbour
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := recommendationpg.SetupRanking(ctx, sqlcgen.New(tx)); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, sql, id, k, split)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (neighbour, error) {
			var n neighbour
			err := r.Scan(&n.id, &n.cosine)
			return n, err
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("rank for %s: %w", short(id), err)
	}
	return out, nil
}

// profileProjectCosines returns the cosine between a profile and each project, or between a
// project and each profile when fromProject is true.
func profileProjectCosines(ctx context.Context, pool *pgxpool.Pool, id string, others []string, fromProject bool) (map[string]float64, error) {
	sql := `SELECT e.project_id::text, (1 - (e.embedding <=> m.embedding))::float8
		FROM project_embeddings e, profile_embeddings m
		WHERE m.user_id = $1::uuid AND e.project_id = ANY($2::uuid[])`
	if fromProject {
		sql = `SELECT e.user_id::text, (1 - (e.embedding <=> m.embedding))::float8
		FROM profile_embeddings e, project_embeddings m
		WHERE m.project_id = $1::uuid AND e.user_id = ANY($2::uuid[])`
	}
	rows, err := pool.Query(ctx, sql, id, others)
	if err != nil {
		return nil, fmt.Errorf("profile-project cosines: %w", err)
	}
	out := map[string]float64{}
	for rows.Next() {
		var other string
		var c float64
		if err := rows.Scan(&other, &c); err != nil {
			return nil, err
		}
		out[other] = c
	}
	return out, rows.Err()
}

// Thresholds of the AI-2.2 map check on profile-project pairs, fixed before the first run
// (docs/evaluation.md): the people map is kept unless its mean absolute calibration error on
// these pairs is above maxCalibrationError points or the cosine where a fitted map gives 50
// moves by more than maxMidpointShift.
const (
	maxCalibrationError = 5.0
	maxMidpointShift    = 0.01
)

// calibrateProjects checks the people score map on profile-project pairs of the TUNING split
// only, in both directions, and fits a map of their own: profile -> project pairs are each
// tuning query profile with its calibNeighbours nearest open tuning projects and as many
// random tuning projects; project -> profile pairs are each tuning project with its nearest
// and random tuning profiles. A pair is "same" when the project's cluster is the profile's
// main cluster.
func calibrateProjects(ctx context.Context, pool *pgxpool.Pool, s samples, ps projectSamples, people recommendationusecase.ScoreMap, o options, out io.Writer) error {
	tuning, tuningProjects := s.ids("tuning"), ps.ids("tuning")
	var tuningOpen []sampleProject
	for _, p := range tuningProjects {
		if p.open {
			tuningOpen = append(tuningOpen, p)
		}
	}
	if len(tuningOpen) < 2 || len(tuning) < 2 {
		return errors.New("too few tuning projects or profiles to calibrate")
	}
	started := time.Now()
	queries := draw(tuning, o.queries, o.seed, 5)
	r := rand.New(rand.NewPCG(o.seed, 6)) //nolint:gosec // reproducible draws of sample data, not a secret
	fmt.Fprintf(out, "\n## Profile-project pairs (AI-3.1, AI-2.3)\n\nProfile -> project: %d tuning profiles, each with its %d nearest open tuning projects (of %d) and %d random tuning projects.\n",
		len(queries), calibNeighbours, len(tuningOpen), calibRandom)
	var toProject []pair
	for _, q := range queries {
		near, err := rankWith(ctx, pool, rankProjectsSQL, q.id, calibNeighbours, "tuning")
		if err != nil {
			return err
		}
		for _, n := range near {
			toProject = append(toProject, pair{n.cosine, ps.byID[n.id].cluster == q.cluster})
		}
		others := make([]string, calibRandom)
		for i := range others {
			others[i] = tuningProjects[r.IntN(len(tuningProjects))].id
		}
		cs, err := profileProjectCosines(ctx, pool, q.id, others, false)
		if err != nil {
			return err
		}
		for _, id := range others {
			toProject = append(toProject, pair{cs[id], ps.byID[id].cluster == q.cluster})
		}
	}
	fmt.Fprintf(out, "Project -> profile: all %d tuning projects, each with its %d nearest tuning profiles and %d random tuning profiles.\n",
		len(tuningProjects), calibNeighbours, calibRandom)
	var toProfile []pair
	for _, p := range tuningProjects {
		near, err := rankWith(ctx, pool, rankResearchersSQL, p.id, calibNeighbours, "tuning")
		if err != nil {
			return err
		}
		for _, n := range near {
			toProfile = append(toProfile, pair{n.cosine, s.byID[n.id].cluster == p.cluster})
		}
		others := make([]string, 0, calibRandom)
		for len(others) < calibRandom {
			if c := tuning[r.IntN(len(tuning))]; c.id != p.ownerID {
				others = append(others, c.id)
			}
		}
		cs, err := profileProjectCosines(ctx, pool, p.id, others, true)
		if err != nil {
			return err
		}
		for _, id := range others {
			toProfile = append(toProfile, pair{cs[id], s.byID[id].cluster == p.cluster})
		}
	}
	fmt.Fprintf(out, "Pairs read in %s.\n\n", time.Since(started).Round(time.Second))

	pooled := append(append([]pair{}, toProject...), toProfile...)
	fmt.Fprintf(out, "| Pairs | Count | Same cluster | People map error (points) | Fitted slope | Fitted intercept | Cosine at 50 (fitted) |\n| --- | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	fmt.Fprintf(out, "| People map (17.1) | | | | %.4f | %.4f | %.4f |\n", people.Slope, people.Intercept, midpoint(people))
	fits := map[string]recommendationusecase.ScoreMap{}
	for _, g := range []struct {
		name  string
		pairs []pair
	}{{"profile -> project", toProject}, {"project -> profile", toProfile}, {"both", pooled}} {
		slope, intercept, err := fitLogistic(g.pairs)
		if err != nil {
			return fmt.Errorf("fit %s: %w", g.name, err)
		}
		m := recommendationusecase.ScoreMap{Model: s.model, Slope: slope, Intercept: intercept}
		fits[g.name] = m
		same := 0
		for _, p := range g.pairs {
			if p.same {
				same++
			}
		}
		fmt.Fprintf(out, "| %s | %d | %.1f%% | %.1f | %.4f | %.4f | %.4f |\n", g.name, len(g.pairs),
			100*float64(same)/float64(len(g.pairs)), calibrationError(g.pairs, people), slope, intercept, midpoint(m))
	}

	both := fits["both"]
	errPeople := calibrationError(pooled, people)
	shift := math.Abs(midpoint(both) - midpoint(people))
	fmt.Fprintf(out, "\nRule: keep the people map unless its error on these pairs is above %.0f points or the 50 point moves more than %.2f.\n", maxCalibrationError, maxMidpointShift)
	fmt.Fprintf(out, "Error %.1f points, 50 point moved %.4f: ", errPeople, shift)
	if errPeople <= maxCalibrationError && shift <= maxMidpointShift {
		fmt.Fprintln(out, "the people map holds; no separate constants.")
	} else {
		fmt.Fprintf(out, "the distributions differ; use separate constants:\n\nRECS_PROJECT_SCORE_SLOPE=%.4f\nRECS_PROJECT_SCORE_INTERCEPT=%.4f\n", both.Slope, both.Intercept)
		dirShift := math.Abs(midpoint(fits["profile -> project"]) - midpoint(fits["project -> profile"]))
		fmt.Fprintf(out, "\nThe two directions' 50 points differ by %.4f (one map for both when at most %.2f).\n", dirShift, maxMidpointShift)
	}
	fmt.Fprintln(out, "\nCalibration table of both directions, scored with the fitted map:")
	printCalibrationTable(out, pooled, both)
	fmt.Fprintln(out, "\nThe same pairs, scored with the people map:")
	printCalibrationTable(out, pooled, people)
	return nil
}

// midpoint is the cosine where m gives 50.
func midpoint(m recommendationusecase.ScoreMap) float64 {
	return -m.Intercept / m.Slope
}

// calibrationError is the mean absolute difference, in points and weighted by pairs,
// between each cosine band's share of same-cluster pairs and the score m gives the band's
// middle; bands with fewer than 20 pairs are left out, as in the calibration table.
func calibrationError(pairs []pair, m recommendationusecase.ScoreMap) float64 {
	const width = 0.02
	type band struct{ n, same int }
	bands := map[int]*band{}
	for _, p := range pairs {
		k := int(math.Floor(p.cosine / width))
		if bands[k] == nil {
			bands[k] = &band{}
		}
		bands[k].n++
		if p.same {
			bands[k].same++
		}
	}
	var sum float64
	n := 0
	for k, b := range bands {
		if b.n < 20 {
			continue
		}
		observed := 100 * float64(b.same) / float64(b.n)
		sum += float64(b.n) * math.Abs(observed-float64(m.Score(float64(k)*width+width/2)))
		n += b.n
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// projectScoreMapFromEnv is the project score map the API would use with these settings.
func projectScoreMapFromEnv(getenv func(string) string) (recommendationusecase.ScoreMap, error) {
	people, err := scoreMapFromEnv(getenv)
	if err != nil {
		return people, err
	}
	m := recommendationusecase.ScoreMap{Model: people.Model, Slope: config.DefaultRecsProjectScoreSlope, Intercept: config.DefaultRecsProjectScoreIntercept}
	for name, dst := range map[string]*float64{"RECS_PROJECT_SCORE_SLOPE": &m.Slope, "RECS_PROJECT_SCORE_INTERCEPT": &m.Intercept} {
		if v := strings.TrimSpace(getenv(name)); v != "" {
			if _, err := fmt.Sscan(v, dst); err != nil {
				return m, fmt.Errorf("%s must be a number", name)
			}
		}
	}
	return m, m.Validate()
}

// evaluateProjects measures project matching on the held-out TEST split, in both
// directions, ranked as the endpoints rank (rankProjectsSQL, rankResearchersSQL):
//
//   - profile -> project (AI-3.1): each test profile is a query against every open sample
//     project of both splits; relevant = the project's cluster is the profile's main cluster;
//   - project -> profile (AI-2.3): each test project is a query against every sample profile
//     but its members; relevant = the profile's main cluster is the project's cluster.
//
// Beside it, the looser rule: the project's cluster is the profile's main or second cluster.
// Nothing is fitted here.
func evaluateProjects(ctx context.Context, pool *pgxpool.Pool, s samples, ps projectSamples, scores recommendationusecase.ScoreMap, seed uint64, out io.Writer) error {
	open := ps.openCount()
	inCluster := func(p sample, cluster string) bool { return p.cluster == cluster }
	inEither := func(p sample, cluster string) bool { return p.cluster == cluster || p.second == cluster }
	r := rand.New(rand.NewPCG(seed, 7)) //nolint:gosec // reproducible draws of sample data, not a secret
	groups := scoreGroups{}
	started := time.Now()

	// profile -> project
	test := s.ids("test")
	fmt.Fprintf(out, "\n## Project matching (AI-3.1, AI-2.3)\n\nProfile -> project: %d test profiles against %d open sample projects, k = %d (project score map).\n",
		len(test), len(openProjects(ps)), evalK)
	var toMain, toRel evalResult
	for _, q := range test {
		top, err := rankWith(ctx, pool, rankProjectsSQL, q.id, evalK, "")
		if err != nil {
			return err
		}
		mainHits, relHits := 0, 0
		for _, n := range top {
			c := ps.byID[n.id].cluster
			if inCluster(q, c) {
				mainHits++
				groups["profile->project top10 same"] = append(groups["profile->project top10 same"], scores.Score(n.cosine))
			} else {
				groups["profile->project top10 other"] = append(groups["profile->project top10 other"], scores.Score(n.cosine))
			}
			if inEither(q, c) {
				relHits++
			}
		}
		rel := open[q.cluster]
		if q.second != "" {
			rel += open[q.second]
		}
		toMain.add(mainHits, open[q.cluster]-ownsOpen(ps, q.id, q.cluster))
		toRel.add(relHits, rel-ownsOpen(ps, q.id, ""))
		others := make([]string, 50)
		for i := range others {
			others[i] = ps.all[r.IntN(len(ps.all))].id
		}
		cs, err := profileProjectCosines(ctx, pool, q.id, others, false)
		if err != nil {
			return err
		}
		for _, id := range others {
			key := "profile->project random other"
			if ps.byID[id].cluster == q.cluster {
				key = "profile->project random same"
			}
			groups[key] = append(groups[key], scores.Score(cs[id]))
		}
	}
	toMain.finish()
	toRel.finish()

	// project -> profile
	testProjects := ps.ids("test")
	fmt.Fprintf(out, "Project -> profile: %d test projects against %d sample profiles, k = %d.\n", len(testProjects), len(s.all), evalK)
	var fromMain, fromRel evalResult
	for _, q := range testProjects {
		top, err := rankWith(ctx, pool, rankResearchersSQL, q.id, evalK, "")
		if err != nil {
			return err
		}
		mainHits, relHits := 0, 0
		for _, n := range top {
			p := s.byID[n.id]
			if inCluster(p, q.cluster) {
				mainHits++
				groups["project->profile top10 same"] = append(groups["project->profile top10 same"], scores.Score(n.cosine))
			} else {
				groups["project->profile top10 other"] = append(groups["project->profile top10 other"], scores.Score(n.cosine))
			}
			if inEither(p, q.cluster) {
				relHits++
			}
		}
		relMain, relEither := 0, 0
		for _, p := range s.all {
			if p.id == q.ownerID {
				continue // a member, left out
			}
			if inCluster(p, q.cluster) {
				relMain++
			}
			if inEither(p, q.cluster) {
				relEither++
			}
		}
		fromMain.add(mainHits, relMain)
		fromRel.add(relHits, relEither)
	}
	fromMain.finish()
	fromRel.finish()
	fmt.Fprintf(out, "Done in %s.\n\n", time.Since(started).Round(time.Second))

	fmt.Fprintf(out, "| Metric | Profile -> project, same cluster | Profile -> project, main or second | Project -> profile, same cluster | Project -> profile, main or second |\n| --- | ---: | ---: | ---: | ---: |\n")
	row := func(name string, f func(evalResult) string) {
		fmt.Fprintf(out, "| %s | %s | %s | %s | %s |\n", name, f(toMain), f(toRel), f(fromMain), f(fromRel))
	}
	row("Queries (test split)", func(r evalResult) string { return fmt.Sprint(r.queries) })
	row("Mean relevant candidates, \\|R\\|", func(r evalResult) string { return fmt.Sprintf("%.1f", r.meanRelevant) })
	row("Recall@10 (capped)", func(r evalResult) string { return fmt.Sprintf("%.4f", r.capped) })
	row("Precision@10", func(r evalResult) string { return fmt.Sprintf("%.4f", r.precision) })
	row("Standard recall@10", func(r evalResult) string { return fmt.Sprintf("%.4f", r.standard) })
	if n := toMain.skipped + fromMain.skipped; n > 0 {
		fmt.Fprintf(out, "\nQueries without a relevant candidate, left out: %d.\n", n)
	}
	fmt.Fprintf(out, "\n| Score of | Pairs | Mean | p10 | p25 | p50 | p75 | p90 |\n| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, g := range []string{"profile->project random same", "profile->project random other", "profile->project top10 same",
		"profile->project top10 other", "project->profile top10 same", "project->profile top10 other"} {
		fmt.Fprintln(out, scoreRow(g, groups[g]))
	}
	return nil
}

func openProjects(ps projectSamples) []sampleProject {
	var out []sampleProject
	for _, p := range ps.all {
		if p.open {
			out = append(out, p)
		}
	}
	return out
}

// ownsOpen counts the open projects of cluster ("" for any) that userID owns: the ranking
// leaves a member's projects out, so they are not relevant candidates either.
func ownsOpen(ps projectSamples, userID, cluster string) int {
	n := 0
	for _, p := range ps.all {
		if p.open && p.ownerID == userID && (cluster == "" || p.cluster == cluster) {
			n++
		}
	}
	return n
}
