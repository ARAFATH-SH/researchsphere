// Package redis implements the project domain's Limiter with Redis counters per user that
// expire at the end of their window: projects created a day and searches a minute
// (docs/decisions.md 2026-11-16), join requests and invitations a day, figures uploaded an
// hour and file saves a minute (2026-11-17).
package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	platformredis "github.com/ARAFATH-SH/researchsphere/backend/internal/platform/redis"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// Key prefixes, followed by the user ID (a UUID, not personal data).
const (
	createPrefix      = "project:create:"
	searchPrefix      = "project:search:"
	joinPrefix        = "project:join:"
	invitePrefix      = "project:invite:"
	figurePrefix      = "project:figure:"
	savePrefix        = "project:save:"
	ticketLimitPrefix = "project:ticket:"
)

// callTimeout bounds each call. A request waits at most this long for Redis before going on
// without the limit (fail open, as the other limits).
const callTimeout = 300 * time.Millisecond

// Limits holds the project counters on a client of its own.
type Limits struct {
	client *goredis.Client
}

var _ domain.Limiter = counter{}

// New returns Limits with a client of its own, made from the shared client's address and
// credentials, that gives up after callTimeout. Close it at shutdown.
func New(shared *goredis.Client) *Limits {
	opts := *shared.Options()
	opts.ContextTimeoutEnabled = true
	opts.PoolSize = 5
	opts.MaxRetries = -1
	opts.DialTimeout = callTimeout
	opts.ReadTimeout = callTimeout
	opts.WriteTimeout = callTimeout
	platformredis.ApplyDefaults(&opts)
	return &Limits{client: goredis.NewClient(&opts)}
}

// Create counts the projects a user creates.
func (l *Limits) Create() domain.Limiter { return counter{client: l.client, prefix: createPrefix} }

// Search counts a user's project searches.
func (l *Limits) Search() domain.Limiter { return counter{client: l.client, prefix: searchPrefix} }

// Join counts the join requests a user sends (FR-11.1).
func (l *Limits) Join() domain.Limiter { return counter{client: l.client, prefix: joinPrefix} }

// Invite counts the invitations an owner sends (FR-11.3).
func (l *Limits) Invite() domain.Limiter { return counter{client: l.client, prefix: invitePrefix} }

// Figure counts the figures a user uploads (FR-12.2).
func (l *Limits) Figure() domain.Limiter { return counter{client: l.client, prefix: figurePrefix} }

// Save counts a user's file content saves (FR-12.6).
func (l *Limits) Save() domain.Limiter { return counter{client: l.client, prefix: savePrefix} }

// Ticket counts the collaboration tickets a user asks for (slice 22).
func (l *Limits) Ticket() domain.Limiter { return counter{client: l.client, prefix: ticketLimitPrefix} }

// Close closes the own client.
func (l *Limits) Close() error { return l.client.Close() }

// counter is a fixed-window count per user under prefix.
type counter struct {
	client *goredis.Client
	prefix string
}

// Used returns the count in the user's current window and the time left in it.
func (c counter) Used(ctx context.Context, userID string) (int, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	k := c.prefix + userID
	var (
		get  *goredis.StringCmd
		pttl *goredis.DurationCmd
	)
	_, err := c.client.Pipelined(ctx, func(p goredis.Pipeliner) error {
		get = p.Get(ctx, k)
		pttl = p.PTTL(ctx, k)
		return nil
	})
	if err != nil && !errors.Is(err, goredis.Nil) {
		return 0, 0, fmt.Errorf("read %s count: %w", c.prefix, err)
	}
	n, err := get.Int()
	if errors.Is(err, goredis.Nil) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("read %s count: %w", c.prefix, err)
	}
	return n, max(pttl.Val(), 0), nil
}

// Record counts one action; the first of a window opens it for window.
func (c counter) Record(ctx context.Context, userID string, window time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	k := c.prefix + userID
	_, err := c.client.TxPipelined(ctx, func(p goredis.Pipeliner) error {
		p.Incr(ctx, k)
		p.Do(ctx, "pexpire", k, window.Milliseconds(), "nx")
		return nil
	})
	if err != nil {
		return fmt.Errorf("record %s: %w", c.prefix, err)
	}
	return nil
}
