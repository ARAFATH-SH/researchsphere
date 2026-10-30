//go:build integration

// End-to-end test of adding a publication from a DOI (FR-5.2, FR-5.4, FR-5.6) through the
// real router with the real PostgreSQL and Redis adapters and a fake OpenAlex. No test
// calls the real OpenAlex.
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeOpenAlex answers GET /works/doi:<doi> from works (raw JSON), with 404 for a DOI it
// does not know and 500 for one in failing, and counts the requests per DOI.
type fakeOpenAlex struct {
	mu      sync.Mutex
	works   map[string]string
	failing map[string]bool
	calls   map[string]int
}

func newFakeOpenAlex(t *testing.T) (*fakeOpenAlex, string) {
	t.Helper()
	f := &fakeOpenAlex{works: map[string]string{}, failing: map[string]bool{}, calls: map[string]int{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv.URL
}

func (f *fakeOpenAlex) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	doi, ok := strings.CutPrefix(r.URL.Path, "/works/doi:")
	f.mu.Lock()
	f.calls[doi]++
	body, known := f.works[doi]
	failing := f.failing[doi]
	f.mu.Unlock()
	switch {
	case !ok || r.URL.Query().Get("mailto") != "it@example.org":
		w.WriteHeader(http.StatusBadRequest)
	case failing:
		w.WriteHeader(http.StatusInternalServerError)
	case !known:
		w.WriteHeader(http.StatusNotFound)
	default:
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func (f *fakeOpenAlex) callsFor(doi string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[doi]
}

// cleanLookups removes the cached works and the users' lookup counters when the test ends.
func (e mediaEnv) cleanLookups(t *testing.T, dois []string, users ...string) {
	t.Helper()
	t.Cleanup(func() {
		var keys []string
		for _, d := range dois {
			keys = append(keys, "profile:openalex:work:"+d)
		}
		for _, u := range users {
			keys = append(keys, "profile:doi-lookup:"+u)
		}
		e.rdb.Del(t.Context(), keys...)
	})
}

func TestPublicationFromDOIEndToEnd(t *testing.T) {
	fake, base := newFakeOpenAlex(t)
	e := newMediaEnvWithOpenAlex(t, base)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	doi, missing, broken := e.newDOI(t), e.newDOI(t), e.newDOI(t)
	e.cleanLookups(t, []string{doi, missing, broken}, ada, bob)
	fake.works[doi] = `{"title":"Notes on the\nEngine","publication_year":2024,
		"primary_location":{"landing_page_url":"https://example.org/engine","source":{"display_name":"Journal of Engines"}},
		"abstract_inverted_index":{"We":[0],"describe":[1],"it.":[2]}}`
	fake.failing[broken] = true
	lookup := func(user, d string) (int, map[string]any) {
		rec := e.send(t, user, http.MethodGet, "/api/v1/publications/lookup?doi="+url.QueryEscape(d), nil, "")
		return rec.Code, decodeBody(t, rec.Body.Bytes())
	}
	fromDOI := func(user, d string) (int, map[string]any) {
		rec := e.send(t, user, http.MethodPost, "/api/v1/profiles/me/publications/from-doi",
			strings.NewReader(fmt.Sprintf(`{"doi":%q}`, d)), "application/json")
		return rec.Code, decodeBody(t, rec.Body.Bytes())
	}

	// 1. Lookup gives a preview and stores nothing.
	code, got := lookup(ada, "https://doi.org/"+strings.ToUpper(doi))
	if code != http.StatusOK || got["doi"] != doi || got["title"] != "Notes on the Engine" || got["year"] != float64(2024) ||
		got["venue"] != "Journal of Engines" || got["abstract"] != "We describe it." || got["url"] != "https://example.org/engine" ||
		got["existsAlready"] != false || got["linkedToMe"] != false {
		t.Fatalf("lookup: %d %v", code, got)
	}
	var stored int
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM publications WHERE doi = $1", doi).Scan(&stored); err != nil || stored != 0 {
		t.Fatalf("records after lookup: %d, %v; want 0", stored, err)
	}

	// 2. From DOI stores it from the cache: no second call to OpenAlex.
	code, created := fromDOI(ada, doi)
	if code != http.StatusCreated || created["source"] != "doi" || created["linked"] != false || created["title"] != "Notes on the Engine" {
		t.Fatalf("from-doi: %d %v", code, created)
	}
	if n := fake.callsFor(doi); n != 1 {
		t.Errorf("OpenAlex calls = %d, want 1", n)
	}

	// 3. A second lookup returns the stored record, still without calling OpenAlex.
	code, got = lookup(ada, doi)
	if code != http.StatusOK || got["existsAlready"] != true || got["linkedToMe"] != true || got["publicationId"] != created["id"] {
		t.Errorf("lookup after adding: %d %v", code, got)
	}
	if code, got = lookup(bob, doi); got["existsAlready"] != true || got["linkedToMe"] != false {
		t.Errorf("Bob's lookup: %d %v", code, got)
	}
	if n := fake.callsFor(doi); n != 1 {
		t.Errorf("OpenAlex calls = %d, want still 1", n)
	}

	// 4. Bob adds it too and is linked to Ada's record (FR-5.4).
	code, got = fromDOI(bob, "doi:"+doi)
	if code != http.StatusCreated || got["linked"] != true || got["id"] != created["id"] {
		t.Errorf("Bob's from-doi: %d %v, want Ada's record linked", code, got)
	}

	// 5. Adding it again is refused.
	if code, got = fromDOI(bob, doi); code != http.StatusConflict || got["code"] != "publication_already_added" {
		t.Errorf("second from-doi: %d %v, want 409 publication_already_added", code, got)
	}

	// 6. Fetched metadata cannot be edited.
	rec := e.send(t, ada, http.MethodPatch, "/api/v1/profiles/me/publications/"+created["id"].(string),
		strings.NewReader(`{"title":"Mine"}`), "application/json")
	if got := decodeBody(t, rec.Body.Bytes()); rec.Code != http.StatusConflict || got["code"] != "publication_shared_readonly" {
		t.Errorf("PATCH: %d %v, want 409 publication_shared_readonly", rec.Code, got)
	}

	// 7. An unknown DOI and an invalid one.
	if code, got = lookup(ada, missing); code != http.StatusNotFound || got["code"] != "doi_not_found" {
		t.Errorf("unknown DOI: %d %v, want 404 doi_not_found", code, got)
	}
	if code, got = lookup(ada, "11.1/x"); code != http.StatusBadRequest || got["code"] != "invalid_doi" {
		t.Errorf("invalid DOI: %d %v, want 400 invalid_doi", code, got)
	}

	// 8. OpenAlex failing: one retry, then the FR-5.6 message, and nothing cached or stored.
	for _, call := range []func(string, string) (int, map[string]any){lookup, fromDOI} {
		code, got = call(ada, broken)
		if code != http.StatusServiceUnavailable || got["code"] != "metadata_unavailable" ||
			got["message"] != "We couldn't reach the publication database. You can add this publication by hand." {
			t.Errorf("OpenAlex down: %d %v, want 503 metadata_unavailable", code, got)
		}
	}
	if n := fake.callsFor(broken); n != 4 {
		t.Errorf("OpenAlex calls for the failing DOI = %d, want 4 (two requests, one retry each)", n)
	}
	if n, _ := e.rdb.Exists(t.Context(), "profile:openalex:work:"+broken).Result(); n != 0 {
		t.Error("a failed lookup was cached")
	}
}

func TestPublicationLookupRateLimitEndToEnd(t *testing.T) {
	fake, base := newFakeOpenAlex(t)
	e := newMediaEnvWithOpenAlex(t, base)
	ada := e.newProfileUser(t)
	doi := e.newDOI(t)
	e.cleanLookups(t, []string{doi}, ada)
	fake.works[doi] = `{"title":"Cached paper"}`

	// The first lookup reaches OpenAlex and is cached; the counter is then set to the limit.
	if rec := e.send(t, ada, http.MethodGet, "/api/v1/publications/lookup?doi="+doi, nil, ""); rec.Code != http.StatusOK {
		t.Fatalf("lookup: %d %s", rec.Code, rec.Body)
	}
	if err := e.rdb.Set(t.Context(), "profile:doi-lookup:"+ada, 30, 10*time.Minute).Err(); err != nil {
		t.Fatal(err)
	}

	// A cached DOI does not count; a new one is refused before OpenAlex is called.
	if rec := e.send(t, ada, http.MethodGet, "/api/v1/publications/lookup?doi="+doi, nil, ""); rec.Code != http.StatusOK {
		t.Errorf("cached lookup at the limit: %d %s", rec.Code, rec.Body)
	}
	other := e.newDOI(t)
	rec := e.send(t, ada, http.MethodGet, "/api/v1/publications/lookup?doi="+other, nil, "")
	if got := decodeBody(t, rec.Body.Bytes()); rec.Code != http.StatusTooManyRequests || got["code"] != "rate_limited" ||
		rec.Header().Get("Retry-After") == "" {
		t.Errorf("lookup over the limit: %d %v Retry-After %q, want 429 rate_limited", rec.Code, got, rec.Header().Get("Retry-After"))
	}
	if n := fake.callsFor(other); n != 0 {
		t.Errorf("OpenAlex calls over the limit = %d, want 0", n)
	}
}
