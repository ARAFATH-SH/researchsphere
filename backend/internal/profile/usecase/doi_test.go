package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

const testDOI = "10.1145/3292500.3330701"

// lookupEnv is a Service over the fake store with a fake source, cache and limiter, and a
// clock that reads 15 June 2026 UTC.
type lookupEnv struct {
	svc     *Service
	store   *fakeStore
	source  *fakeSource
	cache   *fakeCache
	limiter *fakeLimiter
}

func newLookupEnv() lookupEnv {
	e := lookupEnv{
		store:   newFakeStore(testAccounts()...),
		source:  &fakeSource{work: engineWork()},
		cache:   &fakeCache{},
		limiter: &fakeLimiter{},
	}
	e.svc = New(e.store, WithDOILookup(DOILookup{Source: e.source, Cache: e.cache, Limiter: e.limiter}))
	e.svc.now = func() time.Time { return time.Date(2026, time.June, 15, 12, 0, 0, 0, time.UTC) }
	return e
}

// engineWork is what the source answers, before normalization.
func engineWork() domain.WorkMetadata {
	return domain.WorkMetadata{
		Title:    "  Notes on the\nAnalytical   Engine ",
		Year:     iptr(2024),
		Venue:    ptr(" Journal of Engines "),
		Abstract: ptr(" We describe the engine. "),
		URL:      ptr("https://example.org/paper"),
	}
}

func TestLookupDOIFetchesAndNormalizes(t *testing.T) {
	e := newLookupEnv()
	pv, err := e.svc.LookupDOI(context.Background(), adaID, " https://doi.org/10.1145/3292500.3330701 ")
	if err != nil {
		t.Fatal(err)
	}
	if pv.DOI != testDOI || pv.Title != "Notes on the Analytical Engine" || *pv.Year != 2024 ||
		*pv.Venue != "Journal of Engines" || *pv.Abstract != "We describe the engine." ||
		*pv.URL != "https://example.org/paper" || pv.ExistsAlready || pv.LinkedToMe || pv.PublicationID != "" {
		t.Errorf("preview = %+v", pv)
	}
	if e.source.calls != 1 || e.limiter.used[adaID] != 1 {
		t.Errorf("source calls %d, lookups counted %d; want 1 and 1", e.source.calls, e.limiter.used[adaID])
	}
	if w, ok := e.cache.works[testDOI]; !ok || w.Title != pv.Title || e.cache.ttl != 24*time.Hour {
		t.Errorf("cache = %+v (ttl %s), want the normalized work for 24h", e.cache.works, e.cache.ttl)
	}
	// Nothing is stored by a lookup.
	if len(e.store.publications) != 0 {
		t.Errorf("stored %d publications, want 0", len(e.store.publications))
	}
}

func TestLookupDOICacheHitSkipsTheSource(t *testing.T) {
	e := newLookupEnv()
	e.cache.works = map[string]domain.WorkMetadata{testDOI: {DOI: testDOI, Title: "Cached"}}
	pv, err := e.svc.LookupDOI(context.Background(), adaID, testDOI)
	if err != nil || pv.Title != "Cached" {
		t.Fatalf("LookupDOI() = %+v, %v; want the cached work", pv, err)
	}
	if e.source.calls != 0 || e.limiter.used[adaID] != 0 {
		t.Errorf("source calls %d, lookups counted %d; want none", e.source.calls, e.limiter.used[adaID])
	}
}

