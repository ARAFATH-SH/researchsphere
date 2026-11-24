package feeds

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
)

const (
	shaA    = "1111111111111111111111111111111111111111"
	shaB    = "2222222222222222222222222222222222222222"
	contact = "ops@example.org"
)

// fakeGitHub is GitHub's API and raw files for one repository, o/r, branch main: files per
// commit, the head commit, and every request it saw.
type fakeGitHub struct {
	mu       sync.Mutex
	head     string
	files    map[string]map[string]string // commit -> path -> content
	compare  string                       // status of a comparison: ahead, diverged
	requests []string
	agents   []string
	fail     map[string]int // path prefix -> how many more times to answer 503
}

func (g *fakeGitHub) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.requests = append(g.requests, r.Method+" "+r.URL.Path)
		g.agents = append(g.agents, r.Header.Get("User-Agent"))
		for prefix, n := range g.fail {
			if n > 0 && strings.HasPrefix(r.URL.Path, prefix) {
				g.fail[prefix] = n - 1
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
		}
		etag := `W/"head-` + g.head + `"`
		switch p := r.URL.Path; {
		case p == "/api/repos/o/r/commits/main":
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", etag)
			_ = json.NewEncoder(w).Encode(map[string]string{"sha": g.head})
		case strings.HasPrefix(p, "/api/repos/o/r/git/trees/"):
			sha := strings.TrimPrefix(p, "/api/repos/o/r/git/trees/")
			var tree []map[string]string
			for path := range g.files[sha] {
				tree = append(tree, map[string]string{"path": path, "type": "blob"})
			}
			tree = append(tree, map[string]string{"path": "conference", "type": "tree"}, map[string]string{"path": "README.md", "type": "blob"})
			_ = json.NewEncoder(w).Encode(map[string]any{"truncated": false, "tree": tree})
		case strings.HasPrefix(p, "/api/repos/o/r/compare/"):
			base, head, _ := strings.Cut(strings.TrimPrefix(p, "/api/repos/o/r/compare/"), "...")
			var files []map[string]string
			for path, c := range g.files[head] {
				if old, ok := g.files[base][path]; !ok {
					files = append(files, map[string]string{"filename": path, "status": "added"})
				} else if old != c {
					files = append(files, map[string]string{"filename": path, "status": "modified"})
				}
			}
			for path := range g.files[base] {
				if _, ok := g.files[head][path]; !ok {
					files = append(files, map[string]string{"filename": path, "status": "removed"})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": g.compare, "files": files})
		case strings.HasPrefix(p, "/raw/o/r/"):
			sha, path, _ := strings.Cut(strings.TrimPrefix(p, "/raw/o/r/"), "/")
			c, ok := g.files[sha][path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(c))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (g *fakeGitHub) seen(kind string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, r := range g.requests {
		if strings.Contains(r, kind) {
			n++
		}
	}
	return n
}

func (g *fakeGitHub) reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.requests, g.agents = nil, nil
}

// fixtures reads the recorded files under testdata/dir as path -> content.
func fixtures(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	root := filepath.Join("testdata", dir)
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p) //nolint:gosec // a path under the test's own testdata
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func newTestRepo(srv *httptest.Server, format Format, dataDir string, depth int) *Repo {
	return NewRepo(RepoConfig{
		APIBase: srv.URL + "/api", RawBase: srv.URL + "/raw", Owner: "o", Repo: "r", Branch: "main",
		DataDir: dataDir, Depth: depth, Format: format, Contact: contact,
		Pace: -1, Retries: 2, Backoff: time.Millisecond,
	})
}

// The first fetch lists the tree and reads every data file at the head commit; the next,
// with the ETag, gets 304 and reads nothing; a new commit reads only the changed files
// through a comparison; a rewritten history reads everything again.
func TestRepoFetch(t *testing.T) {
	files := fixtures(t, "ccf")
	g := &fakeGitHub{head: shaA, files: map[string]map[string]string{shaA: files}, compare: "ahead"}
	srv := g.serve(t)
	repo := newTestRepo(srv, FormatCCF, "conference", 1)
	ctx := t.Context()

	first, err := repo.Fetch(ctx, domain.RepoState{})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Changed || !first.Full || first.State.CommitSHA != shaA || first.State.ETag == "" || len(first.Files) != len(files) {
		t.Fatalf("first fetch = changed %t full %t state %+v, %d files; want all %d at %s", first.Changed, first.Full, first.State, len(first.Files), len(files), shaA)
	}
	if g.seen("/raw/") != len(files) || g.seen("/git/trees/") != 1 {
		t.Fatalf("requests %v", g.requests)
	}
	for _, ua := range g.agents {
		if ua != "ResearchSphere/1.0 (+https://github.com/ARAFATH-SH/researchsphere; mailto:ops@example.org)" {
			t.Fatalf("User-Agent %q", ua)
		}
	}

	g.reset()
	same, err := repo.Fetch(ctx, first.State)
	if err != nil {
		t.Fatal(err)
	}
	if same.Changed || same.State != first.State || len(g.requests) != 1 {
		t.Fatalf("unchanged fetch = %+v after %v; want one conditional request and no change", same, g.requests)
	}

	// A new commit changes one file, adds one and removes one.
	next := map[string]string{}
	for p, c := range files {
		next[p] = c
	}
	next["conference/DB/sigmod.yml"] += "\n"
	delete(next, "conference/MX/miccai.yml")
	next["conference/AI/new.yml"] = "[]\n"
	g.mu.Lock()
	g.files[shaB], g.head = next, shaB
	g.mu.Unlock()
	g.reset()
	diff, err := repo.Fetch(ctx, first.State)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range diff.Files {
		got = append(got, f.Path)
	}
	slices.Sort(got)
	if !diff.Changed || diff.Full || diff.State.CommitSHA != shaB || !slices.Equal(got, []string{"conference/AI/new.yml", "conference/DB/sigmod.yml"}) ||
		!slices.Equal(diff.Deleted, []string{"conference/MX/miccai.yml"}) {
		t.Fatalf("diff fetch = changed %t full %t files %v deleted %v", diff.Changed, diff.Full, got, diff.Deleted)
	}
	if g.seen("/compare/") != 1 || g.seen("/git/trees/") != 0 || g.seen("/raw/") != 2 {
		t.Fatalf("requests %v", g.requests)
	}

	// A rewritten history: the comparison is not ahead, so everything is read.
	g.mu.Lock()
	g.compare = "diverged"
	g.mu.Unlock()
	g.reset()
	full, err := repo.Fetch(ctx, first.State)
	if err != nil {
		t.Fatal(err)
	}
	if !full.Full || len(full.Files) != len(next) || g.seen("/git/trees/") != 1 {
		t.Fatalf("diverged fetch = full %t, %d files; requests %v", full.Full, len(full.Files), g.requests)
	}
}

// A 503 is tried again (Retry-After honoured), and the fetch succeeds; one that keeps
// failing gives up after the retries with the status.
func TestRepoFetchRetries(t *testing.T) {
	g := &fakeGitHub{head: shaA, files: map[string]map[string]string{shaA: fixtures(t, "hci")}, compare: "ahead",
		fail: map[string]int{"/api/repos/o/r/commits/": 2}}
	srv := g.serve(t)
	repo := newTestRepo(srv, FormatHCI, "conferences", 0)
	if _, err := repo.Fetch(t.Context(), domain.RepoState{}); err != nil {
		t.Fatalf("after two 503s: %v", err)
	}
	if n := g.seen("/commits/"); n != 3 {
		t.Fatalf("commits asked %d times, want 3", n)
	}

	g.mu.Lock()
	g.fail = map[string]int{"/api/repos/o/r/commits/": 10}
	g.mu.Unlock()
	g.reset()
	_, err := repo.Fetch(t.Context(), domain.RepoState{})
	if !IsStatus(err, http.StatusServiceUnavailable) {
		t.Fatalf("err = %v, want 503", err)
	}
	if n := g.seen("/commits/"); n != 3 {
		t.Fatalf("commits asked %d times, want 1 + 2 retries", n)
	}
}

// A file over the cap is refused, not truncated; a 404 is not retried.
func TestFetcherLimits(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/big":
			_, _ = w.Write([]byte(strings.Repeat("x", 2048)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := newFetcher(nil, "ua", 0, 3, time.Millisecond)
	if _, err := f.get(t.Context(), srv.URL+"/big", nil, 1024); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Fatalf("big: err = %v", err)
	}
	calls.Store(0)
	if _, err := f.get(t.Context(), srv.URL+"/gone?api_key=secret", nil, 1024); !IsStatus(err, 404) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("404: err = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("404 asked %d times", calls.Load())
	}
}

// GitHub's used-up rate limit (403, X-RateLimit-Remaining: 0) is reported with its reset
// time and not retried.
func TestFetcherRateLimit(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1795509920")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	f := newFetcher(nil, "ua", 0, 3, time.Millisecond)
	_, err := f.get(t.Context(), srv.URL, nil, 1024)
	if !IsStatus(err, http.StatusForbidden) || !strings.Contains(err.Error(), "rate limit is used up until 2026-11-24T08:45:20Z") || calls.Load() != 1 {
		t.Fatalf("err = %v after %d requests", err, calls.Load())
	}
}

// A request that hangs past the client's timeout fails (and is retried, then gives up).
func TestFetcherTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	f := newFetcher(&http.Client{Timeout: 50 * time.Millisecond}, "ua", 0, 1, time.Millisecond)
	start := time.Now()
	if _, err := f.get(t.Context(), srv.URL, nil, 1024); err == nil {
		t.Fatal("no error")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("the timeout did not apply")
	}
}

// The recorded files parse into editions with their rounds, zones, places and areas; a file
// that is not YAML of the shape is reported and the others still read.
func TestParse(t *testing.T) {
	var ccf []domain.RepoFile
	for p, c := range fixtures(t, "ccf") {
		ccf = append(ccf, domain.RepoFile{Path: "conference/" + p, Content: c})
	}
	ccf = append(ccf, domain.RepoFile{Path: "conference/AI/broken.yml", Content: "- title: [unclosed"})
	repo := NewRepo(RepoConfig{Format: FormatCCF})
	eds, errs := repo.Parse(ccf)
	if len(errs) != 1 || errs[0].Path != "conference/AI/broken.yml" {
		t.Fatalf("errors %v", errs)
	}
	byID := map[string]domain.Edition{}
	for _, e := range eds {
		byID[e.ID] = e
	}
	acl := byID["acl26"]
	if acl.Title != "ACL" || acl.Year != 2026 || acl.TimeZone != "UTC-12" || len(acl.Rounds) != 3 ||
		acl.Rounds[0].Deadline != "2025-10-06 23:59:59" || !strings.Contains(acl.Place, "(Hybrid)") ||
		!slices.Equal(acl.Areas, []string{"AI"}) || !strings.HasPrefix(acl.Ranks, "CCF A") {
		t.Fatalf("acl26 = %+v", acl)
	}
	if m := byID["miccai27"]; len(m.Rounds) != 1 || m.Rounds[0].Deadline != "TBD" {
		t.Fatalf("miccai27 = %+v", m)
	}

	var hci []domain.RepoFile
	for p, c := range fixtures(t, "hci") {
		hci = append(hci, domain.RepoFile{Path: p, Content: c})
	}
	eds, errs = NewRepo(RepoConfig{Format: FormatHCI}).Parse(hci)
	if len(errs) != 0 {
		t.Fatalf("errors %v", errs)
	}
	byID = map[string]domain.Edition{}
	for _, e := range eds {
		byID[e.ID] = e
	}
	chi := byID["chi27"]
	if chi.Title != "CHI" || chi.Year != 2027 || chi.StartsOn != "2027-05-10" || chi.EndsOn != "2027-05-14" ||
		len(chi.Rounds) != 1 || chi.Rounds[0].Deadline != "2026-09-10 23:59:59" || !slices.Equal(chi.Areas, []string{"HCI"}) {
		t.Fatalf("chi27 = %+v", chi)
	}
}

// The OpenAlex feed asks for the filter with mailto and api_key, reads the recorded page,
// and keeps the key out of its errors.
func TestOpenAlexJournals(t *testing.T) {
	page, err := os.ReadFile("testdata/openalex/sources-page1.json")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.RawQuery)
		if r.Header.Get("User-Agent") != UserAgent(contact) {
			t.Errorf("User-Agent %q", r.Header.Get("User-Agent"))
		}
		_, _ = w.Write(page)
	}))
	defer srv.Close()
	now := func() time.Time { return time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC) }
	oa := NewOpenAlex(OpenAlexConfig{BaseURL: srv.URL, Mailto: contact, APIKey: "k3y", MinWorks: 5000, Subfields: []int{1702, 2741},
		Now: now, Pace: -1, Retries: 1, Backoff: time.Millisecond})
	js, err := oa.Journals(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("%d requests; a short page is the last", len(got))
	}
	q := got[0]
	for _, want := range []string{"mailto=ops%40example.org", "api_key=k3y", "per_page=100", "page=1",
		"filter=type%3Ajournal%2Cis_core%3Atrue%2Cworks_count%3A%3E5000%2Clast_publication_year%3A%3E2024%2Ctopics.subfield.id%3A1702%7C2741"} {
		if !strings.Contains(q, want) {
			t.Errorf("query %s lacks %s", q, want)
		}
	}
	if strings.Contains(oa.URL(), "k3y") {
		t.Errorf("URL() shows the key: %s", oa.URL())
	}
	if len(js) != 6 || js[0].ID != "S196734849" || js[0].Name != "Scientific Reports" || js[0].ISSNL != "2045-2322" ||
		js[0].Topics[0].SubfieldID != 1707 || js[0].Topics[0].FieldID != 17 || js[0].Publisher == "" {
		t.Fatalf("journals = %+v", js[:1])
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer bad.Close()
	oa = NewOpenAlex(OpenAlexConfig{BaseURL: bad.URL, APIKey: "k3y", Subfields: []int{1702}, Pace: -1, Retries: 1, Backoff: time.Millisecond})
	if _, err := oa.Journals(t.Context()); err == nil || strings.Contains(err.Error(), "k3y") {
		t.Fatalf("err = %v; want a failure without the key", err)
	}
}

