//go:build integration

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/config"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
	recommendationredis "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/redis"
)

// The recorded fixtures of the real sources (internal/event/adapter/feeds/testdata).
const fixtureDir = "../../internal/event/adapter/feeds/testdata"

const (
	fixtureCCF = "193d48d5d3c112ab4743e365451aae36303a4d91"
	fixtureHCI = "e6af4d6d3152d22fb2e04d6466af865c07273dc9"
)

// fakeSources serves the GitHub API, raw files and OpenAlex from the recorded fixtures,
// and counts what it is asked.
type fakeSources struct {
	mu       sync.Mutex
	requests []string
	agents   map[string]bool
}

func readFixtures(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	root := filepath.Join(fixtureDir, dir)
	if err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p) //nolint:gosec // a path under the test's own testdata
		rel, _ := filepath.Rel(root, p)
		out[filepath.ToSlash(rel)] = string(b)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func (f *fakeSources) serve(t *testing.T) *httptest.Server {
	t.Helper()
	repos := map[string]struct {
		sha   string
		files map[string]string
	}{
		"ccfddl/ccf-deadlines":        {fixtureCCF, readFixtures(t, "ccf")},
		"hci-deadlines/conf-database": {fixtureHCI, readFixtures(t, "hci")},
	}
	page, err := os.ReadFile(filepath.Join(fixtureDir, "openalex", "sources-page1.json"))
	if err != nil {
		t.Fatal(err)
	}
	f.agents = map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.URL.Path)
		f.agents[r.Header.Get("User-Agent")] = true
		f.mu.Unlock()
		p := r.URL.Path
		if p == "/openalex/sources" {
			_, _ = w.Write(page)
			return
		}
		for name, repo := range repos {
			switch {
			case p == "/gh/repos/"+name+"/commits/main":
				etag := `"` + repo.sha + `"`
				if r.Header.Get("If-None-Match") == etag {
					w.WriteHeader(http.StatusNotModified)
					return
				}
				w.Header().Set("ETag", etag)
				_ = json.NewEncoder(w).Encode(map[string]string{"sha": repo.sha})
				return
			case p == "/gh/repos/"+name+"/git/trees/"+repo.sha:
				var tree []map[string]string
				for path := range repo.files {
					tree = append(tree, map[string]string{"path": path, "type": "blob"})
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"tree": tree})
				return
			case strings.HasPrefix(p, "/raw/"+name+"/"+repo.sha+"/"):
				if c, ok := repo.files[strings.TrimPrefix(p, "/raw/"+name+"/"+repo.sha+"/")]; ok {
					_, _ = w.Write([]byte(c))
					return
				}
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeSources) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.requests {
		if strings.HasPrefix(r, prefix) {
			n++
		}
	}
	return n
}

// noLinks answers every homepage check: down.
type noLinks struct{}

func (noLinks) Check(context.Context, string) domain.LinkCheck { return domain.LinkCheck{} }

// venueSyncEnv is a test environment whose import reads the fake sources, at clock.
func venueSyncEnv(t *testing.T, contact string, clock *time.Time) (mediaEnv, *fakeSources) {
	t.Helper()
	fake := &fakeSources{}
	srv := fake.serve(t)
	cfg := config.Config{
		VenueSyncContactEmail: contact, VenueSyncMinInterval: time.Hour, VenueSyncOpenAlexWorks: 5000,
		GitHubAPIURL: srv.URL + "/gh", GitHubRawURL: srv.URL + "/raw", OpenAlexBaseURL: srv.URL + "/openalex",
	}
	f := newVenueFeeds(cfg)
	f.links = noLinks{}
	f.now = func() time.Time { return *clock }
	e := newMediaEnvOnPool(t, testenv.DB(t), "", "", envOptions{venueSync: &venueSyncSetup{cfg: cfg, feeds: f}})
	return e, fake
}

