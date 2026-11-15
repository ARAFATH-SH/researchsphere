package redis

import (
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
)

// Every method returns an error, quickly, when Redis cannot be reached, so the usecase can
// log it and rank without the cache.
func TestCacheReturnsErrorsWhenRedisIsDown(t *testing.T) {
	client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialerRetries: 1, DialTimeout: time.Second})
	c := New(client)
	t.Cleanup(func() {
		_ = c.Close()
		_ = client.Close()
	})
	start := time.Now()
	if _, _, err := c.Get(t.Context(), "u1"); err == nil {
		t.Error("Get() error = nil")
	}
	if err := c.Set(t.Context(), "u1", domain.Kept{}, time.Minute); err == nil {
		t.Error("Set() error = nil")
	}
	if err := c.Delete(t.Context(), "u1"); err == nil {
		t.Error("Delete() error = nil")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("took %v, want each call bounded by its timeout", d)
	}
}
