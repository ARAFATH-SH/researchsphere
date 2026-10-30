//go:build integration

// Tests against the test Redis DB. Run with `make test-integration` (needs `make up`);
// they read TEST_REDIS_URL, and each test starts with that DB empty.
package redis

import (
	"crypto/rand"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
)

func newTestLimiter(t *testing.T) (*Limiter, *goredis.Client, string) {
	t.Helper()
	client := testenv.Redis(t)
	l := New(client)
	user := "it-" + strings.ToLower(rand.Text())
	t.Cleanup(func() { _ = l.Close() })
	return l, client, user
}

func TestLimiterCountsInAFixedWindow(t *testing.T) {
	l, client, user := newTestLimiter(t)
	ctx := t.Context()

	n, left, err := l.Used(ctx, user)
	if err != nil || n != 0 || left != 0 {
		t.Fatalf("Used() before any upload = %d, %s, %v; want 0, 0, nil", n, left, err)
	}
	for range 3 {
		if err := l.Record(ctx, user, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	n, left, err = l.Used(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("Used() = %d, want 3", n)
	}
	if left <= 59*time.Minute || left > time.Hour {
		t.Errorf("time left = %s, want just under 1h", left)
	}
	// A later upload does not move the end of the window.
	client.PExpire(ctx, keyPrefix+user, 10*time.Minute)
	if err := l.Record(ctx, user, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, left, _ = l.Used(ctx, user); left > 10*time.Minute {
		t.Errorf("time left after a 4th upload = %s, want at most the 10m that were left", left)
	}
}

func TestLimiterWindowEnds(t *testing.T) {
	l, _, user := newTestLimiter(t)
	ctx := t.Context()
	if err := l.Record(ctx, user, 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(120 * time.Millisecond)
	if n, _, err := l.Used(ctx, user); err != nil || n != 0 {
		t.Errorf("Used() after the window = %d, %v; want 0, nil", n, err)
	}
}
