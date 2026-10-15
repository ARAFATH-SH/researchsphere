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
// contract; cmd/api builds it from the modules' HTTP adapters. auth (httpx.RequireAuth)
// runs inside each generated operation, where it can see whether the contract secures it.
func NewRouter(logger *slog.Logger, si gen.StrictServerInterface, auth func(http.Handler) http.Handler) http.Handler {
	return newRouter(logger, si, auth, nil)
}

// newRouter lets tests register extra routes on the same mux and middleware.
func newRouter(logger *slog.Logger, si gen.StrictServerInterface, auth func(http.Handler) http.Handler, extra func(*http.ServeMux)) http.Handler {
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
	mountAPI(strict, gen.StdHTTPServerOptions{
		BaseRouter:  mux,
		Middlewares: []gen.MiddlewareFunc{auth},
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

// mountAPI registers every operation in the contract on opts.BaseRouter under BaseURL.
// Every API route must be registered here, through the generated code, so that the
// contract's `security` decides whether it needs a session (FR-3.1, docs/decisions.md);
// a route added to the mux by hand would skip that check.
func mountAPI(si gen.ServerInterface, opts gen.StdHTTPServerOptions) {
	opts.BaseURL = BaseURL
	gen.HandlerWithOptions(si, opts)
}

// Routes returns the pattern of every route the router registers, such as
// "GET /api/v1/health". Tests use it to check each route's access rule.
func Routes() []string {
	rec := &recordingMux{ServeMux: http.NewServeMux()}
	mountAPI(gen.NewStrictHandler(nil, nil), gen.StdHTTPServerOptions{BaseRouter: rec})
	return rec.patterns
}

// recordingMux is a gen.ServeMux that remembers the patterns registered on it.
type recordingMux struct {
	*http.ServeMux
	patterns []string
}

func (m *recordingMux) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	m.patterns = append(m.patterns, pattern)
	m.ServeMux.HandleFunc(pattern, handler)
}
