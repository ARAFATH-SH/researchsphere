package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// RequestIDHeader carries the request ID back to the client.
const RequestIDHeader = "X-Request-ID"

type ctxKey struct{}

// RequestIDFrom returns the request ID stored by RequestID, or "" outside a request.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

// RequestID gives every request a new random ID, stores it in the context and sets the
// X-Request-ID response header. An ID sent by the client is ignored, so a client cannot
// write arbitrary text into the logs.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newID()
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, id)))
	})
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error since Go 1.24.
	return hex.EncodeToString(b)
}

// AccessLog writes one log line per request with the method, path, status and duration.
// The query string is left out because it may carry tokens.
func AccessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			logger.LogAttrs(r.Context(), slog.LevelInfo, "request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.Status()),
				slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
			)
		})
	}
}

// Recover turns a panic into a 500 in the error shape. The panic value and stack go to
// the log only, never to the client.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w}
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(v) // net/http uses this panic to abort a response on purpose.
				}
				logger.LogAttrs(r.Context(), slog.LevelError, "panic recovered",
					slog.Any("panic", v),
					slog.String("stack", string(debug.Stack())),
				)
				if rec.wroteHeader {
					return // Too late to change the response.
				}
				WriteError(rec, http.StatusInternalServerError, CodeInternal, "An unexpected error occurred.")
			}()
			next.ServeHTTP(rec, r)
		})
	}
}

// statusRecorder remembers the status code written through it.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status = status
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(b)
}

// Status returns the status sent, or 200 if the handler wrote nothing.
func (r *statusRecorder) Status() int {
	if !r.wroteHeader {
		return http.StatusOK
	}
	return r.status
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// LogHandler adds the request ID from the context to every log record made with a
// *Context logging call (InfoContext, LogAttrs, ...).
type LogHandler struct{ slog.Handler }

// NewLogHandler wraps h so that records carry request_id.
func NewLogHandler(h slog.Handler) *LogHandler { return &LogHandler{Handler: h} }

func (h *LogHandler) Handle(ctx context.Context, rec slog.Record) error {
	if id := RequestIDFrom(ctx); id != "" {
		rec.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, rec)
}

func (h *LogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &LogHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *LogHandler) WithGroup(name string) slog.Handler {
	return &LogHandler{Handler: h.Handler.WithGroup(name)}
}
