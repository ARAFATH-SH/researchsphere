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
// fn joins it: the outer InTx decides whether it commits.
func (t Transactor) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}
	return pgx.BeginFunc(ctx, t.pool, func(tx pgx.Tx) error {
		return fn(context.WithValue(ctx, txKey{}, tx))
	})
}

// Conn returns the transaction ctx carries (see InTx), or pool when it carries none.
func Conn(ctx context.Context, pool *pgxpool.Pool) DBTX {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return pool
}
