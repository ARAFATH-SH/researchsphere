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

// Other people hear about a change only once it stands (real-time events).
func TestAfterCommit(t *testing.T) {
	pool := testenv.DB(t)
	tr := NewTransactor(pool)

	t.Run("runs after the commit, in order, with no transaction", func(t *testing.T) {
		email := testenv.Email(t)
		var order []string
		err := tr.InTx(t.Context(), func(ctx context.Context) error {
			if err := insertUser(ctx, Conn(ctx, pool), email); err != nil {
				return err
			}
			AfterCommit(ctx, func(ctx context.Context) {
				if countUsers(t, pool, email) != 1 {
					t.Error("the hook ran before the commit")
				}
				if _, ok := Conn(ctx, pool).(interface{ Commit(context.Context) error }); ok {
					t.Error("the hook's context still carries the transaction")
				}
				order = append(order, "first")
			})
			// A nested call joins the outer transaction, so it waits for the outer commit.
			return tr.InTx(ctx, func(ctx context.Context) error {
				AfterCommit(ctx, func(context.Context) { order = append(order, "second") })
				if len(order) != 0 {
					t.Error("a hook ran inside the transaction")
				}
				return nil
			})
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(order) != 2 || order[0] != "first" || order[1] != "second" {
			t.Errorf("order = %v", order)
		}
	})

	t.Run("does not run after a rollback", func(t *testing.T) {
		boom := errors.New("boom")
		ran := false
		err := tr.InTx(t.Context(), func(ctx context.Context) error {
			AfterCommit(ctx, func(context.Context) { ran = true })
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v", err)
		}
		if ran {
			t.Error("the hook ran after a rollback")
		}
	})

	t.Run("runs at once without a transaction, on a context that outlives the request", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		ran := false
		AfterCommit(ctx, func(ctx context.Context) {
			ran = true
			if ctx.Err() != nil {
				t.Errorf("ctx.Err() = %v", ctx.Err())
			}
		})
		if !ran {
			t.Error("the hook did not run")
		}
	})
}
