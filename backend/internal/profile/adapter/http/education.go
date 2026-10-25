package http

import (
	"context"
	"errors"
	"fmt"

	"github.com/oapi-codegen/nullable"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/usecase"
)

// educationErrors maps the usecase's education validation errors to a code and a message
// for the user.
var educationErrors = []struct {
	err           error
	code, message string
}{
	{domain.ErrInvalidEducationInstitution, CodeInvalidInstitution, fmt.Sprintf("Enter an institution of 1 to %d characters on one line.", usecase.MaxEducationInstitutionLength)},
	{domain.ErrInvalidDegree, CodeInvalidDegree, fmt.Sprintf("The degree must be one line of at most %d characters.", usecase.MaxDegreeLength)},
	{domain.ErrInvalidFieldOfStudy, CodeInvalidFieldOfStudy, fmt.Sprintf("The field of study must be one line of at most %d characters.", usecase.MaxFieldOfStudyLength)},
	{domain.ErrInvalidStartYear, CodeInvalidStartYear, fmt.Sprintf("Enter a start year from %d to %d years from now.", usecase.MinEducationYear, usecase.MaxYearsAhead)},
	{domain.ErrInvalidEndYear, CodeInvalidEndYear, "The end year must not be before the start year or more than 10 years from now."},
	{domain.ErrInvalidGrade, CodeInvalidGrade, fmt.Sprintf("The grade must be one line of at most %d characters.", usecase.MaxGradeLength)},
	{domain.ErrInvalidDescription, CodeInvalidDescription, fmt.Sprintf("The description must be at most %d characters.", usecase.MaxEducationDescriptionLength)},
}

func educationError(err error) (gen.InvalidEducationJSONResponse, bool) {
	for _, e := range educationErrors {
		if errors.Is(err, e.err) {
			return gen.InvalidEducationJSONResponse{Code: e.code, Message: e.message}, true
		}
	}
	return gen.InvalidEducationJSONResponse{}, false
}

var (
	educationNotFound     = gen.Error{Code: httpx.CodeNotFound, Message: "This education entry is not on your profile."}
	educationLimitReached = gen.Error{Code: CodeEducationLimitReached, Message: fmt.Sprintf("You can add at most %d education entries.", usecase.MaxEducationEntries)}
	badEducationRequest   = gen.InvalidEducationJSONResponse{Code: httpx.CodeBadRequest, Message: "The request is not valid."}
)

// AddMyEducation adds an education entry to the caller's profile (FR-4.4).
func (h *Handler) AddMyEducation(ctx context.Context, req gen.AddMyEducationRequestObject) (gen.AddMyEducationResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.AddMyEducation401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.AddMyEducation400JSONResponse{InvalidEducationJSONResponse: badEducationRequest}, nil
	}
	e, err := h.svc.AddEducation(ctx, id, usecase.EducationInput{
		Institution:  req.Body.Institution,
		Degree:       req.Body.Degree,
		FieldOfStudy: req.Body.FieldOfStudy,
		StartYear:    req.Body.StartYear,
		EndYear:      req.Body.EndYear,
		Grade:        req.Body.Grade,
		Description:  req.Body.Description,
	})
	if body, ok := educationError(err); ok {
		return gen.AddMyEducation400JSONResponse{InvalidEducationJSONResponse: body}, nil
	}
	switch {
	case errors.Is(err, domain.ErrEducationLimitReached):
		return gen.AddMyEducation409JSONResponse(educationLimitReached), nil
	case errors.Is(err, domain.ErrNotFound):
		// A valid token for an account that no longer exists, as in GET /profiles/me.
		return gen.AddMyEducation401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	case err != nil:
		return nil, err
	}
	body, err := educationBody(e)
	if err != nil {
		return nil, err
	}
	return gen.AddMyEducation201JSONResponse(body), nil
}

// UpdateMyEducation changes the fields the body names on one of the caller's entries
// (FR-4.4). Someone else's entry is not found, like one that does not exist.
func (h *Handler) UpdateMyEducation(ctx context.Context, req gen.UpdateMyEducationRequestObject) (gen.UpdateMyEducationResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.UpdateMyEducation401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.UpdateMyEducation400JSONResponse{InvalidEducationJSONResponse: badEducationRequest}, nil
	}
	e, err := h.svc.UpdateEducation(ctx, id, req.EducationId, usecase.EducationPatch{
		Institution:  field(req.Body.Institution),
		Degree:       field(req.Body.Degree),
		FieldOfStudy: field(req.Body.FieldOfStudy),
		StartYear:    field(req.Body.StartYear),
		EndYear:      field(req.Body.EndYear),
		Grade:        field(req.Body.Grade),
		Description:  field(req.Body.Description),
	})
	if body, ok := educationError(err); ok {
		return gen.UpdateMyEducation400JSONResponse{InvalidEducationJSONResponse: body}, nil
	}
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return gen.UpdateMyEducation404JSONResponse(educationNotFound), nil
	case err != nil:
		return nil, err
	}
	body, err := educationBody(e)
	if err != nil {
		return nil, err
	}
	return gen.UpdateMyEducation200JSONResponse(body), nil
}

// RemoveMyEducation deletes one of the caller's entries (FR-4.4). Someone else's entry is
// not found, like one that does not exist.
func (h *Handler) RemoveMyEducation(ctx context.Context, req gen.RemoveMyEducationRequestObject) (gen.RemoveMyEducationResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.RemoveMyEducation401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	err := h.svc.RemoveEducation(ctx, id, req.EducationId)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return gen.RemoveMyEducation404JSONResponse(educationNotFound), nil
	case err != nil:
		return nil, err
	}
	return gen.RemoveMyEducation204Response{}, nil
}

// field turns a field of the request body into a usecase.Field: missing, null or a value.
func field[T any](n nullable.Nullable[T]) usecase.Field[T] {
	switch {
	case !n.IsSpecified():
		return usecase.Field[T]{}
	case n.IsNull():
		return usecase.Field[T]{Set: true, Null: true}
	default:
		return usecase.Field[T]{Set: true, Value: n.MustGet()}
	}
}

func educationBody(e domain.Education) (gen.Education, error) {
	var id openapi_types.UUID
	if err := id.UnmarshalText([]byte(e.ID)); err != nil {
		return gen.Education{}, fmt.Errorf("education id: %w", err)
	}
	return gen.Education{
		Id:           id,
		Institution:  e.Institution,
		Degree:       e.Degree,
		FieldOfStudy: e.FieldOfStudy,
		StartYear:    e.StartYear,
		EndYear:      e.EndYear,
		Grade:        e.Grade,
		Description:  e.Description,
	}, nil
}

// educationBodies converts entries, never returning a nil slice.
func educationBodies(entries []domain.Education) ([]gen.Education, error) {
	out := make([]gen.Education, 0, len(entries))
	for _, e := range entries {
		b, err := educationBody(e)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}
