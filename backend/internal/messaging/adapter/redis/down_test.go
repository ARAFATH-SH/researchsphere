package redis

import (
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Both methods return an error when Redis cannot be reached, so the usecase can log it and
// let the send go on (fail open).
func TestLimiterReturnsErrorsWhenRedisIsDown(t *testing.T) {
	client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialerRetries: 1, DialTimeout: time.Second})
	l := New(client)
	t.Cleanup(func() {
		_ = l.Close()
		_ = client.Close()
	})
	start := time.Now()
	if _, _, err := l.Used(t.Context(), "messages:minute", "u1"); err == nil {
		t.Error("Used() error = nil")
	}
	if err := l.Record(t.Context(), "messages:minute", "u1", 1, time.Minute); err == nil {
		t.Error("Record() error = nil")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("calls took %s, want them to give up quickly", d)
	}
}
