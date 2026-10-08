// Package health implements GET /api/v1/health: it reports whether the database and
// Redis respond.
package health

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/httpx"
)

// DefaultTimeout bounds each ping, so a hung service cannot hang the health check.
const DefaultTimeout = 2 * time.Second

// Pinger is anything that can tell whether a service responds.
type Pinger interface {
	Ping(ctx context.Context) error
}

// PingerFunc adapts a function to Pinger.
type PingerFunc func(ctx context.Context) error

// Ping calls f.
func (f PingerFunc) Ping(ctx context.Context) error { return f(ctx) }

// Handler answers the health check.
type Handler struct {
	db      Pinger
	redis   Pinger
	timeout time.Duration
	logger  *slog.Logger
}

// NewHandler returns a Handler. A timeout of 0 means DefaultTimeout.
func NewHandler(db, redis Pinger, timeout time.Duration, logger *slog.Logger) *Handler {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Handler{db: db, redis: redis, timeout: timeout, logger: logger}
}

// GetHealth pings the database and Redis at the same time. It returns 200 when both
// respond, and 503 in the error shape naming the services that do not. The underlying
// errors are logged, not returned.
func (h *Handler) GetHealth(ctx context.Context, _ api.GetHealthRequestObject) (api.GetHealthResponseObject, error) {
	checks := []struct {
		name string
		p    Pinger
	}{{"database", h.db}, {"redis", h.redis}}

	errs := make([]error, len(checks))
	var wg sync.WaitGroup
	for i, c := range checks {
		wg.Go(func() {
			pctx, cancel := context.WithTimeout(ctx, h.timeout)
			defer cancel()
			errs[i] = c.p.Ping(pctx)
		})
	}
	wg.Wait()

	var down []string
	for i, c := range checks {
		if errs[i] != nil {
			down = append(down, c.name)
			h.logger.WarnContext(ctx, "health check failed", slog.String("service", c.name), slog.Any("error", errs[i]))
		}
	}
	if len(down) > 0 {
		verb := "is"
		if len(down) > 1 {
			verb = "are"
		}
		return api.GetHealth503JSONResponse{
			Code:    httpx.CodeServiceUnavailable,
			Message: strings.Join(down, " and ") + " " + verb + " not responding",
		}, nil
	}

	return api.GetHealth200JSONResponse{
		Status:   api.HealthStatusOk,
		Database: api.HealthDatabaseOk,
		Redis:    api.HealthRedisOk,
	}, nil
}