func TestLookupDOIStoredRecordSkipsTheSource(t *testing.T) {
	for _, tt := range []struct {
		name       string
		linkAda    bool
		confirmed  bool
		wantLinked bool
	}{
		{"not linked", false, false, false},
		{"linked, unconfirmed", true, false, false},
		{"linked, confirmed", true, true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newLookupEnv()
			users := []string{bobID}
			if tt.linkAda {
				users = append(users, adaID)
			}
			id := e.store.givePublication(domain.Publication{DOI: ptr(testDOI), Title: "Stored", Year: iptr(2019)}, tt.confirmed, users...)
			pv, err := e.svc.LookupDOI(context.Background(), adaID, "DOI:"+strings.ToUpper(testDOI))
			if err != nil {
				t.Fatal(err)
			}
			if !pv.ExistsAlready || pv.PublicationID != id || pv.Title != "Stored" || *pv.Year != 2019 || pv.LinkedToMe != tt.wantLinked {
				t.Errorf("preview = %+v, want the stored record, linkedToMe %v", pv, tt.wantLinked)
			}
			if e.source.calls != 0 || e.limiter.used[adaID] != 0 {
				t.Errorf("source calls %d, lookups counted %d; want none", e.source.calls, e.limiter.used[adaID])
			}
		})
	}
}

func TestLookupDOIErrors(t *testing.T) {
	tests := []struct {
		name      string
		doi       string
		sourceErr error
		want      error
		wantCalls int
	}{
		{"blank DOI", "  ", nil, domain.ErrInvalidDOI, 0},
		{"invalid DOI", "11.1000/x", nil, domain.ErrInvalidDOI, 0},
		{"not found", testDOI, domain.ErrWorkNotFound, domain.ErrWorkNotFound, 1},
		{"unavailable", testDOI, fmt.Errorf("x: %w", domain.ErrMetadataUnavailable), domain.ErrMetadataUnavailable, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newLookupEnv()
			e.source.err = tt.sourceErr
			_, err := e.svc.LookupDOI(context.Background(), adaID, tt.doi)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if e.source.calls != tt.wantCalls {
				t.Errorf("source calls = %d, want %d", e.source.calls, tt.wantCalls)
			}
			// A failure is not cached, but a call that reached the source counts.
			if len(e.cache.works) != 0 {
				t.Errorf("cache = %v, want empty", e.cache.works)
			}
			if e.limiter.used[adaID] != tt.wantCalls {
				t.Errorf("lookups counted = %d, want %d", e.limiter.used[adaID], tt.wantCalls)
			}
		})
	}
}

func TestLookupDOIWithoutASource(t *testing.T) {
	_, err := New(newFakeStore(testAccounts()...)).LookupDOI(context.Background(), adaID, testDOI)
	if !errors.Is(err, domain.ErrMetadataUnavailable) {
		t.Fatalf("err = %v, want ErrMetadataUnavailable", err)
	}
}

func TestLookupDOIRateLimit(t *testing.T) {
	e := newLookupEnv()
	e.limiter.used = map[string]int{adaID: LookupsPerWindow}
	e.limiter.left = 10 * time.Minute
	_, err := e.svc.LookupDOI(context.Background(), adaID, testDOI)
	var limited *domain.RateLimitedError
	if !errors.As(err, &limited) || limited.RetryAfter != 10*time.Minute {
		t.Fatalf("err = %v, want RateLimitedError with 10m left", err)
	}
	if e.source.calls != 0 || e.limiter.used[adaID] != LookupsPerWindow {
		t.Errorf("source calls %d, lookups counted %d; want 0 and unchanged", e.source.calls, e.limiter.used[adaID])
	}

	// A cached DOI and another user are not limited.
	e.cache.works = map[string]domain.WorkMetadata{testDOI: {DOI: testDOI, Title: "Cached"}}
	if _, err := e.svc.LookupDOI(context.Background(), adaID, testDOI); err != nil {
		t.Errorf("cached lookup: %v", err)
	}
	if _, err := e.svc.LookupDOI(context.Background(), bobID, "10.1000/other"); err != nil {
		t.Errorf("Bob's lookup: %v", err)
	}
	// One below the limit is allowed.
	e.limiter.used[adaID] = LookupsPerWindow - 1
	if _, err := e.svc.LookupDOI(context.Background(), adaID, "10.1000/third"); err != nil {
		t.Errorf("lookup one below the limit: %v", err)
	}
}

