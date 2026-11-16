package main

import (
	"context"
	"errors"
	"time"

	embeddingdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/embedding/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/config"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	profiledomain "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
	projectdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
	projectusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/project/usecase"
	recommendationpg "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/postgres"
	recommendationredis "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/redis"
	recommendationdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
	recommendationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/usecase"
)

// Project matching (AI-3.1, AI-2.3) and hybrid project search (AI-5.1; slice 19, chunk 19.2).

// projectScoreMap is the score map of profile-project pairs the settings hold: fitted for
// the same embedding model as the people map (docs/evaluation.md).
func projectScoreMap(cfg config.Config) recommendationusecase.ScoreMap {
	return recommendationusecase.ScoreMap{Model: cfg.RecsScoreModel, Slope: cfg.RecsProjectScoreSlope, Intercept: cfg.RecsProjectScoreIntercept}
}

// enableProjectMatching gives the recommendation service its project lists: the store, the
// same Redis cache, the project module's owner check and the project score map.
func enableProjectMatching(recs *recommendationusecase.Service, store *recommendationpg.Store, cache *recommendationredis.Cache, projects *projectusecase.Service, cfg config.Config) error {
	scores := projectScoreMap(cfg)
	if err := scores.Validate(); err != nil {
		return err
	}
	recs.WithProjects(recommendationusecase.ProjectDeps{
		Store: store, Cache: cache, Access: recommendationProjectAccess{projects: projects}, Scores: scores,
	})
	return nil
}

// recommendationProjectAccess answers the recommendation module with the project module's
// visibility rule and permission table (AI-2.3 uses the row "Invite, decide join requests,
// change roles, remove members", owner only).
type recommendationProjectAccess struct {
	projects *projectusecase.Service
}

var _ recommendationdomain.ProjectAccess = recommendationProjectAccess{}

func (a recommendationProjectAccess) CanSeeRecommendedResearchers(ctx context.Context, userID, projectID string) error {
	err := a.projects.CanSeeRecommendedResearchers(ctx, userID, projectID)
	switch {
	case errors.Is(err, projectdomain.ErrNotFound):
		return recommendationdomain.ErrProjectNotFound
	case errors.Is(err, projectdomain.ErrForbidden):
		return recommendationdomain.ErrNotProjectOwner
	}
	return err
}

// projectChanges tells the recommendation module about project changes once they commit: a
// rollback renews nothing.
type projectChanges struct {
	recs *recommendationusecase.Service
}

var _ projectdomain.Watcher = projectChanges{}

func (w projectChanges) ProjectChanged(ctx context.Context, projectID string) {
	postgres.AfterCommit(ctx, func(ctx context.Context) { w.recs.ProjectChanged(ctx, projectID) })
}

func (w projectChanges) MembersChanged(ctx context.Context, projectID string, userIDs ...string) {
	postgres.AfterCommit(ctx, func(ctx context.Context) { w.recs.MembersChanged(ctx, projectID, userIDs...) })
}

// embeddingChanges renews the project lists when the embedding worker stores or deletes a
// project's embedding (AI-3.1). A profile's embedding needs nothing: each list carries the
// stamp of the embedding it was made from.
type embeddingChanges struct {
	recs *recommendationusecase.Service
}

var _ embeddingdomain.Watcher = embeddingChanges{}

func (w embeddingChanges) EmbeddingChanged(ctx context.Context, ownerType embeddingdomain.OwnerType, ownerID string) {
	if ownerType == embeddingdomain.OwnerProject {
		w.recs.ProjectChanged(ctx, ownerID)
	}
}

// projectQueryEmbedder gives hybrid project search its query vectors from the AI service,
// as researcher search gets them.
type projectQueryEmbedder struct {
	q queryEmbedder
}

var _ projectdomain.QueryEmbedder = projectQueryEmbedder{}

func (e projectQueryEmbedder) EmbedQuery(ctx context.Context, text string) (projectdomain.QueryVector, error) {
	v, err := e.q.EmbedQuery(ctx, text)
	if err != nil {
		return projectdomain.QueryVector{}, err
	}
	return projectdomain.QueryVector{Model: v.Model, Values: v.Values}, nil
}

// projectQueryVectors gives hybrid project search the query vectors researcher search keeps,
// so a text searched for people and then for projects is embedded once. Both normalize the
// text alike (TestQueryTextNormalizedAlike).
type projectQueryVectors struct {
	cache profiledomain.QueryVectorCache
}

var _ projectdomain.QueryVectorCache = projectQueryVectors{}

func (c projectQueryVectors) Get(ctx context.Context, model, text string) ([]float32, bool, error) {
	return c.cache.Get(ctx, model, text)
}

func (c projectQueryVectors) Set(ctx context.Context, text string, v projectdomain.QueryVector, ttl time.Duration) error {
	return c.cache.Set(ctx, text, profiledomain.QueryVector{Model: v.Model, Values: v.Values}, ttl)
}
