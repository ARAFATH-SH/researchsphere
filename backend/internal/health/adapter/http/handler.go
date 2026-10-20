// Package http serves GET /api/v1/health: it turns the health check's report into the
// responses declared in docs/openapi.yaml.
package http

import (
	"context"
	"log/slog"
	"strings"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/health/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// checker is the part of usecase.Checker this handler needs.
type checker interface {
	Check(ctx context.Context) domain.Report
}

// Handler implements the health operations of gen.StrictServerInterface.
type Handler struct {
	checker checker
	logger  *slog.Logger
}

// NewHandler returns a Handler that reports what c finds.
func NewHandler(c checker, logger *slog.Logger) *Handler {
	return &Handler{checker: c, logger: logger}
}

// GetHealth returns 200 when every dependency responds, and 503 in the error shape
// naming those that do not. The underlying errors are logged, not returned.
func (h *Handler) GetHealth(ctx context.Context, _ gen.GetHealthRequestObject) (gen.GetHealthResponseObject, error) {
	report := h.checker.Check(ctx)
	if report.Healthy() {
		return gen.GetHealth200JSONResponse{
			Status:   gen.HealthStatusOk,
			Database: gen.HealthDatabaseOk,
			Redis:    gen.HealthRedisOk,
			Storage:  gen.HealthStorageOk,
		}, nil
	}

	for _, res := range report.Results {
		if res.Err != nil {
			h.logger.WarnContext(ctx, "health check failed", slog.String("service", res.Name), slog.Any("error", res.Err))
		}
	}
	down := report.Down()
	verb := "is"
	if len(down) > 1 {
		verb = "are"
	}
	return gen.GetHealth503JSONResponse{
		Code:    httpx.CodeServiceUnavailable,
		Message: joinNames(down) + " " + verb + " not responding",
	}, nil
}

// joinNames lists names as "a", "a and b" or "a, b and c".
func joinNames(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
