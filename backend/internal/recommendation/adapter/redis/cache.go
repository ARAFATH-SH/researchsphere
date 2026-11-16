// Package redis implements the recommendation domain's Cache with Redis: each user's list,
// kept for a while as JSON with the stamp of the embedding it was made from.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	platformredis "github.com/ARAFATH-SH/researchsphere/backend/internal/platform/redis"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
)

// keyPrefix is followed by the user ID (a UUID, not personal data).
const keyPrefix = "recs:people:"

// callTimeout bounds each call; the usecase goes on without the cache after it.
const callTimeout = 300 * time.Millisecond

// Cache keeps lists through a client of its own.
type Cache struct {
	client *goredis.Client
}

var _ domain.Cache = (*Cache)(nil)

// New returns a Cache with a client of its own, made from the shared client's address and
// credentials, that gives up after callTimeout (see profile/adapter/redis.New). Close it
// at shutdown.
func New(shared *goredis.Client) *Cache {
	opts := *shared.Options()
	opts.ContextTimeoutEnabled = true
	opts.PoolSize = 5
	opts.MaxRetries = -1
	opts.DialTimeout = callTimeout
	opts.ReadTimeout = callTimeout
	opts.WriteTimeout = callTimeout
	platformredis.ApplyDefaults(&opts)
	return &Cache{client: goredis.NewClient(&opts)}
}

// Close closes the own client.
func (c *Cache) Close() error { return c.client.Close() }

// Key is where userID's list is kept.
func Key(userID string) string { return keyPrefix + userID }

type keptJSON struct {
	Model       string     `json:"model"`
	ContentHash string     `json:"contentHash"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	Items       []itemJSON `json:"items"`
}

type itemJSON struct {
	UserID      string   `json:"userId"`
	FullName    string   `json:"fullName"`
	Headline    *string  `json:"headline,omitempty"`
	Institution *string  `json:"institution,omitempty"`
	PhotoFileID *string  `json:"photoFileId,omitempty"`
	Score       int      `json:"score"`
	Shared      []string `json:"shared"`
}

// Get reads userID's list.
func (c *Cache) Get(ctx context.Context, userID string) (domain.Kept, bool, error) {
	return c.get(ctx, Key(userID))
}

// Set keeps userID's list for ttl. The connection statuses are not kept.
func (c *Cache) Set(ctx context.Context, userID string, kept domain.Kept, ttl time.Duration) error {
	return c.set(ctx, Key(userID), kept, ttl)
}

// Delete drops the lists of userIDs.
func (c *Cache) Delete(ctx context.Context, userIDs ...string) error {
	return c.del(ctx, Key, userIDs, "drop kept recommendations")
}

// get reads a people list kept at key.
func (c *Cache) get(ctx context.Context, key string) (domain.Kept, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	b, err := c.client.Get(ctx, key).Bytes()
	if errors.Is(err, goredis.Nil) {
		return domain.Kept{}, false, nil
	}
	if err != nil {
		return domain.Kept{}, false, fmt.Errorf("read kept recommendations: %w", err)
	}
	var k keptJSON
	if err := json.Unmarshal(b, &k); err != nil {
		return domain.Kept{}, false, fmt.Errorf("decode kept recommendations: %w", err)
	}
	out := domain.Kept{
		Stamp: domain.Stamp{Model: k.Model, ContentHash: k.ContentHash, UpdatedAt: k.UpdatedAt},
		Items: make([]domain.Recommendation, len(k.Items)),
	}
	for i, it := range k.Items {
		out.Items[i] = domain.Recommendation{
			Person: domain.Person{UserID: it.UserID, FullName: it.FullName, Headline: it.Headline,
				Institution: it.Institution, PhotoFileID: it.PhotoFileID},
			Score: it.Score, Shared: nonNil(it.Shared),
		}
	}
	return out, true, nil
}

// set keeps a people list at key for ttl.
func (c *Cache) set(ctx context.Context, key string, kept domain.Kept, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	k := keptJSON{Model: kept.Stamp.Model, ContentHash: kept.Stamp.ContentHash, UpdatedAt: kept.Stamp.UpdatedAt,
		Items: make([]itemJSON, len(kept.Items))}
	for i, it := range kept.Items {
		k.Items[i] = itemJSON{UserID: it.UserID, FullName: it.FullName, Headline: it.Headline,
			Institution: it.Institution, PhotoFileID: it.PhotoFileID, Score: it.Score, Shared: it.Shared}
	}
	b, err := json.Marshal(k)
	if err != nil {
		return fmt.Errorf("encode recommendations: %w", err)
	}
	if err := c.client.Set(ctx, key, b, ttl).Err(); err != nil {
		return fmt.Errorf("keep recommendations: %w", err)
	}
	return nil
}
