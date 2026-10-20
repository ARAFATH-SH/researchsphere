//go:build integration

// Repository tests against the real database. Run with `make test-integration`
// (needs `make up` and `make migrate`); they read DATABASE_URL.
package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
)

func newTestStore(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Fatal("DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(t.Context(), url)
	if err != nil {
		t.Fatal("DATABASE_URL is not accepted by the PostgreSQL driver")
	}
	t.Cleanup(pool.Close)
	return NewStore(pool), pool
}

// createUserWithFiles stores a user with one stored file per key, and removes the user
// and any queue rows for the keys when the test ends.
func createUserWithFiles(t *testing.T, pool *pgxpool.Pool, keys ...string) string {
	t.Helper()
	ctx := t.Context()
	email := "it-" + strings.ToLower(rand.Text()) + "@example.org"
	var userID string
	if err := pool.QueryRow(ctx, "INSERT INTO users (email, password_hash) VALUES ($1, 'hash') RETURNING id::text", email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		if _, err := pool.Exec(bg, "DELETE FROM users WHERE id = $1", userID); err != nil {
			t.Errorf("cleanup: %v", err)
		}
		if _, err := pool.Exec(bg, "DELETE FROM storage_deletions WHERE object_key = ANY($1)", keys); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
	for _, k := range keys {
		if _, err := pool.Exec(ctx, `INSERT INTO stored_files (owner_user_id, purpose, object_key, content_type, size_bytes)
			VALUES ($1, 'photo', $2, 'image/png', 100)`, userID, k); err != nil {
			t.Fatal(err)
		}
	}
	return userID
}

func uniqueKeys(n int) []string {
	keys := make([]string, n)
	prefix := "it/" + strings.ToLower(rand.Text())
	for i := range keys {
		keys[i] = prefix + "/" + string(rune('a'+i))
	}
	return keys
}

type queued struct {
	attempts  int
	lastError *string
	tried     bool
}

func queueRows(t *testing.T, pool *pgxpool.Pool, keys []string) map[string]queued {
	t.Helper()
	rows, err := pool.Query(t.Context(), `SELECT object_key, attempts, last_error, last_attempt_at IS NOT NULL
		FROM storage_deletions WHERE object_key = ANY($1)`, keys)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]queued{}
	for rows.Next() {
		var k string
		var q queued
		if err := rows.Scan(&k, &q.attempts, &q.lastError, &q.tried); err != nil {
			t.Fatal(err)
		}
		got[k] = q
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return got
}

// drainKeys drains until the given keys have each been offered once, calling fn for them
// only, so rows left by other tests or by the API do not disturb the test.
func drainKeys(t *testing.T, s *Store, keys []string, fn func(domain.PendingDeletion) error) {
	t.Helper()
	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
	}
	_, err := s.Drain(t.Context(), 1000, func(_ context.Context, d domain.PendingDeletion) error {
		if !want[d.ObjectKey] {
			return errors.New("not this test's row")
		}
		return fn(d)
	})
	if err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
}

// FR-2.8, NFR-13: deleting a user removes the rows and queues every object.
func TestDeletingAUserQueuesItsObjects(t *testing.T) {
	_, pool := newTestStore(t)
	keys := uniqueKeys(2)
	userID := createUserWithFiles(t, pool, keys...)

	if _, err := pool.Exec(t.Context(), "DELETE FROM users WHERE id = $1", userID); err != nil {
		t.Fatal(err)
	}
	var files int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM stored_files WHERE owner_user_id = $1", userID).Scan(&files); err != nil {
		t.Fatal(err)
	}
	if files != 0 {
		t.Errorf("stored_files rows left = %d, want 0", files)
	}
	got := queueRows(t, pool, keys)
	for _, k := range keys {
		if q, ok := got[k]; !ok || q.attempts != 0 || q.tried {
			t.Errorf("queue row for %s = %+v (present %v), want a fresh row", k, q, ok)
		}
	}
}

func TestDrainRemovesDeletedRowsAndRecordsFailures(t *testing.T) {
	s, pool := newTestStore(t)
	keys := uniqueKeys(2)
	ok, failing := keys[0], keys[1]
	userID := createUserWithFiles(t, pool, keys...)
	if _, err := pool.Exec(t.Context(), "DELETE FROM users WHERE id = $1", userID); err != nil {
		t.Fatal(err)
	}

	var offered []string
	drainKeys(t, s, keys, func(d domain.PendingDeletion) error {
		offered = append(offered, d.ObjectKey)
		if d.ObjectKey == failing {
			return errors.New("storage unavailable")
		}
		return nil
	})
	if len(offered) != 2 {
		t.Errorf("offered %v, want both keys", offered)
	}

	got := queueRows(t, pool, keys)
	if _, left := got[ok]; left {
		t.Errorf("row for the deleted object is still queued")
	}
	f, left := got[failing]
	if !left || f.attempts != 1 || f.lastError == nil || *f.lastError != "storage unavailable" || !f.tried {
		t.Errorf("row for the failing object = %+v (present %v), want 1 attempt with the error", f, left)
	}

	// The next pass offers it again with its attempts so far, and a success removes it.
	drainKeys(t, s, []string{failing}, func(d domain.PendingDeletion) error {
		if d.Attempts != 1 {
			t.Errorf("Attempts = %d, want 1", d.Attempts)
		}
		return nil
	})
	if got := queueRows(t, pool, keys); len(got) != 0 {
		t.Errorf("queue = %v, want empty", got)
	}
}

// Rows claimed by one transaction are skipped by another (several API instances).
func TestDrainSkipsRowsAnotherWorkerHolds(t *testing.T) {
	s, pool := newTestStore(t)
	keys := uniqueKeys(1)
	userID := createUserWithFiles(t, pool, keys...)
	if _, err := pool.Exec(t.Context(), "DELETE FROM users WHERE id = $1", userID); err != nil {
		t.Fatal(err)
	}

	drainKeys(t, s, keys, func(domain.PendingDeletion) error {
		// While this transaction holds the row, a second drain must not see it.
		_, err := s.Drain(t.Context(), 1000, func(_ context.Context, d domain.PendingDeletion) error {
			if d.ObjectKey == keys[0] {
				t.Errorf("second worker got %s, which the first one holds", d.ObjectKey)
			}
			return errors.New("not this test's row")
		})
		if err != nil {
			t.Errorf("second Drain() error = %v", err)
		}
		return nil
	})
	if got := queueRows(t, pool, keys); len(got) != 0 {
		t.Errorf("queue = %v, want empty", got)
	}
}
