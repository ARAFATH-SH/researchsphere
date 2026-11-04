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

// publicationErrors maps the usecase's publication validation errors to a code and a
// message for the user.
var publicationErrors = []struct {
	err           error
	code, message string
}{
	{domain.ErrInvalidTitle, CodeInvalidTitle, fmt.Sprintf("Enter a title of 1 to %d characters on one line.", usecase.MaxPublicationTitleLength)},
	{domain.ErrInvalidYear, CodeInvalidYear, fmt.Sprintf("Enter a year from %d to next year.", usecase.MinPublicationYear)},
	{domain.ErrInvalidVenue, CodeInvalidVenue, fmt.Sprintf("The venue must be one line of at most %d characters.", usecase.MaxVenueLength)},
	{domain.ErrInvalidAbstract, CodeInvalidAbstract, fmt.Sprintf("The abstract must be at most %d characters.", usecase.MaxAbstractLength)},
	{domain.ErrInvalidPublicationURL, CodeInvalidURL, fmt.Sprintf("Enter a link that starts with http:// or https:// (at most %d characters).", usecase.MaxPublicationURLLength)},
	{domain.ErrInvalidDOI, CodeInvalidDOI, "Enter a DOI such as 10.1145/3292500.3330701."},
}

func publicationError(err error) (gen.InvalidPublicationJSONResponse, bool) {
	for _, e := range publicationErrors {
		if errors.Is(err, e.err) {
			return gen.InvalidPublicationJSONResponse{Code: e.code, Message: e.message}, true
		}
	}
	return gen.InvalidPublicationJSONResponse{}, false
}

var (
	publicationNotFound       = gen.Error{Code: httpx.CodeNotFound, Message: "This publication is not on your profile."}
	publicationLimitReached   = gen.Error{Code: CodePublicationLimitReached, Message: fmt.Sprintf("You can add at most %d publications.", usecase.MaxPublications)}
	publicationAlreadyAdded   = gen.Error{Code: CodePublicationAlreadyAdded, Message: "This publication is already on your profile."}
	publicationSharedReadonly = gen.Error{Code: CodePublicationSharedReadonly, Message: "This publication is shared with co-authors or was imported, so it cannot be edited."}
	duplicateDOI              = gen.Error{Code: CodeDuplicateDOI, Message: "Another publication already has this DOI. Add it by its DOI instead."}
	badPublicationRequest     = gen.InvalidPublicationJSONResponse{Code: httpx.CodeBadRequest, Message: "The request is not valid."}
	badPublicationPage        = gen.Error{Code: httpx.CodeBadRequest, Message: "Page and page size must be whole numbers of at least 1."}
)

// ListMyPublications returns one page of the caller's confirmed publications (FR-5.5).
func (h *Handler) ListMyPublications(ctx context.Context, req gen.ListMyPublicationsRequestObject) (gen.ListMyPublicationsResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ListMyPublications401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	page, pageSize := pageParams(req.Params.Page, req.Params.PageSize)
	res, err := h.svc.ListOwnPublications(ctx, id, page, pageSize)
	switch {
	case errors.Is(err, domain.ErrInvalidPage):
		return gen.ListMyPublications400JSONResponse(badPublicationPage), nil
	case err != nil:
		return nil, err
	}
	body, err := publicationPageBody(res)
	if err != nil {
		return nil, err
	}
	return gen.ListMyPublications200JSONResponse(body), nil
}

// ListPublications returns one page of any user's confirmed publications to a logged-in
// user (FR-5.5). Unknown, suspended and unverified accounts get the same 404 as their
// profile.
func (h *Handler) ListPublications(ctx context.Context, req gen.ListPublicationsRequestObject) (gen.ListPublicationsResponseObject, error) {
	if _, ok := httpx.UserID(ctx); !ok {
		return gen.ListPublications401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	page, pageSize := pageParams(req.Params.Page, req.Params.PageSize)
	res, err := h.svc.ListPublications(ctx, req.UserId, page, pageSize)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return gen.ListPublications404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{
			Code: httpx.CodeNotFound, Message: "This profile does not exist.",
		}}, nil
	case errors.Is(err, domain.ErrInvalidPage):
		return gen.ListPublications400JSONResponse(badPublicationPage), nil
	case err != nil:
		return nil, err
	}
	body, err := publicationPageBody(res)
	if err != nil {
		return nil, err
	}
	return gen.ListPublications200JSONResponse(body), nil
}

