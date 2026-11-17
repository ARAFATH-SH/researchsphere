package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	moderationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/moderation/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/authz"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/storage"
	projectpg "github.com/ARAFATH-SH/researchsphere/backend/internal/project/adapter/postgres"
	projectredis "github.com/ARAFATH-SH/researchsphere/backend/internal/project/adapter/redis"
	projectdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
	projectusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/project/usecase"
)

// newProjects wires research projects (FR-10): the store, the Redis limits of creation and
// search, the project permission table (authz) and the standings moderation keeps. Close the
// returned limits at shutdown.
//
// Hybrid project search (AI-5.1) embeds its queries with the AI service and shares the query
// vectors researcher search keeps; status, owner and membership changes and deletions are
// passed to watcher, and membership events (FR-9.2) to notifier.
//
// Project files (FR-12) keep figures in files, within the figures limits.
func newProjects(pool *pgxpool.Pool, rdb *goredis.Client, standings *moderationusecase.Standings, search projectSearch, watcher projectdomain.Watcher, notifier projectdomain.Notifier, files storage.Storage, figures figureLimits, logger *slog.Logger) (*projectusecase.Service, *projectredis.Limits) {
	limits := projectredis.New(rdb)
	store := projectpg.NewStore(pool)
	svc := projectusecase.New(projectusecase.Deps{
		Store:         store,
		Tx:            postgres.NewTransactor(pool),
		Permissions:   projectPermissions{},
		Actors:        projectActors{standings: standings},
		CreateLimiter: limits.Create(),
		SearchLimiter: limits.Search(),
		Embedder:      search.embedder,
		VectorCache:   search.vectors,
		AITimeout:     search.timeout,
		Watcher:       watcher,
		Logger:        logger,
		Members:       store,
		Notifier:      notifier,
		JoinLimiter:   limits.Join(),
		InviteLimiter: limits.Invite(),
		FileDeps: projectusecase.FileDeps{
			Files:           store,
			Figures:         figureIntake{},
			Objects:         figureObjects{Storage: files},
			Remover:         objectRemover{files: files, logger: logger},
			FigureLimiter:   limits.Figure(),
			SaveLimiter:     limits.Save(),
			FigureMaxBytes:  figures.figure,
			StorageMaxBytes: figures.storage,
		},
	})
	return svc, limits
}

// projectSearch is what hybrid project search needs from the AI service and the cache.
type projectSearch struct {
	embedder projectdomain.QueryEmbedder
	vectors  projectdomain.QueryVectorCache
	timeout  time.Duration
}

// projectPermissions answers the project module from SRS 2.3's project table, which only
// authz holds (FR-3.2).
type projectPermissions struct{}

var _ projectdomain.Permissions = projectPermissions{}

// projectActions maps the module's actions to the rows of the project table.
var projectActions = map[projectdomain.Action]authz.ProjectAction{
	projectdomain.ActView:          authz.ViewProjectPage,
	projectdomain.ActEditDetails:   authz.EditProjectDetails,
	projectdomain.ActEditAbstract:  authz.CompileAndAssist,
	projectdomain.ActDelete:        authz.TransferOrDeleteProject,
	projectdomain.ActTransfer:      authz.TransferOrDeleteProject,
	projectdomain.ActRequestToJoin: authz.RequestToJoin,
	projectdomain.ActReadFiles:     authz.OpenWorkspace,
	projectdomain.ActEditFiles:     authz.EditFiles,
	// AI-2.3 (recommended researchers, owner only) has no row of its own; its list is for
	// finding people to invite (decisions.md 2026-11-16).
	projectdomain.ActManageMembers: authz.ManageMembers,
}

func (projectPermissions) Can(role projectdomain.Role, action projectdomain.Action) bool {
	a, ok := projectActions[action]
	if !ok {
		return false
	}
	r := authz.ProjectNonMember
	if role != projectdomain.RoleNone {
		r = authz.ProjectRole(role)
	}
	return authz.CanInProject(r, a)
}

// projectActors tells the project module what a user may do now.
type projectActors struct {
	standings *moderationusecase.Standings
}

var _ projectdomain.Actors = projectActors{}

func (a projectActors) Standing(ctx context.Context, userID string) (projectdomain.Standing, error) {
	s, err := a.standings.Standing(ctx, userID)
	if err != nil {
		return projectdomain.Standing{}, err
	}
	return projectdomain.Standing{Suspended: s.Suspended, SuspensionReason: s.SuspensionReason, SuspendedUntil: s.SuspendedUntil}, nil
}
