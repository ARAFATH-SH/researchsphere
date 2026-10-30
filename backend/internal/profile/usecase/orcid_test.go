package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

const testORCID = "0000-0002-1825-0097"

// orcidEnv is a Service over the fake store with a fake ORCID, its cache and limiter, and
// the fake OpenAlex of lookupEnv. Ada's profile has testORCID. The clock reads 15 June
// 2026 UTC.
type orcidEnv struct {
	svc          *Service
	store        *fakeStore
	orcid        *fakeORCID
	orcidCache   *fakeORCIDCache
	orcidLimiter *fakeLimiter
	source       *fakeSource
	cache        *fakeCache
	limiter      *fakeLimiter
}

func newORCIDEnv() orcidEnv {
	e := orcidEnv{
		store:        newFakeStore(testAccounts()...),
		orcid:        &fakeORCID{},
		orcidCache:   &fakeORCIDCache{},
		orcidLimiter: &fakeLimiter{},
		source:       &fakeSource{work: engineWork()},
		cache:        &fakeCache{},
		limiter:      &fakeLimiter{},
	}
	a := e.store.accounts[adaID]
	p := *a.Profile
	p.OrcidID = ptr(testORCID)
	a.Profile = &p
	e.store.accounts[adaID] = a
	e.svc = New(e.store,
		WithDOILookup(DOILookup{Source: e.source, Cache: e.cache, Limiter: e.limiter}),
		WithORCIDImport(ORCIDImport{Source: e.orcid, Cache: e.orcidCache, Limiter: e.orcidLimiter}))
	e.svc.now = func() time.Time { return time.Date(2026, time.June, 15, 12, 0, 0, 0, time.UTC) }
	return e
}

// cacheWorks puts works, already normalized, in the ORCID cache for Ada's iD.
func (e orcidEnv) cacheWorks(works ...domain.ORCIDWork) {
	e.orcidCache.lists = map[string][]domain.ORCIDWork{testORCID: works}
}

func work(putCode, title, doi string) domain.ORCIDWork {
	w := domain.ORCIDWork{PutCode: putCode, Title: title, Year: iptr(2020), Venue: ptr("ORCID Venue")}
	if doi != "" {
		w.DOI = ptr(doi)
		w.URL = ptr("https://doi.org/" + doi)
	}
	return w
}

func TestNormalizeORCID(t *testing.T) {
	tests := []struct {
		in      *string
		want    *string
		wantErr error
	}{
		{nil, nil, nil},
		{ptr("   "), nil, nil},
		{ptr("0000-0002-1825-0097"), ptr("0000-0002-1825-0097"), nil},
		{ptr(" https://orcid.org/0000-0002-1825-0097 "), ptr("0000-0002-1825-0097"), nil},
		{ptr("HTTP://ORCID.ORG/0000-0002-1825-0097"), ptr("0000-0002-1825-0097"), nil},
		{ptr("0000-0002-1694-233x"), ptr("0000-0002-1694-233X"), nil}, // check digit X
		{ptr("0000-0002-1825-0098"), nil, domain.ErrInvalidORCID},     // wrong check digit
		{ptr("0000000218250097"), nil, domain.ErrInvalidORCID},
		{ptr("0000-0002-1825-009"), nil, domain.ErrInvalidORCID},
		{ptr("https://example.org/0000-0002-1825-0097"), nil, domain.ErrInvalidORCID},
		{ptr("0000-0002-1825-0097/works"), nil, domain.ErrInvalidORCID},
	}
	for _, tt := range tests {
		t.Run(ptrValue(tt.in), func(t *testing.T) {
			got, err := NormalizeORCID(tt.in)
			if !errors.Is(err, tt.wantErr) || ptrValue(got) != ptrValue(tt.want) {
				t.Errorf("NormalizeORCID() = %q, %v; want %q, %v", ptrValue(got), err, ptrValue(tt.want), tt.wantErr)
			}
		})
	}
}

