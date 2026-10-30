//go:build integration

// Tests against the test Redis DB. Run with `make test-integration` (needs `make up`);
// they read TEST_REDIS_URL, and each test starts with that DB empty.
package redis

import (
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
)

func newTestAttempts(t *testing.T) (*Attempts, *goredis.Client) {
	t.Helper()
	client := testenv.Redis(t)
	a := New(client)
	t.Cleanup(func() { _ = a.Close() })
	return a, client
}

func TestRecordFailureAndReset(t *testing.T) {
	a, client := newTestAttempts(t)
	ctx := t.Context()
	key := testenv.Email(t)

	for want := 1; want <= 3; want++ {
		got, err := a.RecordFailure(ctx, key, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("RecordFailure() = %d, want %d", got, want)
		}
	}
	// The expiry is set by the first failure and kept by later ones.
	ttl, err := client.TTL(ctx, failuresPrefix+hashKey(key)).Result()
	if err != nil {
		t.Fatal(err)
	}
	if ttl <= 0 || ttl > time.Minute {
		t.Errorf("TTL = %v, want at most the 1-minute window", ttl)
	}
	// Another account has its own counter.
	if got, err := a.RecordFailure(ctx, testenv.Email(t), time.Minute); err != nil || got != 1 {
		t.Errorf("RecordFailure() for another key = %d, %v; want 1, nil", got, err)
	}

	if err := a.Reset(ctx, key); err != nil {
		t.Fatal(err)
	}
	if got, err := a.RecordFailure(ctx, key, time.Minute); err != nil || got != 1 {
		t.Errorf("RecordFailure() after Reset = %d, %v; want 1, nil", got, err)
	}
}

func TestRecordFailureWindowEnds(t *testing.T) {
	a, _ := newTestAttempts(t)
	ctx := t.Context()
	key := testenv.Email(t)

	if _, err := a.RecordFailure(ctx, key, 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if got, err := a.RecordFailure(ctx, key, 100*time.Millisecond); err != nil || got != 1 {
		t.Errorf("RecordFailure() after the window = %d, %v; want 1, nil", got, err)
	}
}

func TestLock(t *testing.T) {
	a, client := newTestAttempts(t)
	ctx := t.Context()
	key := testenv.Email(t)

	if until, err := a.LockedUntil(ctx, key); err != nil || !until.IsZero() {
		t.Fatalf("LockedUntil() before Lock = %v, %v; want zero, nil", until, err)
	}

	until := time.Now().Add(time.Minute).Truncate(time.Millisecond)
	if err := a.Lock(ctx, key, until); err != nil {
		t.Fatal(err)
	}
	got, err := a.LockedUntil(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(until) {
		t.Errorf("LockedUntil() = %v, want %v", got, until)
	}
	if v, _ := client.Get(ctx, lockPrefix+hashKey(key)).Result(); strings.Contains(v, "@") {
		t.Error("the lock stores the email address")
	}

	// The key expires when the lock ends.
	if err := a.Lock(ctx, key, time.Now().Add(100*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if got, err := a.LockedUntil(ctx, key); err != nil || !got.IsZero() {
		t.Errorf("LockedUntil() after the lock ended = %v, %v; want zero, nil", got, err)
	}
}

func TestNoEmailInKeys(t *testing.T) {
	k := failuresPrefix + hashKey("ada@example.org")
	if strings.Contains(k, "ada") || len(k) != len(failuresPrefix)+64 {
		t.Errorf("key %q must hold only the SHA-256 of the address", k)
	}
}
