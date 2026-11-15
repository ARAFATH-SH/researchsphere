//go:build integration

// End-to-end test of researcher search (FR-6.1, FR-6.2) through the real router, the
// real PostgreSQL adapter and the Redis search limit. It reuses the environment of
// media_integration_test.go.
package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/aiclient"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
)

func TestSearchPeopleEndToEnd(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	first, last := testenv.Word(t), testenv.Word(t)
	if _, err := e.pool.Exec(t.Context(), "UPDATE profiles SET full_name = $1, institution = 'BUET' WHERE user_id = $2",
		first+" "+last, bob); err != nil {
		t.Fatal(err)
	}

	search := func(q string) *httptest.ResponseRecorder {
		return e.send(t, ada, http.MethodGet, "/api/v1/search/people?q="+url.QueryEscape(q), nil, "")
	}
	rec := e.send(t, ada, http.MethodGet, "/api/v1/search/people?q="+url.QueryEscape(first), nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("search: %d %s", rec.Code, rec.Body)
	}
	got := decodeBody(t, rec.Body.Bytes())
	items, _ := got["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["userId"] != bob || got["total"] != float64(1) || got["fuzzy"] != false {
		t.Fatalf("search = %v, want only bob", got)
	}

	// A one-letter typo still finds the name and says which (FR-6.2).
	rec = e.send(t, ada, http.MethodGet, "/api/v1/search/people?q="+url.QueryEscape(first+" "+last[:4]+"x"+last[5:]), nil, "")
	if got := decodeBody(t, rec.Body.Bytes()); got["fuzzy"] != true || got["correctedQuery"] != first+" "+last {
		t.Errorf("typo search = %v, want fuzzy with the corrected name", got)
	}

	rec = e.send(t, ada, http.MethodGet, "/api/v1/search/people?q=+a+", nil, "")
	if got := decodeBody(t, rec.Body.Bytes()); rec.Code != http.StatusBadRequest || got["code"] != "invalid_query" {
		t.Errorf("short q: %d %v, want 400 invalid_query", rec.Code, got)
	}

	// 60 searches a minute; the two above count, invalid ones do not.
	for i := 3; i <= 60; i++ {
		if rec := search(first); rec.Code != http.StatusOK {
			t.Fatalf("search %d: %d", i, rec.Code)
		}
	}
	rec = search(first)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" || decodeBody(t, rec.Body.Bytes())["code"] != "rate_limited" {
		t.Errorf("search 61: %d, Retry-After %q; want 429 rate_limited with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}
	// The limit is per user.
	if rec := e.send(t, bob, http.MethodGet, "/api/v1/search/people?q=ab", nil, ""); rec.Code != http.StatusOK {
		t.Errorf("another user's search: %d", rec.Code)
	}
}

// AI-5.1, AI-5.2 and AI-5.4 end to end: the real router, usecase, PostgreSQL adapter, Redis
// vector cache and AI client, against a fake AI service over HTTP.
func TestSearchPeopleHybridEndToEnd(t *testing.T) {
	ai := newFakeAIService(t)
	e := newMediaEnvOnPool(t, testenv.DB(t), "", "", envOptions{ai: ai.client()})
	ada, near := e.newProfileUser(t), e.newProfileUser(t)
	w := strings.ToLower(testenv.Word(t))
	q := "Computers that READ " + w
	normalized := "computers that read " + w
	// near's profile shares no word with q; its embedding is the query's own vector.
	if _, err := e.pool.Exec(t.Context(), `UPDATE profiles SET full_name = $1, headline = 'Radiology AI' WHERE user_id = $2`,
		"Nafisa "+testenv.Word(t), near); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(t.Context(), `INSERT INTO profile_embeddings (user_id, model, content_hash, embedding)
		VALUES ($1, $2, repeat('b', 64), CAST($3::text AS vector))`,
		near, fakeServiceModel, vectorLiteral(aiclient.FakeVector(aiclient.KindQuery, normalized))); err != nil {
		t.Fatal(err)
	}
	search := func(query string) (int, map[string]any) {
		t.Helper()
		rec := e.send(t, ada, http.MethodGet, "/api/v1/search/people?"+query, nil, "")
		return rec.Code, decodeBody(t, rec.Body.Bytes())
	}
	itemIDs := func(body map[string]any) map[string]string {
		out := map[string]string{}
		items, _ := body["items"].([]any)
		for _, it := range items {
			m := it.(map[string]any)
			out[m["userId"].(string)] = m["matchedOn"].(string)
		}
		return out
	}

	code, body := search("q=" + url.QueryEscape(q))
	if code != http.StatusOK || body["mode"] != "hybrid" || itemIDs(body)[near] != "meaning" {
		t.Fatalf("hybrid: %d %v, want near as a meaning match", code, body)
	}
	if got := ai.queried(); len(got) != 1 || got[0] != normalized {
		t.Errorf("embedded %q, want the normalized text once", got)
	}

	// The same text in other case and spacing: the kept vector, no second call.
	code, body = search("mode=auto&q=" + url.QueryEscape("  computers  that read "+strings.ToUpper(w)))
	if code != http.StatusOK || body["mode"] != "hybrid" || len(ai.queried()) != 1 {
		t.Errorf("cached: %d %v, %d embed calls; want hybrid from the cache", code, body["mode"], len(ai.queried()))
	}

	// mode=keyword never calls the AI service and finds no meaning-only match.
	code, body = search("mode=keyword&q=" + url.QueryEscape("other "+w))
	if code != http.StatusOK || body["mode"] != "keyword" || len(ai.queried()) != 1 || itemIDs(body)[near] != "" {
		t.Errorf("keyword: %d %v, %d embed calls", code, body, len(ai.queried()))
	}

	// 300 characters in auto mode, 100 in keyword mode.
	long := strings.Repeat("a", 300)
	if code, body = search("q=" + long); code != http.StatusOK {
		t.Errorf("300 characters: %d %v", code, body)
	}
	if code, body = search("q=" + long + "a"); code != http.StatusBadRequest || body["code"] != "invalid_query" {
		t.Errorf("301 characters: %d %v", code, body)
	}
	if code, body = search("mode=keyword&q=" + long[:101]); code != http.StatusBadRequest || body["code"] != "invalid_query" {
		t.Errorf("101 characters by keyword: %d %v", code, body)
	}
	if code, body = search("mode=meaning&q=ada"); code != http.StatusBadRequest || body["field"] != "mode" {
		t.Errorf("bad mode: %d %v", code, body)
	}

	// AI-5.4: the service errors, then hangs: keyword results, said so, within the timeout.
	ai.down.Store(true)
	code, body = search("q=" + url.QueryEscape("lung "+w))
	if code != http.StatusOK || body["mode"] != "keyword_fallback" {
		t.Errorf("service down: %d %v", code, body)
	}
	ai.down.Store(false)
	ai.slow.Store(true)
	start := time.Now()
	code, body = search("q=" + url.QueryEscape("kidney "+w))
	if took := time.Since(start); code != http.StatusOK || body["mode"] != "keyword_fallback" || took > 3*time.Second {
		t.Errorf("service slow: %d %v after %v; want keyword_fallback after about 1.5 s", code, body["mode"], took)
	}
}

func vectorLiteral(v []float32) string {
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = strconv.FormatFloat(float64(x), 'g', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
