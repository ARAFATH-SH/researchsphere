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
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

func newTestLookups(t *testing.T) (*Lookups, *goredis.Client, string) {
	t.Helper()
	client := testenv.Redis(t)
	l := New(client)
	id := "it-" + strings.ToLower(rand.Text())
	t.Cleanup(func() { _ = l.Close() })
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

func TestORCIDWorksCacheRoundTrip(t *testing.T) {
	l, client, id := newTestLookups(t) // id stands in for the ORCID iD
	ctx := t.Context()

	if _, ok, err := l.GetORCIDWorks(ctx, id); err != nil || ok {
		t.Fatalf("GetORCIDWorks() before Put = %v, %v; want a miss", ok, err)
	}
	year, doi := 2024, "10.5555/x"
	want := []domain.ORCIDWork{{PutCode: "1", Title: "A", Year: &year, DOI: &doi}, {PutCode: "2", Title: "B"}}
	if err := l.PutORCIDWorks(ctx, id, want, time.Hour); err != nil {
		t.Fatal(err)
	}
	got, ok, err := l.GetORCIDWorks(ctx, id)
	if err != nil || !ok {
		t.Fatalf("GetORCIDWorks() = %v, %v; want a hit", ok, err)
	}
	if len(got) != 2 || got[0].PutCode != "1" || *got[0].Year != 2024 || *got[0].DOI != doi || got[1].Title != "B" || got[1].Year != nil {
		t.Errorf("GetORCIDWorks() = %+v, want %+v", got, want)
	}
	if ttl := client.PTTL(ctx, orcidWorksPrefix+id).Val(); ttl <= 59*time.Minute || ttl > time.Hour {
		t.Errorf("TTL = %s, want just under 1h", ttl)
	}

	// An empty list is cached as a hit, not a miss.
	if err := l.PutORCIDWorks(ctx, id, nil, time.Hour); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := l.GetORCIDWorks(ctx, id); err != nil || !ok || len(got) != 0 {
		t.Errorf("GetORCIDWorks() after an empty Put = %v, %v, %v; want an empty hit", got, ok, err)
	}
}

func TestORCIDLimiterHasItsOwnCounter(t *testing.T) {
	l, _, user := newTestLookups(t)
	ctx := t.Context()
	orcid := l.ORCIDLimiter()
	for range 2 {
		if err := orcid.Record(ctx, user, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Record(ctx, user, time.Hour); err != nil {
		t.Fatal(err)
	}
	if n, _, err := orcid.Used(ctx, user); err != nil || n != 2 {
		t.Errorf("ORCID Used() = %d, %v; want 2", n, err)
	}
	if n, _, err := l.Used(ctx, user); err != nil || n != 1 {
		t.Errorf("DOI Used() = %d, %v; want 1", n, err)
	}
}

func TestSearchLimiterHasItsOwnCounter(t *testing.T) {
	l, client, user := newTestLookups(t)
	ctx := t.Context()
	search := l.SearchLimiter()
	for range 60 {
		if err := search.Record(ctx, user, time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	n, left, err := search.Used(ctx, user)
	if err != nil || n != 60 {
		t.Errorf("search Used() = %d, %v; want 60", n, err)
	}
	if left <= 0 || left > time.Minute {
		t.Errorf("search Used() left = %v, want within the minute", left)
	}
	if ttl := client.PTTL(ctx, searchPrefix+user).Val(); ttl <= 0 || ttl > time.Minute {
		t.Errorf("search counter TTL = %v, want at most a minute", ttl)
	}
	if n, _, err := l.Used(ctx, user); err != nil || n != 0 {
		t.Errorf("DOI Used() = %d, %v; want 0", n, err)
	}
}
