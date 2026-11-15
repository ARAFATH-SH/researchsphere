//go:build integration

package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
	recommendationredis "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/redis"
)

// recEnv is a test API whose embedding worker uses the fake AI service.
type recEnv struct {
	mediaEnv
	ai *fakeAIService
}

func newRecEnv(t *testing.T) recEnv {
	t.Helper()
	ai := newFakeAIService(t)
	return recEnv{mediaEnv: newMediaEnvOnPool(t, testenv.DB(t), "", "", envOptions{ai: ai.client()}), ai: ai}
}

// embedAll runs the embedding worker once over everything queued.
func (e recEnv) embedAll(t *testing.T) {
	t.Helper()
	_, worker := newEmbeddings(e.pool, e.ai.client(), slog.New(slog.DiscardHandler))
	worker.Tick(t.Context())
}

func (e recEnv) addInterest(t *testing.T, userID, name string) {
	t.Helper()
	if code, body := e.call(t, userID, http.MethodPost, "/api/v1/profiles/me/interests", fmt.Sprintf(`{"name":%q}`, name)); code != http.StatusCreated {
		t.Fatalf("add interest: %d %v", code, body)
	}
}

func (e recEnv) addSkill(t *testing.T, userID, name string) {
	t.Helper()
	if code, body := e.call(t, userID, http.MethodPost, "/api/v1/profiles/me/skills", fmt.Sprintf(`{"name":%q}`, name)); code != http.StatusCreated {
		t.Fatalf("add skill: %d %v", code, body)
	}
}

// recs calls the endpoint as userID and returns the body.
func (e recEnv) recs(t *testing.T, userID, query string) map[string]any {
	t.Helper()
	code, body := e.call(t, userID, http.MethodGet, "/api/v1/recommendations/people"+query, "")
	if code != http.StatusOK {
		t.Fatalf("recommendations: %d %v", code, body)
	}
	return body
}

func recIDs(body map[string]any) []string {
	var out []string
	for _, it := range body["items"].([]any) {
		out = append(out, it.(map[string]any)["userId"].(string))
	}
	return out
}

func (e recEnv) kept(t *testing.T, userID string) bool {
	t.Helper()
	n, err := e.rdb.Exists(t.Context(), recommendationredis.Key(userID)).Result()
	if err != nil {
		t.Fatal(err)
	}
	return n == 1
}

// AI-4.2, then AI-4.1 end to end: an empty profile is asked for interests, skills and bio;
// one interest makes it processing until the worker embeds it; then a user with that one
// interest and no publications gets recommendations, with scores and shared tags.
func TestRecommendationsEndToEnd(t *testing.T) {
	e := newRecEnv(t)
	ada := e.newProfileUser(t)
	others := make([]string, 12)
	for i := range others {
		others[i] = e.newProfileUser(t)
		e.addInterest(t, others[i], "Hydrology")
		e.addSkill(t, others[i], fmt.Sprintf("Skill %d", i))
	}

	body := e.recs(t, ada, "")
	if body["state"] != "needs_profile" || fmt.Sprint(body["missing"]) != "[interests skills bio]" || len(body["items"].([]any)) != 0 {
		t.Fatalf("empty profile: %v", body)
	}

	e.addInterest(t, ada, "hydrology")
	body = e.recs(t, ada, "")
	if body["state"] != "processing" || len(body["missing"].([]any)) != 0 || len(body["items"].([]any)) != 0 {
		t.Fatalf("before the embedding: %v", body)
	}

	e.embedAll(t)
	var pubs int
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM user_publications WHERE user_id = $1", ada).Scan(&pubs); err != nil || pubs != 0 {
		t.Fatalf("publications %d, %v; the test needs none", pubs, err)
	}
	body = e.recs(t, ada, "")
	if body["state"] != "ready" {
		t.Fatalf("after the embedding: %v", body)
	}
	items := body["items"].([]any)
	if len(items) != 10 {
		t.Fatalf("%d items, want 10 (the maximum)", len(items))
	}
	prev := 101.0
	for _, raw := range items {
		it := raw.(map[string]any)
		score := it["score"].(float64)
		if score < 0 || score > 100 || score > prev {
			t.Errorf("scores not in 0..100 and decreasing: %v after %v", score, prev)
		}
		prev = score
		if fmt.Sprint(it["shared"]) != "[Hydrology]" || it["connectionStatus"] != "none" || it["fullName"] != "Ada" {
			t.Errorf("item %v", it)
		}
		if it["userId"] == ada {
			t.Error("recommended to themselves")
		}
	}
	if got := len(e.recs(t, ada, "?limit=50")["items"].([]any)); got != 10 {
		t.Errorf("limit 50: %d items, want 10", got)
	}
	if got := len(e.recs(t, ada, "?limit=3")["items"].([]any)); got != 3 {
		t.Errorf("limit 3: %d items, want 3", got)
	}
	if code, body := e.call(t, ada, http.MethodGet, "/api/v1/recommendations/people?limit=0", ""); code != http.StatusBadRequest || body["field"] != "limit" {
		t.Errorf("limit 0: %d %v", code, body)
	}
}

