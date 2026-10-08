// Package server builds the API's HTTP handler: the routes generated from
// docs/openapi.yaml, mounted under /api/v1, behind the shared middleware.
package server

import (
	"log/slog"
	"net/http"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/health"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/httpx"
)

// BaseURL is the prefix of every API route (SRS 5.3).
const BaseURL = "/api/v1"

// Handlers implements every operation in the contract. Each module adds its handler
// here; the assertion below fails to compile when an operation has no implementation.
type Handlers struct {
	*health.Handler
}

var _ api.StrictServerInterface = Handlers{}

// New returns the API's root handler.
func New(logger *slog.Logger, h Handlers) http.Handler {
	return newRouter(logger, h, nil)
}

// newRouter lets tests register extra routes on the same mux and middleware.
func newRouter(logger *slog.Logger, h Handlers, extra func(*http.ServeMux)) http.Handler {
	mux := http.NewServeMux()

	strict := api.NewStrictHandlerWithOptions(h, nil, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.WarnContext(r.Context(), "bad request", slog.Any("error", err))
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "The request is not valid.")
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.ErrorContext(r.Context(), "handler failed", slog.Any("error", err))
			httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal, "An unexpected error occurred.")
		},
	})
	api.HandlerWithOptions(strict, api.StdHTTPServerOptions{
		BaseURL:    BaseURL,
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.WarnContext(r.Context(), "bad request parameters", slog.Any("error", err))
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "The request parameters are not valid.")
		},
	})
	if extra != nil {
		extra(mux)
	}

	// Outermost first: the request ID exists before the access log line is written,
	// and the access log sees the 500 that Recover writes.
	handler := httpx.Fallback(mux)
	handler = httpx.Recover(logger)(handler)
	handler = httpx.AccessLog(logger)(handler)
	handler = httpx.RequestID(handler)
	return handler
}
