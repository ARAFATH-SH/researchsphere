package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBTX is what the sqlc-generated code of every module needs: a pool or a transaction.
// Each module's sqlcgen.DBTX has the same methods, so a DBTX passes to sqlcgen.New.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// txKey is the context key of the transaction InTx opened.
type txKey struct{}

// hooksKey is the context key of the commit hooks of that transaction.
type hooksKey struct{}

// commitHooks collects what AfterCommit put off. One transaction runs on one goroutine, so it
// needs no lock.
type commitHooks struct {
	fns []func(context.Context)
}

// Transactor runs writes of several modules in one transaction. A usecase that must write
// together with another module (a connection request and its notification, FR-9.1) calls
// InTx through an interface its domain declares; each store reads the transaction from
// the context with Conn, so neither module knows the other.
type Transactor struct {
	pool *pgxpool.Pool
}

// NewTransactor returns a Transactor on pool.
func NewTransactor(pool *pgxpool.Pool) Transactor {
	return Transactor{pool: pool}
}

// InTx runs fn with a context that carries one transaction, and commits when fn returns
// nil; otherwise, or on a panic, it rolls back. When ctx already carries a transaction,
// fn joins it: the outer InTx decides whether it commits. What AfterCommit put off runs after
// a commit, once the transaction has left the context.
func (t Transactor) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}
	hooks := &commitHooks{}
	err := pgx.BeginFunc(ctx, t.pool, func(tx pgx.Tx) error {
		return fn(context.WithValue(context.WithValue(ctx, txKey{}, tx), hooksKey{}, hooks))
	})
	if err != nil {
		return err
	}
	for _, f := range hooks.fns {
		f(context.WithoutCancel(ctx))
	}
	return nil
}

// AfterCommit runs fn once the transaction ctx carries has committed, in the order the calls
// were made, and never when it rolls back. Without a transaction it runs fn at once. Other
// people must not hear about a change that may still be undone (real-time events,
// docs/realtime.md). fn gets a context that outlives the request and holds no transaction,
// so it cannot fail the change.
func AfterCommit(ctx context.Context, fn func(ctx context.Context)) {
	if hooks, ok := ctx.Value(hooksKey{}).(*commitHooks); ok {
		hooks.fns = append(hooks.fns, fn)
		return
	}
	fn(context.WithoutCancel(ctx))
}

// Conn returns the transaction ctx carries (see InTx), or pool when it carries none.
func Conn(ctx context.Context, pool *pgxpool.Pool) DBTX {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return pool
}
