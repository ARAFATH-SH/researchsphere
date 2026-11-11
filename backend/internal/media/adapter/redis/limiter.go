// Package redis implements the media domain's UploadLimiter with a Redis counter per user
// that expires at the end of its window (FR-4.6, docs/decisions.md).
package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
	platformredis "github.com/ARAFATH-SH/researchsphere/backend/internal/platform/redis"
)

// keyPrefix is followed by the user ID. User IDs are UUIDs, not personal data.
const keyPrefix = "media:upload:"

// callTimeout bounds each call. An upload waits at most this long for Redis before going
// on without the limit (fail open, as the login lockout).
const callTimeout = 300 * time.Millisecond

// Limiter counts uploads through a client of its own.
type Limiter struct {
	client *goredis.Client
}

var _ domain.UploadLimiter = (*Limiter)(nil)

// New returns a Limiter with a client of its own, made from the shared client's address
// and credentials, that gives up after callTimeout (see auth/adapter/redis.New). Close
// the Limiter at shutdown.
func New(shared *goredis.Client) *Limiter {
	opts := *shared.Options()
	opts.ContextTimeoutEnabled = true
	opts.PoolSize = 5
	opts.MaxRetries = -1
	opts.DialTimeout = callTimeout
	opts.ReadTimeout = callTimeout
	opts.WriteTimeout = callTimeout
	platformredis.ApplyDefaults(&opts)
	return &Limiter{client: goredis.NewClient(&opts)}
}

// Close closes the Limiter's own client.
func (l *Limiter) Close() error { return l.client.Close() }

// Used reads the counter and the time until it expires.
func (l *Limiter) Used(ctx context.Context, userID string) (int, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	k := keyPrefix + userID
	var (
		get  *goredis.StringCmd
		pttl *goredis.DurationCmd
	)
	_, err := l.client.Pipelined(ctx, func(p goredis.Pipeliner) error {
		get = p.Get(ctx, k)
		pttl = p.PTTL(ctx, k)
		return nil
	})
	if err != nil && !errors.Is(err, goredis.Nil) {
		return 0, 0, fmt.Errorf("read upload count: %w", err)
	}
	n, err := get.Int()
	if errors.Is(err, goredis.Nil) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("read upload count: %w", err)
	}
	left := max(pttl.Val(), 0) // -1 (no expiry) or -2 (gone meanwhile) count as none left
	return n, left, nil
}

// Record increments the counter. The first upload of a window sets it to expire after
// window; later ones leave the expiry alone (PEXPIRE NX), so the window is fixed.
func (l *Limiter) Record(ctx context.Context, userID string, window time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	k := keyPrefix + userID
	_, err := l.client.TxPipelined(ctx, func(p goredis.Pipeliner) error {
		p.Incr(ctx, k)
		p.Do(ctx, "pexpire", k, window.Milliseconds(), "nx")
		return nil
	})
	if err != nil {
		return fmt.Errorf("record upload: %w", err)
	}
	return nil
}
