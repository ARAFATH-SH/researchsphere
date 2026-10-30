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

var (
	invalidORCIDID   = gen.Error{Code: CodeInvalidORCIDID, Message: "Enter an ORCID iD such as 0000-0002-1825-0097."}
	badORCIDRequest  = gen.Error{Code: httpx.CodeBadRequest, Message: "The request is not valid."}
	orcidTaken       = gen.Error{Code: CodeORCIDTaken, Message: "Another profile has this ORCID iD."}
	orcidNotSet      = gen.Error{Code: CodeORCIDNotSet, Message: "Add your ORCID iD to your profile first."}
	orcidNotFound    = gen.Error{Code: CodeORCIDNotFound, Message: "ORCID has no record with the iD on your profile. Check it on your profile."}
	orcidUnavailable = gen.Error{Code: CodeMetadataUnavailable, Message: "We couldn't reach ORCID. You can still add publications by DOI or by hand."}
	invalidPutCode   = gen.Error{Code: CodeInvalidPutCode, Message: fmt.Sprintf("Select 1 to %d works from your ORCID list.", usecase.MaxImportPutCodes)}
	importOverLimit  = gen.Error{Code: CodePublicationLimitReached, Message: fmt.Sprintf("This import would take you over %d publications. Select fewer works.", usecase.MaxPublications)}
)

// orcidRateLimited is the 429 body and header for a used-up ORCID window.
func orcidRateLimited(e *domain.RateLimitedError) gen.OrcidRateLimitedJSONResponse {
	return gen.OrcidRateLimitedJSONResponse{
		Body:    gen.Error{Code: CodeRateLimited, Message: "You have listed your ORCID works many times in the last hour. Try again later, or add publications by DOI or by hand."},
		Headers: gen.OrcidRateLimitedResponseHeaders{RetryAfter: retryAfter(e)},
	}
}

// SetMyOrcidId saves or clears the ORCID iD on the caller's profile (FR-5.3).
func (h *Handler) SetMyOrcidId(ctx context.Context, req gen.SetMyOrcidIdRequestObject) (gen.SetMyOrcidIdResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.SetMyOrcidId401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.SetMyOrcidId400JSONResponse(badORCIDRequest), nil
	}
	saved, err := h.svc.SetOrcidID(ctx, id, req.Body.OrcidId)
	switch {
	case errors.Is(err, domain.ErrInvalidORCID):
		return gen.SetMyOrcidId400JSONResponse(invalidORCIDID), nil
	case errors.Is(err, domain.ErrORCIDTaken):
		return gen.SetMyOrcidId409JSONResponse(orcidTaken), nil
	case errors.Is(err, domain.ErrNotFound):
		// A valid token for an account that no longer exists.
		return gen.SetMyOrcidId401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	case err != nil:
		return nil, err
	}
	return gen.SetMyOrcidId200JSONResponse{OrcidId: saved}, nil
}