func TestSetOrcidID(t *testing.T) {
	e := newORCIDEnv()
	ctx := context.Background()

	got, err := e.svc.SetOrcidID(ctx, eveID, ptr("https://orcid.org/0000-0002-1694-233x"))
	if err != nil || ptrValue(got) != "0000-0002-1694-233X" {
		t.Fatalf("SetOrcidID() = %q, %v", ptrValue(got), err)
	}
	if v := e.store.accounts[eveID].Profile.OrcidID; ptrValue(v) != "0000-0002-1694-233X" {
		t.Errorf("stored %q", ptrValue(v))
	}
	if _, err := e.svc.SetOrcidID(ctx, eveID, ptr(testORCID)); !errors.Is(err, domain.ErrORCIDTaken) {
		t.Errorf("Ada's iD: err = %v, want ErrORCIDTaken", err)
	}
	if _, err := e.svc.SetOrcidID(ctx, eveID, ptr("1234")); !errors.Is(err, domain.ErrInvalidORCID) {
		t.Errorf("invalid iD: err = %v, want ErrInvalidORCID", err)
	}
	if got, err := e.svc.SetOrcidID(ctx, eveID, ptr("")); err != nil || got != nil || e.store.accounts[eveID].Profile.OrcidID != nil {
		t.Errorf("clearing: %q, %v; stored %v", ptrValue(got), err, e.store.accounts[eveID].Profile.OrcidID)
	}
	if _, err := e.svc.SetOrcidID(ctx, "no-such-user", nil); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown user: err = %v, want ErrNotFound", err)
	}
}

func TestListORCIDWorksNeedsAnORCIDiD(t *testing.T) {
	e := newORCIDEnv()
	for _, user := range []string{eveID, bobID} { // a profile without an iD; no profile row
		if _, err := e.svc.ListORCIDWorks(context.Background(), user); !errors.Is(err, domain.ErrORCIDNotSet) {
			t.Errorf("ListORCIDWorks(%s) err = %v, want ErrORCIDNotSet", user, err)
		}
		if _, err := e.svc.ImportORCIDWorks(context.Background(), user, []string{"1"}); !errors.Is(err, domain.ErrORCIDNotSet) {
			t.Errorf("ImportORCIDWorks(%s) err = %v, want ErrORCIDNotSet", user, err)
		}
	}
	if e.orcid.calls != 0 {
		t.Errorf("ORCID calls = %d, want 0", e.orcid.calls)
	}
}

func TestListORCIDWorksFetchesNormalizesAndMatches(t *testing.T) {
	e := newORCIDEnv()
	stored := e.store.givePublication(domain.Publication{DOI: ptr("10.1000/stored"), Title: "Stored", Source: domain.SourceDOI}, true, adaID)
	other := e.store.givePublication(domain.Publication{DOI: ptr("10.1000/other"), Title: "Bob's", Source: domain.SourceDOI}, true, bobID)
	e.orcid.works = []domain.ORCIDWork{
		{PutCode: " 1 ", Title: "  A\ntitle ", Year: iptr(1850), Venue: ptr("  J  "), DOI: ptr("https://doi.org/10.1000/STORED"), Type: ptr("journal-article")},
		{PutCode: "2", Title: "Other", Year: iptr(2027), DOI: ptr("10.1000/other"), URL: ptr("https://example.org/p")},
		{PutCode: "3", Title: "Bad DOI", DOI: ptr("not a doi"), URL: ptr("ftp://x")},
		{PutCode: "4", Title: strings.Repeat("word ", 150)},
		{PutCode: "5", Title: "   "},
		{PutCode: "", Title: "No put-code"},
	}
	list, err := e.svc.ListORCIDWorks(context.Background(), adaID)
	if err != nil {
		t.Fatal(err)
	}
	if list.OrcidID != testORCID || e.orcid.asked != testORCID || e.orcid.calls != 1 || e.orcidLimiter.used[adaID] != 1 {
		t.Fatalf("list for %q, asked %q, calls %d, counted %d", list.OrcidID, e.orcid.asked, e.orcid.calls, e.orcidLimiter.used[adaID])
	}
	if len(list.Items) != 4 {
		t.Fatalf("items = %+v, want 4 (no title and no put-code left out)", list.Items)
	}
	a, b, c, d := list.Items[0], list.Items[1], list.Items[2], list.Items[3]
	if a.PutCode != "1" || a.Title != "A title" || a.Year != nil || ptrValue(a.Venue) != "J" || ptrValue(a.DOI) != "10.1000/stored" ||
		ptrValue(a.URL) != "https://doi.org/10.1000/stored" || ptrValue(a.Type) != "journal-article" ||
		!a.ExistsAlready || a.PublicationID != stored || !a.LinkedToMe {
		t.Errorf("item 1 = %+v", a)
	}
	if *b.Year != 2027 || ptrValue(b.URL) != "https://example.org/p" || !b.ExistsAlready || b.PublicationID != other || b.LinkedToMe {
		t.Errorf("item 2 = %+v, want stored but not linked to Ada", b)
	}
	if c.DOI != nil || c.URL != nil || c.ExistsAlready {
		t.Errorf("item 3 = %+v, want the invalid DOI and link dropped", c)
	}
	if n := len([]rune(d.Title)); n > MaxPublicationTitleLength || strings.HasSuffix(d.Title, " ") {
		t.Errorf("item 4 title has %d characters, want cut at a word within %d", n, MaxPublicationTitleLength)
	}
	if got := e.orcidCache.lists[testORCID]; len(got) != 4 || got[0].Title != "A title" || e.orcidCache.ttl != time.Hour {
		t.Errorf("cache = %+v (ttl %s), want the normalized list for 1h", got, e.orcidCache.ttl)
	}
}

