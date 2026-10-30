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

// fakeSearcher records the query it gets.
type fakeSearcher struct {
	got       *domain.PeopleQuery
	maxFacets int
	res       domain.PeopleResults
}

func (f *fakeSearcher) SearchPeople(_ context.Context, q domain.PeopleQuery, maxFacets int) (domain.PeopleResults, error) {
	f.got, f.maxFacets = &q, maxFacets
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
		{"101 characters", SearchInput{Q: sp(strings.Repeat("a", 101)), Page: 1}, domain.PeopleQuery{}, domain.ErrInvalidQuery},
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
