package main

import (
	"log/slog"
	"net/http"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	authhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/http"
	healthhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/health/adapter/http"
	healthdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/health/domain"
	healthusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/health/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpserver"
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

// newRouter wires the modules to their dependencies and returns the API's root handler.
// Tests pass fakes; run passes the real PostgreSQL, Redis and SMTP adapters. requireAuth
// guards the operations the contract secures (httpx.RequireAuth).
func newRouter(logger *slog.Logger, db, redis healthdomain.Pinger, auth *authhttp.Handler, requireAuth func(http.Handler) http.Handler) http.Handler {
	checker := healthusecase.New(healthusecase.DefaultTimeout,
		healthdomain.Dependency{Name: "database", Pinger: db},
		healthdomain.Dependency{Name: "redis", Pinger: redis},
	)
	return httpserver.NewRouter(logger, handlers{
		healthHandler: healthhttp.NewHandler(checker, logger),
		authHandler:   auth,
	}, requireAuth)
}