func TestListORCIDWorksCacheHitSkipsTheAdapter(t *testing.T) {
	e := newORCIDEnv()
	e.cacheWorks(work("1", "Cached", ""))
	e.orcidLimiter.used = map[string]int{adaID: ORCIDListsPerWindow} // a cached list needs no call left
	list, err := e.svc.ListORCIDWorks(context.Background(), adaID)
	if err != nil || len(list.Items) != 1 || list.Items[0].Title != "Cached" {
		t.Fatalf("ListORCIDWorks() = %+v, %v; want the cached list", list, err)
	}
	if e.orcid.calls != 0 || e.orcidLimiter.used[adaID] != ORCIDListsPerWindow {
		t.Errorf("ORCID calls %d, counted %d; want no call and nothing counted", e.orcid.calls, e.orcidLimiter.used[adaID])
	}
}

func TestListORCIDWorksRateLimit(t *testing.T) {
	e := newORCIDEnv()
	e.orcidLimiter.left = 20 * time.Minute
	for i := range ORCIDListsPerWindow {
		if _, err := e.svc.ListORCIDWorks(context.Background(), adaID); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
		delete(e.orcidCache.lists, testORCID) // each call misses the cache
	}
	_, err := e.svc.ListORCIDWorks(context.Background(), adaID)
	var limited *domain.RateLimitedError
	if !errors.As(err, &limited) || limited.RetryAfter != 20*time.Minute {
		t.Fatalf("call %d: err = %v, want RateLimitedError after 20m", ORCIDListsPerWindow+1, err)
	}
	if e.orcid.calls != ORCIDListsPerWindow {
		t.Errorf("ORCID calls = %d, want %d", e.orcid.calls, ORCIDListsPerWindow)
	}
}

func TestListORCIDWorksErrors(t *testing.T) {
	for _, wantErr := range []error{domain.ErrORCIDNotFound, domain.ErrMetadataUnavailable} {
		e := newORCIDEnv()
		e.orcid.err = wantErr
		if _, err := e.svc.ListORCIDWorks(context.Background(), adaID); !errors.Is(err, wantErr) {
			t.Errorf("err = %v, want %v", err, wantErr)
		}
		if len(e.orcidCache.lists) != 0 || e.orcidLimiter.used[adaID] != 1 {
			t.Errorf("%v: cached %v, counted %d; want nothing cached and the call counted", wantErr, e.orcidCache.lists, e.orcidLimiter.used[adaID])
		}
	}
	// Without an ORCID source, as without WithORCIDImport.
	e := newORCIDEnv()
	e.svc.orcid = ORCIDImport{}
	if _, err := e.svc.ListORCIDWorks(context.Background(), adaID); !errors.Is(err, domain.ErrMetadataUnavailable) {
		t.Errorf("no source: err = %v, want ErrMetadataUnavailable", err)
	}
}

func TestListORCIDWorksFailsOpenWithoutRedis(t *testing.T) {
	e := newORCIDEnv()
	e.orcidCache.err, e.orcidLimiter.err = errors.New("redis down"), errors.New("redis down")
	e.orcid.works = []domain.ORCIDWork{work("1", "T", "")}
	if list, err := e.svc.ListORCIDWorks(context.Background(), adaID); err != nil || len(list.Items) != 1 {
		t.Fatalf("ListORCIDWorks() = %+v, %v; want the list", list, err)
	}
}