// A homepage that answers over https is OK; 403 (a publisher refusing robots) is OK too; 404
// is not; a 405 to HEAD is asked again with GET; a redirect to http, too many redirects, a
// timeout and a private address are not OK.
func TestLinks(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
		case "/forbidden":
			w.WriteHeader(http.StatusForbidden)
		case "/missing":
			http.NotFound(w, r)
		case "/head-refused":
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
		case "/to-http":
			http.Redirect(w, r, "http://example.org/", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/slow":
			time.Sleep(300 * time.Millisecond)
		case "/broken":
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer srv.Close()
	at := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	l := NewLinks(LinkConfig{Contact: contact, Timeout: 100 * time.Millisecond, AllowPrivate: true, Now: func() time.Time { return at }})
	l.http.Transport.(*http.Transport).TLSClientConfig = srv.Client().Transport.(*http.Transport).TLSClientConfig
	for _, tt := range []struct {
		path string
		ok   bool
	}{
		{"/ok", true}, {"/forbidden", true}, {"/missing", false}, {"/head-refused", true}, {"/to-http", false},
		{"/loop", false}, {"/slow", false}, {"/broken", false},
	} {
		c := l.Check(t.Context(), srv.URL+tt.path)
		if c.OK != tt.ok || !c.CheckedAt.Equal(at) {
			t.Errorf("%s: %+v, want ok %t", tt.path, c, tt.ok)
		}
	}
	if c := l.Check(t.Context(), "http://example.org"); c.OK {
		t.Error("http:// checked as OK")
	}

	// Without AllowPrivate, the test server's loopback address is refused before any request.
	var hits atomic.Int32
	plain := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer plain.Close()
	guarded := NewLinks(LinkConfig{Timeout: time.Second})
	guarded.http.Transport.(*http.Transport).TLSClientConfig = plain.Client().Transport.(*http.Transport).TLSClientConfig
	if c := guarded.Check(context.Background(), plain.URL+"/ok"); c.OK || hits.Load() != 0 {
		t.Fatalf("loopback: %+v after %d requests; want refused", c, hits.Load())
	}
	for _, a := range []string{"10.0.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "::1", "fe80::1", "0.0.0.0"} {
		if publicAddr(netip.MustParseAddr(a)) {
			t.Errorf("%s counted as public", a)
		}
	}
	if !publicAddr(netip.MustParseAddr("93.184.216.34")) {
		t.Error("a public address refused")
	}
}
