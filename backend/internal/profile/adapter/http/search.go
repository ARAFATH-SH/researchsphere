package http

import (
	"context"
	"errors"
	"fmt"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/usecase"
)

// CodeInvalidQuery is the error code of a missing, too short or too long search text.
const CodeInvalidQuery = "invalid_query"

var (
	invalidQuery = gen.SearchPeople400JSONResponse{
		Code: CodeInvalidQuery, Message: "Enter 2 to 100 characters to search.",
	}
	badSearchFilter = gen.SearchPeople400JSONResponse{
		Code: httpx.CodeBadRequest, Message: "The search filters are not valid.",
	}
	badSearchPage = gen.SearchPeople400JSONResponse{
		Code: httpx.CodeBadRequest, Message: "The page must be a whole number from 1 to 50.",
	}
)

// SearchPeople searches researchers by name, institution, research interest and headline
// (FR-6.1, FR-6.2), narrowed by institution and research interests (FR-6.3).
func (h *Handler) SearchPeople(ctx context.Context, req gen.SearchPeopleRequestObject) (gen.SearchPeopleResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.SearchPeople401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	p := req.Params
	in := usecase.SearchInput{Q: p.Q, Institution: p.Institution, Page: 1}
	if p.Interest != nil {
		in.Interests = *p.Interest
	}
	if p.Page != nil {
		in.Page = *p.Page
	}
	res, err := h.svc.SearchPeople(ctx, id, in)
	var limited *domain.RateLimitedError
	switch {
	case errors.Is(err, domain.ErrInvalidQuery):
		return invalidQuery, nil
	case errors.Is(err, domain.ErrInvalidSearchFilter):
		return badSearchFilter, nil
	case errors.Is(err, domain.ErrInvalidPage):
		return badSearchPage, nil
	case errors.As(err, &limited):
		return gen.SearchPeople429JSONResponse{SearchRateLimitedJSONResponse: gen.SearchRateLimitedJSONResponse{
			Body:    gen.Error{Code: CodeRateLimited, Message: "You have searched many times in the last minute. Try again in a moment."},
			Headers: gen.SearchRateLimitedResponseHeaders{RetryAfter: retryAfter(limited)},
		}}, nil
	case err != nil:
		return nil, err
	}
	body, err := searchPageBody(res, in.Page)
	if err != nil {
		return nil, err
	}
	return gen.SearchPeople200JSONResponse(body), nil
}

// searchPageBody builds the response. Every list is empty, never null.
func searchPageBody(res domain.PeopleResults, page int) (gen.PeopleSearchPage, error) {
	items := make([]gen.PersonSearchResult, len(res.Hits))
	for i, hit := range res.Hits {
		var id openapi_types.UUID
		if err := id.UnmarshalText([]byte(hit.UserID)); err != nil {
			return gen.PeopleSearchPage{}, fmt.Errorf("search result user id: %w", err)
		}
		interests := hit.Interests
		if interests == nil {
			interests = []string{}
		}
		status, requestID, err := connectionFields(hit.Connection)
		if err != nil {
			return gen.PeopleSearchPage{}, err
		}
		items[i] = gen.PersonSearchResult{
			UserId:      id,
			FullName:    hit.FullName,
			Headline:    hit.Headline,
			Institution: hit.Institution,
			PhotoUrl:    mediaURL(hit.PhotoFileID),
			Interests:   interests,
			MatchedOn:   gen.PersonSearchResultMatchedOn(hit.MatchedOn),

			ConnectionStatus:    status,
			ConnectionRequestId: requestID,
		}
	}
	return gen.PeopleSearchPage{
		Items:          items,
		Total:          res.Total,
		Page:           page,
		PageSize:       usecase.SearchPageSize,
		Fuzzy:          res.Fuzzy,
		CorrectedQuery: res.CorrectedQuery,
		Facets: gen.PeopleSearchFacets{
			Institutions: facetBodies(res.Institutions),
			Interests:    facetBodies(res.Interests),
		},
	}, nil
}

func facetBodies(fs []domain.Facet) []gen.SearchFacet {
	out := make([]gen.SearchFacet, len(fs))
	for i, f := range fs {
		out[i] = gen.SearchFacet{Name: f.Name, Count: f.Count}
	}
	return out
}
