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

// languageErrors maps the usecase's language validation errors to a code and a message
// for the user.
var languageErrors = []struct {
	err           error
	code, message string
}{
	{domain.ErrInvalidLanguageName, CodeInvalidLanguageName, fmt.Sprintf("Enter a language of 1 to %d characters on one line.", usecase.MaxLanguageNameLength)},
	{domain.ErrInvalidProficiency, CodeInvalidProficiency, "Choose a proficiency from the list."},
}

func languageError(err error) (gen.InvalidLanguageJSONResponse, bool) {
	for _, e := range languageErrors {
		if errors.Is(err, e.err) {
			return gen.InvalidLanguageJSONResponse{Code: e.code, Message: e.message}, true
		}
	}
	return gen.InvalidLanguageJSONResponse{}, false
}

var (
	languageNotFound     = gen.Error{Code: httpx.CodeNotFound, Message: "This language is not on your profile."}
	languageExists       = gen.Error{Code: CodeLanguageExists, Message: "This language is already on your profile."}
	languageLimitReached = gen.Error{Code: CodeLanguageLimitReached, Message: fmt.Sprintf("You can add at most %d languages.", usecase.MaxLanguages)}
	badLanguageRequest   = gen.InvalidLanguageJSONResponse{Code: httpx.CodeBadRequest, Message: "The request is not valid."}
)

// AddMyLanguage adds a language to the caller's profile (FR-4.9).
func (h *Handler) AddMyLanguage(ctx context.Context, req gen.AddMyLanguageRequestObject) (gen.AddMyLanguageResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.AddMyLanguage401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.AddMyLanguage400JSONResponse{InvalidLanguageJSONResponse: badLanguageRequest}, nil
	}
	l, err := h.svc.AddLanguage(ctx, id, usecase.LanguageInput{
		Name:        req.Body.Name,
		Proficiency: domain.Proficiency(req.Body.Proficiency),
	})
	if body, ok := languageError(err); ok {
		return gen.AddMyLanguage400JSONResponse{InvalidLanguageJSONResponse: body}, nil
	}
	switch {
	case errors.Is(err, domain.ErrLanguageExists):
		return gen.AddMyLanguage409JSONResponse(languageExists), nil
	case errors.Is(err, domain.ErrLanguageLimitReached):
		return gen.AddMyLanguage409JSONResponse(languageLimitReached), nil
	case errors.Is(err, domain.ErrNotFound):
		// A valid token for an account that no longer exists, as in GET /profiles/me.
		return gen.AddMyLanguage401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	case err != nil:
		return nil, err
	}
	body, err := languageBody(l)
	if err != nil {
		return nil, err
	}
	return gen.AddMyLanguage201JSONResponse(body), nil
}

// UpdateMyLanguage changes the fields the body names on one of the caller's languages
// (FR-4.9). Someone else's language is not found, like one that does not exist.
func (h *Handler) UpdateMyLanguage(ctx context.Context, req gen.UpdateMyLanguageRequestObject) (gen.UpdateMyLanguageResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.UpdateMyLanguage401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.UpdateMyLanguage400JSONResponse{InvalidLanguageJSONResponse: badLanguageRequest}, nil
	}
	l, err := h.svc.UpdateLanguage(ctx, id, req.LanguageId, usecase.LanguagePatch{
		Name:        field(req.Body.Name),
		Proficiency: convert(field(req.Body.Proficiency), func(p gen.LanguageProficiency) domain.Proficiency { return domain.Proficiency(p) }),
	})
	if body, ok := languageError(err); ok {
		return gen.UpdateMyLanguage400JSONResponse{InvalidLanguageJSONResponse: body}, nil
	}
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return gen.UpdateMyLanguage404JSONResponse(languageNotFound), nil
	case errors.Is(err, domain.ErrLanguageExists):
		return gen.UpdateMyLanguage409JSONResponse(languageExists), nil
	case err != nil:
		return nil, err
	}
	body, err := languageBody(l)
	if err != nil {
		return nil, err
	}
	return gen.UpdateMyLanguage200JSONResponse(body), nil
}

// RemoveMyLanguage deletes one of the caller's languages (FR-4.9). Someone else's
// language is not found, like one that does not exist.
func (h *Handler) RemoveMyLanguage(ctx context.Context, req gen.RemoveMyLanguageRequestObject) (gen.RemoveMyLanguageResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.RemoveMyLanguage401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	err := h.svc.RemoveLanguage(ctx, id, req.LanguageId)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return gen.RemoveMyLanguage404JSONResponse(languageNotFound), nil
	case err != nil:
		return nil, err
	}
	return gen.RemoveMyLanguage204Response{}, nil
}

func languageBody(l domain.Language) (gen.Language, error) {
	var id openapi_types.UUID
	if err := id.UnmarshalText([]byte(l.ID)); err != nil {
		return gen.Language{}, fmt.Errorf("language id: %w", err)
	}
	return gen.Language{Id: id, Name: l.Name, Proficiency: gen.LanguageProficiency(l.Proficiency)}, nil
}

// languageBodies converts languages, never returning a nil slice.
func languageBodies(languages []domain.Language) ([]gen.Language, error) {
	out := make([]gen.Language, 0, len(languages))
	for _, l := range languages {
		b, err := languageBody(l)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}