// Every link change between two users clears both users' kept lists; another user's list
// stays. The embedding change is checked by its stamp: a new embedding ranks again.
func TestRecommendationsCacheInvalidation(t *testing.T) {
	e := newRecEnv(t)
	ada, bob, cy := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	for _, u := range []string{ada, bob, cy} {
		e.addInterest(t, u, "Remote sensing")
	}
	e.embedAll(t)
	fill := func() {
		t.Helper()
		for _, u := range []string{ada, bob, cy} {
			e.recs(t, u, "")
			if !e.kept(t, u) {
				t.Fatalf("%s's list was not kept", u[:8])
			}
		}
	}
	wantCleared := func(event string) {
		t.Helper()
		if e.kept(t, ada) || e.kept(t, bob) {
			t.Errorf("%s: a kept list of the pair stayed", event)
		}
		if !e.kept(t, cy) {
			t.Errorf("%s: cleared a third user's list", event)
		}
	}

	fill()
	req := e.connect(t, ada, bob)
	wantCleared("request sent")
	fill()
	if ids := recIDs(e.recs(t, bob, "")); strings.Contains(strings.Join(ids, ","), ada) {
		t.Error("the receiver of a pending request still sees its sender")
	}
	if code, body := e.call(t, ada, http.MethodDelete, "/api/v1/connections/requests/"+req, ""); code != http.StatusNoContent {
		t.Fatalf("cancel: %d %v", code, body)
	}
	wantCleared("request cancelled")

	fill()
	req = e.connect(t, bob, ada)
	wantCleared("request received")
	fill()
	e.reject(t, ada, req)
	wantCleared("request rejected")

	fill()
	req = e.connect(t, ada, bob)
	fill()
	e.accept(t, bob, req)
	wantCleared("request accepted")
	fill()
	if code, body := e.call(t, bob, http.MethodDelete, "/api/v1/connections/"+ada, ""); code != http.StatusNoContent {
		t.Fatalf("remove: %d %v", code, body)
	}
	wantCleared("connection removed")

	fill()
	e.block(t, bob, ada)
	wantCleared("blocked")
	if ids := recIDs(e.recs(t, ada, "")); strings.Contains(strings.Join(ids, ","), bob) {
		t.Error("a user who blocked the caller is recommended")
	}
	fill()
	e.unblock(t, bob, ada)
	wantCleared("unblocked")

	// The caller's embedding changes: the kept list no longer matches and is made again.
	fill()
	before, err := e.rdb.Get(t.Context(), recommendationredis.Key(ada)).Result()
	if err != nil {
		t.Fatal(err)
	}
	e.addInterest(t, ada, "Flood mapping")
	e.embedAll(t)
	e.recs(t, ada, "")
	after, err := e.rdb.Get(t.Context(), recommendationredis.Key(ada)).Result()
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Error("the kept list was used after the caller's embedding changed")
	}
}

// A kept list drops a user suspended since, though nothing cleared it.
func TestRecommendationsDropSuspendedFromKeptList(t *testing.T) {
	e := newRecEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.addInterest(t, ada, "Genomics")
	e.addInterest(t, bob, "Genomics")
	e.embedAll(t)
	if ids := recIDs(e.recs(t, ada, "")); len(ids) != 1 || ids[0] != bob {
		t.Fatalf("items %v, want bob", ids)
	}
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET suspended_at = now(), suspension_reason = 'reason long enough' WHERE id = $1", bob); err != nil {
		t.Fatal(err)
	}
	if !e.kept(t, ada) {
		t.Fatal("the list should still be kept")
	}
	if ids := recIDs(e.recs(t, ada, "")); len(ids) != 0 {
		t.Errorf("items %v after the suspension, want none", ids)
	}
}
