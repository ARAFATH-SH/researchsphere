//go:build integration

package main

import (
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	recommendationredis "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/redis"
)

func (e recEnv) venueGeneration(t *testing.T) int64 {
	t.Helper()
	n, err := e.rdb.Get(t.Context(), recommendationredis.VenueGenerationKey).Int64()
	if err != nil && err.Error() != "redis: nil" {
		t.Fatal(err)
	}
	return n
}

// newAdminVenue adds a venue as an administrator and returns its ID.
func (e recEnv) newAdminVenue(t *testing.T, admin, body string) string {
	t.Helper()
	code, v := e.asAdmin(t, admin, http.MethodPost, "/api/v1/admin/venues", body)
	if code != http.StatusCreated {
		t.Fatalf("create venue: %d %v", code, v)
	}
	return v["id"].(string)
}

func conferenceJSON(name, deadline string) string {
	return fmt.Sprintf(`{"name":%q,"type":"conference","fields":["water_resources"],"attendance":"online","timeZone":"AoE",
		"submissionDeadline":%q,"officialUrl":"https://conf.example.org","scope":"Floods, rivers and hydrology."}`, name, deadline)
}

func venueListIDs(body map[string]any, key string) []string {
	var out []string
	for _, it := range body[key].([]any) {
		out = append(out, it.(map[string]any)["id"].(string))
	}
	return out
}

