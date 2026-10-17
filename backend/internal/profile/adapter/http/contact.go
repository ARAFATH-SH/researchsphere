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

// contactErrors maps the usecase's contact and link validation errors to a code and a
// message for the user.
var contactErrors = []struct {
	err           error
	code, message string
}{
	{domain.ErrInvalidPhone, CodeInvalidPhone, "Enter the phone number in international format, such as +8801712345678."},
	{domain.ErrInvalidBirthday, CodeInvalidBirthday, "Choose a day that exists in the chosen month."},
	{domain.ErrInvalidVisibility, CodeInvalidVisibility, "Choose who can see each item."},
	{domain.ErrInvalidLinkKind, CodeInvalidLinkKind, "Choose what the link points to."},
	{domain.ErrInvalidLinkURL, CodeInvalidLinkURL, fmt.Sprintf("Enter a link that starts with https:// (at most %d characters).", usecase.MaxLinkURLLength)},
}

func contactError(err error) (gen.Error, bool) {
	for _, e := range contactErrors {
		if errors.Is(err, e.err) {
			return gen.Error{Code: e.code, Message: e.message}, true
		}
	}
	return gen.Error{}, false
}

var (
	badContactRequest = gen.Error{Code: httpx.CodeBadRequest, Message: "The request is not valid."}
	linkNotFound      = gen.Error{Code: httpx.CodeNotFound, Message: "This link is not on your profile."}
	linkLimitReached  = gen.Error{Code: CodeLinkLimitReached, Message: fmt.Sprintf("You can add at most %d links.", usecase.MaxLinks)}
)

// GetMyContact returns the caller's contact info and who may see each item (FR-4.7).
func (h *Handler) GetMyContact(ctx context.Context, _ gen.GetMyContactRequestObject) (gen.GetMyContactResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.GetMyContact401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	c, err := h.svc.GetContact(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return gen.GetMyContact401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if err != nil {
		return nil, err
	}
	body, err := contactBody(c)
	if err != nil {
		return nil, err
	}
	return gen.GetMyContact200JSONResponse(body), nil
}

// UpdateMyContact replaces the caller's phone, birthday and visibilities (FR-4.7).
func (h *Handler) UpdateMyContact(ctx context.Context, req gen.UpdateMyContactRequestObject) (gen.UpdateMyContactResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.UpdateMyContact401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.UpdateMyContact400JSONResponse(badContactRequest), nil
	}
	in := usecase.ContactInput{
		EmailVisibility:    domain.Visibility(req.Body.EmailVisibility),
		Phone:              req.Body.Phone,
		PhoneVisibility:    domain.Visibility(req.Body.PhoneVisibility),
		BirthdayVisibility: domain.Visibility(req.Body.BirthdayVisibility),
	}
	if b := req.Body.Birthday; b != nil {
		in.Birthday = &domain.Birthday{Month: b.Month, Day: b.Day}
	}
	c, err := h.svc.UpdateContact(ctx, id, in)
	if e, ok := contactError(err); ok {
		return gen.UpdateMyContact400JSONResponse(e), nil
	}
	if errors.Is(err, domain.ErrNotFound) {
		return gen.UpdateMyContact401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if err != nil {
		return nil, err
	}
	body, err := contactBody(c)
	if err != nil {
		return nil, err
	}
	return gen.UpdateMyContact200JSONResponse(body), nil
}

// AddMyLink adds a link to the caller's profile (FR-4.7).
func (h *Handler) AddMyLink(ctx context.Context, req gen.AddMyLinkRequestObject) (gen.AddMyLinkResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.AddMyLink401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.AddMyLink400JSONResponse(badContactRequest), nil
	}
	l, err := h.svc.AddLink(ctx, id, domain.LinkKind(req.Body.Kind), req.Body.Url)
	if e, ok := contactError(err); ok {
		return gen.AddMyLink400JSONResponse(e), nil
	}
	switch {
	case errors.Is(err, domain.ErrLinkLimitReached):
		return gen.AddMyLink409JSONResponse(linkLimitReached), nil
	case errors.Is(err, domain.ErrNotFound):
		// A valid token for an account that no longer exists, as in GET /profiles/me.
		return gen.AddMyLink401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	case err != nil:
		return nil, err
	}
	body, err := linkBody(l)
	if err != nil {
		return nil, err
	}
	return gen.AddMyLink201JSONResponse(body), nil
}

// RemoveMyLink removes one of the caller's links (FR-4.7). Someone else's link gets the
// same 404 as one that does not exist.
func (h *Handler) RemoveMyLink(ctx context.Context, req gen.RemoveMyLinkRequestObject) (gen.RemoveMyLinkResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.RemoveMyLink401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	err := h.svc.RemoveLink(ctx, id, req.LinkId)
	if errors.Is(err, domain.ErrNotFound) {
		return gen.RemoveMyLink404JSONResponse(linkNotFound), nil
	}
	if err != nil {
		return nil, err
	}
	return gen.RemoveMyLink204Response{}, nil
}

// contactBody builds the owner's view of their contact info.
func contactBody(c domain.Contact) (gen.Contact, error) {
	links, err := linkBodies(c.Links)
	if err != nil {
		return gen.Contact{}, err
	}
	return gen.Contact{
		Email:              openapi_types.Email(c.Email),
		EmailVisibility:    gen.Visibility(c.EmailVisibility),
		Phone:              c.Phone,
		PhoneVisibility:    gen.Visibility(c.PhoneVisibility),
		Birthday:           birthdayBody(c.Birthday),
		BirthdayVisibility: gen.Visibility(c.BirthdayVisibility),
		Links:              links,
	}, nil
}

// publicContactBody builds the contact object of a profile: only what the viewer may
// see; a nil item is left out of the JSON.
func publicContactBody(c domain.VisibleContact) (gen.PublicContact, error) {
	links, err := linkBodies(c.Links)
	if err != nil {
		return gen.PublicContact{}, err
	}
	out := gen.PublicContact{Phone: c.Phone, Birthday: birthdayBody(c.Birthday), Links: links}
	if c.Email != nil {
		email := openapi_types.Email(*c.Email)
		out.Email = &email
	}
	return out, nil
}

func birthdayBody(b *domain.Birthday) *gen.Birthday {
	if b == nil {
		return nil
	}
	return &gen.Birthday{Month: b.Month, Day: b.Day}
}

func linkBodies(links []domain.Link) ([]gen.ProfileLink, error) {
	out := make([]gen.ProfileLink, 0, len(links))
	for _, l := range links {
		b, err := linkBody(l)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

func linkBody(l domain.Link) (gen.ProfileLink, error) {
	var id openapi_types.UUID
	if err := id.UnmarshalText([]byte(l.ID)); err != nil {
		return gen.ProfileLink{}, fmt.Errorf("link id: %w", err)
	}
	return gen.ProfileLink{Id: id, Kind: gen.LinkKind(l.Kind), Url: l.URL}, nil
}