func TestImportORCIDWorksInvalidPutCodes(t *testing.T) {
	e := newORCIDEnv()
	e.cacheWorks(work("1", "A", ""), work("2", "B", ""))
	tooMany := make([]string, MaxImportPutCodes+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprint(i)
	}
	tests := map[string][]string{
		"empty":       {},
		"over 100":    tooMany,
		"repeated":    {"1", "1"},
		"not in list": {"1", "3"},
	}
	for name, codes := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := e.svc.ImportORCIDWorks(context.Background(), adaID, codes); !errors.Is(err, domain.ErrInvalidPutCode) {
				t.Errorf("err = %v, want ErrInvalidPutCode", err)
			}
			if len(e.store.publications) != 0 {
				t.Errorf("stored %d publications, want 0", len(e.store.publications))
			}
		})
	}
}

func TestImportORCIDWorksLinksCreatesAndSkips(t *testing.T) {
	e := newORCIDEnv()
	mine := e.store.givePublication(domain.Publication{DOI: ptr("10.1000/mine"), Title: "Mine"}, true, adaID)
	bobs := e.store.givePublication(domain.Publication{DOI: ptr("10.1000/bobs"), Title: "Bob's"}, true, bobID)
	unconfirmed := e.store.givePublication(domain.Publication{DOI: ptr("10.1000/unconfirmed"), Title: "Pending"}, false, adaID)
	e.cacheWorks(
		work("1", "Mine", "10.1000/mine"),
		work("2", "Bob's", "10.1000/bobs"),
		work("3", "Pending", "10.1000/unconfirmed"),
		work("4", "New with DOI", "10.1000/new"),
		work("5", "No DOI", ""),
		work("6", "Same DOI again", "10.1000/new"),
	)
	res, err := e.svc.ImportORCIDWorks(context.Background(), adaID, []string{"1", "2", "3", "4", "5", "6"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 2 || res.Linked != 2 || res.Skipped != 2 || len(res.Items) != 6 {
		t.Fatalf("result = %+v, want 2 created, 2 linked, 2 skipped", res)
	}
	newID := res.Items[3].PublicationID
	want := []ImportedWork{
		{"1", ImportSkipped, mine},
		{"2", ImportLinked, bobs},
		{"3", ImportLinked, unconfirmed},
		{"4", ImportCreated, newID},
		{"5", ImportCreated, res.Items[4].PublicationID},
		{"6", ImportSkipped, newID},
	}
	for i, w := range want {
		if res.Items[i] != w {
			t.Errorf("item %d = %+v, want %+v", i, res.Items[i], w)
		}
	}
	for _, id := range []string{mine, bobs, unconfirmed, newID, res.Items[4].PublicationID} {
		if state, _ := e.store.PublicationLink(context.Background(), adaID, id); state != domain.Confirmed {
			t.Errorf("Ada's link to %s = %v, want confirmed", id, state)
		}
	}
	// The new DOI was fetched from OpenAlex, once, and has its abstract; the work without
	// a DOI comes from ORCID.
	if e.source.calls != 1 {
		t.Errorf("OpenAlex calls = %d, want 1", e.source.calls)
	}
	fetched := e.store.publications[newID]
	if fetched.Source != domain.SourceDOI || ptrValue(fetched.Abstract) != "We describe the engine." || fetched.Title != "Notes on the Analytical Engine" {
		t.Errorf("fetched record = %+v", fetched)
	}
	orcid := e.store.publications[res.Items[4].PublicationID]
	if orcid.Source != domain.SourceORCID || orcid.Title != "No DOI" || *orcid.Year != 2020 || ptrValue(orcid.Venue) != "ORCID Venue" ||
		orcid.DOI != nil || ptrValue(orcid.CreatedBy) != adaID {
		t.Errorf("ORCID record = %+v", orcid)
	}
	// An ORCID record cannot be edited (11.1 rule).
	if _, err := e.svc.UpdatePublication(context.Background(), adaID, orcid.ID, PublicationPatch{Title: Field[string]{Set: true, Value: "X"}}); !errors.Is(err, domain.ErrPublicationSharedReadonly) {
		t.Errorf("UpdatePublication() on an ORCID record err = %v, want ErrPublicationSharedReadonly", err)
	}
}

func TestImportORCIDWorksFallsBackToORCIDData(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"OpenAlex unavailable", domain.ErrMetadataUnavailable},
		{"OpenAlex does not know the DOI", domain.ErrWorkNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newORCIDEnv()
			e.source.err = tt.err
			e.cacheWorks(work("1", "From ORCID", "10.1000/a"))
			res, err := e.svc.ImportORCIDWorks(context.Background(), adaID, []string{"1"})
			if err != nil {
				t.Fatal(err)
			}
			p := e.store.publications[res.Items[0].PublicationID]
			if res.Created != 1 || p.Source != domain.SourceORCID || p.Title != "From ORCID" || ptrValue(p.DOI) != "10.1000/a" || p.Abstract != nil {
				t.Errorf("result %+v, record %+v; want an ORCID record", res, p)
			}
		})
	}
	t.Run("DOI lookups used up", func(t *testing.T) {
		e := newORCIDEnv()
		e.limiter.used = map[string]int{adaID: LookupsPerWindow}
		e.cacheWorks(work("1", "From ORCID", "10.1000/a"))
		res, err := e.svc.ImportORCIDWorks(context.Background(), adaID, []string{"1"})
		if err != nil {
			t.Fatal(err)
		}
		if p := e.store.publications[res.Items[0].PublicationID]; p.Source != domain.SourceORCID || e.source.calls != 0 {
			t.Errorf("record %+v, OpenAlex calls %d; want an ORCID record and no call", p, e.source.calls)
		}
	})
}