// AddMyPublication adds a publication to the caller's profile by hand (FR-5.1), or links
// the caller to the stored one with the same DOI (FR-5.4).
func (h *Handler) AddMyPublication(ctx context.Context, req gen.AddMyPublicationRequestObject) (gen.AddMyPublicationResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.AddMyPublication401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.AddMyPublication400JSONResponse{InvalidPublicationJSONResponse: badPublicationRequest}, nil
	}
	b := req.Body
	year := b.Year
	res, err := h.svc.AddPublication(ctx, id, usecase.PublicationInput{
		Title:    b.Title,
		Year:     &year,
		Venue:    b.Venue,
		Abstract: b.Abstract,
		URL:      b.Url,
		DOI:      b.Doi,
	})
	if body, ok := publicationError(err); ok {
		return gen.AddMyPublication400JSONResponse{InvalidPublicationJSONResponse: body}, nil
	}
	switch {
	case errors.Is(err, domain.ErrPublicationLimitReached):
		return gen.AddMyPublication409JSONResponse(publicationLimitReached), nil
	case errors.Is(err, domain.ErrPublicationAlreadyAdded):
		return gen.AddMyPublication409JSONResponse(publicationAlreadyAdded), nil
	case errors.Is(err, domain.ErrNotFound):
		// A valid token for an account that no longer exists, as in GET /profiles/me.
		return gen.AddMyPublication401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	case err != nil:
		return nil, err
	}
	p, err := publicationBody(res.Publication)
	if err != nil {
		return nil, err
	}
	return gen.AddMyPublication201JSONResponse{
		Id: p.Id, Doi: p.Doi, Title: p.Title, Year: p.Year, Venue: p.Venue, Abstract: p.Abstract, Url: p.Url,
		Source: p.Source, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, Linked: res.Linked,
	}, nil
}

// UpdateMyPublication changes the fields the body names on one of the caller's
// publications (FR-5.1). One the caller is not linked to is not found, like one that does
// not exist.
func (h *Handler) UpdateMyPublication(ctx context.Context, req gen.UpdateMyPublicationRequestObject) (gen.UpdateMyPublicationResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.UpdateMyPublication401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.UpdateMyPublication400JSONResponse{InvalidPublicationJSONResponse: badPublicationRequest}, nil
	}
	b := req.Body
	p, err := h.svc.UpdatePublication(ctx, id, req.PublicationId, usecase.PublicationPatch{
		Title:    field(b.Title),
		Year:     field(b.Year),
		Venue:    field(b.Venue),
		Abstract: field(b.Abstract),
		URL:      field(b.Url),
		DOI:      field(b.Doi),
	})
	if body, ok := publicationError(err); ok {
		return gen.UpdateMyPublication400JSONResponse{InvalidPublicationJSONResponse: body}, nil
	}
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return gen.UpdateMyPublication404JSONResponse(publicationNotFound), nil
	case errors.Is(err, domain.ErrPublicationSharedReadonly):
		return gen.UpdateMyPublication409JSONResponse(publicationSharedReadonly), nil
	case errors.Is(err, domain.ErrDuplicateDOI):
		return gen.UpdateMyPublication409JSONResponse(duplicateDOI), nil
	case err != nil:
		return nil, err
	}
	body, err := publicationBody(p)
	if err != nil {
		return nil, err
	}
	return gen.UpdateMyPublication200JSONResponse(body), nil
}

// RemoveMyPublication removes a publication from the caller's profile (FR-5.7). One the
// caller is not linked to is not found, like one that does not exist.
func (h *Handler) RemoveMyPublication(ctx context.Context, req gen.RemoveMyPublicationRequestObject) (gen.RemoveMyPublicationResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.RemoveMyPublication401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	err := h.svc.RemovePublication(ctx, id, req.PublicationId)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return gen.RemoveMyPublication404JSONResponse(publicationNotFound), nil
	case err != nil:
		return nil, err
	}
	return gen.RemoveMyPublication204Response{}, nil
}

// pageParams returns the page and page size asked for, with the defaults for missing
// ones; the usecase refuses values below 1 and caps the size.
func pageParams(page, pageSize *int) (int, int) {
	p, size := 1, usecase.DefaultPublicationPageSize
	if page != nil {
		p = *page
	}
	if pageSize != nil {
		size = *pageSize
	}
	return p, size
}

func publicationPageBody(res domain.PublicationPage) (gen.PublicationPage, error) {
	items := make([]gen.Publication, 0, len(res.Items))
	for _, p := range res.Items {
		b, err := publicationBody(p)
		if err != nil {
			return gen.PublicationPage{}, err
		}
		items = append(items, b)
	}
	return gen.PublicationPage{Items: items, Page: res.Page, PageSize: res.PageSize, Total: res.Total}, nil
}

func publicationBody(p domain.Publication) (gen.Publication, error) {
	var id openapi_types.UUID
	if err := id.UnmarshalText([]byte(p.ID)); err != nil {
		return gen.Publication{}, fmt.Errorf("publication id: %w", err)
	}
	return gen.Publication{
		Id:        id,
		Doi:       p.DOI,
		Title:     p.Title,
		Year:      p.Year,
		Venue:     p.Venue,
		Abstract:  p.Abstract,
		Url:       p.URL,
		Source:    gen.PublicationSource(p.Source),
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}, nil
}
