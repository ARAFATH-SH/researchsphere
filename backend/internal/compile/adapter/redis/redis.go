// Package redis implements the compile domain's Limiter (compiles a user may start a minute,
// a fixed-window counter that fails open) and Workers (a key the worker's claims keep alive).
package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/compile/domain"
	platformredis "github.com/ARAFATH-SH/researchsphere/backend/internal/platform/redis"
)

const (
	// limitPrefix is followed by the user ID.
	limitPrefix = "compile:user:"
	// workerKey exists while a worker asked for work within its TTL.
	workerKey = "compile:worker:seen"
	// callTimeout bounds each call; a compile waits no longer for Redis.
	callTimeout = 300 * time.Millisecond
)

// Client holds the compile keys on a client of its own.
type Client struct {
	client *goredis.Client
}

var (
	_ domain.Limiter = (*Client)(nil)
	_ domain.Workers = (*Client)(nil)
)

// New returns a Client made from the shared client's address and credentials, giving up
// after callTimeout. Close it at shutdown.
func New(shared *goredis.Client) *Client {
	opts := *shared.Options()
	opts.ContextTimeoutEnabled = true
	opts.PoolSize = 5
	opts.MaxRetries = -1
	opts.DialTimeout = callTimeout
	opts.ReadTimeout = callTimeout
	opts.WriteTimeout = callTimeout
	platformredis.ApplyDefaults(&opts)
	return &Client{client: goredis.NewClient(&opts)}
}

// Close closes the client.
func (c *Client) Close() error { return c.client.Close() }

// Used returns the user's compiles in the current window and the time left in it.
func (c *Client) Used(ctx context.Context, userID string) (int, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	k := limitPrefix + userID
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
		return 0, 0, fmt.Errorf("read compile count: %w", err)
	}
	n, err := get.Int()
	if errors.Is(err, goredis.Nil) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("read compile count: %w", err)
	}
	return n, max(pttl.Val(), 0), nil
}

// Record counts one compile; the first of a window opens it.
func (c *Client) Record(ctx context.Context, userID string, window time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	k := limitPrefix + userID
	_, err := c.client.TxPipelined(ctx, func(p goredis.Pipeliner) error {
		p.Incr(ctx, k)
		p.Do(ctx, "pexpire", k, window.Milliseconds(), "nx")
		return nil
	})
	if err != nil {
		return fmt.Errorf("record compile: %w", err)
	}
	return nil
}

// Seen records that a worker asked for work, for ttl.
func (c *Client) Seen(ctx context.Context, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if err := c.client.Set(ctx, workerKey, "1", ttl).Err(); err != nil {
		return fmt.Errorf("record compile worker: %w", err)
	}
	return nil
}

// Alive reports whether a worker asked for work within the last TTL.
func (c *Client) Alive(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	n, err := c.client.Exists(ctx, workerKey).Result()
	if err != nil {
		return false, fmt.Errorf("read compile worker: %w", err)
	}
	return n > 0, nil
}
