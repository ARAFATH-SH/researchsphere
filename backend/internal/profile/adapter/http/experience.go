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

// experienceErrors maps the usecase's experience validation errors to a code and a
// message for the user.
var experienceErrors = []struct {
	err           error
	code, message string
}{
	{domain.ErrInvalidTitle, CodeInvalidTitle, fmt.Sprintf("Enter a title of 1 to %d characters on one line.", usecase.MaxTitleLength)},
	{domain.ErrInvalidOrganization, CodeInvalidOrganization, fmt.Sprintf("Enter an organization of 1 to %d characters on one line.", usecase.MaxOrganizationLength)},
	{domain.ErrInvalidEmploymentType, CodeInvalidEmploymentType, "Choose an employment type from the list."},
	{domain.ErrInvalidLocation, CodeInvalidLocation, fmt.Sprintf("The location must be one line of at most %d characters.", usecase.MaxLocationLength)},
	{domain.ErrInvalidLocationType, CodeInvalidLocationType, "Choose on-site, hybrid or remote."},
	{domain.ErrInvalidStartDate, CodeInvalidStartDate, fmt.Sprintf("Enter a start month from January %d to this month.", usecase.MinExperienceYear)},
	{domain.ErrInvalidEndDate, CodeInvalidEndDate, fmt.Sprintf("Enter an end month from January %d to this month.", usecase.MinExperienceYear)},
	{domain.ErrInvalidIsCurrent, CodeInvalidIsCurrent, "Say whether this is your current role."},
	{domain.ErrEndDateRequired, CodeEndDateRequired, "Enter an end month, or mark this as your current role."},
	{domain.ErrEndDateNotAllowed, CodeEndDateNotAllowed, "A current role has no end month."},
	{domain.ErrInvalidDateRange, CodeInvalidDateRange, "The end month must not be before the start month."},
	{domain.ErrInvalidDescription, CodeInvalidDescription, fmt.Sprintf("The description must be at most %d characters.", usecase.MaxExperienceDescriptionLength)},
}

func experienceError(err error) (gen.InvalidExperienceJSONResponse, bool) {
	for _, e := range experienceErrors {
		if errors.Is(err, e.err) {
			return gen.InvalidExperienceJSONResponse{Code: e.code, Message: e.message}, true
		}
	}
	return gen.InvalidExperienceJSONResponse{}, false
}

var (
	experienceNotFound     = gen.Error{Code: httpx.CodeNotFound, Message: "This experience entry is not on your profile."}
	experienceLimitReached = gen.Error{Code: CodeExperienceLimitReached, Message: fmt.Sprintf("You can add at most %d experience entries.", usecase.MaxExperienceEntries)}
	badExperienceRequest   = gen.InvalidExperienceJSONResponse{Code: httpx.CodeBadRequest, Message: "The request is not valid."}
)

// AddMyExperience adds a work experience entry to the caller's profile (FR-4.8).
func (h *Handler) AddMyExperience(ctx context.Context, req gen.AddMyExperienceRequestObject) (gen.AddMyExperienceResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.AddMyExperience401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.AddMyExperience400JSONResponse{InvalidExperienceJSONResponse: badExperienceRequest}, nil
	}
	b := req.Body
	in := usecase.ExperienceInput{
		Title:          b.Title,
		Organization:   b.Organization,
		EmploymentType: domain.EmploymentType(b.EmploymentType),
		Location:       b.Location,
		Start:          yearMonth(b.StartDate),
		IsCurrent:      b.IsCurrent,
		Description:    b.Description,
	}
	if b.LocationType != nil {
		lt := domain.LocationType(*b.LocationType)
		in.LocationType = &lt
	}
	if b.EndDate != nil {
		end := yearMonth(*b.EndDate)
		in.End = &end
	}
	e, err := h.svc.AddExperience(ctx, id, in)
	if body, ok := experienceError(err); ok {
		return gen.AddMyExperience400JSONResponse{InvalidExperienceJSONResponse: body}, nil
	}
	switch {
	case errors.Is(err, domain.ErrExperienceLimitReached):
		return gen.AddMyExperience409JSONResponse(experienceLimitReached), nil
	case errors.Is(err, domain.ErrNotFound):
		// A valid token for an account that no longer exists, as in GET /profiles/me.
		return gen.AddMyExperience401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	case err != nil:
		return nil, err
	}
	body, err := experienceBody(e)
	if err != nil {
		return nil, err
	}
	return gen.AddMyExperience201JSONResponse(body), nil
}

