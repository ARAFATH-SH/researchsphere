//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
)

// countUsers counts the users rows with the given address, through the pool.
func countUsers(t *testing.T, conn DBTX, email string) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(t.Context(), "SELECT count(*) FROM users WHERE email = $1", email).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func insertUser(ctx context.Context, conn DBTX, email string) error {
	_, err := conn.Exec(ctx, "INSERT INTO users (email, password_hash) VALUES ($1, 'hash')", email)
	return err
}

// Writes through Conn inside InTx, nested InTx included, commit together or not at all.
func TestInTx(t *testing.T) {
	pool := testenv.DB(t)
	tr := NewTransactor(pool)

	t.Run("commits writes of the outer and the nested call together", func(t *testing.T) {
		a, b := testenv.Email(t), testenv.Email(t)
		err := tr.InTx(t.Context(), func(ctx context.Context) error {
			if err := insertUser(ctx, Conn(ctx, pool), a); err != nil {
				return err
			}
			if countUsers(t, pool, a) != 0 {
				t.Error("an uncommitted write is visible outside the transaction")
			}
			if countUsers(t, Conn(ctx, pool), a) != 1 {
				t.Error("Conn inside InTx does not see the transaction's own write")
			}
			return tr.InTx(ctx, func(ctx context.Context) error { return insertUser(ctx, Conn(ctx, pool), b) })
		})
		if err != nil {
			t.Fatal(err)
		}
		if countUsers(t, pool, a) != 1 || countUsers(t, pool, b) != 1 {
			t.Error("the writes were not committed")
		}
	})

	t.Run("an error in the nested call rolls back the outer write", func(t *testing.T) {
		a := testenv.Email(t)
		boom := errors.New("boom")
		err := tr.InTx(t.Context(), func(ctx context.Context) error {
			if err := insertUser(ctx, Conn(ctx, pool), a); err != nil {
				return err
			}
			return tr.InTx(ctx, func(context.Context) error { return boom })
		})
		if !errors.Is(err, boom) {
			t.Fatalf("got %v, want boom", err)
		}
		if countUsers(t, pool, a) != 0 {
			t.Error("the outer write was committed")
		}
	})

	t.Run("Conn outside InTx is the pool", func(t *testing.T) {
		if Conn(t.Context(), pool) != DBTX(pool) {
			t.Error("Conn without a transaction is not the pool")
		}
	})
}
