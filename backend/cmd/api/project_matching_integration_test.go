//go:build integration

package main

import (
	"fmt"
	"net/http"
	"slices"
	"testing"

	recommendationredis "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/redis"
)

// newProject creates a project as owner through the API and returns its ID.
func (e recEnv) newProject(t *testing.T, owner, title, description string, skills ...string) string {
	t.Helper()
	sk := "[]"
	if len(skills) > 0 {
		sk = fmt.Sprintf("[%q", skills[0])
		for _, s := range skills[1:] {
			sk += fmt.Sprintf(",%q", s)
		}
		sk += "]"
	}
	code, p := e.json(t, owner, http.MethodPost, "/api/v1/projects",
		fmt.Sprintf(`{"title":%q,"description":%q,"requiredSkills":%s}`, title, description, sk))
	if code != http.StatusCreated {
		t.Fatalf("create project: %d %v", code, p)
	}
	return p["id"].(string)
}

func (e recEnv) projectRecs(t *testing.T, userID string) map[string]any {
	t.Helper()
	code, body := e.json(t, userID, http.MethodGet, "/api/v1/recommendations/projects", "")
	if code != http.StatusOK {
		t.Fatalf("project recommendations: %d %v", code, body)
	}
	return body
}

func recProjectIDs(body map[string]any) []string {
	var out []string
	for _, it := range body["items"].([]any) {
		out = append(out, it.(map[string]any)["project"].(map[string]any)["id"].(string))
	}
	return out
}

func (e recEnv) setStatus(t *testing.T, owner, projectID, status string) {
	t.Helper()
	if code, body := e.json(t, owner, http.MethodPatch, "/api/v1/projects/"+projectID, fmt.Sprintf(`{"status":%q}`, status)); code != http.StatusOK {
		t.Fatalf("set status %s: %d %v", status, code, body)
	}
}

func (e recEnv) generation(t *testing.T) int64 {
	t.Helper()
	n, err := e.rdb.Get(t.Context(), recommendationredis.GenerationKey).Int64()
	if err != nil && err.Error() != "redis: nil" {
		t.Fatal(err)
	}
	return n
}

func (e recEnv) exists(t *testing.T, key string) bool {
	t.Helper()
	n, err := e.rdb.Exists(t.Context(), key).Result()
	if err != nil {
		t.Fatal(err)
	}
	return n == 1
}

