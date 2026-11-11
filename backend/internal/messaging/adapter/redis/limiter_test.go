//go:build integration

// Tests against the test Redis DB. Run with `make test-integration` (needs `make up`);
// they read TEST_REDIS_URL, and each test starts with that DB empty.
package redis

import (
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
)

func newTestLimiter(t *testing.T) *Limiter {
	t.Helper()
	l := New(testenv.Redis(t))
	t.Cleanup(func() { _ = l.Close() })
	return l
}

func TestLimiterCountsInAFixedWindow(t *testing.T) {
	l := newTestLimiter(t)
	ctx := t.Context()

	n, left, err := l.Used(ctx, "messages:day", "u1")
	if err != nil || n != 0 || left != 0 {
		t.Fatalf("Used() before any send = %d, %s, %v; want 0, 0, nil", n, left, err)
	}
	for range 3 {
		if err := l.Record(ctx, "messages:day", "u1", 1, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	n, left, err = l.Used(ctx, "messages:day", "u1")
	if err != nil || n != 3 || left <= 59*time.Minute || left > time.Hour {
		t.Fatalf("Used() = %d, %s, %v; want 3 and about an hour", n, left, err)
	}
	// A later send with another length does not move the end of the window.
	if err := l.Record(ctx, "messages:day", "u1", 1, 10*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, left, _ = l.Used(ctx, "messages:day", "u1"); left > time.Hour {
		t.Errorf("left = %s: the window moved", left)
	}
}

func TestLimiterKeepsWindowsAndUsersApart(t *testing.T) {
	l := newTestLimiter(t)
	ctx := t.Context()
	if err := l.Record(ctx, "messages:minute", "u1", 1, time.Minute); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, user string }{{"messages:day", "u1"}, {"requests:minute", "u1"}, {"messages:minute", "u2"}} {
		if n, _, err := l.Used(ctx, c.name, c.user); err != nil || n != 0 {
			t.Errorf("Used(%s, %s) = %d, %v", c.name, c.user, n, err)
		}
	}
}

func TestLimiterWindowExpires(t *testing.T) {
	l := newTestLimiter(t)
	ctx := t.Context()
	if err := l.Record(ctx, "messages:minute", "u1", 1, 30*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	if n, _, err := l.Used(ctx, "messages:minute", "u1"); err != nil || n != 0 {
		t.Errorf("after the window: %d, %v", n, err)
	}
}

// Added group members count several at once (FR-16.10).
func TestLimiterRecordsSeveral(t *testing.T) {
	l := newTestLimiter(t)
	ctx := t.Context()
	if err := l.Record(ctx, "group-members:day", "u1", 49, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := l.Record(ctx, "group-members:day", "u1", 3, time.Hour); err != nil {
		t.Fatal(err)
	}
	if n, left, err := l.Used(ctx, "group-members:day", "u1"); err != nil || n != 52 || left <= 59*time.Minute {
		t.Errorf("Used() = %d, %s, %v; want 52 and about an hour", n, left, err)
	}
}
