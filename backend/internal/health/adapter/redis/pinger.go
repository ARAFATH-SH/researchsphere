// Package redis implements the health domain's Pinger for Redis.
package redis

import (
	"context"

	goredis "github.com/redis/go-redis/v9"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/health/domain"
)

// Pinger checks that Redis answers PING through the shared client.
type Pinger struct {
	client *goredis.Client
}

var _ domain.Pinger = (*Pinger)(nil)

// NewPinger returns a Pinger that uses client.
func NewPinger(client *goredis.Client) *Pinger { return &Pinger{client: client} }

// Ping sends PING.
func (p *Pinger) Ping(ctx context.Context) error { return p.client.Ping(ctx).Err() }
