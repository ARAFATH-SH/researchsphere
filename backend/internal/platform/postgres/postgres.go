// Package postgres opens the shared PostgreSQL connection pool.
package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool returns a pool for databaseURL. It does not connect until first used, so the
// server starts while the database is down; the health check reports it. The parse
// error is not wrapped because it may quote the URL, which can contain a password.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, errors.New("DATABASE_URL is not accepted by the PostgreSQL driver")
	}
	return pool, nil
}
