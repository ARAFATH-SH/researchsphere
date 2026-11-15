package usecase

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// Researcher search limits (FR-6.1, FR-6.3).
const (
	MinQueryLength       = 2
	MaxQueryLength       = 100 // keyword mode (FR-6)
	MaxMeaningQuery      = 300 // auto mode (AI-5.1)
	SearchPageSize       = 20  // FR-6.1
	MaxSearchPage        = 50
	MaxSearchInterests   = 10 // interest filters per search
	MaxSearchFacets      = 20 // options per facet, besides the selected interests
	SearchesPerWindow    = 60
	SearchWindow         = time.Minute
	maxInstitutionFilter = MaxInstitutionLength

	// DefaultSearchAITimeout bounds the query embedding of a hybrid search; after it the
	// keyword search runs instead (AI-5.4). SEARCH_AI_TIMEOUT overrides it.
	DefaultSearchAITimeout = 1500 * time.Millisecond
	// QueryVectorTTL is how long a query vector is kept.
	QueryVectorTTL = 24 * time.Hour
)

// errSearchNotConfigured is the error of a search on a Service made without
// WithPeopleSearch.
var errSearchNotConfigured = errors.New("profile: people search is not configured")

// PeopleSearch is what researcher search needs. Searcher and Limiter are required.
// Without an Embedder every auto search falls back to keywords (AI-5.4); without a Cache
// every query is embedded. AITimeout 0 means DefaultSearchAITimeout.
type PeopleSearch struct {
	Searcher  domain.PeopleSearcher
	Limiter   domain.SearchLimiter
	Embedder  domain.QueryEmbedder
	Cache     domain.QueryVectorCache
	AITimeout time.Duration
	Logger    *slog.Logger
}

// WithPeopleSearch lets the Service search researchers (FR-6, AI-5).
func WithPeopleSearch(p PeopleSearch) Option {
	return func(s *Service) {
		if p.AITimeout <= 0 {
			p.AITimeout = DefaultSearchAITimeout
		}
		s.search = p
		s.searchAI = &searchAI{}
	}
}

// searchAI is the state hybrid search keeps between calls.
type searchAI struct {
	down  atomic.Bool // the last query embedding failed; logged once until one succeeds
	mu    sync.Mutex
	model string // the model of the last query vector made, the cache key's model
}

func (a *searchAI) lastModel() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.model
}

func (a *searchAI) setModel(m string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.model = m
}

// SearchInput is a researcher search as the caller sends it. A nil Q is a missing one;
// an empty Mode is auto.
type SearchInput struct {
	Q           *string
	Mode        string
	Institution *string
	Interests   []string
	Page        int
}

// SearchPeople searches verified, not suspended researchers by name, institution,
// research interest and headline (FR-6.1, FR-6.2), narrowed by institution and research
// interests (FR-6.3), and in auto mode also by meaning (AI-5.1, AI-5.2). It returns
// domain.ErrInvalidSearchMode for a mode other than auto or keyword,
// domain.ErrInvalidQuery for a missing text or one that is not 2 to 100 characters (keyword
// mode) or 2 to 300 (auto mode) after trimming, domain.ErrInvalidSearchFilter for a bad
// filter, domain.ErrInvalidPage for a page outside 1 to 50, and *domain.RateLimitedError
// after SearchesPerWindow searches in a SearchWindow. When the limit cannot be read, the
// search goes ahead. When the query cannot be embedded in time, the keyword search runs
// and the results say so (AI-5.4). Each result carries the searcher's connection with that
// person. Nobody finds a person who has blocked them (FR-16.9).
func (s *Service) SearchPeople(ctx context.Context, userID string, in SearchInput) (domain.PeopleResults, error) {
	mode := domain.SearchMode(in.Mode)
	switch mode {
	case "":
		mode = domain.SearchAuto
	case domain.SearchAuto, domain.SearchKeyword:
	default:
		return domain.PeopleResults{}, domain.ErrInvalidSearchMode
	}
	maxLen := MaxQueryLength
	if mode == domain.SearchAuto {
		maxLen = MaxMeaningQuery
	}
	q, err := searchQuery(in, maxLen)
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
	q.ViewerID = userID
	var res domain.PeopleResults
	if mode == domain.SearchKeyword {
		res, err = p.Searcher.SearchPeople(ctx, q, MaxSearchFacets)
		res.Mode = domain.ResultKeyword
	} else {
		res, err = s.searchHybrid(ctx, q)
	}
	if err != nil {
		return domain.PeopleResults{}, err
	}
	return res, s.fillConnections(ctx, userID, res.Hits)
}

