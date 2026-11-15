// Package redis implements the profile domain's WorkCache, ORCIDWorksCache,
// LookupLimiter, SearchLimiter and QueryVectorCache with Redis: fetched DOI metadata and ORCID works lists
// kept for a while, and counters per user that expire at the end of their window
// (FR-5.2, FR-5.3, FR-6, docs/decisions.md).
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	platformredis "github.com/ARAFATH-SH/researchsphere/backend/internal/platform/redis"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// Key prefixes. The limit keys are followed by the user ID (a UUID, not personal data),
// the work cache key by the bare, lower-case DOI and the ORCID cache key by the ORCID iD
// (public).
const (
	limitPrefix      = "profile:doi-lookup:"
	orcidLimitPrefix = "profile:orcid-list:"
	searchPrefix     = "profile:people-search:"
	workPrefix       = "profile:openalex:work:"
	orcidWorksPrefix = "profile:orcid:works:"
)

// callTimeout bounds each call. A lookup waits at most this long for Redis before going
// on without the cache or the limit (fail open, as the upload limit).
const callTimeout = 300 * time.Millisecond

// Lookups caches works and ORCID lists and counts lookups through a client of its own.
// Its Used and Record count DOI lookups; ORCIDLimiter counts ORCID list calls.
type Lookups struct {
	client *goredis.Client
	doi    counter
}

var (
	_ domain.WorkCache       = (*Lookups)(nil)
	_ domain.ORCIDWorksCache = (*Lookups)(nil)
	_ domain.LookupLimiter   = (*Lookups)(nil)
	_ domain.LookupLimiter   = counter{}
	_ domain.SearchLimiter   = counter{}
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
	platformredis.ApplyDefaults(&opts)
	client := goredis.NewClient(&opts)
	return &Lookups{client: client, doi: counter{client: client, prefix: limitPrefix}}
}

// ORCIDLimiter returns the counter of the user's ORCID list calls, on the same client.
func (l *Lookups) ORCIDLimiter() domain.LookupLimiter {
	return counter{client: l.client, prefix: orcidLimitPrefix}
}

// SearchLimiter returns the counter of the user's researcher searches, on the same
// client.
func (l *Lookups) SearchLimiter() domain.SearchLimiter {
	return counter{client: l.client, prefix: searchPrefix}
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

// cachedORCIDWork is the JSON form of a cached ORCID work.
type cachedORCIDWork struct {
	PutCode string  `json:"putCode"`
	Title   string  `json:"title"`
	Year    *int    `json:"year,omitempty"`
	Venue   *string `json:"venue,omitempty"`
	DOI     *string `json:"doi,omitempty"`
	Type    *string `json:"type,omitempty"`
	URL     *string `json:"url,omitempty"`
}

// GetORCIDWorks reads the cached works of the ORCID iD.
func (l *Lookups) GetORCIDWorks(ctx context.Context, orcidID string) ([]domain.ORCIDWork, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	b, err := l.client.Get(ctx, orcidWorksPrefix+orcidID).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read cached ORCID works: %w", err)
	}
	var cs []cachedORCIDWork
	if err := json.Unmarshal(b, &cs); err != nil {
		return nil, false, fmt.Errorf("decode cached ORCID works: %w", err)
	}
	ws := make([]domain.ORCIDWork, len(cs))
	for i, c := range cs {
		ws[i] = domain.ORCIDWork(c)
	}
	return ws, true, nil
}

// PutORCIDWorks caches the works under the ORCID iD for ttl.
func (l *Lookups) PutORCIDWorks(ctx context.Context, orcidID string, works []domain.ORCIDWork, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	cs := make([]cachedORCIDWork, len(works))
	for i, w := range works {
		cs[i] = cachedORCIDWork(w)
	}
	b, err := json.Marshal(cs)
	if err != nil {
		return fmt.Errorf("encode ORCID works: %w", err)
	}
	if err := l.client.Set(ctx, orcidWorksPrefix+orcidID, b, ttl).Err(); err != nil {
		return fmt.Errorf("cache ORCID works: %w", err)
	}
	return nil
}

// Used reads the DOI lookup counter and the time until it expires.
func (l *Lookups) Used(ctx context.Context, userID string) (int, time.Duration, error) {
	return l.doi.Used(ctx, userID)
}

// Record counts one DOI lookup.
func (l *Lookups) Record(ctx context.Context, userID string, window time.Duration) error {
	return l.doi.Record(ctx, userID, window)
}

// counter is a per-user counter under prefix that expires at the end of its window.
type counter struct {
	client *goredis.Client
	prefix string
}

// Used reads the counter and the time until it expires.
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
		return fmt.Errorf("record lookup: %w", err)
	}
	return nil
}
