// Package http implements the embedding operations of docs/openapi.yaml: the admin page's
// statistics and the rebuild (AI-1.6). cmd/api checks the platform action before a request
// reaches these handlers (platformGuards).
package http

import (
	"context"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/embedding/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/embedding/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// Service is the part of the usecase the handlers call.
type Service interface {
	Stats(ctx context.Context) (usecase.Stats, error)
	Rebuild(ctx context.Context, adminID string) (int, error)
}

// Handler serves the embedding operations.
type Handler struct {
	svc Service
}

// NewHandler returns a Handler.
func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

var unauthenticated = gen.UnauthorizedJSONResponse{Code: httpx.CodeUnauthenticated, Message: "Log in to continue."}

// GetEmbeddingStats returns the current model and the counts of each owner type.
func (h *Handler) GetEmbeddingStats(ctx context.Context, _ gen.GetEmbeddingStatsRequestObject) (gen.GetEmbeddingStatsResponseObject, error) {
	if _, ok := httpx.UserID(ctx); !ok {
		return gen.GetEmbeddingStats401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	st, err := h.svc.Stats(ctx)
	if err != nil {
		return nil, err
	}
	body := gen.GetEmbeddingStats200JSONResponse{OwnerTypes: make([]gen.EmbeddingOwnerStats, len(st.Owners))}
	if st.Model != "" {
		body.Model = &st.Model
	}
	for i, o := range st.Owners {
		body.OwnerTypes[i] = ownerStats(o)
	}
	return body, nil
}

// RebuildEmbeddings queues every owner to be embedded again.
func (h *Handler) RebuildEmbeddings(ctx context.Context, _ gen.RebuildEmbeddingsRequestObject) (gen.RebuildEmbeddingsResponseObject, error) {
	admin, ok := httpx.UserID(ctx)
	if !ok {
		return gen.RebuildEmbeddings401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	n, err := h.svc.Rebuild(ctx, admin)
	if err != nil {
		return nil, err
	}
	return gen.RebuildEmbeddings202JSONResponse{Queued: n}, nil
}

func ownerStats(o domain.OwnerStats) gen.EmbeddingOwnerStats {
	return gen.EmbeddingOwnerStats{
		OwnerType: gen.EmbeddingOwnerStatsOwnerType(o.OwnerType), Total: o.Total,
		CurrentModel: o.CurrentModel, Queued: o.Queued, Failed: o.Failed,
	}
}
