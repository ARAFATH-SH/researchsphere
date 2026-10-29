package redis

import (
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// Every method returns an error when Redis cannot be reached, so the usecase can log it
// and let the lookup go on (fail open).
func TestLookupsReturnErrorsWhenRedisIsDown(t *testing.T) {
	client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialerRetries: 1, DialTimeout: time.Second})
	l := New(client)
	t.Cleanup(func() {
		_ = l.Close()
		_ = client.Close()
	})
	start := time.Now()
	if _, _, err := l.Used(t.Context(), "u1"); err == nil {
		t.Error("Used() error = nil")
	}
	if err := l.Record(t.Context(), "u1", time.Hour); err == nil {
		t.Error("Record() error = nil")
	}
	if _, _, err := l.GetWork(t.Context(), "10.5555/x"); err == nil {
		t.Error("GetWork() error = nil")
	}
	if err := l.PutWork(t.Context(), domain.WorkMetadata{DOI: "10.5555/x", Title: "T"}, time.Hour); err == nil {
		t.Error("PutWork() error = nil")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("calls took %s, want them to give up quickly", d)
	}
}
