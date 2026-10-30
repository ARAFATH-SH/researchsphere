package usecase

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// Researcher search limits (FR-6.1, FR-6.3).
const (
	MinQueryLength       = 2
	MaxQueryLength       = 100
	SearchPageSize       = 20 // FR-6.1
	MaxSearchPage        = 50
	MaxSearchInterests   = 10 // interest filters per search
	MaxSearchFacets      = 20 // options per facet, besides the selected interests
	SearchesPerWindow    = 60
	SearchWindow         = time.Minute
	maxInstitutionFilter = MaxInstitutionLength
)

// errSearchNotConfigured is the error of a search on a Service made without
// WithPeopleSearch.
var errSearchNotConfigured = errors.New("profile: people search is not configured")

// PeopleSearch is what researcher search needs: the searcher and the per-user limit.
// Every field is required.
type PeopleSearch struct {
	Searcher domain.PeopleSearcher
	Limiter  domain.SearchLimiter
	Logger   *slog.Logger
}

// WithPeopleSearch lets the Service search researchers (FR-6).
func WithPeopleSearch(p PeopleSearch) Option {
	return func(s *Service) { s.search = p }
}

// SearchInput is a researcher search as the caller sends it. A nil Q is a missing one.
type SearchInput struct {
	Q           *string
	Institution *string
	Interests   []string
	Page        int
}

// SearchPeople searches verified, not suspended researchers by name, institution,
// research interest and headline (FR-6.1, FR-6.2), narrowed by institution and research
// interests (FR-6.3). It returns domain.ErrInvalidQuery for a missing text or one that is
// not 2 to 100 characters after trimming, domain.ErrInvalidSearchFilter for a bad filter,
// domain.ErrInvalidPage for a page outside 1 to 50, and *domain.RateLimitedError after
// SearchesPerWindow searches in a SearchWindow. When the limit cannot be read, the search
// goes ahead.
func (s *Service) SearchPeople(ctx context.Context, userID string, in SearchInput) (domain.PeopleResults, error) {
	q, err := searchQuery(in)
	if err != nil {
		return domain.PeopleResults{}, err
	}
	p := s.search
	if p.Searcher == nil || p.Limiter == nil {
		return domain.PeopleResults{}, errSearchNotConfigured
	}
	used, left, err := p.Limiter.Used(ctx, userID)
	switch {
	case err != nil:
		s.logger().WarnContext(ctx, "search rate limit unavailable, allowing the search", slog.Any("error", err))
	case used >= SearchesPerWindow:
		return domain.PeopleResults{}, &domain.RateLimitedError{RetryAfter: left}
	}
	if err := p.Limiter.Record(ctx, userID, SearchWindow); err != nil {
		s.logger().WarnContext(ctx, "search rate limit not recorded", slog.Any("error", err))
	}
	return p.Searcher.SearchPeople(ctx, q, MaxSearchFacets)
}

// searchQuery validates and normalizes in.
func searchQuery(in SearchInput) (domain.PeopleQuery, error) {
	if in.Q == nil {
		return domain.PeopleQuery{}, domain.ErrInvalidQuery
	}
	text := strings.TrimSpace(*in.Q)
	if n := utf8.RuneCountInString(text); n < MinQueryLength || n > MaxQueryLength || !utf8.ValidString(text) {
		return domain.PeopleQuery{}, domain.ErrInvalidQuery
	}
	q := domain.PeopleQuery{Q: text, Page: in.Page, PageSize: SearchPageSize}
	if q.Page < 1 || q.Page > MaxSearchPage {
		return domain.PeopleQuery{}, domain.ErrInvalidPage
	}
	if in.Institution != nil {
		inst := strings.TrimSpace(*in.Institution)
		if utf8.RuneCountInString(inst) > maxInstitutionFilter || !utf8.ValidString(inst) {
			return domain.PeopleQuery{}, domain.ErrInvalidSearchFilter
		}
		if inst != "" {
			q.Institution = &inst
		}
	}
	seen := make(map[string]bool, len(in.Interests))
	for _, raw := range in.Interests {
		name, err := NormalizeTagName(raw)
		if err != nil {
			return domain.PeopleQuery{}, domain.ErrInvalidSearchFilter
		}
		if key := strings.ToLower(name); !seen[key] {
			seen[key] = true
			q.Interests = append(q.Interests, name)
		}
	}
	if len(q.Interests) > MaxSearchInterests {
		return domain.PeopleQuery{}, domain.ErrInvalidSearchFilter
	}
	return q, nil
}
