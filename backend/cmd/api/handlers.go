package main

import (
	"log/slog"
	"net/http"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	healthhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/health/adapter/http"
	healthdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/health/domain"
	healthusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/health/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpserver"
)

// handlers implements every operation in the contract by embedding each module's HTTP
// adapter. The assertion below fails to compile when an operation has no implementation.
type handlers struct {
	*healthhttp.Handler
}

var _ gen.StrictServerInterface = handlers{}

// newRouter wires the modules to their dependencies and returns the API's root handler.
// Tests pass fake pingers; run passes the real PostgreSQL and Redis adapters.
func newRouter(logger *slog.Logger, db, redis healthdomain.Pinger) http.Handler {
	checker := healthusecase.New(healthusecase.DefaultTimeout,
		healthdomain.Dependency{Name: "database", Pinger: db},
		healthdomain.Dependency{Name: "redis", Pinger: redis},
	)
	return httpserver.NewRouter(logger, handlers{
		Handler: healthhttp.NewHandler(checker, logger),
	})
}