// FR-14.5 end to end with the recorded files of the real sources: a dry run reports and
// writes nothing; a real run creates conferences and rolling journals with their audit and
// shows its counts per source; the next run within the hour is refused; after it, an
// unchanged repository costs one conditional request; a researcher may do none of it.
func TestVenueSyncEndToEnd(t *testing.T) {
	clock := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	e, fake := venueSyncEnv(t, "ops@example.org", &clock)
	admin, researcher := e.newProfileUser(t), e.newProfileUser(t)
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET role = 'admin' WHERE id = $1", admin); err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct{ method, path string }{{http.MethodGet, "/api/v1/admin/venues/sync"}, {http.MethodPost, "/api/v1/admin/venues/sync?dryRun=true"}} {
		if code, body := e.asRole(t, researcher, "researcher", r.method, r.path, ""); code != http.StatusForbidden {
			t.Fatalf("researcher %s %s: %d %v", r.method, r.path, code, body)
		}
	}

	code, body := e.asAdmin(t, admin, http.MethodPost, "/api/v1/admin/venues/sync?dryRun=true", "")
	if code != http.StatusAccepted || len(body["runs"].([]any)) != 3 || body["dryRun"] != true {
		t.Fatalf("dry run: %d %v", code, body)
	}
	e.venueSync.Wait()
	var n int
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM venues").Scan(&n); err != nil || n != 0 {
		t.Fatalf("a dry run wrote %d venues (%v)", n, err)
	}
	// Venue recommendations (AI-6.1) are renewed only by a run that wrote venues.
	venueGen := func() string { return e.rdb.Get(t.Context(), recommendationredis.VenueGenerationKey).Val() }
	if g := venueGen(); g != "" {
		t.Errorf("a dry run raised the venues generation to %s", g)
	}
	dry := e.syncStatus(t, admin)
	for _, src := range dry {
		run := src["lastDryRun"].(map[string]any)
		if src["lastRun"] != nil || run["status"] != "succeeded" || run["created"].(float64) == 0 {
			t.Fatalf("dry status of %s: %v", src["source"], src)
		}
	}

	code, body = e.asAdmin(t, admin, http.MethodPost, "/api/v1/admin/venues/sync", `{"sources":[]}`)
	if code != http.StatusAccepted {
		t.Fatalf("real run: %d %v", code, body)
	}
	e.venueSync.Wait()
	if g := venueGen(); g != "3" {
		t.Errorf("venues generation after three runs that wrote venues = %q, want 3", g)
	}
	status := e.syncStatus(t, admin)
	created := map[string]float64{}
	for _, src := range status {
		run := src["lastRun"].(map[string]any)
		if run["status"] != "succeeded" || run["trigger"] != "on_demand" {
			t.Fatalf("status of %s: %v", src["source"], src)
		}
		created[src["source"].(string)] = run["created"].(float64)
		dryCreated := src["lastDryRun"].(map[string]any)["created"].(float64)
		if dryCreated != run["created"].(float64) {
			t.Errorf("%s: the dry run said %v created, the real run %v", src["source"], dryCreated, run["created"])
		}
	}
	if status[0]["commit"] != fixtureCCF || status[1]["commit"] != fixtureHCI || status[2]["commit"] != nil || status[0]["licence"] != "MIT" {
		t.Fatalf("status = %v", status)
	}
	// ccf: ACL 2027 and SIGMOD's open editions are created; MICCAI 2027 and 2028 are TBD,
	// rejected. hci: CHI 2027 (its deadline closed 75 days before the clock) and more; the
	// recorded ccf files hold no CHI, so nothing is skipped as a duplicate. OpenAlex: only
	// Scientific Reports has its main topic in a listed subfield.
	if created["ccfddl"] < 2 || created["hci_deadlines"] < 1 || created["openalex"] != 1 {
		t.Fatalf("created = %v", created)
	}
	ccfRun := status[0]["lastRun"].(map[string]any)
	if !strings.Contains(jsonOf(t, ccfRun["rejects"]), `"code":"deadline_tbd"`) {
		t.Errorf("ccf rejects = %v", ccfRun["rejects"])
	}

	// The venues: an imported conference with its zone and place, a rolling journal linked to
	// its ISSN record (the fake homepage check says down).
	code, body = e.asRole(t, researcher, "researcher", http.MethodGet, "/api/v1/venues?q=ACL", "")
	if code != http.StatusOK || !strings.Contains(jsonOf(t, body), `"timeZone":"AoE"`) || !strings.Contains(jsonOf(t, body), `"country":"JP"`) {
		t.Fatalf("ACL: %d %v", code, body)
	}
	code, body = e.asRole(t, researcher, "researcher", http.MethodGet, "/api/v1/venues?rolling=true", "")
	items := body["items"].([]any)
	if code != http.StatusOK || len(items) != 1 {
		t.Fatalf("rolling: %d %v", code, body)
	}
	sr := items[0].(map[string]any)
	if sr["name"] != "Scientific Reports" || sr["rolling"] != true || sr["linkKind"] != "issn_portal" ||
		sr["officialUrl"] != "https://portal.issn.org/resource/ISSN/2045-2322" || sr["submissionDeadline"] != nil || sr["past"] != false {
		t.Fatalf("journal = %v", sr)
	}
	code, body = e.asRole(t, researcher, "researcher", http.MethodGet, "/api/v1/venues/"+sr["id"].(string), "")
	if _, hasLocked := body["locked"]; code != http.StatusOK || body["source"] != "openalex" || hasLocked {
		t.Fatalf("journal page: %d %v", code, body)
	}
	code, body = e.asAdmin(t, admin, http.MethodGet, "/api/v1/venues/"+sr["id"].(string), "")
	if code != http.StatusOK || body["locked"] != false {
		t.Fatalf("journal page as admin: %d %v", code, body)
	}
	if err := e.pool.QueryRow(t.Context(), `SELECT count(*) FROM venue_actions WHERE actor <> 'import'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d audit rows not by the import (%v)", n, err)
	}

	// Within the hour a real run is refused; a dry run is not.
	if code, body := e.asAdmin(t, admin, http.MethodPost, "/api/v1/admin/venues/sync", ""); code != http.StatusTooManyRequests || body["code"] != "venue_sync_too_soon" {
		t.Fatalf("too soon: %d %v", code, body)
	}
	// An hour later, the repositories have not moved: one conditional request each, no file.
	clock = clock.Add(2 * time.Hour)
	raw := fake.count("/raw/")
	if code, _ := e.asAdmin(t, admin, http.MethodPost, "/api/v1/admin/venues/sync", `{"sources":["ccfddl","hci_deadlines"]}`); code != http.StatusAccepted {
		t.Fatalf("second run: %d", code)
	}
	e.venueSync.Wait()
	if fake.count("/raw/") != raw || fake.count("/gh/repos/ccfddl/ccf-deadlines/git/trees/") != 2 {
		t.Fatalf("an unchanged repository was read again: %v", fake.requests)
	}
	second := e.syncStatus(t, admin)[0]["lastRun"].(map[string]any)
	if second["created"].(float64) != 0 || second["unchanged"].(float64) != created["ccfddl"] {
		t.Fatalf("second ccf run = %v", second)
	}
	for ua := range fake.agents {
		if !strings.Contains(ua, "mailto:ops@example.org") {
			t.Errorf("User-Agent %q has no contact", ua)
		}
	}
}

// Without a contact address the import refuses to read any source.
func TestVenueSyncUnconfigured(t *testing.T) {
	clock := time.Now()
	e, fake := venueSyncEnv(t, "", &clock)
	admin := e.newProfileUser(t)
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET role = 'admin' WHERE id = $1", admin); err != nil {
		t.Fatal(err)
	}
	if code, body := e.asAdmin(t, admin, http.MethodPost, "/api/v1/admin/venues/sync?dryRun=true", ""); code != http.StatusServiceUnavailable || body["code"] != "venue_sync_unconfigured" {
		t.Fatalf("%d %v", code, body)
	}
	if code, body := e.asAdmin(t, admin, http.MethodPost, "/api/v1/admin/venues/sync", `{"sources":["wikicfp"]}`); code != http.StatusBadRequest || body["field"] != "sources" {
		t.Fatalf("unknown source: %d %v", code, body)
	}
	if len(fake.requests) != 0 {
		t.Fatalf("requests %v", fake.requests)
	}
}

func (e mediaEnv) syncStatus(t *testing.T, admin string) []map[string]any {
	t.Helper()
	code, body := e.asAdmin(t, admin, http.MethodGet, "/api/v1/admin/venues/sync", "")
	if code != http.StatusOK {
		t.Fatalf("status: %d %v", code, body)
	}
	var out []map[string]any
	for _, s := range body["sources"].([]any) {
		out = append(out, s.(map[string]any))
	}
	return out
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// asRole sends a request as a verified user with role and decodes the answer.
func (e mediaEnv) asRole(t *testing.T, userID, role, method, path, body string) (int, map[string]any) {
	t.Helper()
	rec := e.sendRole(t, userID, role, true, method, path, body)
	return rec.Code, decodeBody(t, rec.Body.Bytes())
}