func TestImportORCIDWorksStopsCallingOpenAlexAfterAFailure(t *testing.T) {
	e := newORCIDEnv()
	e.source.errFor = map[string]error{"10.1000/a": domain.ErrWorkNotFound, "10.1000/b": domain.ErrMetadataUnavailable}
	e.cacheWorks(work("1", "A", "10.1000/a"), work("2", "B", "10.1000/b"), work("3", "C", "10.1000/c"))
	res, err := e.svc.ImportORCIDWorks(context.Background(), adaID, []string{"1", "2", "3"})
	if err != nil {
		t.Fatal(err)
	}
	if e.source.calls != 2 {
		t.Errorf("OpenAlex calls = %d, want 2 (not found goes on, unavailable stops)", e.source.calls)
	}
	for _, it := range res.Items {
		if p := e.store.publications[it.PublicationID]; p.Source != domain.SourceORCID {
			t.Errorf("%s: source %s, want orcid", it.PutCode, p.Source)
		}
	}
}

func TestImportORCIDWorksChecksTheLimitBeforeAnything(t *testing.T) {
	e := newORCIDEnv()
	for i := range MaxPublications - 1 {
		e.store.givePublication(domain.Publication{Title: fmt.Sprint("P", i)}, true, adaID)
	}
	already := e.store.givePublication(domain.Publication{DOI: ptr("10.1000/mine"), Title: "Mine"}, true, adaID) // 300th
	e.cacheWorks(work("1", "Mine", "10.1000/mine"), work("2", "New", "10.1000/new"))
	before := len(e.store.publications)

	_, err := e.svc.ImportORCIDWorks(context.Background(), adaID, []string{"1", "2"})
	if !errors.Is(err, domain.ErrPublicationLimitReached) {
		t.Fatalf("err = %v, want ErrPublicationLimitReached", err)
	}
	if len(e.store.publications) != before || e.source.calls != 0 || e.limiter.used[adaID] != 0 {
		t.Errorf("stored %d new, OpenAlex calls %d, lookups counted %d; want none",
			len(e.store.publications)-before, e.source.calls, e.limiter.used[adaID])
	}
	// Skipping the one already on the profile adds nothing, so it is allowed.
	res, err := e.svc.ImportORCIDWorks(context.Background(), adaID, []string{"1"})
	if err != nil || res.Skipped != 1 || res.Items[0].PublicationID != already {
		t.Errorf("ImportORCIDWorks([1]) = %+v, %v; want one skipped", res, err)
	}
}

func TestImportORCIDWorksFetchesTheListOnACacheMiss(t *testing.T) {
	e := newORCIDEnv()
	e.orcid.works = []domain.ORCIDWork{work("1", "A", "")}
	res, err := e.svc.ImportORCIDWorks(context.Background(), adaID, []string{"1"})
	if err != nil || res.Created != 1 {
		t.Fatalf("ImportORCIDWorks() = %+v, %v", res, err)
	}
	if e.orcid.calls != 1 || e.orcidLimiter.used[adaID] != 1 {
		t.Errorf("ORCID calls %d, counted %d; want 1 and 1", e.orcid.calls, e.orcidLimiter.used[adaID])
	}
	// Over the ORCID limit, an import that misses the cache is refused.
	e.orcidCache.lists = nil
	e.orcidLimiter.used[adaID] = ORCIDListsPerWindow
	var limited *domain.RateLimitedError
	if _, err := e.svc.ImportORCIDWorks(context.Background(), adaID, []string{"1"}); !errors.As(err, &limited) {
		t.Errorf("err = %v, want RateLimitedError", err)
	}
}