// AI-3.1 and AI-2.3 end to end: the states, the items, the shared skills, the owner rule,
// and that neither list holds the caller or a member.
func TestProjectMatchingEndToEnd(t *testing.T) {
	e := newRecEnv(t)
	ada, bob, cy := e.projectUser(t), e.projectUser(t), e.projectUser(t)

	if body := e.projectRecs(t, ada); body["state"] != "needs_profile" || fmt.Sprint(body["missing"]) != "[interests skills bio]" {
		t.Fatalf("empty profile: %v", body)
	}
	e.addSkill(t, ada, "Python")
	e.addInterest(t, cy, "Flood risk")
	if body := e.projectRecs(t, ada); body["state"] != "processing" || len(body["items"].([]any)) != 0 {
		t.Fatalf("before the embedding: %v", body)
	}

	p := e.newProject(t, bob, "Flood maps", "Mapping floods from satellite images", "python", "GIS")
	e.newProject(t, ada, "Ada's own work", "Something else entirely") // never recommended to ada
	researchersPath := "/api/v1/projects/" + p + "/recommended-researchers"
	if code, body := e.json(t, bob, http.MethodGet, researchersPath, ""); code != http.StatusOK || body["state"] != "processing" {
		t.Fatalf("researchers before the project's embedding: %d %v", code, body)
	}
	e.embedAll(t)

	body := e.projectRecs(t, ada)
	if body["state"] != "ready" || fmt.Sprint(recProjectIDs(body)) != fmt.Sprint([]string{p}) {
		t.Fatalf("ready: %v, want only bob's project (not ada's own)", body)
	}
	it := body["items"].([]any)[0].(map[string]any)
	proj := it["project"].(map[string]any)
	if fmt.Sprint(it["shared"]) != "[Python]" || proj["memberCount"] != float64(1) || proj["title"] != "Flood maps" ||
		proj["owner"].(map[string]any)["userId"] != bob || fmt.Sprint(proj["requiredSkills"]) != "[GIS Python]" {
		t.Errorf("item = %v", it)
	}
	if s := it["score"].(float64); s < 0 || s > 100 {
		t.Errorf("score %v", s)
	}

	code, body := e.json(t, bob, http.MethodGet, researchersPath, "")
	if code != http.StatusOK || body["state"] != "ready" || len(body["missing"].([]any)) != 0 {
		t.Fatalf("researchers: %d %v", code, body)
	}
	ids := recIDs(body)
	if slices.Contains(ids, bob) || !slices.Contains(ids, ada) || !slices.Contains(ids, cy) {
		t.Errorf("researchers %v: want ada and cy, never the owner", ids)
	}
	for _, raw := range body["items"].([]any) {
		r := raw.(map[string]any)
		want := "[]"
		if r["userId"] == ada {
			want = "[Python]"
		}
		if fmt.Sprint(r["shared"]) != want || r["connectionStatus"] != "none" {
			t.Errorf("researcher %v, want shared %s", r, want)
		}
	}

	// Owner only: a visible project is 403 to others, an archived one 404.
	if code, _ := e.json(t, ada, http.MethodGet, researchersPath, ""); code != http.StatusForbidden {
		t.Errorf("another user: %d, want 403", code)
	}
	e.setStatus(t, bob, p, "archived")
	if code, _ := e.json(t, ada, http.MethodGet, researchersPath, ""); code != http.StatusNotFound {
		t.Errorf("another user, archived: %d, want 404", code)
	}
	if code, _ := e.json(t, bob, http.MethodGet, researchersPath, ""); code != http.StatusOK {
		t.Errorf("owner, archived: %d, want 200", code)
	}
	if code, _ := e.json(t, ada, http.MethodGet, "/api/v1/projects/not-a-uuid/recommended-researchers", ""); code != http.StatusNotFound {
		t.Errorf("malformed ID: %d, want 404", code)
	}
	if ids := recProjectIDs(e.projectRecs(t, ada)); len(ids) != 0 {
		t.Errorf("an archived project is still recommended: %v", ids)
	}
}