// UpdateMyExperience changes the fields the body names on one of the caller's entries
// (FR-4.8). Someone else's entry is not found, like one that does not exist.
func (h *Handler) UpdateMyExperience(ctx context.Context, req gen.UpdateMyExperienceRequestObject) (gen.UpdateMyExperienceResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.UpdateMyExperience401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.UpdateMyExperience400JSONResponse{InvalidExperienceJSONResponse: badExperienceRequest}, nil
	}
	b := req.Body
	e, err := h.svc.UpdateExperience(ctx, id, req.ExperienceId, usecase.ExperiencePatch{
		Title:          field(b.Title),
		Organization:   field(b.Organization),
		EmploymentType: convert(field(b.EmploymentType), func(t gen.EmploymentType) domain.EmploymentType { return domain.EmploymentType(t) }),
		Location:       field(b.Location),
		LocationType:   convert(field(b.LocationType), func(t gen.LocationType) domain.LocationType { return domain.LocationType(t) }),
		Start:          convert(field(b.StartDate), yearMonth),
		End:            convert(field(b.EndDate), yearMonth),
		IsCurrent:      field(b.IsCurrent),
		Description:    field(b.Description),
	})
	if body, ok := experienceError(err); ok {
		return gen.UpdateMyExperience400JSONResponse{InvalidExperienceJSONResponse: body}, nil
	}
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return gen.UpdateMyExperience404JSONResponse(experienceNotFound), nil
	case err != nil:
		return nil, err
	}
	body, err := experienceBody(e)
	if err != nil {
		return nil, err
	}
	return gen.UpdateMyExperience200JSONResponse(body), nil
}

// RemoveMyExperience deletes one of the caller's entries (FR-4.8). Someone else's entry
// is not found, like one that does not exist.
func (h *Handler) RemoveMyExperience(ctx context.Context, req gen.RemoveMyExperienceRequestObject) (gen.RemoveMyExperienceResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.RemoveMyExperience401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	err := h.svc.RemoveExperience(ctx, id, req.ExperienceId)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return gen.RemoveMyExperience404JSONResponse(experienceNotFound), nil
	case err != nil:
		return nil, err
	}
	return gen.RemoveMyExperience204Response{}, nil
}

// convert changes the value of a usecase.Field, keeping whether it was set or null.
func convert[A, B any](f usecase.Field[A], to func(A) B) usecase.Field[B] {
	out := usecase.Field[B]{Set: f.Set, Null: f.Null}
	if f.Set && !f.Null {
		out.Value = to(f.Value)
	}
	return out
}

func yearMonth(m gen.YearMonth) domain.YearMonth {
	return domain.YearMonth{Year: m.Year, Month: m.Month}
}

func experienceBody(e domain.Experience) (gen.Experience, error) {
	var id openapi_types.UUID
	if err := id.UnmarshalText([]byte(e.ID)); err != nil {
		return gen.Experience{}, fmt.Errorf("experience id: %w", err)
	}
	body := gen.Experience{
		Id:             id,
		Title:          e.Title,
		Organization:   e.Organization,
		EmploymentType: gen.EmploymentType(e.EmploymentType),
		Location:       e.Location,
		StartDate:      gen.YearMonth{Year: e.Start.Year, Month: e.Start.Month},
		IsCurrent:      e.IsCurrent,
		Description:    e.Description,
	}
	if e.LocationType != nil {
		lt := gen.LocationType(*e.LocationType)
		body.LocationType = &lt
	}
	if e.End != nil {
		body.EndDate = &gen.YearMonth{Year: e.End.Year, Month: e.End.Month}
	}
	return body, nil
}

// experienceBodies converts entries, never returning a nil slice.
func experienceBodies(entries []domain.Experience) ([]gen.Experience, error) {
	out := make([]gen.Experience, 0, len(entries))
	for _, e := range entries {
		b, err := experienceBody(e)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}
