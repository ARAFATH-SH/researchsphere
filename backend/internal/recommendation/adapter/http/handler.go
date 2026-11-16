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
	Projects(ctx context.Context, userID string, limit int) (usecase.ProjectResult, error)
	ProjectResearchers(ctx context.Context, userID, projectID string) (usecase.ResearcherResult, error)
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
	forbidden = gen.ForbiddenJSONResponse{Code: httpx.CodeForbidden, Message: "You do not have permission to do this."}
	notFound  = gen.NotFoundJSONResponse{Code: httpx.CodeNotFound, Message: "This project does not exist."}
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

// RecommendProjects returns the open projects most similar to the caller's profile (AI-3.1).
func (h *Handler) RecommendProjects(ctx context.Context, req gen.RecommendProjectsRequestObject) (gen.RecommendProjectsResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.RecommendProjects401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	limit := usecase.MaxLimit
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	res, err := h.svc.Projects(ctx, id, limit)
	if errors.Is(err, domain.ErrInvalidLimit) {
		return gen.RecommendProjects400JSONResponse(badLimit), nil
	}
	if err != nil {
		return nil, err
	}
	body := gen.ProjectRecommendations{
		State:   gen.ProjectRecommendationsState(res.State),
		Missing: make([]gen.ProjectRecommendationsMissing, len(res.Missing)),
		Items:   make([]gen.ProjectRecommendation, len(res.Items)),
	}
	for i, f := range res.Missing {
		body.Missing[i] = gen.ProjectRecommendationsMissing(f)
	}
	for i, it := range res.Items {
		p := it.Project
		var pid, oid openapi_types.UUID
		if err := pid.UnmarshalText([]byte(p.ID)); err != nil {
			return nil, fmt.Errorf("recommended project id: %w", err)
		}
		if err := oid.UnmarshalText([]byte(p.Owner.UserID)); err != nil {
			return nil, fmt.Errorf("recommended project owner id: %w", err)
		}
		body.Items[i] = gen.ProjectRecommendation{
			Project: gen.RecommendedProject{
				Id: pid, Title: p.Title, Description: p.Description, RequiredSkills: nonNil(p.Skills),
				Owner:       gen.ProjectPerson{UserId: oid, FullName: p.Owner.FullName, Headline: p.Owner.Headline, PhotoUrl: mediaURL(p.Owner.PhotoFileID)},
				MemberCount: p.MemberCount,
			},
			Score:  it.Score,
			Shared: nonNil(it.Shared),
		}
	}
	return gen.RecommendProjects200JSONResponse(body), nil
}

// RecommendProjectResearchers returns the researchers most similar to a project, for its
// owner (AI-2.3).
func (h *Handler) RecommendProjectResearchers(ctx context.Context, req gen.RecommendProjectResearchersRequestObject) (gen.RecommendProjectResearchersResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.RecommendProjectResearchers401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	res, err := h.svc.ProjectResearchers(ctx, id, req.ProjectId)
	switch {
	case errors.Is(err, domain.ErrProjectNotFound):
		return gen.RecommendProjectResearchers404JSONResponse{NotFoundJSONResponse: notFound}, nil
	case errors.Is(err, domain.ErrNotProjectOwner):
		return gen.RecommendProjectResearchers403JSONResponse{ForbiddenJSONResponse: forbidden}, nil
	case err != nil:
		return nil, err
	}
	body, err := resultBody(usecase.Result{State: res.State, Missing: []domain.Field{}, Items: res.Items})
	if err != nil {
		return nil, err
	}
	return gen.RecommendProjectResearchers200JSONResponse(body), nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
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