// searchHybrid embeds the text and runs the hybrid search; when the text cannot be
// embedded it runs the keyword search instead (AI-5.4).
func (s *Service) searchHybrid(ctx context.Context, q domain.PeopleQuery) (domain.PeopleResults, error) {
	v, ok := s.queryVector(ctx, q.Q)
	if !ok {
		res, err := s.search.Searcher.SearchPeople(ctx, q, MaxSearchFacets)
		res.Mode = domain.ResultKeywordFallback
		return res, err
	}
	res, err := s.search.Searcher.SearchPeopleHybrid(ctx, q, v, MaxSearchFacets)
	res.Mode = domain.ResultHybrid
	return res, err
}

// queryVector returns the vector of the normalized text: the kept one, else a new one from
// the embedder within AITimeout, which is then kept for QueryVectorTTL. Cache errors are
// logged and ignored. false means the text could not be embedded; the first failure after
// a success is logged, and so is the first success after a failure.
func (s *Service) queryVector(ctx context.Context, text string) (domain.QueryVector, bool) {
	p, ai := s.search, s.searchAI
	key := NormalizeQueryText(text)
	if model := ai.lastModel(); p.Cache != nil && model != "" {
		values, ok, err := p.Cache.Get(ctx, model, key)
		switch {
		case err != nil:
			s.logger().WarnContext(ctx, "query vector cache unavailable", slog.Any("error", err))
		case ok:
			return domain.QueryVector{Model: model, Values: values}, true
		}
	}
	if p.Embedder == nil {
		if ai.down.CompareAndSwap(false, true) {
			s.logger().WarnContext(ctx, "semantic search is off: the AI service is not configured; searching by keyword")
		}
		return domain.QueryVector{}, false
	}
	ectx, cancel := context.WithTimeout(ctx, p.AITimeout)
	v, err := p.Embedder.EmbedQuery(ectx, key)
	cancel()
	if err == nil && (v.Model == "" || len(v.Values) == 0) {
		err = errors.New("empty query vector")
	}
	if err != nil {
		if ctx.Err() != nil {
			return domain.QueryVector{}, false // the caller gave up; the service may be fine
		}
		if ai.down.CompareAndSwap(false, true) {
			s.logger().WarnContext(ctx, "AI service not answering; searching by keyword until it is back", slog.Any("error", err))
		}
		return domain.QueryVector{}, false
	}
	if ai.down.CompareAndSwap(true, false) {
		s.logger().InfoContext(ctx, "AI service answering again; searching by meaning")
	}
	ai.setModel(v.Model)
	if p.Cache != nil {
		if err := p.Cache.Set(ctx, key, v, QueryVectorTTL); err != nil {
			s.logger().WarnContext(ctx, "query vector not kept", slog.Any("error", err))
		}
	}
	return v, true
}

// NormalizeQueryText is the text that is embedded and the cache key: lower case, with
// every run of whitespace one space. The search text is already trimmed.
func NormalizeQueryText(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

// fillConnections sets the searcher's link with each person of the page, with one lookup
// for the whole page (FR-7).
func (s *Service) fillConnections(ctx context.Context, viewerID string, hits []domain.PersonHit) error {
	if s.conns == nil || len(hits) == 0 {
		for i := range hits {
			hits[i].Connection = noConnection(viewerID, hits[i].UserID)
		}
		return nil
	}
	ids := make([]string, len(hits))
	for i, h := range hits {
		ids[i] = h.UserID
	}
	rel, err := s.conns.Relations(ctx, viewerID, ids)
	if err != nil {
		return err
	}
	for i, h := range hits {
		hits[i].Connection = rel[h.UserID]
	}
	return nil
}

// searchQuery validates and normalizes in.
func searchQuery(in SearchInput, maxLen int) (domain.PeopleQuery, error) {
	if in.Q == nil {
		return domain.PeopleQuery{}, domain.ErrInvalidQuery
	}
	text := strings.TrimSpace(*in.Q)
	if n := utf8.RuneCountInString(text); n < MinQueryLength || n > maxLen || !utf8.ValidString(text) {
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
