//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/embedding/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/embedding/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
)

const (
	modelA = "e5@aaaa"
	modelB = "e5@bbbb"
)

// db is a test database with helpers to seed users, tags and publications.
type db struct {
	t    *testing.T
	pool *pgxpool.Pool
}

func newDB(t *testing.T) db {
	t.Helper()
	return db{t: t, pool: testenv.DB(t)}
}

func (d db) exec(sql string, args ...any) {
	d.t.Helper()
	if _, err := d.pool.Exec(d.t.Context(), sql, args...); err != nil {
		d.t.Fatalf("%s: %v", sql, err)
	}
}

func (d db) scalar(sql string, args ...any) int {
	d.t.Helper()
	var n int
	if err := d.pool.QueryRow(d.t.Context(), sql, args...).Scan(&n); err != nil {
		d.t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// user stores a verified user with a profile and returns its ID; the profile insert queues it.
func (d db) user(verified bool) string {
	d.t.Helper()
	var id string
	verifiedAt := "NULL"
	if verified {
		verifiedAt = "now()"
	}
	if err := d.pool.QueryRow(d.t.Context(), `INSERT INTO users (email, password_hash, email_verified_at)
		VALUES ($1, 'hash', `+verifiedAt+`) RETURNING id::text`, testenv.Email(d.t)).Scan(&id); err != nil {
		d.t.Fatal(err)
	}
	d.exec("INSERT INTO profiles (user_id, full_name, headline) VALUES ($1, 'Ada', 'Graph learning')", id)
	return id
}

func (d db) interest(userID string) string {
	d.t.Helper()
	var id string
	if err := d.pool.QueryRow(d.t.Context(), "INSERT INTO research_interests (name) VALUES ($1) RETURNING id::text", "i "+testenv.Word(d.t)).Scan(&id); err != nil {
		d.t.Fatal(err)
	}
	if userID != "" {
		d.exec("INSERT INTO user_interests (user_id, interest_id) VALUES ($1, $2)", userID, id)
	}
	return id
}

func (d db) skill(userID string) string {
	d.t.Helper()
	var id string
	if err := d.pool.QueryRow(d.t.Context(), "INSERT INTO skills (name) VALUES ($1) RETURNING id::text", "s "+testenv.Word(d.t)).Scan(&id); err != nil {
		d.t.Fatal(err)
	}
	if userID != "" {
		d.exec("INSERT INTO user_skills (user_id, skill_id) VALUES ($1, $2)", userID, id)
	}
	return id
}

// publication stores a publication; each user in confirmed is linked and confirmed, each in
// unconfirmed linked only.
func (d db) publication(title string, year any, confirmed, unconfirmed []string) string {
	d.t.Helper()
	var id string
	if err := d.pool.QueryRow(d.t.Context(), "INSERT INTO publications (title, abstract, publication_year) VALUES ($1, $2, $3) RETURNING id::text",
		title, "Abstract of "+title, year).Scan(&id); err != nil {
		d.t.Fatal(err)
	}
	for _, u := range confirmed {
		d.exec("INSERT INTO user_publications (user_id, publication_id, confirmed_at) VALUES ($1, $2, now())", u, id)
	}
	for _, u := range unconfirmed {
		d.exec("INSERT INTO user_publications (user_id, publication_id) VALUES ($1, $2)", u, id)
	}
	return id
}

func (d db) clearJobs() { d.exec("DELETE FROM embedding_jobs") }

func vector(x int) []float32 {
	v := make([]float32, 768)
	v[x%768] = 1
	return v
}

func (d db) embedding(s *Store, userID, model string) {
	d.t.Helper()
	ok, err := s.SaveEmbedding(d.t.Context(), domain.Embedding{
		OwnerType: domain.OwnerProfile, OwnerID: userID, Model: model, ContentHash: strings.Repeat("a", 64), ChunkCount: 1, Vector: vector(1),
	})
	if err != nil || !ok {
		d.t.Fatalf("save embedding: %v, %v", ok, err)
	}
}

// queued returns the owners with a job, as conn sees them.
func queued(t *testing.T, conn interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}) map[string]bool {
	t.Helper()
	rows, err := conn.Query(t.Context(), "SELECT owner_id::text FROM embedding_jobs WHERE owner_type = 'profile'")
	if err != nil {
		t.Fatal(err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// Every change to a profile's source text queues the profile in the same transaction
// (AI-1.5): the job is there inside the transaction and gone after a rollback. A change to a
// field that is not in the text queues nothing.
func TestTriggersQueueInTheSameTransaction(t *testing.T) {
	d := newDB(t)
	ada, bob, cy := d.user(true), d.user(true), d.user(true)
	interest := d.interest(ada)
	freeInterest := d.interest("")
	skill := d.skill(ada)
	freeSkill := d.skill("")
	shared := d.publication("Shared paper", 2024, []string{ada, bob}, []string{cy})
	unconfirmed := d.publication("Claimed paper", 2023, nil, []string{ada})
	unverified := d.user(false)
	suspended := d.user(true)
	d.exec("UPDATE users SET suspended_at = now() - interval '2 days' WHERE id = $1", suspended)
	ended := d.user(true)
	d.exec("UPDATE users SET suspended_at = now() - interval '2 days', suspended_until = now() - interval '1 hour', suspension_reason = 'reason long enough' WHERE id = $1", ended)

	tests := []struct {
		name string
		sql  string
		args []any
		want []string // owners queued; empty means none
	}{
		{"profile created", "INSERT INTO users (id, email, password_hash) VALUES ('00000000-0000-4000-8000-000000000001', 'new@example.org', 'h'); INSERT INTO profiles (user_id, full_name) VALUES ('00000000-0000-4000-8000-000000000001', 'New')", nil, []string{"00000000-0000-4000-8000-000000000001"}},
		{"headline changed", "UPDATE profiles SET headline = 'Rivers' WHERE user_id = $1", []any{ada}, []string{ada}},
		{"bio changed", "UPDATE profiles SET bio = 'About me' WHERE user_id = $1", []any{ada}, []string{ada}},
		{"institution changed (not in the text)", "UPDATE profiles SET institution = 'BUET' WHERE user_id = $1", []any{ada}, nil},
		{"interest added", "INSERT INTO user_interests (user_id, interest_id) VALUES ($1, $2)", []any{ada, freeInterest}, []string{ada}},
		{"interest removed", "DELETE FROM user_interests WHERE user_id = $1 AND interest_id = $2", []any{ada, interest}, []string{ada}},
		{"skill added", "INSERT INTO user_skills (user_id, skill_id) VALUES ($1, $2)", []any{ada, freeSkill}, []string{ada}},
		{"skill removed", "DELETE FROM user_skills WHERE user_id = $1 AND skill_id = $2", []any{ada, skill}, []string{ada}},
		{"publication linked", "INSERT INTO user_publications (user_id, publication_id, confirmed_at) VALUES ($1, $2, now())", []any{cy, unconfirmed}, []string{cy}},
		{"publication confirmed", "UPDATE user_publications SET confirmed_at = now() WHERE user_id = $1 AND publication_id = $2", []any{ada, unconfirmed}, []string{ada}},
		{"publication removed", "DELETE FROM user_publications WHERE user_id = $1 AND publication_id = $2", []any{ada, shared}, []string{ada}},
		{"shared publication's title edited", "UPDATE publications SET title = 'Renamed' WHERE id = $1", []any{shared}, []string{ada, bob}},
		{"shared publication's abstract edited", "UPDATE publications SET abstract = 'New abstract' WHERE id = $1", []any{shared}, []string{ada, bob}},
		{"publication's venue edited (not in the text)", "UPDATE publications SET venue_name = 'NeurIPS' WHERE id = $1", []any{shared}, nil},
		{"email verified", "UPDATE users SET email_verified_at = now() WHERE id = $1", []any{unverified}, []string{unverified}},
		{"suspension restored", "UPDATE users SET suspended_at = NULL WHERE id = $1", []any{suspended}, []string{suspended}},
		{"ended suspension cleared", "UPDATE users SET suspended_at = NULL, suspended_until = NULL, suspension_reason = NULL WHERE id = $1 AND suspended_until <= now()", []any{ended}, []string{ended}},
		{"user suspended (deletes, queues nothing)", "UPDATE users SET suspended_at = now() WHERE id = $1", []any{ada}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d.clearJobs()
			ctx := t.Context()
			tx, err := d.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			for _, stmt := range strings.Split(tt.sql, "; ") {
				args := tt.args
				if !strings.Contains(stmt, "$") {
					args = nil
				}
				if _, err := tx.Exec(ctx, stmt, args...); err != nil {
					t.Fatalf("%s: %v", stmt, err)
				}
			}
			inTx := queued(t, tx)
			if len(inTx) != len(tt.want) {
				t.Errorf("queued in the transaction: %v, want %v", inTx, tt.want)
			}
			for _, w := range tt.want {
				if !inTx[w] {
					t.Errorf("%s not queued in the transaction", w)
				}
			}
			if got := queued(t, d.pool); len(got) != 0 {
				t.Errorf("visible outside the transaction before commit: %v", got)
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			if got := queued(t, d.pool); len(got) != 0 {
				t.Errorf("left after rollback: %v", got)
			}
		})
	}
}

// A suspension deletes the embedding at once, in its own transaction.
func TestSuspensionDeletesTheEmbedding(t *testing.T) {
	d := newDB(t)
	s := NewStore(d.pool)
	ada := d.user(true)
	d.embedding(s, ada, modelA)
	d.exec("UPDATE users SET suspended_at = now() WHERE id = $1", ada)
	if n := d.scalar("SELECT count(*) FROM profile_embeddings WHERE user_id = $1", ada); n != 0 {
		t.Errorf("embeddings after suspension = %d, want 0", n)
	}
	// Saving while suspended stores nothing.
	ok, err := s.SaveEmbedding(t.Context(), domain.Embedding{OwnerType: domain.OwnerProfile, OwnerID: ada, Model: modelA,
		ContentHash: strings.Repeat("b", 64), ChunkCount: 1, Vector: vector(2)})
	if err != nil || ok {
		t.Errorf("save while suspended = %v, %v; want false", ok, err)
	}
	unverified := d.user(false)
	if ok, _ := s.SaveEmbedding(t.Context(), domain.Embedding{OwnerType: domain.OwnerProfile, OwnerID: unverified, Model: modelA,
		ContentHash: strings.Repeat("b", 64), ChunkCount: 1, Vector: vector(2)}); ok {
		t.Error("an unverified user's embedding was stored")
	}
}

// A suspension that commits while the worker saves: either the save sees it, or the
// suspension waits for the save and its trigger deletes the row. No suspended user is left
// with an embedding.
func TestSuspensionRacingASave(t *testing.T) {
	d := newDB(t)
	s := NewStore(d.pool)
	ada := d.user(true)
	ctx := t.Context()

	saved, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- postgres.NewTransactor(d.pool).InTx(ctx, func(ctx context.Context) error {
			ok, err := s.SaveEmbedding(ctx, domain.Embedding{OwnerType: domain.OwnerProfile, OwnerID: ada, Model: modelA,
				ContentHash: strings.Repeat("c", 64), ChunkCount: 1, Vector: vector(3)})
			if err == nil && !ok {
				err = fmt.Errorf("nothing saved")
			}
			close(saved)
			<-release
			return err
		})
	}()
	<-saved
	suspended := make(chan error, 1)
	go func() {
		_, err := d.pool.Exec(context.Background(), "UPDATE users SET suspended_at = now() WHERE id = $1", ada)
		suspended <- err
	}()
	select {
	case err := <-suspended:
		close(release)
		t.Fatalf("the suspension did not wait for the save (err %v)", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-suspended; err != nil {
		t.Fatal(err)
	}
	if n := d.scalar("SELECT count(*) FROM profile_embeddings WHERE user_id = $1", ada); n != 0 {
		t.Errorf("suspended user has %d embeddings, want 0", n)
	}
}

// One pending row per owner: a new request raises the generation and resets a failed job,
// and a user's change moves a re-queued job ahead.
func TestEnqueueKeepsOneRowPerOwner(t *testing.T) {
	d := newDB(t)
	ada := d.user(true)
	d.exec("UPDATE embedding_jobs SET priority = 1, attempts = 5, failed_at = now(), last_error = 'boom' WHERE owner_id = $1", ada)
	d.exec("UPDATE profiles SET headline = 'Again' WHERE user_id = $1", ada)
	var gen, attempts, priority int
	var failed bool
	if err := d.pool.QueryRow(t.Context(), `SELECT generation, attempts, priority, failed_at IS NOT NULL FROM embedding_jobs WHERE owner_id = $1`, ada).
		Scan(&gen, &attempts, &priority, &failed); err != nil {
		t.Fatal(err)
	}
	if gen != 2 || attempts != 0 || priority != 0 || failed {
		t.Errorf("job = generation %d, attempts %d, priority %d, failed %v; want 2, 0, 0, false", gen, attempts, priority, failed)
	}
	if n := d.scalar("SELECT count(*) FROM embedding_jobs WHERE owner_id = $1", ada); n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
}

func TestClaimFinishReleaseRetry(t *testing.T) {
	d := newDB(t)
	s := NewStore(d.pool)
	ctx := t.Context()
	ada, bob := d.user(true), d.user(true)
	d.exec("UPDATE embedding_jobs SET priority = 1 WHERE owner_id = $1", ada)

	jobs, err := s.ClaimJobs(ctx, 1, time.Minute)
	if err != nil || len(jobs) != 1 || jobs[0].OwnerID != bob {
		t.Fatalf("claim = %+v, %v; want bob first (a user's change before a re-queue)", jobs, err)
	}
	if again, _ := s.ClaimJobs(ctx, 5, time.Minute); len(again) != 1 || again[0].OwnerID != ada {
		t.Errorf("second claim = %+v, want only ada: bob is leased", again)
	}

	// A change while leased: finishing keeps the job and ends the lease.
	d.exec("UPDATE profiles SET bio = 'changed' WHERE user_id = $1", bob)
	if err := s.FinishJob(ctx, jobs[0]); err != nil {
		t.Fatal(err)
	}
	if n := d.scalar("SELECT count(*) FROM embedding_jobs WHERE owner_id = $1 AND leased_until IS NULL", bob); n != 1 {
		t.Errorf("bob's newer job: %d rows, want it kept and released", n)
	}
	bobJobs, _ := s.ClaimJobs(ctx, 5, time.Minute)
	if len(bobJobs) != 1 || bobJobs[0].Generation != 2 {
		t.Fatalf("reclaim = %+v, want bob at generation 2", bobJobs)
	}
	if err := s.FinishJob(ctx, bobJobs[0]); err != nil {
		t.Fatal(err)
	}
	if n := d.scalar("SELECT count(*) FROM embedding_jobs WHERE owner_id = $1", bob); n != 0 {
		t.Errorf("bob's job not deleted")
	}

	// Release ends the lease without counting; Retry counts and can fail the job.
	d.exec("UPDATE embedding_jobs SET leased_until = NULL")
	ja, _ := s.ClaimJobs(ctx, 5, time.Minute)
	if err := s.ReleaseJob(ctx, ja[0]); err != nil {
		t.Fatal(err)
	}
	ja, _ = s.ClaimJobs(ctx, 5, time.Minute)
	if len(ja) != 1 || ja[0].Attempts != 0 {
		t.Fatalf("after release: %+v", ja)
	}
	if err := s.RetryJob(ctx, ja[0], "400 bad_request", time.Now().Add(-time.Second), true); err != nil {
		t.Fatal(err)
	}
	var attempts int
	var lastErr string
	var failed bool
	if err := d.pool.QueryRow(ctx, "SELECT attempts, last_error, failed_at IS NOT NULL FROM embedding_jobs WHERE owner_id = $1", ada).Scan(&attempts, &lastErr, &failed); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || lastErr != "400 bad_request" || !failed {
		t.Errorf("after retry: attempts %d, error %q, failed %v", attempts, lastErr, failed)
	}
	if more, _ := s.ClaimJobs(ctx, 5, time.Minute); len(more) != 0 {
		t.Errorf("a failed job was claimed: %+v", more)
	}
	// An expired lease lets another worker take the job.
	d.exec("UPDATE profiles SET bio = 'x' WHERE user_id = $1", ada)
	if _, err := s.ClaimJobs(ctx, 5, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if again, _ := s.ClaimJobs(ctx, 5, time.Minute); len(again) != 1 {
		t.Errorf("after the lease ran out: %d jobs, want 1", len(again))
	}
}

// The source text holds the public fields only, and confirmed publications newest first.
func TestProfileSource(t *testing.T) {
	d := newDB(t)
	s := NewStore(d.pool)
	ada := d.user(true)
	d.exec("UPDATE profiles SET bio = 'My bio', institution = 'Secret U' WHERE user_id = $1", ada)
	d.exec("INSERT INTO profile_contact (user_id, phone) VALUES ($1, '+8801712345678') ON CONFLICT (user_id) DO UPDATE SET phone = EXCLUDED.phone", ada)
	d.interest(ada)
	d.skill(ada)
	d.publication("Old", 2019, []string{ada}, nil)
	d.publication("New", 2025, []string{ada}, nil)
	d.publication("Undated", nil, []string{ada}, nil)
	d.publication("Not mine yet", 2026, nil, []string{ada})

	src, err := s.ProfileSource(t.Context(), ada)
	if err != nil {
		t.Fatal(err)
	}
	if !src.Eligible || src.Headline != "Graph learning" || src.Bio != "My bio" || len(src.Interests) != 1 || len(src.Skills) != 1 {
		t.Errorf("source = %+v", src)
	}
	var titles []string
	for _, p := range src.Publications {
		titles = append(titles, p.Title)
	}
	if strings.Join(titles, ",") != "New,Old,Undated" {
		t.Errorf("publications = %v, want New,Old,Undated (confirmed only, newest first)", titles)
	}
	text := usecase.ProfileText(src)
	for _, secret := range []string{"Secret U", "+8801712345678", "@"} {
		if strings.Contains(text, secret) {
			t.Errorf("text contains %q:\n%s", secret, text)
		}
	}

	d.exec("UPDATE users SET suspended_at = now() WHERE id = $1", ada)
	if src, _ := s.ProfileSource(t.Context(), ada); src.Eligible {
		t.Error("a suspended user is eligible")
	}
	d.exec("UPDATE users SET suspended_until = now() - interval '1 minute', suspended_at = now() - interval '1 day', suspension_reason = 'reason long enough' WHERE id = $1", ada)
	if src, _ := s.ProfileSource(t.Context(), ada); !src.Eligible {
		t.Error("a user whose suspension has ended is not eligible")
	}
	if _, err := s.ProfileSource(t.Context(), "00000000-0000-4000-8000-000000000009"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown user: %v, want ErrNotFound", err)
	}
}

// Account deletion removes the embedding by cascade; the worker drops the job left behind.
func TestDeletedAccount(t *testing.T) {
	d := newDB(t)
	s := NewStore(d.pool)
	ada := d.user(true)
	d.interest(ada)
	d.embedding(s, ada, modelA)
	d.exec("DELETE FROM users WHERE id = $1", ada)
	if n := d.scalar("SELECT count(*) FROM profile_embeddings WHERE user_id = $1", ada); n != 0 {
		t.Errorf("embedding left after deletion")
	}
	w := usecase.NewWorker(s, &countingEmbedder{model: modelA}, fastConfig(), time.Now, slog.New(slog.DiscardHandler))
	w.Tick(t.Context())
	if n := d.scalar("SELECT count(*) FROM embedding_jobs WHERE owner_id = $1", ada); n != 0 {
		t.Errorf("job for a deleted user left: %d", n)
	}
}

func TestQueueStaleAllRebuildAndStats(t *testing.T) {
	d := newDB(t)
	s := NewStore(d.pool)
	ctx := t.Context()
	ada, bob, cy := d.user(true), d.user(true), d.user(true)
	d.user(false) // unverified: never queued by QueueAll
	d.embedding(s, ada, modelA)
	d.embedding(s, bob, modelB)
	d.embedding(s, cy, modelA)
	d.clearJobs()
	d.exec("UPDATE profiles SET bio = 'pending change' WHERE user_id = $1", cy) // cy already waits

	n, err := s.QueueStale(ctx, modelB, 500)
	if err != nil || n != 1 {
		t.Errorf("QueueStale = %d, %v; want 1 (ada; cy already waits, bob is current)", n, err)
	}
	if p := d.scalar("SELECT priority FROM embedding_jobs WHERE owner_id = $1", ada); p != 1 {
		t.Errorf("stale owner's priority = %d, want 1 (low)", p)
	}
	if p := d.scalar("SELECT priority FROM embedding_jobs WHERE owner_id = $1", cy); p != 0 {
		t.Errorf("a user's change lost its priority: %d", p)
	}

	st, err := s.Stats(ctx, modelB)
	if err != nil || len(st) != 1 {
		t.Fatal(st, err)
	}
	if st[0] != (domain.OwnerStats{OwnerType: domain.OwnerProfile, Total: 3, CurrentModel: 1, Queued: 2, Failed: 0}) {
		t.Errorf("stats = %+v", st[0])
	}

	admin := d.user(true)
	d.clearJobs()
	for range 2 { // idempotent: one job per owner
		if n, err := s.Rebuild(ctx, admin); err != nil || n != 4 {
			t.Errorf("Rebuild = %d, %v; want the 4 verified users", n, err)
		}
	}
	if n := d.scalar("SELECT count(*) FROM embedding_jobs WHERE force AND priority = 1"); n != 4 {
		t.Errorf("forced low-priority jobs = %d, want 4", n)
	}
	if n := d.scalar("SELECT count(*) FROM ai_admin_actions WHERE admin_id = $1 AND action = 'embeddings_rebuild' AND queued = 4", admin); n != 2 {
		t.Errorf("audit rows = %d, want 2", n)
	}
	d.exec("UPDATE embedding_jobs SET failed_at = now(), last_error = 'x' WHERE owner_id = $1", ada)
	if st, _ := s.Stats(ctx, ""); st[0].Failed != 1 || st[0].Queued != 3 || st[0].CurrentModel != 0 {
		t.Errorf("stats with no model = %+v", st[0])
	}

	d.clearJobs()
	if n, err := s.QueueAll(ctx, false); err != nil || n != 4 {
		t.Errorf("QueueAll = %d, %v", n, err)
	}
	if n := d.scalar("SELECT count(*) FROM embedding_jobs WHERE force"); n != 0 {
		t.Errorf("backfill forced %d jobs, want 0", n)
	}
}

// countingEmbedder is the AI service for the worker tests. It fails the test when two calls
// for the same text run at once, and records the texts.
type countingEmbedder struct {
	model    string
	delay    time.Duration
	mu       sync.Mutex
	inFlight map[string]bool
	overlap  atomic.Int32
	calls    map[string]int
}

func (e *countingEmbedder) EmbedDocument(_ context.Context, text string, _ int) ([]float32, string, int, error) {
	e.mu.Lock()
	if e.inFlight == nil {
		e.inFlight, e.calls = map[string]bool{}, map[string]int{}
	}
	if e.inFlight[text] {
		e.overlap.Add(1)
	}
	e.inFlight[text] = true
	e.calls[text]++
	e.mu.Unlock()
	time.Sleep(e.delay)
	e.mu.Lock()
	delete(e.inFlight, text)
	e.mu.Unlock()
	return vector(len(text)), e.model, 1, nil
}

func (e *countingEmbedder) Model(context.Context) (string, error) { return e.model, nil }

func fastConfig() usecase.WorkerConfig {
	cfg := usecase.DefaultWorkerConfig()
	cfg.Poll, cfg.Batch = 10*time.Millisecond, 4
	return cfg
}

// Two workers on one queue never process the same owner at once, and every owner is embedded
// exactly once.
func TestTwoWorkersNeverProcessTheSameOwner(t *testing.T) {
	d := newDB(t)
	const owners = 30
	for i := range owners {
		id := d.user(true)
		d.exec("UPDATE profiles SET headline = $2 WHERE user_id = $1", id, fmt.Sprintf("owner %d", i))
	}
	ai := &countingEmbedder{model: modelA, delay: 5 * time.Millisecond}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for range 2 {
		w := usecase.NewWorker(NewStore(d.pool), ai, fastConfig(), time.Now, slog.New(slog.DiscardHandler))
		wg.Go(func() { w.Run(ctx) })
	}
	for ctx.Err() == nil && d.scalar("SELECT count(*) FROM embedding_jobs") > 0 {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	wg.Wait()
	if n := d.scalar("SELECT count(*) FROM profile_embeddings"); n != owners {
		t.Errorf("embeddings = %d, want %d", n, owners)
	}
	if ai.overlap.Load() != 0 {
		t.Errorf("%d owners were processed by two workers at once", ai.overlap.Load())
	}
	for text, n := range ai.calls {
		if n != 1 {
			t.Errorf("%q embedded %d times, want once", text, n)
		}
	}
}

// AI-1.6: when the service reports another model, the old embeddings are re-queued and made
// again, without blocking a user's change.
func TestModelChangeReembedsEveryone(t *testing.T) {
	d := newDB(t)
	s := NewStore(d.pool)
	ada, bob := d.user(true), d.user(true)
	w := usecase.NewWorker(s, &countingEmbedder{model: modelA}, fastConfig(), time.Now, slog.New(slog.DiscardHandler))
	w.Tick(t.Context())
	if n := d.scalar("SELECT count(*) FROM profile_embeddings WHERE model = $1", modelA); n != 2 {
		t.Fatalf("model A embeddings = %d, want 2", n)
	}

	w2 := usecase.NewWorker(s, &countingEmbedder{model: modelB}, fastConfig(), time.Now, slog.New(slog.DiscardHandler))
	w2.Tick(t.Context())
	w2.Tick(t.Context())
	for _, id := range []string{ada, bob} {
		var model string
		var chunks int
		if err := d.pool.QueryRow(t.Context(), "SELECT model, chunk_count FROM profile_embeddings WHERE user_id = $1", id).Scan(&model, &chunks); err != nil {
			t.Fatal(err)
		}
		if model != modelB || chunks != 1 {
			t.Errorf("%s: model %s, chunks %d; want %s", id, model, chunks, modelB)
		}
	}
}
