package main

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	moderationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/moderation/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/authz"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	projectpg "github.com/ARAFATH-SH/researchsphere/backend/internal/project/adapter/postgres"
	projectredis "github.com/ARAFATH-SH/researchsphere/backend/internal/project/adapter/redis"
	projectdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
	projectusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/project/usecase"
)

// newProjects wires research projects (FR-10): the store, the Redis limits of creation and
// search, the project permission table (authz) and the standings moderation keeps. Close the
// returned limits at shutdown.
func newProjects(pool *pgxpool.Pool, rdb *goredis.Client, standings *moderationusecase.Standings, logger *slog.Logger) (*projectusecase.Service, *projectredis.Limits) {
	limits := projectredis.New(rdb)
	svc := projectusecase.New(projectusecase.Deps{
		Store:         projectpg.NewStore(pool),
		Tx:            postgres.NewTransactor(pool),
		Permissions:   projectPermissions{},
		Actors:        projectActors{standings: standings},
		CreateLimiter: limits.Create(),
		SearchLimiter: limits.Search(),
		Logger:        logger,
	})
	return svc, limits
}

// projectPermissions answers the project module from SRS 2.3's project table, which only
// authz holds (FR-3.2).
type projectPermissions struct{}

var _ projectdomain.Permissions = projectPermissions{}

// projectActions maps the module's actions to the rows of the project table.
var projectActions = map[projectdomain.Action]authz.ProjectAction{
	projectdomain.ActView:         authz.ViewProjectPage,
	projectdomain.ActEditDetails:  authz.EditProjectDetails,
	projectdomain.ActEditAbstract: authz.CompileAndAssist,
	projectdomain.ActDelete:       authz.TransferOrDeleteProject,
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
