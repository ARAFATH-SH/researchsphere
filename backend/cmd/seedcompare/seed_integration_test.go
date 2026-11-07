//go:build integration

package main

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
)

func count(t *testing.T, pool *pgxpool.Pool, sql string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(t.Context(), sql).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A second run leaves the same rows, and other people's rows are never touched.
func TestSeedIsIdempotent(t *testing.T) {
	pool := testenv.DB(t)
	var other string
	if err := pool.QueryRow(t.Context(), `INSERT INTO users (email, password_hash) VALUES ($1, 'x') RETURNING id`, testenv.Email(t)).Scan(&other); err != nil {
		t.Fatal(err)
	}
	for run := 1; run <= 2; run++ {
		if err := seed(t.Context(), pool, "hash"); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		for _, tc := range []struct {
			what, sql string
			want      int
		}{
			{"verified accounts", `SELECT count(*) FROM users WHERE email LIKE '%@compare.researchsphere.local' AND email_verified_at IS NOT NULL`, 4},
			{"accepted connections", `SELECT count(*) FROM connections WHERE status = 'accepted'`, 1},
			{"active conversations", `SELECT count(*) FROM conversations WHERE state = 'active'`, 1},
			{"pending requests", `SELECT count(*) FROM conversations WHERE state = 'request'`, 1},
			{"messages", `SELECT count(*) FROM messages`, len(script) + 1},
			{"read markers", `SELECT count(*) FROM conversation_members WHERE last_read_message_id IS NOT NULL`, 3},
			{"users", `SELECT count(*) FROM users`, 5},
			{"administrators", `SELECT count(*) FROM users WHERE role = 'admin' AND email LIKE 'compare-admin@%'`, 1},
			{"researchers among the fake accounts", `SELECT count(*) FROM users WHERE role = 'researcher' AND email LIKE '%@compare.researchsphere.local'`, 3},
			{"open reports", `SELECT count(*) FROM message_reports WHERE resolved_at IS NULL`, 1},
			{"reports by Beta on a message of Alpha", `SELECT count(*) FROM message_reports r
				JOIN users b ON b.id = r.reporter_id AND b.email LIKE 'compare-beta@%'
				JOIN users a ON a.id = r.reported_id AND a.email LIKE 'compare-alpha@%'
				JOIN messages m ON m.id = r.message_id AND m.sender_id = a.id`, 1},
		} {
			if got := count(t, pool, tc.sql); got != tc.want {
				t.Errorf("run %d: %s = %d, want %d", run, tc.what, got, tc.want)
			}
		}
	}
	if days := count(t, pool, `SELECT count(DISTINCT created_at::date) FROM messages`); days < 4 {
		t.Errorf("the messages span %d days, want several", days)
	}
	// IDs must follow the script, or the thread would show it out of order.
	if n := count(t, pool, `SELECT count(*) FROM (
		SELECT created_at < lag(created_at) OVER (PARTITION BY conversation_id ORDER BY id) AS back FROM messages) m WHERE back`); n != 0 {
		t.Errorf("%d messages sort before the ones written earlier", n)
	}
}