// Cache invalidation: a status change, an embedding change and a deletion renew every user's
// project list (a new generation); a block clears both users' lists and the researcher lists
// of what they own; a profile embedding change ranks again by its stamp.
func TestProjectMatchingCacheInvalidation(t *testing.T) {
	e := newRecEnv(t)
	ada, bob, cy := e.projectUser(t), e.projectUser(t), e.projectUser(t)
	for _, u := range []string{ada, cy} {
		e.addSkill(t, u, "Python")
	}
	p := e.newProject(t, bob, "Flood maps", "Mapping floods", "Python")
	tick := func() {
		t.Helper()
		e.embedWorker.Tick(t.Context()) // the API's worker, watched by the recommendations
	}
	tick()
	researchers := "/api/v1/projects/" + p + "/recommended-researchers"
	fill := func() {
		t.Helper()
		e.projectRecs(t, ada)
		e.projectRecs(t, cy)
		if code, _ := e.json(t, bob, http.MethodGet, researchers, ""); code != http.StatusOK {
			t.Fatal("researcher list")
		}
		for _, k := range []string{recommendationredis.ProjectsKey(ada), recommendationredis.ProjectsKey(cy), recommendationredis.ResearchersKey(p)} {
			if !e.exists(t, k) {
				t.Fatalf("%s was not kept", k)
			}
		}
	}

	fill()
	gen := e.generation(t)
	e.setStatus(t, bob, p, "closed")
	if e.generation(t) != gen+1 || e.exists(t, recommendationredis.ResearchersKey(p)) {
		t.Errorf("closing: generation %d (was %d), researcher list kept %v", e.generation(t), gen, e.exists(t, recommendationredis.ResearchersKey(p)))
	}
	if ids := recProjectIDs(e.projectRecs(t, ada)); len(ids) != 0 {
		t.Errorf("closed project recommended: %v", ids)
	}
	// Reopening must show it again at once: a recheck alone could only drop it.
	e.setStatus(t, bob, p, "open")
	if ids := recProjectIDs(e.projectRecs(t, ada)); !slices.Contains(ids, p) {
		t.Errorf("reopened project missing: %v", ids)
	}

	fill()
	gen = e.generation(t)
	if code, body := e.json(t, bob, http.MethodPatch, "/api/v1/projects/"+p, `{"description":"Mapping floods and droughts"}`); code != http.StatusOK {
		t.Fatalf("edit: %d %v", code, body)
	}
	if e.generation(t) != gen {
		t.Error("a text edit renewed the lists before its embedding changed")
	}
	tick()
	if e.generation(t) != gen+1 || e.exists(t, recommendationredis.ResearchersKey(p)) {
		t.Errorf("new project embedding: generation %d (was %d)", e.generation(t), gen)
	}

	fill()
	e.block(t, ada, bob)
	if e.exists(t, recommendationredis.ProjectsKey(ada)) || e.exists(t, recommendationredis.ResearchersKey(p)) {
		t.Error("a block kept ada's project list or bob's researcher list")
	}
	if !e.exists(t, recommendationredis.ProjectsKey(cy)) {
		t.Error("a block cleared a third user's list")
	}
	if ids := recProjectIDs(e.projectRecs(t, ada)); len(ids) != 0 {
		t.Errorf("project of a blocked owner recommended: %v", ids)
	}
	if code, body := e.json(t, bob, http.MethodGet, researchers, ""); code != http.StatusOK || slices.Contains(recIDs(body), ada) {
		t.Errorf("blocked user recommended to the owner: %v", body)
	}
	e.unblock(t, ada, bob)

	fill()
	gen = e.generation(t)
	if code, _ := e.json(t, bob, http.MethodDelete, "/api/v1/projects/"+p+"?confirm=Flood%20maps", ""); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if e.generation(t) <= gen {
		t.Error("a deletion did not renew the lists")
	}
	if ids := recProjectIDs(e.projectRecs(t, cy)); len(ids) != 0 {
		t.Errorf("deleted project recommended: %v", ids)
	}
}

// AI-5.1 and AI-5.4 for projects end to end: auto searches by meaning too (a project with
// none of the words is found, matched on meaning), only open projects; keyword mode does
// not; a down AI service falls back and says so.
func TestProjectSearchModesEndToEnd(t *testing.T) {
	e := newRecEnv(t)
	ada, bob := e.projectUser(t), e.projectUser(t)
	open := e.newProject(t, bob, "Inundation forecasting", "Predicting where water rises")
	closed := e.newProject(t, bob, "Dry seasons", "Drought study")
	e.setStatus(t, bob, closed, "closed")
	e.embedAll(t)

	search := func(query string) map[string]any {
		t.Helper()
		code, body := e.json(t, ada, http.MethodGet, "/api/v1/search/projects?"+query, "")
		if code != http.StatusOK {
			t.Fatalf("search %s: %d %v", query, code, body)
		}
		return body
	}
	body := search("q=flood%20risk")
	if body["mode"] != "hybrid" {
		t.Fatalf("mode %v, want hybrid", body["mode"])
	}
	items := body["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != open || items[0].(map[string]any)["matchedOn"] != "meaning" {
		t.Errorf("hybrid items %v, want the open project matched on meaning", items)
	}
	if !slices.Contains(e.ai.queried(), "flood risk") {
		t.Errorf("queries sent %v", e.ai.queried())
	}
	if body := search("q=flood%20risk&mode=keyword"); body["mode"] != "keyword" || len(body["items"].([]any)) != 0 {
		t.Errorf("keyword mode: %v", body)
	}
	e.ai.down.Store(true)
	if body := search("q=a%20different%20text"); body["mode"] != "keyword_fallback" {
		t.Errorf("AI down: mode %v", body["mode"])
	}
	if code, body := e.json(t, ada, http.MethodGet, "/api/v1/search/projects?q=flood&mode=fuzzy", ""); code != http.StatusBadRequest || body["field"] != "mode" {
		t.Errorf("bad mode: %d %v", code, body)
	}
}