func TestLookupDOIFailsOpen(t *testing.T) {
	e := newLookupEnv()
	e.cache.err = errors.New("redis down")
	e.limiter.err = errors.New("redis down")
	pv, err := e.svc.LookupDOI(context.Background(), adaID, testDOI)
	if err != nil || pv.Title != "Notes on the Analytical Engine" {
		t.Fatalf("LookupDOI() = %+v, %v; want the fetched work", pv, err)
	}
	if e.source.calls != 1 {
		t.Errorf("source calls = %d, want 1", e.source.calls)
	}
}

func TestNormalizeWork(t *testing.T) {
	long := func(word string, n int) string { return strings.TrimSpace(strings.Repeat(word+" ", n)) }
	tests := []struct {
		name   string
		modify func(*domain.WorkMetadata)
		check  func(t *testing.T, w domain.WorkMetadata)
		want   error
	}{
		{"no title", func(w *domain.WorkMetadata) { w.Title = " \n " }, nil, domain.ErrWorkNotFound},
		{"title cut at a word boundary", func(w *domain.WorkMetadata) { w.Title = long("abcd", 120) }, func(t *testing.T, w domain.WorkMetadata) {
			// 100 words of 4 letters and 99 spaces are 499 characters; the 101st does not fit.
			if n := utf8.RuneCountInString(w.Title); n != 499 || strings.HasSuffix(w.Title, " ") {
				t.Errorf("title has %d characters, want 499 with no trailing space", n)
			}
		}, nil},
		{"title of one long word cut at the limit", func(w *domain.WorkMetadata) { w.Title = strings.Repeat("é", 600) }, func(t *testing.T, w domain.WorkMetadata) {
			if n := utf8.RuneCountInString(w.Title); n != MaxPublicationTitleLength {
				t.Errorf("title has %d characters, want %d", n, MaxPublicationTitleLength)
			}
		}, nil},
		{"venue on one line, cut", func(w *domain.WorkMetadata) { w.Venue = ptr("A\r\nB " + long("venue", 60)) }, func(t *testing.T, w domain.WorkMetadata) {
			if w.Venue == nil || strings.ContainsAny(*w.Venue, "\r\n") || utf8.RuneCountInString(*w.Venue) > MaxVenueLength || !strings.HasPrefix(*w.Venue, "A B venue") {
				t.Errorf("venue = %q", ptrValue(w.Venue))
			}
		}, nil},
		{"blank venue and abstract", func(w *domain.WorkMetadata) { w.Venue = ptr(" "); w.Abstract = ptr("\n") }, func(t *testing.T, w domain.WorkMetadata) {
			if w.Venue != nil || w.Abstract != nil {
				t.Errorf("venue %q, abstract %q; want nil", ptrValue(w.Venue), ptrValue(w.Abstract))
			}
		}, nil},
		{"abstract cut at a word boundary", func(w *domain.WorkMetadata) { w.Abstract = ptr(long("word", 1200)) }, func(t *testing.T, w domain.WorkMetadata) {
			// 1000 words and 999 spaces are 4999 characters.
			if n := utf8.RuneCountInString(*w.Abstract); n != 4999 || !strings.HasSuffix(*w.Abstract, "word") {
				t.Errorf("abstract has %d characters, want 4999 ending in a whole word", n)
			}
		}, nil},
		{"abstract that ends at the limit", func(w *domain.WorkMetadata) { w.Abstract = ptr(strings.Repeat("a", 5000) + " more") }, func(t *testing.T, w domain.WorkMetadata) {
			if *w.Abstract != strings.Repeat("a", 5000) {
				t.Errorf("abstract has %d characters, want the first word of 5000", len(*w.Abstract))
			}
		}, nil},
		{"line breaks kept in the abstract", func(w *domain.WorkMetadata) { w.Abstract = ptr("One.\nTwo.") }, func(t *testing.T, w domain.WorkMetadata) {
			if *w.Abstract != "One.\nTwo." {
				t.Errorf("abstract = %q", *w.Abstract)
			}
		}, nil},
		{"no year", func(w *domain.WorkMetadata) { w.Year = nil }, func(t *testing.T, w domain.WorkMetadata) {
			if w.Year != nil {
				t.Errorf("year = %d, want nil", *w.Year)
			}
		}, nil},
		{"year 1899 dropped", func(w *domain.WorkMetadata) { w.Year = iptr(1899) }, func(t *testing.T, w domain.WorkMetadata) {
			if w.Year != nil {
				t.Errorf("year = %d, want nil", *w.Year)
			}
		}, nil},
		{"next year kept", func(w *domain.WorkMetadata) { w.Year = iptr(2027) }, func(t *testing.T, w domain.WorkMetadata) {
			if w.Year == nil || *w.Year != 2027 {
				t.Errorf("year = %v, want 2027", w.Year)
			}
		}, nil},
		{"two years ahead dropped", func(w *domain.WorkMetadata) { w.Year = iptr(2028) }, func(t *testing.T, w domain.WorkMetadata) {
			if w.Year != nil {
				t.Errorf("year = %d, want nil", *w.Year)
			}
		}, nil},
		{"no link: doi.org", func(w *domain.WorkMetadata) { w.URL = nil }, func(t *testing.T, w domain.WorkMetadata) {
			if ptrValue(w.URL) != "https://doi.org/"+testDOI {
				t.Errorf("url = %q", ptrValue(w.URL))
			}
		}, nil},
		{"invalid link: doi.org", func(w *domain.WorkMetadata) { w.URL = ptr("ftp://example.org/x") }, func(t *testing.T, w domain.WorkMetadata) {
			if ptrValue(w.URL) != "https://doi.org/"+testDOI {
				t.Errorf("url = %q", ptrValue(w.URL))
			}
		}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newLookupEnv()
			w := engineWork()
			tt.modify(&w)
			got, err := e.svc.normalizeWork(testDOI, w)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if err == nil {
				if got.DOI != testDOI {
					t.Errorf("DOI = %q, want %q", got.DOI, testDOI)
				}
				tt.check(t, got)
			}
		})
	}
}

