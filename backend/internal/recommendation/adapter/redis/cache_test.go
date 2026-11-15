//go:build integration

package redis

import (
	"reflect"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
)

// A list is kept with its stamp for the TTL and dropped by Delete, for every user named.
func TestCacheRoundTrip(t *testing.T) {
	rdb := testenv.Redis(t)
	c := New(rdb)
	t.Cleanup(func() { _ = c.Close() })
	ctx := t.Context()

	if _, found, err := c.Get(ctx, "u1"); err != nil || found {
		t.Fatalf("empty cache: found %v, %v", found, err)
	}
	head, photo := "Hydrologist", "11111111-1111-4111-8111-111111111111"
	kept := domain.Kept{
		Stamp: domain.Stamp{Model: "e5@abc", ContentHash: "h", UpdatedAt: time.Date(2026, 11, 15, 10, 0, 0, 123456000, time.UTC)},
		Items: []domain.Recommendation{
			{Person: domain.Person{UserID: "u2", FullName: "Nafisa", Headline: &head, PhotoFileID: &photo}, Score: 91, Shared: []string{"GIS"}},
			{Person: domain.Person{UserID: "u3", FullName: "Rafi"}, Score: 40, Shared: []string{}},
		},
	}
	for _, u := range []string{"u1", "u4", "u5"} {
		if err := c.Set(ctx, u, kept, 10*time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	got, found, err := c.Get(ctx, "u1")
	if err != nil || !found {
		t.Fatalf("Get: found %v, %v", found, err)
	}
	if !got.Stamp.Equal(kept.Stamp) || !reflect.DeepEqual(got.Items, kept.Items) {
		t.Errorf("Get = %+v, want %+v", got, kept)
	}
	if ttl := rdb.TTL(ctx, Key("u1")).Val(); ttl <= 9*time.Minute || ttl > 10*time.Minute {
		t.Errorf("TTL = %v, want 10 minutes", ttl)
	}
	if err := c.Delete(ctx, "u1", "u4"); err != nil {
		t.Fatal(err)
	}
	for u, want := range map[string]bool{"u1": false, "u4": false, "u5": true} {
		if _, found, err := c.Get(ctx, u); err != nil || found != want {
			t.Errorf("%s after Delete: found %v (%v), want %v", u, found, err, want)
		}
	}
	if err := c.Delete(ctx); err != nil {
		t.Errorf("Delete of nobody: %v", err)
	}
}
