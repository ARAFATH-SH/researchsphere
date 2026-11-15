// Package http implements the recommendation operations of docs/openapi.yaml. cmd/api
// checks the platform action before a request reaches these handlers (platformGuards).
package http

import (
	"context"
	"errors"
	"fmt"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/usecase"
)

// Service is the part of the usecase the handlers call.
type Service interface {
	People(ctx context.Context, userID string, limit int) (usecase.Result, error)
}

// Handler serves the recommendation operations.
type Handler struct {
	svc Service
}

// NewHandler returns a Handler.
func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

var (
	unauthenticated = gen.UnauthorizedJSONResponse{Code: httpx.CodeUnauthenticated, Message: "Log in to continue."}
	limitField      = "limit"
	badLimit        = gen.RecommendPeople400JSONResponse{
		Code: httpx.CodeBadRequest, Message: "The limit must be a whole number from 1.", Field: &limitField,
	}
)

// RecommendPeople returns the caller's collaborator recommendations (AI-2.1, AI-2.2, AI-4).
func (h *Handler) RecommendPeople(ctx context.Context, req gen.RecommendPeopleRequestObject) (gen.RecommendPeopleResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.RecommendPeople401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	limit := usecase.MaxLimit
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	res, err := h.svc.People(ctx, id, limit)
	if errors.Is(err, domain.ErrInvalidLimit) {
		return badLimit, nil
	}
	if err != nil {
		return nil, err
	}
	body, err := resultBody(res)
	if err != nil {
		return nil, err
	}
	return gen.RecommendPeople200JSONResponse(body), nil
}

// resultBody builds the response. Every list is empty, never null.
func resultBody(res usecase.Result) (gen.PeopleRecommendations, error) {
	body := gen.PeopleRecommendations{
		State:   gen.PeopleRecommendationsState(res.State),
		Missing: make([]gen.PeopleRecommendationsMissing, len(res.Missing)),
		Items:   make([]gen.PersonRecommendation, len(res.Items)),
	}
	for i, f := range res.Missing {
		body.Missing[i] = gen.PeopleRecommendationsMissing(f)
	}
	for i, it := range res.Items {
		var uid openapi_types.UUID
		if err := uid.UnmarshalText([]byte(it.UserID)); err != nil {
			return gen.PeopleRecommendations{}, fmt.Errorf("recommended user id: %w", err)
		}
		shared := it.Shared
		if shared == nil {
			shared = []string{}
		}
		body.Items[i] = gen.PersonRecommendation{
			UserId:           uid,
			FullName:         it.FullName,
			Headline:         it.Headline,
			Institution:      it.Institution,
			PhotoUrl:         mediaURL(it.PhotoFileID),
			ConnectionStatus: gen.ConnectionStatus(it.Connection),
			Score:            it.Score,
			Shared:           shared,
		}
	}
	return body, nil
}

// mediaURL is where the client reads a profile photo (FR-4.6), nil when none is set.
func mediaURL(fileID *string) *string {
	if fileID == nil {
		return nil
	}
	u := httpx.MediaURL(*fileID)
	return &u
}
