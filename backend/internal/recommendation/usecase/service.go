// Package usecase holds the recommendation module's logic: who to show a user as possible
// collaborators (AI-2.1), with a score and shared tags (AI-2.2), and what to show instead
// when the profile is empty or not embedded yet (AI-4). It imports only its own domain.
package usecase

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
)

// Limits.
const (
	// MaxLimit is how many recommendations a list holds (AI-2.1).
	MaxLimit = 10
	// CacheTTL is how long a user's list is kept. The caller's own changes clear it at once;
	// this bounds how long other users' changes take to show.
	CacheTTL = 10 * time.Minute
)

// Result is what a user sees.
type Result struct {
	State   domain.State
	Missing []domain.Field // with StateNeedsProfile only
	Items   []domain.Recommendation
}

// Service makes recommendation lists.
type Service struct {
	store     domain.Store
	cache     domain.Cache
	relations domain.Relations
	scores    ScoreMap
	logger    *slog.Logger

	warned sync.Map // embedding model -> struct{}: models already warned about
}

// New returns a Service. scores must be valid (ScoreMap.Validate).
func New(store domain.Store, cache domain.Cache, relations domain.Relations, scores ScoreMap, logger *slog.Logger) *Service {
	return &Service{store: store, cache: cache, relations: relations, scores: scores, logger: logger}
}

// People returns userID's recommendations, at most limit (above MaxLimit is MaxLimit;
// below 1 is domain.ErrInvalidLimit).
//
// The state is checked in this order: no interests, skills or bio is StateNeedsProfile
// (AI-4.2), whatever else the profile has; then no embedding is StateProcessing; otherwise
// the list is ready. A profile with one interest or skill and no publications has an
// embedding made from that text, so it gets a list (AI-4.1).
func (s *Service) People(ctx context.Context, userID string, limit int) (Result, error) {
	if limit < 1 {
		return Result{}, domain.ErrInvalidLimit
	}
	limit = min(limit, MaxLimit)

	self, err := s.store.Self(ctx, userID)
	if err != nil {
		return Result{}, err
	}
	if missing := missingFields(self); len(missing) == 3 {
		return Result{State: domain.StateNeedsProfile, Missing: missing, Items: []domain.Recommendation{}}, nil
	}
	if self.Embedding == nil {
		return Result{State: domain.StateProcessing, Missing: []domain.Field{}, Items: []domain.Recommendation{}}, nil
	}
	s.checkModel(ctx, self.Embedding.Model)

	items, err := s.list(ctx, userID, self)
	if err != nil {
		return Result{}, err
	}
	if len(items) > limit {
		items = items[:limit]
	}
	if err := s.fillConnections(ctx, userID, items); err != nil {
		return Result{}, err
	}
	return Result{State: domain.StateReady, Missing: []domain.Field{}, Items: items}, nil
}

// missingFields lists the fields AI-4.2 asks for that the profile lacks.
func missingFields(self domain.Self) []domain.Field {
	missing := make([]domain.Field, 0, 3)
	if len(self.Interests) == 0 {
		missing = append(missing, domain.FieldInterests)
	}
	if len(self.Skills) == 0 {
		missing = append(missing, domain.FieldSkills)
	}
	if !self.HasBio {
		missing = append(missing, domain.FieldBio)
	}
	return missing
}

// list returns the kept list when it was made from the stored embedding, without the users
// who can no longer be recommended, or makes and keeps a new one.
func (s *Service) list(ctx context.Context, userID string, self domain.Self) ([]domain.Recommendation, error) {
	kept, found, err := s.cache.Get(ctx, userID)
	if err != nil {
		s.logger.WarnContext(ctx, "recommendation cache unavailable, ranking again", slog.Any("error", err))
	}
	if err == nil && found && kept.Stamp.Equal(*self.Embedding) {
		return s.stillEligible(ctx, userID, kept.Items)
	}
	items, err := s.rank(ctx, userID, self)
	if err != nil {
		return nil, err
	}
	if err := s.cache.Set(ctx, userID, domain.Kept{Stamp: *self.Embedding, Items: items}, CacheTTL); err != nil {
		s.logger.WarnContext(ctx, "recommendation list not cached", slog.Any("error", err))
	}
	return cloneItems(items), nil
}

// rank makes a new list.
//
// AI-2.4: the order is the cosine similarity of the two profile embeddings and nothing
// else. Citation, publication and connection counts, and any other popularity signal, are
// never read, so neither the order nor the score can favour a well-known researcher over
// one who is just starting (AI-4.1). The store returns candidates already in that order;
// the score is a fixed increasing function of the same cosine, so it cannot reorder them.
func (s *Service) rank(ctx context.Context, userID string, self domain.Self) ([]domain.Recommendation, error) {
	cands, err := s.store.Rank(ctx, userID, MaxLimit)
	if err != nil {
		return nil, err
	}
	items := make([]domain.Recommendation, len(cands))
	for i, c := range cands {
		items[i] = domain.Recommendation{
			Person: c.Person,
			Score:  s.scores.Score(c.Cosine),
			Shared: SharedTags(self.Interests, self.Skills, c.Interests, c.Skills),
		}
	}
	return items, nil
}

// stillEligible drops the kept users that were suspended, blocked, connected or asked
// since the list was made. The caller's own changes clear the list; this covers the
// changes of others, such as a moderator's suspension.
func (s *Service) stillEligible(ctx context.Context, userID string, kept []domain.Recommendation) ([]domain.Recommendation, error) {
	if len(kept) == 0 {
		return []domain.Recommendation{}, nil
	}
	ids := make([]string, len(kept))
	for i, it := range kept {
		ids[i] = it.UserID
	}
	ok, err := s.store.StillEligible(ctx, userID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Recommendation, 0, len(kept))
	for _, it := range kept {
		if ok[it.UserID] {
			out = append(out, it)
		}
	}
	return out, nil
}

// fillConnections sets each item's connection status, read now: it is never cached.
func (s *Service) fillConnections(ctx context.Context, userID string, items []domain.Recommendation) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.UserID
	}
	st, err := s.relations.Statuses(ctx, userID, ids)
	if err != nil {
		return err
	}
	for i := range items {
		status, found := st[items[i].UserID]
		if !found {
			status = domain.StatusNone
		}
		items[i].Connection = status
	}
	return nil
}

// checkModel warns, once per model, when the caller's embedding was made by another model
// than the one the score map was fitted for: the scores are then not calibrated and the
// map must be refitted (make calibrate-recs, docs/evaluation.md).
func (s *Service) checkModel(ctx context.Context, model string) {
	if model == s.scores.Model {
		return
	}
	if _, loaded := s.warned.LoadOrStore(model, struct{}{}); loaded {
		return
	}
	s.logger.WarnContext(ctx, "recommendation score map was fitted for another embedding model; refit it with make calibrate-recs",
		slog.String("embedding_model", model), slog.String("score_model", s.scores.Model))
}

// cloneItems copies the list so the caller may change items the cache adapter still holds.
func cloneItems(items []domain.Recommendation) []domain.Recommendation {
	out := make([]domain.Recommendation, len(items))
	copy(out, items)
	return out
}