// AI-6.1 to AI-6.4 end to end: the states, members only (404 to anyone else, even when the
// project page is visible), the items and the notice, journals apart, and every way a kept
// list is made again: a venue added, edited, removed or imported, and a listed deadline
// passing with no write at all.
func TestVenueRecommendationsEndToEnd(t *testing.T) {
	e := newRecEnv(t)
	owner, viewer, editor, stranger := e.projectUser(t), e.projectUser(t), e.projectUser(t), e.projectUser(t)
	admin := e.newProfileUser(t)
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET role = 'admin' WHERE id = $1", admin); err != nil {
		t.Fatal(err)
	}
	p := e.newProject(t, owner, "Flood mapping from satellites", "We are looking for people who know GIS.", "GIS")
	for user, role := range map[string]string{viewer: "viewer", editor: "editor"} {
		if _, err := e.pool.Exec(t.Context(), "INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, $3::project_role)", p, user, role); err != nil {
			t.Fatal(err)
		}
	}
	path := "/api/v1/projects/" + p + "/recommended-venues"
	get := func(user string) map[string]any {
		t.Helper()
		code, body := e.json(t, user, http.MethodGet, path, "")
		if code != http.StatusOK {
			t.Fatalf("venues as %s: %d %v", user, code, body)
		}
		if body["notice"] != "Matched on topic only. This says nothing about a venue's quality or ranking." {
			t.Errorf("notice = %v", body["notice"])
		}
		return body
	}

	// AI-6.3: a title and description are not enough.
	if body := get(owner); body["state"] != "needs_abstract" || len(body["venues"].([]any)) != 0 || len(body["journals"].([]any)) != 0 {
		t.Fatalf("no abstract: %v", body)
	}
	// Members only: viewer and editor see it; a stranger who can see the project gets 404.
	for _, u := range []string{viewer, editor} {
		get(u)
	}
	if code, _ := e.json(t, stranger, http.MethodGet, "/api/v1/projects/"+p, ""); code != http.StatusOK {
		t.Fatalf("the stranger cannot see the open project: %d", code)
	}
	for _, pth := range []string{path, "/api/v1/projects/00000000-0000-4000-8000-000000000000/recommended-venues"} {
		if code, body := e.json(t, stranger, http.MethodGet, pth, ""); code != http.StatusNotFound || body["code"] != "not_found" {
			t.Errorf("stranger %s: %d %v, want 404", pth, code, body)
		}
	}

	if code, body := e.json(t, owner, http.MethodPatch, "/api/v1/projects/"+p, `{"abstract":"We map river floods in Bangladesh from radar images."}`); code != http.StatusOK {
		t.Fatalf("set abstract: %d %v", code, body)
	}
	if body := get(owner); body["state"] != "processing" {
		t.Fatalf("before the embedding: %v", body)
	}

	far := time.Now().UTC().AddDate(0, 2, 0).Format("2006-01-02")
	a := e.newAdminVenue(t, admin, conferenceJSON("Conference on Floods", far))
	removed := e.newAdminVenue(t, admin, conferenceJSON("Conference on Rivers", far))
	journal := e.newAdminVenue(t, admin, `{"name":"Journal of Floods","type":"journal","rolling":true,"fields":["water_resources"],
		"officialUrl":"https://jof.example.org","scope":"Floods."}`)
	e.embedAll(t)
	var n int
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM project_abstract_embeddings WHERE project_id = $1", p).Scan(&n); err != nil || n != 1 {
		t.Fatalf("title-and-abstract embeddings = %d, %v", n, err)
	}
	// The fake AI's vectors are arbitrary: make every venue match the project exactly, so the
	// journal scores above the floor.
	if _, err := e.pool.Exec(t.Context(), `UPDATE venue_embeddings SET embedding =
		(SELECT embedding FROM project_abstract_embeddings WHERE project_id = $1)`, p); err != nil {
		t.Fatal(err)
	}

	body := get(viewer)
	if body["state"] != "ready" || !slices.Equal(venueListIDs(body, "journals"), []string{journal}) {
		t.Fatalf("ready: %v", body)
	}
	if v := venueListIDs(body, "venues"); len(v) != 2 || !slices.Contains(v, a) || !slices.Contains(v, removed) {
		t.Fatalf("venues = %v, want both conferences and not the journal", v)
	}
	item := body["venues"].([]any)[0].(map[string]any)
	sd := item["submissionDeadline"].(map[string]any)
	if item["timeZone"] != "AoE" || sd["local"] != far+"T23:59:59" || item["score"].(float64) < 0 || item["score"].(float64) > 100 ||
		item["daysLeft"].(float64) < 55 || item["type"] != "conference" || item["linkKind"] != "official" {
		t.Errorf("venue item = %v", item)
	}
	if j := body["journals"].([]any)[0].(map[string]any); j["submissionDeadline"] != nil || j["daysLeft"] != nil || j["timeZone"] != nil {
		t.Errorf("journal item = %v", j)
	}
	if !e.exists(t, recommendationredis.VenuesKey(p)) {
		t.Fatal("list not kept")
	}

	// Removed: the generation rises and the venue is gone at once.
	gen := e.venueGeneration(t)
	if code, _ := e.asAdmin(t, admin, http.MethodPost, "/api/v1/admin/venues/"+removed+"/remove", `{"reason":"Duplicate entry of rivers"}`); code != http.StatusNoContent {
		t.Fatalf("remove: %d", code)
	}
	if e.venueGeneration(t) <= gen || slices.Contains(venueListIDs(get(owner), "venues"), removed) {
		t.Error("a removed venue is still listed")
	}

	// Edited: a renamed venue shows its new name at once; an edit that changes nothing
	// renews nothing.
	if code, _ := e.asAdmin(t, admin, http.MethodPatch, "/api/v1/admin/venues/"+a, `{"name":"Conference on Floods and Droughts"}`); code != http.StatusOK {
		t.Fatal("rename")
	}
	if v := get(owner)["venues"].([]any)[0].(map[string]any); v["name"] != "Conference on Floods and Droughts" {
		t.Errorf("after the rename: %v", v)
	}
	gen = e.venueGeneration(t)
	if code, _ := e.asAdmin(t, admin, http.MethodPatch, "/api/v1/admin/venues/"+a, `{"name":"Conference on Floods and Droughts"}`); code != http.StatusOK {
		t.Fatal("no-op edit")
	}
	if e.venueGeneration(t) != gen {
		t.Error("an edit that changed nothing raised the generation")
	}

	// Imported: a real import raises the generation, a dry run does not.
	data := fmt.Sprintf(`[{"sourceKey":"cfp-9","name":"Workshop on Rivers","type":"workshop","attendance":"online","fields":["water_resources"],
		"timeZone":"UTC","submissionDeadline":%q,"officialUrl":"https://wor.example.org","scope":"Rivers."}]`, far)
	if code, _ := e.importAs(t, admin, "admin", "?dryRun=true", "venues.json", data); code != http.StatusOK || e.venueGeneration(t) != gen {
		t.Errorf("dry-run import: %d, generation %d -> %d", code, gen, e.venueGeneration(t))
	}
	if code, _ := e.importAs(t, admin, "admin", "", "venues.json", data); code != http.StatusOK || e.venueGeneration(t) <= gen {
		t.Errorf("import: %d, generation %d -> %d", code, gen, e.venueGeneration(t))
	}

	// AI-6.1 at the boundary: a listed deadline a few seconds ahead. The kept list lives no
	// longer than that, and once it passes the venue is gone with no write in between.
	soon := time.Now().UTC().Add(4 * time.Second).Truncate(time.Second)
	if code, v := e.asAdmin(t, admin, http.MethodPatch, "/api/v1/admin/venues/"+a,
		fmt.Sprintf(`{"timeZone":"UTC","submissionDeadline":%q}`, soon.Format("2006-01-02T15:04:05"))); code != http.StatusOK {
		t.Fatalf("set a near deadline: %d %v", code, v)
	}
	body = get(owner)
	if !slices.Contains(venueListIDs(body, "venues"), a) || body["venues"].([]any)[0].(map[string]any)["daysLeft"] != float64(0) {
		t.Fatalf("before the deadline: %v", body["venues"])
	}
	if ttl := e.rdb.TTL(t.Context(), recommendationredis.VenuesKey(p)).Val(); ttl <= 0 || ttl > 4*time.Second {
		t.Errorf("TTL = %v, want at most the 4 s to the deadline", ttl)
	}
	gen = e.venueGeneration(t)
	time.Sleep(time.Until(soon) + 100*time.Millisecond)
	if v := venueListIDs(get(owner), "venues"); slices.Contains(v, a) {
		t.Errorf("after the deadline: %v still lists it", v)
	}
	if e.venueGeneration(t) != gen {
		t.Error("something wrote between the two reads; the test would not show the deadline rule")
	}

	// The abstract cleared: needs_abstract again, and the worker deletes the second vector.
	if code, _ := e.json(t, owner, http.MethodPatch, "/api/v1/projects/"+p, `{"abstract":null}`); code != http.StatusOK {
		t.Fatal("clear abstract")
	}
	if body := get(editor); body["state"] != "needs_abstract" {
		t.Errorf("abstract cleared: %v", body)
	}
	e.embedAll(t)
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM project_abstract_embeddings WHERE project_id = $1", p).Scan(&n); err != nil || n != 0 {
		t.Errorf("title-and-abstract embeddings after clearing = %d, %v", n, err)
	}
}
