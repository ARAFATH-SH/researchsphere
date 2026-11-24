package usecase

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
)

// Venue recommendations (AI-6.1 to AI-6.4; slice 25, chunk 25.1).

// Venue list sizes.
const (
	// VenueLimit is how many venues with a deadline a project is shown (AI-6.1).
	VenueLimit = 5
	// JournalLimit is how many rolling journals are shown at most, beside them.
	JournalLimit = 3
)

// errVenuesNotConfigured is the error of a venue list on a Service made without WithVenues.
var errVenuesNotConfigured = errors.New("recommendation: venue matching is not configured")

// VenueDeps is what venue recommendations need. Store, Cache, Access and Scores are required.
type VenueDeps struct {
	Store  domain.VenueStore
	Cache  domain.VenueCache
	Access domain.VenueAccess
	// Scores maps the cosine of a project's title-and-abstract embedding and a venue's
	// embedding to the 0 to 100 score (make calibrate-venues, docs/evaluation.md).
	Scores ScoreMap
	// JournalMinScore hides a rolling journal scoring below it, so a poor fit never shows
	// just to fill the list; 0 shows every one.
	JournalMinScore int
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// WithVenues lets the Service recommend venues to projects.
func (s *Service) WithVenues(d VenueDeps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	s.venues = &d
	return s
}

// VenueResult is what a project's member sees (AI-6).
type VenueResult struct {
	State    domain.State // StateReady, StateNeedsAbstract or StateProcessing
	Venues   []domain.VenueRecommendation
	Journals []domain.VenueRecommendation
	Notice   string // always domain.VenueNotice (AI-6.4)
}

// ProjectVenues returns the venues most similar to the project's title and abstract, for its
// members (AI-6.1). It returns domain.ErrProjectNotFound for anyone else. The state is
// checked in this order: no abstract is StateNeedsAbstract (AI-6.3), whatever else the
// project has; then no title-and-abstract embedding is StateProcessing; otherwise ready.
func (s *Service) ProjectVenues(ctx context.Context, userID, projectID string) (VenueResult, error) {
	d := s.venues
	if d == nil {
		return VenueResult{}, errVenuesNotConfigured
	}
	if err := d.Access.CanSeeRecommendedVenues(ctx, userID, projectID); err != nil {
		return VenueResult{}, err
	}
	self, err := d.Store.VenueSelf(ctx, projectID)
	if err != nil {
		return VenueResult{}, err
	}
	res := VenueResult{Venues: []domain.VenueRecommendation{}, Journals: []domain.VenueRecommendation{}, Notice: domain.VenueNotice}
	switch {
	case !self.HasAbstract:
		res.State = domain.StateNeedsAbstract
		return res, nil
	case self.Embedding == nil:
		res.State = domain.StateProcessing
		return res, nil
	}
	s.checkModel(ctx, self.Embedding.Model)

	now := d.Now()
	venues, journals, err := s.venueLists(ctx, projectID, *self.Embedding, now)
	if err != nil {
		return VenueResult{}, err
	}
	for i := range venues {
		venues[i].DaysLeft = daysLeft(venues[i].Deadline, now)
	}
	res.State, res.Venues, res.Journals = domain.StateReady, venues, journals
	return res, nil
}

// venueLists returns the kept lists when they were made from the stored embedding at the
// current venues generation and no listed deadline has passed since, without the venues
// removed or past since; otherwise it makes and keeps new ones. When the generation cannot
// be read, the lists are made and not kept.
func (s *Service) venueLists(ctx context.Context, projectID string, stamp domain.Stamp, now time.Time) (venues, journals []domain.VenueRecommendation, err error) {
	d := s.venues
	gen, genErr := d.Cache.VenueGeneration(ctx)
	if genErr != nil {
		s.logger.WarnContext(ctx, "venue recommendation cache unavailable, ranking again", slog.Any("error", genErr))
	} else {
		kept, found, err := d.Cache.GetVenues(ctx, projectID)
		if err != nil {
			s.logger.WarnContext(ctx, "venue recommendation cache unavailable, ranking again", slog.Any("error", err))
		}
		if err == nil && found && kept.Stamp.Equal(stamp) && kept.Generation == gen && keptFresh(kept.Venues, now) {
			return s.venuesStillEligible(ctx, kept.Venues, kept.Journals)
		}
	}
	venues, journals, err = s.rankVenues(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	if genErr == nil {
		if ttl := venueTTL(venues, now); ttl > 0 {
			kept := domain.KeptVenues{Stamp: stamp, Generation: gen, Venues: venues, Journals: journals}
			if err := d.Cache.SetVenues(ctx, projectID, kept, ttl); err != nil {
				s.logger.WarnContext(ctx, "venue recommendation list not cached", slog.Any("error", err))
			}
		}
	}
	return cloneVenues(venues), cloneVenues(journals), nil
}

// rankVenues makes new lists.
//
// AI-2.4: the order is the cosine similarity of the project's title-and-abstract embedding
// and each venue's embedding and nothing else; the score is a fixed increasing function of
// the same cosine. Journals below JournalMinScore are left out, so the list may be empty.
func (s *Service) rankVenues(ctx context.Context, projectID string) (venues, journals []domain.VenueRecommendation, err error) {
	d := s.venues
	vc, err := d.Store.RankVenues(ctx, projectID, VenueLimit)
	if err != nil {
		return nil, nil, err
	}
	jc, err := d.Store.RankJournals(ctx, projectID, JournalLimit)
	if err != nil {
		return nil, nil, err
	}
	venues = make([]domain.VenueRecommendation, 0, len(vc))
	for _, c := range vc {
		venues = append(venues, domain.VenueRecommendation{VenueCard: c.VenueCard, Score: d.Scores.Score(c.Cosine)})
	}
	journals = make([]domain.VenueRecommendation, 0, len(jc))
	for _, c := range jc {
		if score := d.Scores.Score(c.Cosine); score >= d.JournalMinScore {
			journals = append(journals, domain.VenueRecommendation{VenueCard: c.VenueCard, Score: score})
		}
	}
	return venues, journals, nil
}

// venuesStillEligible drops the kept venues removed or past since the lists were made. A
// venue change raises the generation, so this only matters when raising it failed.
func (s *Service) venuesStillEligible(ctx context.Context, venues, journals []domain.VenueRecommendation) ([]domain.VenueRecommendation, []domain.VenueRecommendation, error) {
	ids := make([]string, 0, len(venues)+len(journals))
	for _, v := range venues {
		ids = append(ids, v.ID)
	}
	for _, v := range journals {
		ids = append(ids, v.ID)
	}
	if len(ids) == 0 {
		return []domain.VenueRecommendation{}, []domain.VenueRecommendation{}, nil
	}
	ok, err := s.venues.Store.VenuesStillEligible(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	keep := func(in []domain.VenueRecommendation) []domain.VenueRecommendation {
		out := make([]domain.VenueRecommendation, 0, len(in))
		for _, v := range in {
			if ok[v.ID] {
				out = append(out, v)
			}
		}
		return out
	}
	return keep(venues), keep(journals), nil
}

// earliestDeadline is the first deadline of the listed venues; ok is false when none has one.
func earliestDeadline(venues []domain.VenueRecommendation) (time.Time, bool) {
	var first time.Time
	found := false
	for _, v := range venues {
		if v.Deadline != nil && (!found || v.Deadline.Before(first)) {
			first, found = *v.Deadline, true
		}
	}
	return first, found
}

// keptFresh reports whether no listed deadline has passed at now: once one has, the venue
// that would take its place must be found, so the lists are made again.
func keptFresh(venues []domain.VenueRecommendation, now time.Time) bool {
	first, ok := earliestDeadline(venues)
	return !ok || now.Before(first)
}

// venueTTL is how long new lists may be kept: CacheTTL, or less when a listed deadline passes
// sooner. 0 or less means they must not be kept.
func venueTTL(venues []domain.VenueRecommendation, now time.Time) time.Duration {
	ttl := CacheTTL
	if first, ok := earliestDeadline(venues); ok {
		ttl = min(ttl, first.Sub(now))
	}
	return ttl
}

// daysLeft is the whole days from now until deadline, rounded down and never below 0; nil
// without a deadline.
func daysLeft(deadline *time.Time, now time.Time) *int {
	if deadline == nil {
		return nil
	}
	n := max(int(deadline.Sub(now)/(24*time.Hour)), 0)
	return &n
}

func cloneVenues(in []domain.VenueRecommendation) []domain.VenueRecommendation {
	out := make([]domain.VenueRecommendation, len(in))
	copy(out, in)
	return out
}

// ProjectVenuesChanged is told that a project's title-and-abstract embedding was stored or
// deleted, or the project was deleted: its kept venue lists are dropped.
func (s *Service) ProjectVenuesChanged(ctx context.Context, projectIDs ...string) {
	if s.venues == nil {
		return
	}
	if err := s.venues.Cache.DeleteVenues(ctx, projectIDs...); err != nil {
		s.logger.WarnContext(ctx, "venue recommendation lists not cleared after a project change", slog.Any("error", err))
	}
}

// VenuesChanged is told, after the commit, that a venue was added, edited, removed or
// imported, or that a venue embedding changed: every kept venue list is made again.
func (s *Service) VenuesChanged(ctx context.Context) {
	if s.venues == nil {
		return
	}
	if err := s.venues.Cache.NextVenueGeneration(ctx); err != nil {
		s.logger.WarnContext(ctx, "venue recommendation lists not renewed after a venue change", slog.Any("error", err))
	}
}
