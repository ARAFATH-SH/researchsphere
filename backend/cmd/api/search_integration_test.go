//go:build integration

// End-to-end test of researcher search (FR-6.1, FR-6.2) through the real router, the
// real PostgreSQL adapter and the Redis search limit. It reuses the environment of
// media_integration_test.go.
package main

import (
	"context"
	"crypto/rand"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// searchWord returns a random capitalized word that no stored name contains.
func searchWord(t *testing.T) string {
	t.Helper()
	b := []byte{'Q', 0, 0, 0, 0, 0, 0, 0, 0}
	for i := 1; i < len(b); i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(26))
		if err != nil {
			t.Fatal(err)
		}
		b[i] = "abcdefghijklmnopqrstuvwxyz"[n.Int64()]
	}
	return string(b)
}

func TestSearchPeopleEndToEnd(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	t.Cleanup(func() { e.rdb.Del(context.Background(), "profile:people-search:"+ada, "profile:people-search:"+bob) })
	first, last := searchWord(t), searchWord(t)
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
