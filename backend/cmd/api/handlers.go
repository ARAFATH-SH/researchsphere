package main

import (
	"log/slog"
	"net/http"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	authhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/http"
	healthhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/health/adapter/http"
	healthdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/health/domain"
	healthusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/health/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/authz"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpserver"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// Every module's adapter type is called Handler; the aliases give the embedded fields
// different names.
type (
	healthHandler = healthhttp.Handler
	authHandler   = authhttp.Handler
)

// handlers implements every operation in the contract by embedding each module's HTTP
// adapter. The assertion below fails to compile when an operation has no implementation.
type handlers struct {
	*healthHandler
	*authHandler
}

var _ gen.StrictServerInterface = handlers{}

// platformGuards names the platform action (SRS 2.3, FR-3.2) each route needs beyond a
// session, by the route's pattern. A route not listed needs only a session; every route
// under /api/v1/admin/ must be listed (routes_test.go).
var platformGuards = map[string]authz.Action{
	"GET /api/v1/admin/users": authz.SuspendUser, // listing users comes before suspending one (FR-15.3)
}

// guardPlatform runs httpx.RequirePlatform with the action platformGuards gives the
// matched route. It runs inside RequireAuth, so a request without a session gets 401
// before any 403. A request that matched no route pattern is refused: every API route
// is registered on the ServeMux, so that would mean the guard cannot tell which route
// it is guarding.
func guardPlatform(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Pattern == "" {
			httpx.WriteForbidden(w)
			return
		}
		action, ok := platformGuards[r.Pattern]
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		httpx.RequirePlatform(action)(next).ServeHTTP(w, r)
	})
}

// newRouter wires the modules to their dependencies and returns the API's root handler.
// Tests pass fakes; run passes the real PostgreSQL, Redis and SMTP adapters. requireAuth
// guards the operations the contract secures (httpx.RequireAuth); guardPlatform runs
// inside it.
func newRouter(logger *slog.Logger, db, redis healthdomain.Pinger, auth *authhttp.Handler, requireAuth func(http.Handler) http.Handler) http.Handler {
	checker := healthusecase.New(healthusecase.DefaultTimeout,
		healthdomain.Dependency{Name: "database", Pinger: db},
		healthdomain.Dependency{Name: "redis", Pinger: redis},
	)
	return httpserver.NewRouter(logger, handlers{
		healthHandler: healthhttp.NewHandler(checker, logger),
		authHandler:   auth,
	}, func(next http.Handler) http.Handler { return requireAuth(guardPlatform(next)) })
}
