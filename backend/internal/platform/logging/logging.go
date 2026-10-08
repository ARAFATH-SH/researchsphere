// Package logging builds the structured JSON logger and carries the request ID in the
// context, so every log line made while serving a request names that request.
package logging

import (
	"context"
	"io"
	"log/slog"
)

type ctxKey struct{}

// WithRequestID returns a copy of ctx that carries the request ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// RequestID returns the request ID stored by WithRequestID, or "" outside a request.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

// New returns a JSON logger writing to w at the given level. Records made with a
// *Context call (InfoContext, LogAttrs, ...) carry request_id.
func New(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(NewHandler(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})))
}

// Handler adds the request ID from the context to every log record.
type Handler struct{ slog.Handler }

// NewHandler wraps h so that records carry request_id.
func NewHandler(h slog.Handler) *Handler { return &Handler{Handler: h} }

func (h *Handler) Handle(ctx context.Context, rec slog.Record) error {
	if id := RequestID(ctx); id != "" {
		rec.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, rec)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{Handler: h.Handler.WithGroup(name)}
}
