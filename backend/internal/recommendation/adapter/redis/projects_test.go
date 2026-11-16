//go:build integration

package redis

import (
	"reflect"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
)

// A project list is kept with its stamp and generation for the TTL and dropped by
// DeleteProjects; a researcher list per project likewise; the generation starts at 0, grows
// by one per NextGeneration and has no expiry.
func TestProjectCacheRoundTrip(t *testing.T) {
	rdb := testenv.Redis(t)
	c := New(rdb)
	t.Cleanup(func() { _ = c.Close() })
	ctx := t.Context()

	if g, err := c.Generation(ctx); err != nil || g != 0 {
		t.Fatalf("Generation of an empty cache = %d, %v", g, err)
	}
	for range 2 {
		if err := c.NextGeneration(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if g, err := c.Generation(ctx); err != nil || g != 2 {
		t.Errorf("Generation = %d, %v; want 2", g, err)
	}
	if ttl := rdb.TTL(ctx, GenerationKey).Val(); ttl != -1 {
		t.Errorf("generation TTL = %v, want none", ttl)
	}

	head, photo := "Hydrologist", "11111111-1111-4111-8111-111111111111"
	kept := domain.KeptProjects{
		Stamp:      domain.Stamp{Model: "e5@abc", ContentHash: "h", UpdatedAt: time.Date(2026, 11, 16, 10, 0, 0, 123456000, time.UTC)},
		Generation: 2,
		Items: []domain.ProjectRecommendation{
			{Project: domain.ProjectCard{ID: "p1", Title: "Flood maps", Description: "Maps", Skills: []string{"GIS"},
				Owner: domain.ProjectOwner{UserID: "u9", FullName: "Nafisa", Headline: &head, PhotoFileID: &photo}, MemberCount: 3},
				Score: 88, Shared: []string{"GIS"}},
			{Project: domain.ProjectCard{ID: "p2", Title: "Rivers", Skills: []string{}, Owner: domain.ProjectOwner{UserID: "u8", FullName: "Rafi"}, MemberCount: 1},
				Score: 40, Shared: []string{}},
		},
	}
	if _, found, err := c.GetProjects(ctx, "u1"); err != nil || found {
		t.Fatalf("empty: found %v, %v", found, err)
	}
	for _, u := range []string{"u1", "u2"} {
		if err := c.SetProjects(ctx, u, kept, 10*time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	got, found, err := c.GetProjects(ctx, "u1")
	if err != nil || !found || !got.Stamp.Equal(kept.Stamp) || got.Generation != 2 || !reflect.DeepEqual(got.Items, kept.Items) {
		t.Errorf("GetProjects = %+v, %v, %v; want %+v", got, found, err, kept)
	}
	if ttl := rdb.TTL(ctx, ProjectsKey("u1")).Val(); ttl <= 9*time.Minute || ttl > 10*time.Minute {
		t.Errorf("TTL = %v", ttl)
	}
	if err := c.DeleteProjects(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := c.GetProjects(ctx, "u1"); found {
		t.Error("u1's list survived DeleteProjects")
	}
	if _, found, _ := c.GetProjects(ctx, "u2"); !found {
		t.Error("DeleteProjects dropped another user's list")
	}

	people := domain.Kept{Stamp: kept.Stamp, Items: []domain.Recommendation{{Person: domain.Person{UserID: "u3", FullName: "Ada"}, Score: 70, Shared: []string{}}}}
	if err := c.SetResearchers(ctx, "p1", people, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if got, found, err := c.GetResearchers(ctx, "p1"); err != nil || !found || !reflect.DeepEqual(got.Items, people.Items) {
		t.Errorf("GetResearchers = %+v, %v, %v", got, found, err)
	}
	// The researcher list of project p1 and the people list of a user p1 never share a key.
	if _, found, _ := c.Get(ctx, "p1"); found {
		t.Error("the researcher list is readable as a people list")
	}
	if err := c.DeleteResearchers(ctx, "p1"); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := c.GetResearchers(ctx, "p1"); found {
		t.Error("the researcher list survived DeleteResearchers")
	}
	if err := c.DeleteResearchers(ctx); err != nil {
		t.Errorf("DeleteResearchers of nothing: %v", err)
	}
}
