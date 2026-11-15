package usecase

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// fakeSearcher records the query it gets and which search ran.
type fakeSearcher struct {
	got       *domain.PeopleQuery
	maxFacets int
	res       domain.PeopleResults
	hybrid    int                // hybrid searches run
	keyword   int                // keyword searches run
	vector    domain.QueryVector // of the last hybrid search
}

func (f *fakeSearcher) SearchPeople(_ context.Context, q domain.PeopleQuery, maxFacets int) (domain.PeopleResults, error) {
	f.got, f.maxFacets = &q, maxFacets
	f.keyword++
	return f.res, nil
}

func (f *fakeSearcher) SearchPeopleHybrid(_ context.Context, q domain.PeopleQuery, v domain.QueryVector, maxFacets int) (domain.PeopleResults, error) {
	f.got, f.maxFacets, f.vector = &q, maxFacets, v
	f.hybrid++
	return f.res, nil
}

func newSearchService(limiter *fakeLimiter) (*Service, *fakeSearcher) {
	searcher := &fakeSearcher{res: domain.PeopleResults{Total: 1}}
	return New(nil, WithPeopleSearch(PeopleSearch{Searcher: searcher, Limiter: limiter})), searcher
}

func sp(s string) *string { return &s }

// The text is trimmed and must then be 2 to 100 characters (FR-6.1); filters are
// normalized (FR-6.3); the page is 1 to 50.
func TestSearchPeopleValidation(t *testing.T) {
	tests := []struct {
		name    string
		in      SearchInput
		want    domain.PeopleQuery
		wantErr error
	}{
		{"trimmed text", SearchInput{Q: sp("  Ada  "), Page: 1},
			domain.PeopleQuery{Q: "Ada", Page: 1, PageSize: 20}, nil},
		{"two characters", SearchInput{Q: sp("Al"), Page: 1}, domain.PeopleQuery{Q: "Al", Page: 1, PageSize: 20}, nil},
		{"100 characters", SearchInput{Q: sp(strings.Repeat("é", 100)), Page: 50},
			domain.PeopleQuery{Q: strings.Repeat("é", 100), Page: 50, PageSize: 20}, nil},
		{"missing text", SearchInput{Page: 1}, domain.PeopleQuery{}, domain.ErrInvalidQuery},
		{"empty text", SearchInput{Q: sp(""), Page: 1}, domain.PeopleQuery{}, domain.ErrInvalidQuery},
		{"one character after trimming", SearchInput{Q: sp("  a  "), Page: 1}, domain.PeopleQuery{}, domain.ErrInvalidQuery},
		{"only spaces", SearchInput{Q: sp("     "), Page: 1}, domain.PeopleQuery{}, domain.ErrInvalidQuery},
		{"101 characters in keyword mode", SearchInput{Q: sp(strings.Repeat("a", 101)), Mode: "keyword", Page: 1},
			domain.PeopleQuery{}, domain.ErrInvalidQuery},
		{"101 characters in auto mode", SearchInput{Q: sp(strings.Repeat("a", 101)), Page: 1},
			domain.PeopleQuery{Q: strings.Repeat("a", 101), Page: 1, PageSize: 20}, nil},
		{"300 characters in auto mode", SearchInput{Q: sp(strings.Repeat("ক", 300)), Mode: "auto", Page: 1},
			domain.PeopleQuery{Q: strings.Repeat("ক", 300), Page: 1, PageSize: 20}, nil},
		{"301 characters in auto mode", SearchInput{Q: sp(strings.Repeat("a", 301)), Page: 1},
			domain.PeopleQuery{}, domain.ErrInvalidQuery},
		{"300 characters after trimming", SearchInput{Q: sp("  " + strings.Repeat("a", 300) + "  "), Page: 1},
			domain.PeopleQuery{Q: strings.Repeat("a", 300), Page: 1, PageSize: 20}, nil},
		{"unknown mode", SearchInput{Q: sp("Ada"), Mode: "semantic", Page: 1}, domain.PeopleQuery{}, domain.ErrInvalidSearchMode},
		{"mode in upper case", SearchInput{Q: sp("Ada"), Mode: "AUTO", Page: 1}, domain.PeopleQuery{}, domain.ErrInvalidSearchMode},
		{"not UTF-8", SearchInput{Q: sp("a\xffb"), Page: 1}, domain.PeopleQuery{}, domain.ErrInvalidQuery},
		{"page 0", SearchInput{Q: sp("Ada"), Page: 0}, domain.PeopleQuery{}, domain.ErrInvalidPage},
		{"page 51", SearchInput{Q: sp("Ada"), Page: 51}, domain.PeopleQuery{}, domain.ErrInvalidPage},
		{"institution trimmed", SearchInput{Q: sp("Ada"), Institution: sp(" MIT "), Page: 1},
			domain.PeopleQuery{Q: "Ada", Institution: sp("MIT"), Page: 1, PageSize: 20}, nil},
		{"empty institution is no filter", SearchInput{Q: sp("Ada"), Institution: sp("  "), Page: 1},
			domain.PeopleQuery{Q: "Ada", Page: 1, PageSize: 20}, nil},
		{"institution too long", SearchInput{Q: sp("Ada"), Institution: sp(strings.Repeat("x", 201)), Page: 1},
			domain.PeopleQuery{}, domain.ErrInvalidSearchFilter},
		{"interests normalized, duplicates dropped ignoring case",
			SearchInput{Q: sp("Ada"), Interests: []string{" Eye   health ", "NLP", "eye health"}, Page: 1},
			domain.PeopleQuery{Q: "Ada", Interests: []string{"Eye health", "NLP"}, Page: 1, PageSize: 20}, nil},
		{"empty interest", SearchInput{Q: sp("Ada"), Interests: []string{" "}, Page: 1},
			domain.PeopleQuery{}, domain.ErrInvalidSearchFilter},
		{"interest too long", SearchInput{Q: sp("Ada"), Interests: []string{strings.Repeat("x", 61)}, Page: 1},
			domain.PeopleQuery{}, domain.ErrInvalidSearchFilter},
		{"11 interests", SearchInput{Q: sp("Ada"), Interests: strings.Split("a b c d e f g h i j k", " "), Page: 1},
			domain.PeopleQuery{}, domain.ErrInvalidSearchFilter},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := &fakeLimiter{}
			s, searcher := newSearchService(limiter)
			res, err := s.SearchPeople(t.Context(), adaID, tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if searcher.got != nil || limiter.used[adaID] != 0 {
					t.Errorf("an invalid search reached the searcher or the limiter")
				}
				return
			}
			tt.want.ViewerID = adaID
			if !reflect.DeepEqual(*searcher.got, tt.want) {
				t.Errorf("query = %+v, want %+v", *searcher.got, tt.want)
			}
			if searcher.maxFacets != MaxSearchFacets || res.Total != 1 {
				t.Errorf("maxFacets = %d, total = %d", searcher.maxFacets, res.Total)
			}
		})
	}
}

