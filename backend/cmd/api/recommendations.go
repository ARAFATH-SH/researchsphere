package main

import (
	"context"
	"log/slog"

	goredis "github.com/redis/go-redis/v9"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/config"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	recommendationpg "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/postgres"
	recommendationredis "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/redis"
	recommendationdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
	recommendationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/usecase"
	socialdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/social/domain"
	socialusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/social/usecase"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newRecommendations wires collaborator recommendations (AI-2.1): the store, the Redis
// cache, the connection statuses from the social module, and the score map from the
// settings. It tells social to clear both users' lists on every link change. Close the
// returned cache at shutdown.
//
// Project matching (AI-3.1, AI-2.3) is added afterwards with enableProjectMatching, once the
// project module exists.
func newRecommendations(pool *pgxpool.Pool, rdb *goredis.Client, cfg config.Config, social *socialusecase.Service, logger *slog.Logger) (*recommendationusecase.Service, *recommendationpg.Store, *recommendationredis.Cache, error) {
	scores := scoreMap(cfg)
	if err := scores.Validate(); err != nil {
		return nil, nil, nil, err
	}
	cache := recommendationredis.New(rdb)
	store := recommendationpg.NewStore(pool)
	svc := recommendationusecase.New(store, cache, recommendationRelations{social: social}, scores, logger)
	social.WatchLinks(recommendationLinks{cache: cache, recs: svc, logger: logger})
	return svc, store, cache, nil
}

// scoreMap is the AI-2.2 score map the settings hold.
func scoreMap(cfg config.Config) recommendationusecase.ScoreMap {
	return recommendationusecase.ScoreMap{Model: cfg.RecsScoreModel, Slope: cfg.RecsScoreSlope, Intercept: cfg.RecsScoreIntercept}
}

// recommendationLinks clears the recommendation lists of both users when the link between
// them changes (a request sent, accepted, rejected or cancelled, a connection removed, a
// block added or ended), once the change has committed: a rollback clears nothing. It
// clears their project lists and the researcher lists of the projects they own too, since a
// block between them changes both (AI-3.1, AI-2.3).
type recommendationLinks struct {
	cache  recommendationdomain.Cache
	recs   *recommendationusecase.Service
	logger *slog.Logger
}

var _ socialdomain.LinkWatcher = recommendationLinks{}

// LinkChanged schedules the clearing after the commit. A failure leaves a list that is at
// most recommendationusecase.CacheTTL old, which the usecase rechecks on reading.
func (r recommendationLinks) LinkChanged(ctx context.Context, a, b string) {
	postgres.AfterCommit(ctx, func(ctx context.Context) {
		if err := r.cache.Delete(ctx, a, b); err != nil {
			r.logger.WarnContext(ctx, "recommendation lists not cleared after a link change", slog.Any("error", err))
		}
		r.recs.LinkChanged(ctx, a, b)
	})
}

// recommendationRelations gives the recommendation module the caller's connection status
// with each user from the social module, which owns connections.
type recommendationRelations struct {
	social *socialusecase.Service
}

var _ recommendationdomain.Relations = recommendationRelations{}

// Statuses converts the social module's statuses; the values are the same strings.
func (r recommendationRelations) Statuses(ctx context.Context, viewerID string, userIDs []string) (map[string]recommendationdomain.ConnectionStatus, error) {
	rs, err := r.social.Relations(ctx, viewerID, userIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]recommendationdomain.ConnectionStatus, len(rs))
	for id, rel := range rs {
		out[id] = recommendationdomain.ConnectionStatus(rel.Status)
	}
	return out, nil
}
