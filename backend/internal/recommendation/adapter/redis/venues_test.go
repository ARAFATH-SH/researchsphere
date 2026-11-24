//go:build integration

package redis

import (
	"reflect"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
)

// A project's venue lists are kept with their stamp and generation for the TTL given (the
// usecase shortens it to the first listed deadline), without days left, and dropped by
// DeleteVenues; the venues generation starts at 0, grows by one per NextVenueGeneration, has
// no expiry and is apart from the projects generation.
func TestVenueCacheRoundTrip(t *testing.T) {
	rdb := testenv.Redis(t)
	c := New(rdb)
	t.Cleanup(func() { _ = c.Close() })
	ctx := t.Context()

	if g, err := c.VenueGeneration(ctx); err != nil || g != 0 {
		t.Fatalf("VenueGeneration of an empty cache = %d, %v", g, err)
	}
	for range 3 {
		if err := c.NextVenueGeneration(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if g, err := c.VenueGeneration(ctx); err != nil || g != 3 {
		t.Errorf("VenueGeneration = %d, %v; want 3", g, err)
	}
	if g, _ := c.Generation(ctx); g != 0 {
		t.Errorf("projects generation = %d, want 0 (apart)", g)
	}
	if ttl := rdb.TTL(ctx, VenueGenerationKey).Val(); ttl != -1 {
		t.Errorf("generation TTL = %v, want none", ttl)
	}

	short, aoe, days := "ICFM", "AoE", 12
	deadline := time.Date(2027, 3, 2, 11, 59, 59, 0, time.UTC)
	fields := []domain.VenueField{{Key: "water_resources", Label: "Water resources and hydrology"}}
	kept := domain.KeptVenues{
		Stamp:      domain.Stamp{Model: "e5@abc", ContentHash: "h", UpdatedAt: time.Date(2026, 11, 24, 10, 0, 0, 123456000, time.UTC)},
		Generation: 3,
		Venues: []domain.VenueRecommendation{{VenueCard: domain.VenueCard{ID: "v1", Name: "Conference on Floods", ShortName: &short,
			Type: "conference", Fields: fields, TimeZone: &aoe, Deadline: &deadline, OfficialURL: "https://f.example.org", LinkKind: "official"},
			Score: 77, DaysLeft: &days}},
		Journals: []domain.VenueRecommendation{{VenueCard: domain.VenueCard{ID: "j1", Name: "Journal of Hydrology", Type: "journal",
			Fields: fields, OfficialURL: "https://portal.issn.org/x", LinkKind: "issn_portal"}, Score: 55}},
	}
	if _, found, err := c.GetVenues(ctx, "p1"); err != nil || found {
		t.Fatalf("empty: found %v, %v", found, err)
	}
	for _, p := range []string{"p1", "p2"} {
		if err := c.SetVenues(ctx, p, kept, 90*time.Second); err != nil {
			t.Fatal(err)
		}
	}
	got, found, err := c.GetVenues(ctx, "p1")
	want := kept
	want.Venues = []domain.VenueRecommendation{kept.Venues[0]}
	want.Venues[0].DaysLeft = nil // never kept
	if err != nil || !found || !got.Stamp.Equal(kept.Stamp) || got.Generation != 3 ||
		!reflect.DeepEqual(got.Venues, want.Venues) || !reflect.DeepEqual(got.Journals, kept.Journals) {
		t.Errorf("GetVenues = %+v, %v, %v; want %+v", got, found, err, want)
	}
	if ttl := rdb.TTL(ctx, VenuesKey("p1")).Val(); ttl <= 0 || ttl > 90*time.Second {
		t.Errorf("TTL = %v, want at most 90s", ttl)
	}
	if err := c.DeleteVenues(ctx, "p1"); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := c.GetVenues(ctx, "p1"); found {
		t.Error("p1 still kept after DeleteVenues")
	}
	if _, found, _ := c.GetVenues(ctx, "p2"); !found {
		t.Error("p2 dropped by DeleteVenues(p1)")
	}
	if err := c.DeleteVenues(ctx); err != nil {
		t.Errorf("DeleteVenues of nothing: %v", err)
	}
}
