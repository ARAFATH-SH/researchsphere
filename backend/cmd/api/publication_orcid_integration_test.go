//go:build integration

// End-to-end test of importing publications from ORCID (FR-5.3, FR-5.4, FR-5.6) through
// the real router with the real PostgreSQL and Redis adapters, a fake ORCID and a fake
// OpenAlex. No test calls the real ORCID or OpenAlex.
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
)

// fakeORCIDAPI answers GET /v3.0/<orcid>/works from records (raw JSON), with 404 for an
// iD it does not know and 500 for one in failing, and counts the requests per iD.
type fakeORCIDAPI struct {
	mu      sync.Mutex
	records map[string]string
	failing map[string]bool
	calls   map[string]int
}

func newFakeORCID(t *testing.T) (*fakeORCIDAPI, string) {
	t.Helper()
	f := &fakeORCIDAPI{records: map[string]string{}, failing: map[string]bool{}, calls: map[string]int{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv.URL
}

func (f *fakeORCIDAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rest, ok := strings.CutPrefix(r.URL.Path, "/v3.0/")
	id, ok2 := strings.CutSuffix(rest, "/works")
	f.mu.Lock()
	f.calls[id]++
	body, known := f.records[id]
	failing := f.failing[id]
	f.mu.Unlock()
	switch {
	case !ok || !ok2 || r.Header.Get("Accept") != "application/json":
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

func (f *fakeORCIDAPI) callsFor(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[id]
}

// orcidWork is one work group of an ORCID works list, with a DOI when doi is not empty.
func orcidWork(putCode int, title, doi string) string {
	ids := `[]`
	if doi != "" {
		ids = fmt.Sprintf(`[{"external-id-type":"doi","external-id-value":%q,"external-id-relationship":"self"}]`, doi)
	}
	return fmt.Sprintf(`{"work-summary":[{"put-code":%d,"title":{"title":{"value":%q}},
		"external-ids":{"external-id":%s},"type":"journal-article",
		"publication-date":{"year":{"value":"2021"}},"journal-title":{"value":"ORCID Journal"}}]}`, putCode, title, ids)
}

func TestPublicationFromORCIDEndToEnd(t *testing.T) {
	orcidFake, orcidURL := newFakeORCID(t)
	openAlex, openAlexURL := newFakeOpenAlex(t)
	e := newMediaEnvWithSources(t, openAlexURL, orcidURL)
	ada, bob, carl := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	adaORCID, carlORCID := testenv.ORCIDiD(t), testenv.ORCIDiD(t)
	bobs, known, broken := e.newDOI(t), e.newDOI(t), e.newDOI(t)
	openAlex.works[known] = `{"title":"Known to OpenAlex","publication_year":2022,"abstract_inverted_index":{"An":[0],"abstract.":[1]}}`
	openAlex.failing[broken] = true
	orcidFake.records[adaORCID] = `{"group":[` + strings.Join([]string{
		orcidWork(1, "Bob's paper", bobs),
		orcidWork(2, "Known paper", known),
		orcidWork(3, "Broken paper", broken),
		orcidWork(4, "Paper without DOI", ""),
	}, ",") + `]}`
	orcidFake.failing[carlORCID] = true

	send := func(user, method, path, body string) (int, map[string]any) {
		var rec *httptest.ResponseRecorder
		if body == "" {
			rec = e.send(t, user, method, path, nil, "")
		} else {
			rec = e.send(t, user, method, path, strings.NewReader(body), "application/json")
		}
		return rec.Code, decodeBody(t, rec.Body.Bytes())
	}
	list := func(user string) (int, map[string]any) {
		return send(user, http.MethodGet, "/api/v1/profiles/me/publications/orcid-works", "")
	}
	importWorks := func(user string, codes ...string) (int, map[string]any) {
		return send(user, http.MethodPost, "/api/v1/profiles/me/publications/from-orcid",
			`{"putCodes":["`+strings.Join(codes, `","`)+`"]}`)
	}

	// 1. Without an ORCID iD on the profile, both are refused.
	if code, got := list(ada); code != http.StatusConflict || got["code"] != "orcid_not_set" {
		t.Fatalf("list without iD: %d %v, want 409 orcid_not_set", code, got)
	}
	if code, got := importWorks(ada, "1"); code != http.StatusConflict || got["code"] != "orcid_not_set" {
		t.Fatalf("import without iD: %d %v, want 409 orcid_not_set", code, got)
	}

	// 2. Ada saves her iD as a link; Bob cannot take it.
	if code, got := send(ada, http.MethodPut, "/api/v1/profiles/me/orcid", `{"orcidId":"https://orcid.org/`+adaORCID+`"}`); code != http.StatusOK || got["orcidId"] != adaORCID {
		t.Fatalf("set iD: %d %v", code, got)
	}
	if code, got := send(bob, http.MethodPut, "/api/v1/profiles/me/orcid", `{"orcidId":"`+adaORCID+`"}`); code != http.StatusConflict || got["code"] != "orcid_taken" {
		t.Errorf("Bob taking Ada's iD: %d %v, want 409 orcid_taken", code, got)
	}
	if code, got := send(ada, http.MethodGet, "/api/v1/profiles/me", ""); code != http.StatusOK || got["orcidId"] != adaORCID {
		t.Errorf("profile: %d orcidId %v", code, got["orcidId"])
	}

	// 3. Bob already has one of the papers.
	rec := e.send(t, bob, http.MethodPost, "/api/v1/profiles/me/publications",
		strings.NewReader(fmt.Sprintf(`{"title":"Bob's paper","year":2021,"doi":%q}`, bobs)), "application/json")
	if rec.Code != http.StatusCreated {
		t.Fatalf("Bob's paper: %d %s", rec.Code, rec.Body)
	}
	bobsID := decodeBody(t, rec.Body.Bytes())["id"]

	// 4. The list shows the four works, the stored one marked, and is cached.
	code, got := list(ada)
	items, _ := got["items"].([]any)
	if code != http.StatusOK || got["orcidId"] != adaORCID || len(items) != 4 {
		t.Fatalf("list: %d %v", code, got)
	}
	first := items[0].(map[string]any)
	if first["putCode"] != "1" || first["doi"] != bobs || first["existsAlready"] != true || first["publicationId"] != bobsID ||
		first["linkedToMe"] != false || first["year"] != float64(2021) || first["venue"] != "ORCID Journal" {
		t.Errorf("first item = %v", first)
	}
	if last := items[3].(map[string]any); last["doi"] != nil || last["existsAlready"] != false {
		t.Errorf("last item = %v", last)
	}
	if code, _ := list(ada); code != http.StatusOK || orcidFake.callsFor(adaORCID) != 1 {
		t.Errorf("second list: %d, ORCID calls %d; want 200 from the cache, 1 call", code, orcidFake.callsFor(adaORCID))
	}

	// 5. An unknown put-code is refused and nothing is stored.
	if code, got := importWorks(ada, "1", "99"); code != http.StatusBadRequest || got["code"] != "invalid_put_code" {
		t.Errorf("unknown put-code: %d %v, want 400 invalid_put_code", code, got)
	}

	// 6. Import all four: linked to Bob's record, one from OpenAlex, two from ORCID data.
	code, got = importWorks(ada, "1", "2", "3", "4")
	if code != http.StatusOK || got["created"] != float64(3) || got["linked"] != float64(1) || got["skipped"] != float64(0) {
		t.Fatalf("import: %d %v", code, got)
	}
	results := got["items"].([]any)
	if r := results[0].(map[string]any); r["result"] != "linked" || r["publicationId"] != bobsID {
		t.Errorf("item 1 = %v, want linked to Bob's record", r)
	}
	sourceOf := func(i int) (string, *string) {
		id := results[i].(map[string]any)["publicationId"]
		var source string
		var abstract *string
		if err := e.pool.QueryRow(t.Context(), "SELECT source::text, abstract FROM publications WHERE id = $1", id).Scan(&source, &abstract); err != nil {
			t.Fatal(err)
		}
		return source, abstract
	}
	if src, abs := sourceOf(1); src != "doi" || abs == nil || *abs != "An abstract." {
		t.Errorf("known DOI: source %s, abstract %v; want doi with the OpenAlex abstract", src, abs)
	}
	for _, i := range []int{2, 3} {
		if src, _ := sourceOf(i); src != "orcid" {
			t.Errorf("item %d: source %s, want orcid", i+1, src)
		}
	}

	// 7. The profile now lists four; importing the works with a DOI again skips them. (A
	// work without a DOI would be created again: no put-code is stored, docs/decisions.md.)
	if code, got := send(ada, http.MethodGet, "/api/v1/profiles/me/publications", ""); code != http.StatusOK || got["total"] != float64(4) {
		t.Errorf("Ada's list: %d %v, want 4", code, got)
	}
	code, got = importWorks(ada, "1", "2", "3")
	if code != http.StatusOK || got["skipped"] != float64(3) || got["created"] != float64(0) {
		t.Errorf("second import: %d %v, want 3 skipped", code, got)
	}

	// 8. Records made from ORCID data cannot be edited.
	orcidID := results[3].(map[string]any)["publicationId"].(string)
	if code, got := send(ada, http.MethodPatch, "/api/v1/profiles/me/publications/"+orcidID, `{"title":"Mine"}`); code != http.StatusConflict || got["code"] != "publication_shared_readonly" {
		t.Errorf("PATCH an ORCID record: %d %v, want 409 publication_shared_readonly", code, got)
	}

	// 9. ORCID failing: one retry, then the FR-5.6 message, nothing cached.
	if code, _ := send(carl, http.MethodPut, "/api/v1/profiles/me/orcid", `{"orcidId":"`+carlORCID+`"}`); code != http.StatusOK {
		t.Fatalf("Carl's iD: %d", code)
	}
	code, got = list(carl)
	if code != http.StatusServiceUnavailable || got["code"] != "metadata_unavailable" ||
		got["message"] != "We couldn't reach ORCID. You can still add publications by DOI or by hand." {
		t.Errorf("ORCID down: %d %v, want 503 metadata_unavailable", code, got)
	}
	if n := orcidFake.callsFor(carlORCID); n != 2 {
		t.Errorf("ORCID calls for the failing iD = %d, want 2 (one retry)", n)
	}
	if n, _ := e.rdb.Exists(t.Context(), "profile:orcid:works:"+carlORCID).Result(); n != 0 {
		t.Error("a failed list was cached")
	}

	// 10. An iD ORCID does not know.
	unknown := testenv.ORCIDiD(t)
	if code, _ := send(carl, http.MethodPut, "/api/v1/profiles/me/orcid", `{"orcidId":"`+unknown+`"}`); code != http.StatusOK {
		t.Fatalf("Carl's second iD: %d", code)
	}
	if code, got := list(carl); code != http.StatusNotFound || got["code"] != "orcid_not_found" {
		t.Errorf("unknown iD: %d %v, want 404 orcid_not_found", code, got)
	}
}

func TestORCIDListRateLimitEndToEnd(t *testing.T) {
	orcidFake, orcidURL := newFakeORCID(t)
	e := newMediaEnvWithSources(t, "", orcidURL)
	ada := e.newProfileUser(t)
	id := testenv.ORCIDiD(t)
	orcidFake.records[id] = `{"group":[]}`
	if rec := e.send(t, ada, http.MethodPut, "/api/v1/profiles/me/orcid", strings.NewReader(`{"orcidId":"`+id+`"}`), "application/json"); rec.Code != http.StatusOK {
		t.Fatalf("set iD: %d %s", rec.Code, rec.Body)
	}
	for i := range 10 {
		rec := e.send(t, ada, http.MethodGet, "/api/v1/profiles/me/publications/orcid-works", nil, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d: %d %s", i+1, rec.Code, rec.Body)
		}
		e.rdb.Del(t.Context(), "profile:orcid:works:"+id) // each call misses the cache
	}
	rec := e.send(t, ada, http.MethodGet, "/api/v1/profiles/me/publications/orcid-works", nil, "")
	if got := decodeBody(t, rec.Body.Bytes()); rec.Code != http.StatusTooManyRequests || got["code"] != "rate_limited" || rec.Header().Get("Retry-After") == "" {
		t.Errorf("11th call: %d %v (Retry-After %q), want 429 rate_limited", rec.Code, got, rec.Header().Get("Retry-After"))
	}
	if n := orcidFake.callsFor(id); n != 10 {
		t.Errorf("ORCID calls = %d, want 10", n)
	}
}