// Each user may search 60 times a minute; the 61st gets the time left.
func TestSearchPeopleRateLimit(t *testing.T) {
	limiter := &fakeLimiter{left: 40 * time.Second}
	s, searcher := newSearchService(limiter)
	in := SearchInput{Q: sp("Ada"), Page: 1}
	for i := range SearchesPerWindow {
		if _, err := s.SearchPeople(t.Context(), adaID, in); err != nil {
			t.Fatalf("search %d: %v", i+1, err)
		}
	}
	searcher.got = nil
	_, err := s.SearchPeople(t.Context(), adaID, in)
	var limited *domain.RateLimitedError
	if !errors.As(err, &limited) || limited.RetryAfter != 40*time.Second {
		t.Fatalf("err = %v, want RateLimitedError with 40s left", err)
	}
	if searcher.got != nil {
		t.Error("a limited search reached the searcher")
	}
	if _, err := s.SearchPeople(t.Context(), "someone-else", in); err != nil {
		t.Errorf("another user's search: %v", err)
	}
}

// When Redis does not answer, searches go ahead (fail open, like the DOI limit).
func TestSearchPeopleLimiterDown(t *testing.T) {
	s, searcher := newSearchService(&fakeLimiter{err: errors.New("redis down")})
	if _, err := s.SearchPeople(t.Context(), adaID, SearchInput{Q: sp("Ada"), Page: 1}); err != nil || searcher.got == nil {
		t.Errorf("err = %v, searched = %v; want the search to run", err, searcher.got != nil)
	}
}

func TestSearchPeopleNotConfigured(t *testing.T) {
	if _, err := New(nil).SearchPeople(t.Context(), adaID, SearchInput{Q: sp("Ada"), Page: 1}); err == nil {
		t.Error("err = nil, want an error")
	}
}

// Each result carries the searcher's link with that person, read for the whole page in
// one lookup (FR-7); without a lookup the own row is self and the rest none.
func TestSearchPeopleFillsConnections(t *testing.T) {
	const reqID = "00000000-0000-0000-0000-0000000000a1"
	hits := func() []domain.PersonHit {
		return []domain.PersonHit{{UserID: adaID}, {UserID: bobID}, {UserID: eveID}}
	}
	conns := &fakeConnections{byUser: map[string]domain.ConnectionRelation{
		adaID: {Status: domain.ConnectionPendingSent, RequestID: sp(reqID)},
		bobID: {Status: domain.ConnectionConnected},
		eveID: {Status: domain.ConnectionSelf},
	}}
	searcher := &fakeSearcher{res: domain.PeopleResults{Hits: hits(), Total: 3}}
	svc := New(nil, WithPeopleSearch(PeopleSearch{Searcher: searcher, Limiter: &fakeLimiter{}}), WithConnections(conns))
	res, err := svc.SearchPeople(t.Context(), eveID, SearchInput{Q: sp("ada"), Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(conns.lists) != 1 || !reflect.DeepEqual(conns.lists[0], []string{adaID, bobID, eveID}) {
		t.Errorf("Relations calls %v, want one with the whole page", conns.lists)
	}
	for _, h := range res.Hits {
		if !reflect.DeepEqual(h.Connection, conns.byUser[h.UserID]) {
			t.Errorf("%s: %+v, want %+v", h.UserID, h.Connection, conns.byUser[h.UserID])
		}
	}

	searcher.res.Hits = hits()
	svc = New(nil, WithPeopleSearch(PeopleSearch{Searcher: searcher, Limiter: &fakeLimiter{}}))
	res, _ = svc.SearchPeople(t.Context(), eveID, SearchInput{Q: sp("ada"), Page: 1})
	want := []domain.ConnectionStatus{domain.ConnectionNone, domain.ConnectionNone, domain.ConnectionSelf}
	for i, h := range res.Hits {
		if h.Connection.Status != want[i] || h.Connection.RequestID != nil {
			t.Errorf("no lookup, %s: %+v, want %s", h.UserID, h.Connection, want[i])
		}
	}

	conns.err = errors.New("boom")
	searcher.res.Hits = hits()
	svc = New(nil, WithPeopleSearch(PeopleSearch{Searcher: searcher, Limiter: &fakeLimiter{}}), WithConnections(conns))
	if _, err := svc.SearchPeople(t.Context(), eveID, SearchInput{Q: sp("ada"), Page: 1}); !errors.Is(err, conns.err) {
		t.Errorf("lookup error: %v, want it returned", err)
	}
}
