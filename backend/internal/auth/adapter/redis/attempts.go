// Package redis implements the auth domain's LoginAttempts with Redis counters that
// expire on their own (FR-2.7, docs/decisions.md).
package redis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
	platformredis "github.com/ARAFATH-SH/researchsphere/backend/internal/platform/redis"
)

// Key prefixes. The rest of each key is the SHA-256 of the account key, so no email
// address is stored in Redis.
const (
	failuresPrefix = "auth:login:fail:"
	lockPrefix     = "auth:login:lock:"
)

// callTimeout bounds each counter call. A login waits at most this long for Redis
// before going on without the lockout (fail open, docs/decisions.md).
const callTimeout = 300 * time.Millisecond

// Attempts counts failed logins and holds locks through a client of its own.
type Attempts struct {
	client *goredis.Client
}

var _ domain.LoginAttempts = (*Attempts)(nil)

// New returns Attempts with a client of their own, made from the shared client's address
// and credentials. The shared client ignores context deadlines and waits seconds for a
// Redis that hangs; this client gives up after callTimeout. The shared client is not
// changed. Close the Attempts at shutdown.
func New(shared *goredis.Client) *Attempts {
	opts := *shared.Options()
	opts.ContextTimeoutEnabled = true // honour the callTimeout deadline of each call
	opts.PoolSize = 5
	opts.MaxRetries = -1 // no retries; go-redis reads 0 as its default of 3
	opts.DialTimeout = callTimeout
	opts.ReadTimeout = callTimeout
	opts.WriteTimeout = callTimeout
	platformredis.ApplyDefaults(&opts)
	return &Attempts{client: goredis.NewClient(&opts)}
}

// Close closes the Attempts' own client.
func (a *Attempts) Close() error { return a.client.Close() }

// RecordFailure increments the account's counter. The first failure of a window sets
// the counter to expire after window; later ones leave the expiry alone (PEXPIRE NX,
// in milliseconds: EXPIRE would round a window to whole seconds).
func (a *Attempts) RecordFailure(ctx context.Context, key string, window time.Duration) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	k := failuresPrefix + hashKey(key)
	var incr *goredis.IntCmd
	_, err := a.client.TxPipelined(ctx, func(p goredis.Pipeliner) error {
		incr = p.Incr(ctx, k)
		p.Do(ctx, "pexpire", k, window.Milliseconds(), "nx")
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("record failed login: %w", err)
	}
	return int(incr.Val()), nil
}

// Reset deletes the account's counter.
func (a *Attempts) Reset(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if err := a.client.Del(ctx, failuresPrefix+hashKey(key)).Err(); err != nil {
		return fmt.Errorf("reset failed logins: %w", err)
	}
	return nil
}

// Lock stores the end of the lock, in Unix milliseconds, in a key that expires then.
func (a *Attempts) Lock(ctx context.Context, key string, until time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	err := a.client.SetArgs(ctx, lockPrefix+hashKey(key), until.UnixMilli(), goredis.SetArgs{ExpireAt: until}).Err()
	if err != nil {
		return fmt.Errorf("lock account: %w", err)
	}
	return nil
}

// LockedUntil reads the end of the lock. A missing key means no lock.
func (a *Attempts) LockedUntil(ctx context.Context, key string) (time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	v, err := a.client.Get(ctx, lockPrefix+hashKey(key)).Result()
	if errors.Is(err, goredis.Nil) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("read account lock: %w", err)
	}
	ms, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("read account lock: stored value is not a time: %w", err)
	}
	return time.UnixMilli(ms), nil
}

func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
