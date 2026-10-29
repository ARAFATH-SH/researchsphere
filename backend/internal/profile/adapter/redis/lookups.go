// Package redis implements the profile domain's WorkCache and LookupLimiter with Redis:
// fetched DOI metadata kept for a while, and a counter per user that expires at the end
// of its window (FR-5.2, docs/decisions.md).
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// Key prefixes. The limit key is followed by the user ID (a UUID, not personal data), the
// cache key by the bare, lower-case DOI.
const (
	limitPrefix = "profile:doi-lookup:"
	workPrefix  = "profile:openalex:work:"
)

// callTimeout bounds each call. A lookup waits at most this long for Redis before going
// on without the cache or the limit (fail open, as the upload limit).
const callTimeout = 300 * time.Millisecond

// Lookups caches works and counts lookups through a client of its own.
type Lookups struct {
	client *goredis.Client
}

var (
	_ domain.WorkCache     = (*Lookups)(nil)
	_ domain.LookupLimiter = (*Lookups)(nil)
)

// New returns Lookups with a client of its own, made from the shared client's address
// and credentials, that gives up after callTimeout (see media/adapter/redis.New). Close
// it at shutdown.
func New(shared *goredis.Client) *Lookups {
	opts := *shared.Options()
	opts.ContextTimeoutEnabled = true
	opts.PoolSize = 5
	opts.MaxRetries = -1
	opts.DialTimeout = callTimeout
	opts.ReadTimeout = callTimeout
	opts.WriteTimeout = callTimeout
	return &Lookups{client: goredis.NewClient(&opts)}
}

// Close closes the own client.
func (l *Lookups) Close() error { return l.client.Close() }

// cachedWork is the JSON form of a cached work.
type cachedWork struct {
	DOI      string  `json:"doi"`
	Title    string  `json:"title"`
	Year     *int    `json:"year,omitempty"`
	Venue    *string `json:"venue,omitempty"`
	Abstract *string `json:"abstract,omitempty"`
	URL      *string `json:"url,omitempty"`
}

// GetWork reads the cached work for doi.
func (l *Lookups) GetWork(ctx context.Context, doi string) (domain.WorkMetadata, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	b, err := l.client.Get(ctx, workPrefix+doi).Bytes()
	if errors.Is(err, goredis.Nil) {
		return domain.WorkMetadata{}, false, nil
	}
	if err != nil {
		return domain.WorkMetadata{}, false, fmt.Errorf("read cached work: %w", err)
	}
	var c cachedWork
	if err := json.Unmarshal(b, &c); err != nil {
		return domain.WorkMetadata{}, false, fmt.Errorf("decode cached work: %w", err)
	}
	return domain.WorkMetadata(c), true, nil
}

// PutWork caches w under its DOI for ttl.
func (l *Lookups) PutWork(ctx context.Context, w domain.WorkMetadata, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	b, err := json.Marshal(cachedWork(w))
	if err != nil {
		return fmt.Errorf("encode work: %w", err)
	}
	if err := l.client.Set(ctx, workPrefix+w.DOI, b, ttl).Err(); err != nil {
		return fmt.Errorf("cache work: %w", err)
	}
	return nil
}

// Used reads the counter and the time until it expires.
func (l *Lookups) Used(ctx context.Context, userID string) (int, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	k := limitPrefix + userID
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
		return 0, 0, fmt.Errorf("read lookup count: %w", err)
	}
	n, err := get.Int()
	if errors.Is(err, goredis.Nil) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("read lookup count: %w", err)
	}
	left := max(pttl.Val(), 0) // -1 (no expiry) or -2 (gone meanwhile) count as none left
	return n, left, nil
}

// Record increments the counter. The first lookup of a window sets it to expire after
// window; later ones leave the expiry alone (PEXPIRE NX), so the window is fixed.
func (l *Lookups) Record(ctx context.Context, userID string, window time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	k := limitPrefix + userID
	_, err := l.client.TxPipelined(ctx, func(p goredis.Pipeliner) error {
		p.Incr(ctx, k)
		p.Do(ctx, "pexpire", k, window.Milliseconds(), "nx")
		return nil
	})
	if err != nil {
		return fmt.Errorf("record lookup: %w", err)
	}
	return nil
}