func TestAddPublicationFromDOICreates(t *testing.T) {
	e := newLookupEnv()
	res, err := e.svc.AddPublicationFromDOI(context.Background(), adaID, "https://doi.org/"+testDOI)
	if err != nil {
		t.Fatal(err)
	}
	p := res.Publication
	if res.Linked || p.Source != domain.SourceDOI || ptrValue(p.DOI) != testDOI || p.Title != "Notes on the Analytical Engine" ||
		*p.Year != 2024 || ptrValue(p.CreatedBy) != adaID {
		t.Errorf("added %+v (linked %v)", p, res.Linked)
	}
	if state, _ := e.store.PublicationLink(context.Background(), adaID, p.ID); state != domain.Confirmed {
		t.Errorf("link is %v, want confirmed", state)
	}
	// A fetched record cannot be changed (11.1 rule).
	_, err = e.svc.UpdatePublication(context.Background(), adaID, p.ID, PublicationPatch{Title: Field[string]{Set: true, Value: "Mine"}})
	if !errors.Is(err, domain.ErrPublicationSharedReadonly) {
		t.Errorf("PATCH err = %v, want ErrPublicationSharedReadonly", err)
	}
}

func TestAddPublicationFromDOIUsesTheCache(t *testing.T) {
	e := newLookupEnv()
	if _, err := e.svc.LookupDOI(context.Background(), adaID, testDOI); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.AddPublicationFromDOI(context.Background(), adaID, testDOI); err != nil {
		t.Fatal(err)
	}
	if e.source.calls != 1 || e.limiter.used[adaID] != 1 {
		t.Errorf("source calls %d, lookups counted %d; want 1 and 1", e.source.calls, e.limiter.used[adaID])
	}
}

