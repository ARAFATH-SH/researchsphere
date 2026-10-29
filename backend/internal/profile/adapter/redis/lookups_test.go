//go:build integration

// Tests against the real Redis. Run with `make test-integration` (needs `make up`);
// they read REDIS_URL.
package redis

import (
	"crypto/rand"
	"os"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

func newTestLookups(t *testing.T) (*Lookups, *goredis.Client, string) {
	t.Helper()
	url := os.Getenv("REDIS_URL")
	if url == "" {
		t.Fatal("REDIS_URL is not set")
	}
	opts, err := goredis.ParseURL(url)
	if err != nil {
		t.Fatal("REDIS_URL is not accepted by the Redis client")
	}
	client := goredis.NewClient(opts)
	l := New(client)
	id := "it-" + strings.ToLower(rand.Text())
	t.Cleanup(func() {
		client.Del(t.Context(), limitPrefix+id, workPrefix+"10.5555/"+id)
		_ = l.Close()
		_ = client.Close()
	})
	return l, client, id
}

func TestWorkCacheRoundTrip(t *testing.T) {
	l, client, id := newTestLookups(t)
	ctx := t.Context()
	doi := "10.5555/" + id

	if _, ok, err := l.GetWork(ctx, doi); err != nil || ok {
		t.Fatalf("GetWork() before Put = %v, %v; want a miss", ok, err)
	}
	year, venue := 2024, "KDD"
	want := domain.WorkMetadata{DOI: doi, Title: "A title", Year: &year, Venue: &venue}
	if err := l.PutWork(ctx, want, time.Hour); err != nil {
		t.Fatal(err)
	}
	got, ok, err := l.GetWork(ctx, doi)
	if err != nil || !ok {
		t.Fatalf("GetWork() = %v, %v; want a hit", ok, err)
	}
	if got.DOI != doi || got.Title != "A title" || *got.Year != 2024 || *got.Venue != "KDD" || got.Abstract != nil || got.URL != nil {
		t.Errorf("GetWork() = %+v, want %+v", got, want)
	}
	if ttl := client.PTTL(ctx, workPrefix+doi).Val(); ttl <= 59*time.Minute || ttl > time.Hour {
		t.Errorf("TTL = %s, want just under 1h", ttl)
	}
}

func TestLimiterCountsInAFixedWindow(t *testing.T) {
	l, client, user := newTestLookups(t)
	ctx := t.Context()

	n, left, err := l.Used(ctx, user)
	if err != nil || n != 0 || left != 0 {
		t.Fatalf("Used() before any lookup = %d, %s, %v; want 0, 0, nil", n, left, err)
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
	// A later Record keeps the window's end.
	client.PExpire(ctx, limitPrefix+user, time.Minute)
	if err := l.Record(ctx, user, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, left, _ = l.Used(ctx, user); left > time.Minute {
		t.Errorf("time left after a later Record = %s, want at most 1m", left)
	}
}
