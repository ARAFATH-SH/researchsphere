// Package httpserver builds the API's HTTP handler (the routes generated from
// docs/openapi.yaml, mounted under /api/v1, behind the shared middleware) and serves it.
package httpserver

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

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
			writeBodyError(w, err)
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
			fe := paramError(err)
			if fe == nil {
				logger.WarnContext(r.Context(), "bad request parameters")
				httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "The request parameters are not valid.")
				return
			}
			// The parameter's name only: err quotes the value that was sent.
			logger.WarnContext(r.Context(), "bad request parameter", slog.String("field", fe.Field))
			httpx.WriteFieldError(w, http.StatusBadRequest, httpx.CodeBadRequest, fe.Error(), fe.Field)
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

// writeBodyError answers a JSON body that the generated handlers could not decode, naming
// the field when one is at fault. Nothing here repeats what was sent, apart from the name
// of an unknown field.
func writeBodyError(w http.ResponseWriter, err error) {
	var (
		field   *gen.FieldError
		unknown *gen.UnknownFieldError
	)
	switch {
	case errors.As(err, &field):
		httpx.WriteFieldError(w, http.StatusBadRequest, httpx.CodeBadRequest, field.Error(), field.Field)
	case errors.As(err, &unknown):
		httpx.WriteFieldError(w, http.StatusBadRequest, httpx.CodeBadRequest,
			fmt.Sprintf("The request has an unknown field %q.", unknown.Field), unknown.Field)
	case errors.Is(err, gen.ErrMalformedBody):
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "The request body is not valid JSON.")
	case errors.Is(err, gen.ErrBodyNotObject):
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "The request body must be a JSON object.")
	case errors.Is(err, io.EOF):
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "The request body is required.")
	default:
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "The request is not valid.")
	}
}

// paramError says which path or query parameter is at fault and why, nil for any other
// error. A parameter that is not a string fails on its number or boolean parsing.
func paramError(err error) *gen.FieldError {
	var (
		required *gen.RequiredParamError
		invalid  *gen.InvalidParamFormatError
	)
	switch {
	case errors.As(err, &required):
		return &gen.FieldError{Field: required.ParamName, Problem: "is required"}
	case errors.As(err, &invalid):
		var parse *strconv.NumError
		if errors.As(invalid.Err, &parse) {
			if parse.Func == "ParseBool" {
				return &gen.FieldError{Field: invalid.ParamName, Problem: "must be true or false"}
			}
			return &gen.FieldError{Field: invalid.ParamName, Problem: "must be a whole number"}
		}
		return &gen.FieldError{Field: invalid.ParamName, Problem: "is not valid"}
	}
	return nil
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
