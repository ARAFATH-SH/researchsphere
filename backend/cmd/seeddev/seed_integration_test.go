//go:build integration

package main

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
)

func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(t.Context(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A second run leaves the same rows, repairs what changed and never touches other accounts.
func TestSeedIsIdempotentAndRepairs(t *testing.T) {
	pool := testenv.DB(t)
	ctx := t.Context()
	other := testenv.Email(t)
	if _, err := pool.Exec(ctx, `INSERT INTO users (email, password_hash) VALUES ($1, 'other-hash')`, other); err != nil {
		t.Fatal(err)
	}

	check := func(run int, hash string) {
		t.Helper()
		for _, tc := range []struct {
			what, sql string
			want      int
		}{
			{"seeded accounts", `SELECT count(*) FROM users WHERE email LIKE '%@example.com'`, 5},
			{"verified, unsuspended, with the hash", `SELECT count(*) FROM users WHERE email LIKE '%@example.com'
				AND email_verified_at IS NOT NULL AND suspended_at IS NULL AND password_hash = $1`, 5},
			{"profiles", `SELECT count(*) FROM profiles p JOIN users u ON u.id = p.user_id WHERE u.email LIKE '%@example.com'`, 5},
			{"administrators", `SELECT count(*) FROM users WHERE email = 'admin@example.com' AND role = 'admin'`, 1},
			{"researchers", `SELECT count(*) FROM users WHERE email LIKE 'test_@example.com' AND role = 'researcher'`, 4},
		} {
			args := []any{}
			if tc.what == "verified, unsuspended, with the hash" {
				args = append(args, hash)
			}
			if got := count(t, pool, tc.sql, args...); got != tc.want {
				t.Errorf("run %d: %s = %d, want %d", run, tc.what, got, tc.want)
			}
		}
		if got := count(t, pool, `SELECT count(*) FROM users WHERE email = $1 AND password_hash = 'other-hash' AND email_verified_at IS NULL`, other); got != 1 {
			t.Errorf("run %d: another account was changed", run)
		}
	}

	if err := seed(ctx, pool, "hash-1"); err != nil {
		t.Fatal(err)
	}
	check(1, "hash-1")
	var verifiedAt string
	if err := pool.QueryRow(ctx, `SELECT email_verified_at::text FROM users WHERE email = 'test1@example.com'`).Scan(&verifiedAt); err != nil {
		t.Fatal(err)
	}

	// Break the accounts the way a developer might, then seed again.
	if _, err := pool.Exec(ctx, `UPDATE users SET role = 'admin', suspended_at = now(), suspension_reason = 'Suspended while testing.' WHERE email = 'test2@example.com'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET role = 'researcher', email_verified_at = NULL WHERE email = 'admin@example.com'`); err != nil {
		t.Fatal(err)
	}
	if err := seed(ctx, pool, "hash-2"); err != nil {
		t.Fatal(err)
	}
	check(2, "hash-2")
	var again string
	if err := pool.QueryRow(ctx, `SELECT email_verified_at::text FROM users WHERE email = 'test1@example.com'`).Scan(&again); err != nil || again != verifiedAt {
		t.Errorf("verified time changed from %s to %s (%v); a repair keeps it", verifiedAt, again, err)
	}
}