// ListMyOrcidWorks lists the works of the ORCID iD on the caller's profile (FR-5.3), or
// says clearly that ORCID failed, so the caller can add publications another way (FR-5.6).
func (h *Handler) ListMyOrcidWorks(ctx context.Context, _ gen.ListMyOrcidWorksRequestObject) (gen.ListMyOrcidWorksResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ListMyOrcidWorks401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	list, err := h.svc.ListORCIDWorks(ctx, id)
	var limited *domain.RateLimitedError
	switch {
	case errors.Is(err, domain.ErrORCIDNotSet):
		return gen.ListMyOrcidWorks409JSONResponse{OrcidNotSetJSONResponse: gen.OrcidNotSetJSONResponse(orcidNotSet)}, nil
	case errors.Is(err, domain.ErrORCIDNotFound):
		return gen.ListMyOrcidWorks404JSONResponse{OrcidNotFoundJSONResponse: gen.OrcidNotFoundJSONResponse(orcidNotFound)}, nil
	case errors.As(err, &limited):
		return gen.ListMyOrcidWorks429JSONResponse{OrcidRateLimitedJSONResponse: orcidRateLimited(limited)}, nil
	case errors.Is(err, domain.ErrMetadataUnavailable):
		return gen.ListMyOrcidWorks503JSONResponse{OrcidUnavailableJSONResponse: gen.OrcidUnavailableJSONResponse(orcidUnavailable)}, nil
	case errors.Is(err, domain.ErrNotFound):
		return gen.ListMyOrcidWorks401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	case err != nil:
		return nil, err
	}
	body := gen.OrcidWorkList{OrcidId: list.OrcidID, Items: make([]gen.OrcidWork, len(list.Items))}
	for i, w := range list.Items {
		item := gen.OrcidWork{
			PutCode: w.PutCode, Title: w.Title, Year: w.Year, Venue: w.Venue, Doi: w.DOI, Type: w.Type, Url: w.URL,
			ExistsAlready: w.ExistsAlready, LinkedToMe: w.LinkedToMe,
		}
		if w.PublicationID != "" {
			pid, err := parsePublicationID(w.PublicationID)
			if err != nil {
				return nil, err
			}
			item.PublicationId = &pid
		}
		body.Items[i] = item
	}
	return gen.ListMyOrcidWorks200JSONResponse(body), nil
}

// ImportMyOrcidWorks imports the ORCID works the caller selected (FR-5.3), linking the
// caller to stored records with the same DOI (FR-5.4).
func (h *Handler) ImportMyOrcidWorks(ctx context.Context, req gen.ImportMyOrcidWorksRequestObject) (gen.ImportMyOrcidWorksResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ImportMyOrcidWorks401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.ImportMyOrcidWorks400JSONResponse(badORCIDRequest), nil
	}
	res, err := h.svc.ImportORCIDWorks(ctx, id, req.Body.PutCodes)
	var limited *domain.RateLimitedError
	switch {
	case errors.Is(err, domain.ErrInvalidPutCode):
		return gen.ImportMyOrcidWorks400JSONResponse(invalidPutCode), nil
	case errors.Is(err, domain.ErrORCIDNotSet):
		return gen.ImportMyOrcidWorks409JSONResponse(orcidNotSet), nil
	case errors.Is(err, domain.ErrPublicationLimitReached):
		return gen.ImportMyOrcidWorks409JSONResponse(importOverLimit), nil
	case errors.Is(err, domain.ErrORCIDNotFound):
		return gen.ImportMyOrcidWorks404JSONResponse{OrcidNotFoundJSONResponse: gen.OrcidNotFoundJSONResponse(orcidNotFound)}, nil
	case errors.As(err, &limited):
		return gen.ImportMyOrcidWorks429JSONResponse{OrcidRateLimitedJSONResponse: orcidRateLimited(limited)}, nil
	case errors.Is(err, domain.ErrMetadataUnavailable):
		return gen.ImportMyOrcidWorks503JSONResponse{OrcidUnavailableJSONResponse: gen.OrcidUnavailableJSONResponse(orcidUnavailable)}, nil
	case errors.Is(err, domain.ErrNotFound):
		return gen.ImportMyOrcidWorks401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	case err != nil:
		return nil, err
	}
	body := gen.OrcidImportResult{
		Items:   make([]gen.OrcidImportItem, len(res.Items)),
		Created: res.Created, Linked: res.Linked, Skipped: res.Skipped,
	}
	for i, it := range res.Items {
		pid, err := parsePublicationID(it.PublicationID)
		if err != nil {
			return nil, err
		}
		body.Items[i] = gen.OrcidImportItem{PutCode: it.PutCode, Result: gen.OrcidImportOutcome(it.Outcome), PublicationId: pid}
	}
	return gen.ImportMyOrcidWorks200JSONResponse(body), nil
}

func parsePublicationID(s string) (openapi_types.UUID, error) {
	var pid openapi_types.UUID
	err := pid.UnmarshalText([]byte(s))
	return pid, err
}
