// Package postgres implements the health domain's Pinger for PostgreSQL.
package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/health/domain"
)

// Pinger checks that PostgreSQL answers through the shared pool.
type Pinger struct {
	pool *pgxpool.Pool
}

var _ domain.Pinger = (*Pinger)(nil)

// NewPinger returns a Pinger that uses pool.
func NewPinger(pool *pgxpool.Pool) *Pinger { return &Pinger{pool: pool} }

// Ping acquires a connection and sends an empty query.
func (p *Pinger) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }
