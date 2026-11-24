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

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// Hybrid project search (AI-5.1 for projects, AI-5.4).
const (
	// DefaultSearchAITimeout bounds the query embedding of a hybrid search; after it the
	// keyword search runs instead (AI-5.4). SEARCH_AI_TIMEOUT overrides it.
	DefaultSearchAITimeout = 1500 * time.Millisecond
	// QueryVectorTTL is how long a query vector is kept.
	QueryVectorTTL = 24 * time.Hour
)

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

// Search searches open projects by title, required skill and description (FR-10.3) and, in
// auto mode (an empty mode is auto), also by meaning (AI-5.1). It returns
// domain.ErrInvalidSearchMode for a mode other than auto or keyword, domain.ErrInvalidQuery
// for a missing text or one that is not 2 to 100 characters (keyword mode) or 2 to 300
// (auto mode) after trimming, domain.ErrInvalidPage for a page outside 1 to 50, and
// *domain.RateLimitedError after SearchesPerWindow searches in a SearchWindow, in either
// mode; invalid searches do not count. When the limit cannot be read, the search goes
// ahead. When the text cannot be embedded in time, the keyword search runs and the result
// says so (AI-5.4).
func (s *Service) Search(ctx context.Context, viewerID string, q *string, mode string, page int) (domain.SearchResult, error) {
	m := domain.SearchMode(mode)
	switch m {
	case "":
		m = domain.SearchAuto
	case domain.SearchAuto, domain.SearchKeyword:
	default:
		return domain.SearchResult{}, domain.ErrInvalidSearchMode
	}
	maxLen := MaxQueryLength
	if m == domain.SearchAuto {
		maxLen = MaxMeaningQuery
	}
	if q == nil {
		return domain.SearchResult{}, domain.ErrInvalidQuery
	}
	text, ok := cleanText(*q)
	if n := utf8.RuneCountInString(text); !ok || n < MinQueryLength || n > maxLen {
		return domain.SearchResult{}, domain.ErrInvalidQuery
	}
	if page < 1 || page > MaxSearchPage {
		return domain.SearchResult{}, domain.ErrInvalidPage
	}
	used, left, err := s.d.SearchLimiter.Used(ctx, viewerID)
	switch {
	case err != nil:
		s.d.Logger.WarnContext(ctx, "project search limit unavailable, allowing the search", slog.Any("error", err))
	case used >= SearchesPerWindow:
		return domain.SearchResult{}, &domain.RateLimitedError{RetryAfter: left}
	}
	if err := s.d.SearchLimiter.Record(ctx, viewerID, SearchWindow); err != nil {
		s.d.Logger.WarnContext(ctx, "project search limit not recorded", slog.Any("error", err))
	}
	sq := domain.SearchQuery{ViewerID: viewerID, Q: text, Limit: SearchPageSize, Offset: (page - 1) * SearchPageSize}
	if m == domain.SearchKeyword {
		res, err := s.d.Store.Search(ctx, sq)
		res.Mode = domain.ResultKeyword
		return res, err
	}
	v, ok := s.queryVector(ctx, text)
	if !ok {
		res, err := s.d.Store.Search(ctx, sq)
		res.Mode = domain.ResultKeywordFallback
		return res, err
	}
	res, err := s.d.Store.SearchHybrid(ctx, sq, v)
	res.Mode = domain.ResultHybrid
	return res, err
}

// queryVector returns the vector of the normalized text: the kept one, else a new one from
// the embedder within AITimeout, which is then kept for QueryVectorTTL. Cache errors are
// logged and ignored. false means the text could not be embedded; the first failure after
// a success is logged, and so is the first success after a failure. The log never holds the
// search text.
func (s *Service) queryVector(ctx context.Context, text string) (domain.QueryVector, bool) {
	d, ai := s.d, &s.ai
	key := NormalizeQueryText(text)
	if model := ai.lastModel(); d.VectorCache != nil && model != "" {
		values, ok, err := d.VectorCache.Get(ctx, model, key)
		switch {
		case err != nil:
			d.Logger.WarnContext(ctx, "query vector cache unavailable", slog.Any("error", err))
		case ok:
			return domain.QueryVector{Model: model, Values: values}, true
		}
	}
	if d.Embedder == nil {
		if ai.down.CompareAndSwap(false, true) {
			d.Logger.WarnContext(ctx, "semantic project search is off: the AI service is not configured; searching by keyword")
		}
		return domain.QueryVector{}, false
	}
	ectx, cancel := context.WithTimeout(ctx, d.AITimeout)
	v, err := d.Embedder.EmbedQuery(ectx, key)
	cancel()
	if err == nil && (v.Model == "" || len(v.Values) == 0) {
		err = errors.New("empty query vector")
	}
	if err != nil {
		if ctx.Err() != nil {
			return domain.QueryVector{}, false // the caller gave up; the service may be fine
		}
		if ai.down.CompareAndSwap(false, true) {
			d.Logger.WarnContext(ctx, "AI service not answering; searching projects by keyword until it is back", slog.Any("error", err))
		}
		return domain.QueryVector{}, false
	}
	if ai.down.CompareAndSwap(true, false) {
		d.Logger.InfoContext(ctx, "AI service answering again; searching projects by meaning")
	}
	ai.setModel(v.Model)
	if d.VectorCache != nil {
		if err := d.VectorCache.Set(ctx, key, v, QueryVectorTTL); err != nil {
			d.Logger.WarnContext(ctx, "query vector not kept", slog.Any("error", err))
		}
	}
	return v, true
}

// NormalizeQueryText is the text that is embedded and the cache key: lower case, with every
// run of whitespace one space. It must equal profile search's, so the two searches share
// kept vectors (cmd/api checks it).
func NormalizeQueryText(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

// CanSeeRecommendedResearchers returns nil when viewerID may see the project's recommended
// researchers (AI-2.3), domain.ErrNotFound when they cannot see the project, and
// domain.ErrForbidden when their role lacks ActManageMembers (owner only).
func (s *Service) CanSeeRecommendedResearchers(ctx context.Context, viewerID, projectID string) error {
	p, err := s.d.Store.Get(ctx, projectID, viewerID)
	if err != nil {
		return err
	}
	if !p.Access.Visible() || !s.d.Permissions.Can(p.MyRole, domain.ActView) {
		return domain.ErrNotFound
	}
	if !s.d.Permissions.Can(p.MyRole, domain.ActManageMembers) {
		return domain.ErrForbidden
	}
	return nil
}

// CanSeeRecommendedVenues answers whether viewerID may see the project's venue
// recommendations (AI-6): nil for a member whose role has the row "See venue recommendations
// for the project", else ErrNotFound, also when the caller can see the project page: the
// list is for members only and does not say whether the project exists (decisions.md,
// 2026-11-24).
func (s *Service) CanSeeRecommendedVenues(ctx context.Context, viewerID, projectID string) error {
	p, err := s.d.Store.Get(ctx, projectID, viewerID)
	if err != nil {
		return err
	}
	if !p.Access.Visible() || !s.d.Permissions.Can(p.MyRole, domain.ActSeeVenueRecommendations) {
		return domain.ErrNotFound
	}
	return nil
}
