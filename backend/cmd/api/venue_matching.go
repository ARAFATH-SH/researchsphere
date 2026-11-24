package main

import (
	"context"
	"errors"
	"log/slog"

	eventdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/config"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	projectdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
	projectusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/project/usecase"
	recommendationpg "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/postgres"
	recommendationredis "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/redis"
	recommendationdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
	recommendationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/usecase"
)

// Venue recommendations (AI-6.1 to AI-6.4; slice 25, chunk 25.1).

// venueScoreMap is the score map of project-venue pairs the settings hold: fitted for the same
// embedding model as the people map (docs/evaluation.md).
func venueScoreMap(cfg config.Config) recommendationusecase.ScoreMap {
	return recommendationusecase.ScoreMap{Model: cfg.RecsScoreModel, Slope: cfg.RecsVenueScoreSlope, Intercept: cfg.RecsVenueScoreIntercept}
}

// enableVenueMatching gives the recommendation service its venue lists: the store, the same
// Redis cache, the project module's member check, the venue score map and the journal floor.
func enableVenueMatching(recs *recommendationusecase.Service, store *recommendationpg.Store, cache *recommendationredis.Cache, projects *projectusecase.Service, cfg config.Config) error {
	scores := venueScoreMap(cfg)
	if err := scores.Validate(); err != nil {
		return err
	}
	recs.WithVenues(recommendationusecase.VenueDeps{
		Store: store, Cache: cache, Access: recommendationVenueAccess{projects: projects},
		Scores: scores, JournalMinScore: cfg.RecsJournalMinScore,
	})
	return nil
}

// recommendationVenueAccess answers the recommendation module with the project module's rule
// for the row "See venue recommendations for the project" (viewer, editor, owner). Anyone
// else gets not found (decisions.md 2026-11-24).
type recommendationVenueAccess struct {
	projects *projectusecase.Service
}

var _ recommendationdomain.VenueAccess = recommendationVenueAccess{}

func (a recommendationVenueAccess) CanSeeRecommendedVenues(ctx context.Context, userID, projectID string) error {
	err := a.projects.CanSeeRecommendedVenues(ctx, userID, projectID)
	if errors.Is(err, projectdomain.ErrNotFound) {
		return recommendationdomain.ErrProjectNotFound
	}
	return err
}

// venueChanges tells the recommendation module that venues changed, once the change has
// committed (the event module calls it after its transactions; AfterCommit runs it at once
// outside one): every kept venue list is made again.
type venueChanges struct {
	recs *recommendationusecase.Service
}

var _ eventdomain.Watcher = venueChanges{}

func (w venueChanges) VenuesChanged(ctx context.Context) {
	postgres.AfterCommit(ctx, func(ctx context.Context) { w.recs.VenuesChanged(ctx) })
}

// venueGenerationRaiser is the watcher of `make venue-sync`, which runs without the API: it
// raises the venues generation in Redis directly, so the running API makes its kept venue
// lists again.
type venueGenerationRaiser struct {
	cache  recommendationdomain.VenueCache
	logger *slog.Logger
}

var _ eventdomain.Watcher = venueGenerationRaiser{}

func (w venueGenerationRaiser) VenuesChanged(ctx context.Context) {
	if err := w.cache.NextVenueGeneration(ctx); err != nil {
		w.logger.WarnContext(ctx, "venue recommendation lists not renewed after the import", slog.Any("error", err))
	}
}