func TestAddPublicationFromDOILinksInsteadOfDuplicating(t *testing.T) {
	for _, tt := range []struct {
		name      string
		adaLinked bool // Ada has an unconfirmed link, as after an import
	}{{"new link", false}, {"unconfirmed link confirmed", true}} {
		t.Run(tt.name, func(t *testing.T) {
			e := newLookupEnv()
			id := e.store.givePublication(domain.Publication{DOI: ptr(testDOI), Title: "Bob's", Year: iptr(2019)}, true, bobID)
			if tt.adaLinked {
				e.store.link(adaID, id, false)
			}
			res, err := e.svc.AddPublicationFromDOI(context.Background(), adaID, testDOI)
			if err != nil {
				t.Fatal(err)
			}
			if !res.Linked || res.Publication.ID != id || len(e.store.publications) != 1 {
				t.Errorf("got %+v with %d records, want Bob's record linked", res, len(e.store.publications))
			}
			if state, _ := e.store.PublicationLink(context.Background(), adaID, id); state != domain.Confirmed {
				t.Errorf("link is %v, want confirmed", state)
			}
			if e.source.calls != 0 {
				t.Errorf("source calls = %d, want 0", e.source.calls)
			}
		})
	}
}

func TestAddPublicationFromDOIStoredMeanwhile(t *testing.T) {
	// Bob stores the DOI while Ada's request is fetching it: Ada is linked to Bob's.
	e := newLookupEnv()
	e.store.hideDOIOnce = true
	id := e.store.givePublication(domain.Publication{DOI: ptr(testDOI), Title: "Bob's", Year: iptr(2019)}, true, bobID)
	res, err := e.svc.AddPublicationFromDOI(context.Background(), adaID, testDOI)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Linked || res.Publication.ID != id || len(e.store.publications) != 1 {
		t.Errorf("got %+v with %d records, want Bob's record linked", res, len(e.store.publications))
	}
}

func TestAddPublicationFromDOIRefusals(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(e lookupEnv)
		doi       string
		want      error
		wantCalls int
	}{
		{"already confirmed", func(e lookupEnv) {
			e.store.givePublication(domain.Publication{DOI: ptr(testDOI), Title: "Mine", Year: iptr(2019)}, true, adaID)
		}, testDOI, domain.ErrPublicationAlreadyAdded, 0},
		{"at the limit", func(e lookupEnv) {
			for i := range MaxPublications {
				e.store.givePublication(domain.Publication{Title: fmt.Sprintf("P%d", i), Year: iptr(2020)}, i%2 == 0, adaID)
			}
		}, testDOI, domain.ErrPublicationLimitReached, 0},
		{"at the limit, linking", func(e lookupEnv) {
			for i := range MaxPublications {
				e.store.givePublication(domain.Publication{Title: fmt.Sprintf("P%d", i), Year: iptr(2020)}, true, adaID)
			}
			e.store.givePublication(domain.Publication{DOI: ptr(testDOI), Title: "Bob's", Year: iptr(2019)}, true, bobID)
		}, testDOI, domain.ErrPublicationLimitReached, 0},
		{"invalid DOI", func(lookupEnv) {}, "nope", domain.ErrInvalidDOI, 0},
		{"not found", func(e lookupEnv) { e.source.err = domain.ErrWorkNotFound }, testDOI, domain.ErrWorkNotFound, 1},
		{"unavailable", func(e lookupEnv) { e.source.err = domain.ErrMetadataUnavailable }, testDOI, domain.ErrMetadataUnavailable, 1},
		{"rate limited", func(e lookupEnv) { e.limiter.used = map[string]int{adaID: LookupsPerWindow} }, testDOI, &domain.RateLimitedError{}, 0},
		{"unknown account", func(e lookupEnv) { delete(e.store.accounts, adaID) }, testDOI, domain.ErrNotFound, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newLookupEnv()
			tt.setup(e)
			before := len(e.store.publications)
			_, err := e.svc.AddPublicationFromDOI(context.Background(), adaID, tt.doi)
			var limited *domain.RateLimitedError
			if errors.As(tt.want, &limited) {
				if !errors.As(err, &limited) {
					t.Fatalf("err = %v, want RateLimitedError", err)
				}
			} else if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if e.source.calls != tt.wantCalls {
				t.Errorf("source calls = %d, want %d", e.source.calls, tt.wantCalls)
			}
			if len(e.store.publications) != before {
				t.Errorf("records: %d, want %d", len(e.store.publications), before)
			}
		})
	}
}

func ptrValue(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
