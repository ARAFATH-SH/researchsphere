//go:build integration

package main

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	authpg "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/postgres"
	authusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
)

func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(t.Context(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// seedTo runs what the command runs for -n n, without the target check: the tests use the
// test database, which the command refuses.
func seedTo(t *testing.T, pool *pgxpool.Pool, g *generator, n int) plan {
	t.Helper()
	ids, err := existingSamples(t.Context(), pool, g.seed)
	if err != nil {
		t.Fatal(err)
	}
	p := makePlan(ids, n)
	if err := seed(t.Context(), pool, g, p, ids, "sample-hash", nil); err != nil {
		t.Fatal(err)
	}
	return p
}

const sampleUsers = `SELECT count(*) FROM users WHERE email::text LIKE '%@sample.researchsphere.local'`

// A second run adds nothing, a larger n adds only the difference, and what is stored is what
// the generator made.
func TestSeedTopsUpWithoutDuplicates(t *testing.T) {
	pool := testenv.DB(t)
	g := testGenerator(t, defaultSeed)

	if p := seedTo(t, pool, g, 40); len(p.missing) != 40 || p.existing != 0 {
		t.Fatalf("first run plan = %d existing, %d missing; want 0 and 40", p.existing, len(p.missing))
	}
	profilesBefore := count(t, pool, `SELECT count(*) FROM profiles`)
	pubsBefore := count(t, pool, `SELECT count(*) FROM publications`)
	if p := seedTo(t, pool, g, 40); len(p.missing) != 0 {
		t.Fatalf("second run would add %d", len(p.missing))
	}
	if got := count(t, pool, `SELECT count(*) FROM profiles`); got != profilesBefore {
		t.Errorf("a second run changed the profiles from %d to %d", profilesBefore, got)
	}
	if got := count(t, pool, `SELECT count(*) FROM publications`); got != pubsBefore {
		t.Errorf("a second run changed the publications from %d to %d", pubsBefore, got)
	}
	if p := seedTo(t, pool, g, 60); len(p.missing) != 20 || p.existing != 40 {
		t.Fatalf("top-up plan = %d existing, %d missing; want 40 and 20", p.existing, len(p.missing))
	}

	for _, tc := range []struct {
		what, sql string
		want      int
	}{
		{"sample users", sampleUsers, 60},
		{"label rows", `SELECT count(*) FROM sample_profiles`, 60},
		{"distinct seqs 0 to 59", `SELECT count(DISTINCT seq) FROM sample_profiles WHERE seq BETWEEN 0 AND 59`, 60},
		{"verified, researcher, not suspended, with the hash", `SELECT count(*) FROM users u JOIN sample_profiles s ON s.user_id = u.id
			WHERE u.email_verified_at IS NOT NULL AND u.role = 'researcher' AND u.suspended_at IS NULL AND u.password_hash = 'sample-hash'`, 60},
		{"profiles", `SELECT count(*) FROM profiles p JOIN sample_profiles s ON s.user_id = p.user_id`, 60},
		{"publications with a DOI or URL", `SELECT count(*) FROM publications WHERE doi IS NOT NULL OR url IS NOT NULL`, 0},
		{"unconfirmed publication links", `SELECT count(*) FROM user_publications WHERE confirmed_at IS NULL`, 0},
		{"connections not accepted", `SELECT count(*) FROM connections WHERE status <> 'accepted'`, 0},
		{"sample jobs at priority 0", `SELECT count(*) FROM embedding_jobs j JOIN sample_profiles s ON s.user_id = j.owner_id WHERE j.priority = 0`, 0},
		{"sample profiles queued", `SELECT count(*) FROM embedding_jobs j JOIN sample_profiles s ON s.user_id = j.owner_id WHERE j.owner_type = 'profile'`, 60},
	} {
		if got := count(t, pool, tc.sql); got != tc.want {
			t.Errorf("%s = %d, want %d", tc.what, got, tc.want)
		}
	}

	// Each stored profile and label is the generator's profile of that seq.
	rows, err := pool.Query(t.Context(), `
		SELECT s.seq, u.email::text, p.full_name, coalesce(p.headline, ''), s.main_cluster, coalesce(s.second_cluster, ''),
		       s.split, s.sparse, s.bangla, s.seed,
		       (SELECT count(*) FROM user_interests i WHERE i.user_id = s.user_id),
		       (SELECT count(*) FROM publications pub WHERE pub.created_by = s.user_id)
		FROM sample_profiles s JOIN users u ON u.id = s.user_id JOIN profiles p ON p.user_id = s.user_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var seq, interests, pubs int
		var mail, name, headline, main, second, split, bangla string
		var sparse bool
		var seed int64
		if err := rows.Scan(&seq, &mail, &name, &headline, &main, &second, &split, &sparse, &bangla, &seed, &interests, &pubs); err != nil {
			t.Fatal(err)
		}
		want := g.profile(seq)
		wantSecond := ""
		if want.label.second >= 0 {
			wantSecond = g.c.clusters[want.label.second].ID
		}
		if mail != want.email || name != want.fullName || headline != want.headline ||
			main != g.c.clusters[want.label.main].ID || second != wantSecond || split != want.label.split ||
			sparse != want.label.sparse || bangla != want.label.bangla || seed != defaultSeed ||
			interests != len(want.interests) || pubs != len(want.publications) {
			t.Errorf("profile %d is stored differently from the generator's", seq)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestSeedRefusesAnotherSeed(t *testing.T) {
	pool := testenv.DB(t)
	seedTo(t, pool, testGenerator(t, defaultSeed), 5)
	_, err := existingSamples(t.Context(), pool, defaultSeed+1)
	if err == nil || !strings.Contains(err.Error(), "seed 16") {
		t.Fatalf("existingSamples with another seed: error = %v, want a refusal naming seed 16", err)
	}
}

// An account that is not a sample profile but has a sample address stops the run before it
// writes anything.
func TestSeedRefusesTakenAddress(t *testing.T) {
	pool := testenv.DB(t)
	if _, err := pool.Exec(t.Context(), `INSERT INTO users (email, password_hash) VALUES ($1, 'x')`, email(3)); err != nil {
		t.Fatal(err)
	}
	g := testGenerator(t, defaultSeed)
	ids, err := existingSamples(t.Context(), pool, defaultSeed)
	if err != nil {
		t.Fatal(err)
	}
	err = seed(t.Context(), pool, g, makePlan(ids, 10), ids, "sample-hash", nil)
	if err == nil || !strings.Contains(err.Error(), email(3)) {
		t.Fatalf("seed() error = %v, want one naming %s", err, email(3))
	}
	if got := count(t, pool, `SELECT count(*) FROM sample_profiles`); got != 0 {
		t.Errorf("label rows = %d, want 0", got)
	}
}

// The purge deletes every sample account and nothing else, with the cleanup of the account
// deletion: a publication shared with a real user stays theirs, the rest go.
func TestPurgeRemovesOnlySampleAccounts(t *testing.T) {
	pool := testenv.DB(t)
	ctx := t.Context()
	g := testGenerator(t, defaultSeed)
	seedTo(t, pool, g, 30)

	var real, stray, samplePub, sampleUser string
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash) VALUES ($1, 'x') RETURNING id`, testenv.Email(t)).Scan(&real); err != nil {
		t.Fatal(err)
	}
	// At the sample domain but not a sample profile: the purge needs both.
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash) VALUES ('someone@sample.researchsphere.local', 'x') RETURNING id`).Scan(&stray); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT up.publication_id, up.user_id FROM user_publications up
		JOIN sample_profiles s ON s.user_id = up.user_id ORDER BY s.seq LIMIT 1`).Scan(&samplePub, &sampleUser); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO profiles (user_id, full_name) VALUES ($1, 'Real Person')`, []any{real}},
		{`INSERT INTO user_publications (user_id, publication_id, confirmed_at) VALUES ($1, $2, now())`, []any{real, samplePub}},
		{`INSERT INTO connections (requester_id, receiver_id, status) VALUES ($1, $2, 'accepted')`, []any{real, sampleUser}},
	} {
		if _, err := pool.Exec(ctx, step.sql, step.args...); err != nil {
			t.Fatal(err)
		}
	}
	samplePubs := count(t, pool, `SELECT count(*) FROM publications`)

	auth := authusecase.New(authpg.NewStore(pool), nil, nil, nil, nil, authusecase.Config{}, time.Now, slog.New(slog.DiscardHandler))
	deleted, kept, err := purge(ctx, pool, auth, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 30 || kept != 0 {
		t.Errorf("purge() = %d deleted, %d kept; want 30 and 0", deleted, kept)
	}
	for _, tc := range []struct {
		what, sql string
		args      []any
		want      int
	}{
		{"label rows", `SELECT count(*) FROM sample_profiles`, nil, 0},
		{"users at the sample domain", sampleUsers, nil, 1},
		{"the stray account", `SELECT count(*) FROM users WHERE id = $1`, []any{stray}, 1},
		{"the real account", `SELECT count(*) FROM users WHERE id = $1`, []any{real}, 1},
		{"the real profile", `SELECT count(*) FROM profiles WHERE user_id = $1`, []any{real}, 1},
		{"the shared publication", `SELECT count(*) FROM publications WHERE id = $1`, []any{samplePub}, 1},
		{"its authors", `SELECT count(*) FROM user_publications WHERE publication_id = $1`, []any{samplePub}, 1},
		{"all publications", `SELECT count(*) FROM publications`, nil, 1},
		{"connections", `SELECT count(*) FROM connections`, nil, 0},
		{"embedding jobs of deleted users", `SELECT count(*) FROM embedding_jobs j WHERE NOT EXISTS (SELECT 1 FROM users u WHERE u.id = j.owner_id)`, nil, 0},
	} {
		if got := count(t, pool, tc.sql, tc.args...); got != tc.want {
			t.Errorf("%s = %d, want %d", tc.what, got, tc.want)
		}
	}
	if samplePubs < 2 {
		t.Errorf("only %d publications were seeded; the check of their deletion means little", samplePubs)
	}

	// The purge runs again without error and finds nothing.
	if deleted, _, err := purge(ctx, pool, auth, io.Discard); err != nil || deleted != 0 {
		t.Errorf("second purge = %d, %v; want 0, nil", deleted, err)
	}
}
