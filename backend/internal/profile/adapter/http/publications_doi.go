package http

import (
	"context"
	"errors"
	"math"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

var (
	invalidDOI          = gen.Error{Code: CodeInvalidDOI, Message: "Enter a DOI such as 10.1145/3292500.3330701."}
	badDOIRequest       = gen.Error{Code: httpx.CodeBadRequest, Message: "The request is not valid."}
	doiNotFound         = gen.Error{Code: CodeDOINotFound, Message: "We couldn't find a publication with this DOI. Check it, or add the publication by hand."}
	metadataUnavailable = gen.Error{Code: CodeMetadataUnavailable, Message: "We couldn't reach the publication database. You can add this publication by hand."}
)

// lookupRateLimited is the 429 body and header for a used-up lookup window.
func lookupRateLimited(e *domain.RateLimitedError) gen.LookupRateLimitedJSONResponse {
	return gen.LookupRateLimitedJSONResponse{
		Body:    gen.Error{Code: CodeRateLimited, Message: "You have looked up many DOIs in the last hour. Try again later, or add the publication by hand."},
		Headers: gen.LookupRateLimitedResponseHeaders{RetryAfter: retryAfter(e)},
	}
}

// retryAfter is the time left in whole seconds, rounded up, and at least 1.
func retryAfter(e *domain.RateLimitedError) int {
	return max(1, int(math.Ceil(e.RetryAfter.Seconds())))
}

// LookupPublicationByDOI returns the metadata of a DOI for the caller to confirm (FR-5.2),
// or says clearly that the metadata service failed, so the caller can add the
// publication by hand (FR-5.6).
func (h *Handler) LookupPublicationByDOI(ctx context.Context, req gen.LookupPublicationByDOIRequestObject) (gen.LookupPublicationByDOIResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.LookupPublicationByDOI401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	var doi string // missing is as invalid as empty; the usecase refuses both
	if req.Params.Doi != nil {
		doi = *req.Params.Doi
	}
	pv, err := h.svc.LookupDOI(ctx, id, doi)
	var limited *domain.RateLimitedError
	switch {
	case errors.Is(err, domain.ErrInvalidDOI):
		return gen.LookupPublicationByDOI400JSONResponse{InvalidDOIJSONResponse: gen.InvalidDOIJSONResponse(invalidDOI)}, nil
	case errors.Is(err, domain.ErrWorkNotFound):
		return gen.LookupPublicationByDOI404JSONResponse{DOINotFoundJSONResponse: gen.DOINotFoundJSONResponse(doiNotFound)}, nil
	case errors.As(err, &limited):
		return gen.LookupPublicationByDOI429JSONResponse{LookupRateLimitedJSONResponse: lookupRateLimited(limited)}, nil
	case errors.Is(err, domain.ErrMetadataUnavailable):
		return gen.LookupPublicationByDOI503JSONResponse{MetadataUnavailableJSONResponse: gen.MetadataUnavailableJSONResponse(metadataUnavailable)}, nil
	case err != nil:
		return nil, err
	}
	body := gen.PublicationPreview{
		Doi: pv.DOI, Title: pv.Title, Year: pv.Year, Venue: pv.Venue, Abstract: pv.Abstract, Url: pv.URL,
		ExistsAlready: pv.ExistsAlready, LinkedToMe: pv.LinkedToMe,
	}
	if pv.PublicationID != "" {
		var pid openapi_types.UUID
		if err := pid.UnmarshalText([]byte(pv.PublicationID)); err != nil {
			return nil, err
		}
		body.PublicationId = &pid
	}
	return gen.LookupPublicationByDOI200JSONResponse(body), nil
}

// AddMyPublicationFromDOI adds the publication with a DOI the caller has confirmed
// (FR-5.2), or links the caller to the stored one (FR-5.4).
func (h *Handler) AddMyPublicationFromDOI(ctx context.Context, req gen.AddMyPublicationFromDOIRequestObject) (gen.AddMyPublicationFromDOIResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.AddMyPublicationFromDOI401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.AddMyPublicationFromDOI400JSONResponse{InvalidDOIJSONResponse: gen.InvalidDOIJSONResponse(badDOIRequest)}, nil
	}
	res, err := h.svc.AddPublicationFromDOI(ctx, id, req.Body.Doi)
	var limited *domain.RateLimitedError
	switch {
	case errors.Is(err, domain.ErrInvalidDOI):
		return gen.AddMyPublicationFromDOI400JSONResponse{InvalidDOIJSONResponse: gen.InvalidDOIJSONResponse(invalidDOI)}, nil
	case errors.Is(err, domain.ErrWorkNotFound):
		return gen.AddMyPublicationFromDOI404JSONResponse{DOINotFoundJSONResponse: gen.DOINotFoundJSONResponse(doiNotFound)}, nil
	case errors.Is(err, domain.ErrPublicationLimitReached):
		return gen.AddMyPublicationFromDOI409JSONResponse(publicationLimitReached), nil
	case errors.Is(err, domain.ErrPublicationAlreadyAdded):
		return gen.AddMyPublicationFromDOI409JSONResponse(publicationAlreadyAdded), nil
	case errors.As(err, &limited):
		return gen.AddMyPublicationFromDOI429JSONResponse{LookupRateLimitedJSONResponse: lookupRateLimited(limited)}, nil
	case errors.Is(err, domain.ErrMetadataUnavailable):
		return gen.AddMyPublicationFromDOI503JSONResponse{MetadataUnavailableJSONResponse: gen.MetadataUnavailableJSONResponse(metadataUnavailable)}, nil
	case errors.Is(err, domain.ErrNotFound):
		// A valid token for an account that no longer exists, as in addMyPublication.
		return gen.AddMyPublicationFromDOI401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	case err != nil:
		return nil, err
	}
	p, err := publicationBody(res.Publication)
	if err != nil {
		return nil, err
	}
	return gen.AddMyPublicationFromDOI201JSONResponse{
		Id: p.Id, Doi: p.Doi, Title: p.Title, Year: p.Year, Venue: p.Venue, Abstract: p.Abstract, Url: p.Url,
		Source: p.Source, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, Linked: res.Linked,
	}, nil
}
