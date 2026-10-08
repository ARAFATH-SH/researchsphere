// Package httpserver builds the API's HTTP handler (the routes generated from
// docs/openapi.yaml, mounted under /api/v1, behind the shared middleware) and serves it.
package httpserver

import (
	"log/slog"
	"net/http"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// BaseURL is the prefix of every API route (SRS 5.3).
const BaseURL = "/api/v1"

// NewRouter returns the API's root handler. si implements every operation in the
// contract; cmd/api builds it from the modules' HTTP adapters.
func NewRouter(logger *slog.Logger, si gen.StrictServerInterface) http.Handler {
	return newRouter(logger, si, nil)
}

// newRouter lets tests register extra routes on the same mux and middleware.
func newRouter(logger *slog.Logger, si gen.StrictServerInterface, extra func(*http.ServeMux)) http.Handler {
	mux := http.NewServeMux()

	strict := gen.NewStrictHandlerWithOptions(si, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.WarnContext(r.Context(), "bad request", slog.Any("error", err))
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "The request is not valid.")
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.ErrorContext(r.Context(), "handler failed", slog.Any("error", err))
			httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal, "An unexpected error occurred.")
		},
	})
	gen.HandlerWithOptions(strict, gen.StdHTTPServerOptions{
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
