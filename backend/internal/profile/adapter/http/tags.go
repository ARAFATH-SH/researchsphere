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

// The operations for interests and skills are the same apart from the kind and the
// generated types; each pair calls one of the helpers below and maps its outcome.

// addResult says which response adding a tag leads to.
type addResult int

const (
	addCreated      addResult = iota // 201: the tag is new to the user
	addExisting                      // 200: the user already had it
	addInvalid                       // 400 with the error in addOutcome.err
	addNoSession                     // 401
	addLimitReached                  // 409 with the error in addOutcome.err
)

type addOutcome struct {
	result addResult
	tag    gen.Tag   // for addCreated and addExisting
	err    gen.Error // for addInvalid and addLimitReached
}

var invalidTagName = gen.Error{Code: CodeInvalidTagName, Message: "Enter a name of 1 to 60 characters."}

// add runs AddTag for the caller and sorts the result into an addOutcome. Unexpected
// errors are returned to the strict server, which answers 500.
func (h *Handler) add(ctx context.Context, kind domain.TagKind, body *gen.AddTagRequest) (addOutcome, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return addOutcome{result: addNoSession}, nil
	}
	if body == nil {
		return addOutcome{result: addInvalid, err: gen.Error{Code: httpx.CodeBadRequest, Message: "The request is not valid."}}, nil
	}
	tag, created, err := h.svc.AddTag(ctx, id, kind, body.Name)
	switch {
	case errors.Is(err, domain.ErrInvalidTagName):
		return addOutcome{result: addInvalid, err: invalidTagName}, nil
	case errors.Is(err, domain.ErrTagLimitReached):
		return addOutcome{result: addLimitReached, err: limitError(kind)}, nil
	case errors.Is(err, domain.ErrNotFound):
		// A valid token for an account that no longer exists, as in GET /profiles/me.
		return addOutcome{result: addNoSession}, nil
	case err != nil:
		return addOutcome{}, err
	}
	t, err := tagBody(tag)
	if err != nil {
		return addOutcome{}, err
	}
	if created {
		return addOutcome{result: addCreated, tag: t}, nil
	}
	return addOutcome{result: addExisting, tag: t}, nil
}

func limitError(kind domain.TagKind) gen.Error {
	if kind == domain.KindSkill {
		return gen.Error{Code: CodeTagLimitReached, Message: fmt.Sprintf("You can add at most %d skills.", usecase.MaxSkills)}
	}
	return gen.Error{Code: CodeTagLimitReached, Message: fmt.Sprintf("You can add at most %d research interests.", usecase.MaxInterests)}
}

// AddMyInterest adds a research interest to the caller's profile (FR-4.2): 201 when it is
// new to the user, 200 when the user already had it.
func (h *Handler) AddMyInterest(ctx context.Context, req gen.AddMyInterestRequestObject) (gen.AddMyInterestResponseObject, error) {
	o, err := h.add(ctx, domain.KindInterest, req.Body)
	if err != nil {
		return nil, err
	}
	switch o.result {
	case addCreated:
		return gen.AddMyInterest201JSONResponse(o.tag), nil
	case addExisting:
		return gen.AddMyInterest200JSONResponse(o.tag), nil
	case addInvalid:
		return gen.AddMyInterest400JSONResponse(o.err), nil
	case addLimitReached:
		return gen.AddMyInterest409JSONResponse(o.err), nil
	default:
		return gen.AddMyInterest401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
}

// AddMySkill adds a skill to the caller's profile (FR-4.2): 201 when it is new to the
// user, 200 when the user already had it.
func (h *Handler) AddMySkill(ctx context.Context, req gen.AddMySkillRequestObject) (gen.AddMySkillResponseObject, error) {
	o, err := h.add(ctx, domain.KindSkill, req.Body)
	if err != nil {
		return nil, err
	}
	switch o.result {
	case addCreated:
		return gen.AddMySkill201JSONResponse(o.tag), nil
	case addExisting:
		return gen.AddMySkill200JSONResponse(o.tag), nil
	case addInvalid:
		return gen.AddMySkill400JSONResponse(o.err), nil
	case addLimitReached:
		return gen.AddMySkill409JSONResponse(o.err), nil
	default:
		return gen.AddMySkill401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
}

var tagNotFound = gen.Error{Code: httpx.CodeNotFound, Message: "This tag is not on your profile."}

// remove runs RemoveTag for the caller. found is false when the user does not have the
// tag; authenticated is false when the context has no user.
func (h *Handler) remove(ctx context.Context, kind domain.TagKind, tagID string) (authenticated, found bool, err error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return false, false, nil
	}
	err = h.svc.RemoveTag(ctx, id, kind, tagID)
	if errors.Is(err, domain.ErrNotFound) {
		return true, false, nil
	}
	return true, err == nil, err
}

// RemoveMyInterest takes a research interest off the caller's profile (FR-4.2).
func (h *Handler) RemoveMyInterest(ctx context.Context, req gen.RemoveMyInterestRequestObject) (gen.RemoveMyInterestResponseObject, error) {
	authenticated, found, err := h.remove(ctx, domain.KindInterest, req.TagId)
	switch {
	case err != nil:
		return nil, err
	case !authenticated:
		return gen.RemoveMyInterest401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	case !found:
		return gen.RemoveMyInterest404JSONResponse(tagNotFound), nil
	default:
		return gen.RemoveMyInterest204Response{}, nil
	}
}

// RemoveMySkill takes a skill off the caller's profile (FR-4.2).
func (h *Handler) RemoveMySkill(ctx context.Context, req gen.RemoveMySkillRequestObject) (gen.RemoveMySkillResponseObject, error) {
	authenticated, found, err := h.remove(ctx, domain.KindSkill, req.TagId)
	switch {
	case err != nil:
		return nil, err
	case !authenticated:
		return gen.RemoveMySkill401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	case !found:
		return gen.RemoveMySkill404JSONResponse(tagNotFound), nil
	default:
		return gen.RemoveMySkill204Response{}, nil
	}
}

var invalidLimit = gen.Error{Code: httpx.CodeBadRequest, Message: "The limit must be a whole number of at least 1."}

// suggest runs SuggestTags with the defaults for missing parameters. ok is false when
// the limit is below 1.
func (h *Handler) suggest(ctx context.Context, kind domain.TagKind, q *string, limit *int) (body gen.TagSuggestions, ok bool, err error) {
	query, n := "", usecase.DefaultSuggestions
	if q != nil {
		query = *q
	}
	if limit != nil {
		n = *limit
	}
	found, err := h.svc.SuggestTags(ctx, kind, query, n)
	if errors.Is(err, domain.ErrInvalidLimit) {
		return gen.TagSuggestions{}, false, nil
	}
	if err != nil {
		return gen.TagSuggestions{}, false, err
	}
	items, err := tagBodies(found)
	if err != nil {
		return gen.TagSuggestions{}, false, err
	}
	return gen.TagSuggestions{Items: items}, true, nil
}

// SuggestInterestTags suggests existing research interests while the user types (FR-4.3).
func (h *Handler) SuggestInterestTags(ctx context.Context, req gen.SuggestInterestTagsRequestObject) (gen.SuggestInterestTagsResponseObject, error) {
	body, ok, err := h.suggest(ctx, domain.KindInterest, req.Params.Q, req.Params.Limit)
	switch {
	case err != nil:
		return nil, err
	case !ok:
		return gen.SuggestInterestTags400JSONResponse(invalidLimit), nil
	default:
		return gen.SuggestInterestTags200JSONResponse(body), nil
	}
}

// SuggestSkillTags suggests existing skills while the user types (FR-4.3).
func (h *Handler) SuggestSkillTags(ctx context.Context, req gen.SuggestSkillTagsRequestObject) (gen.SuggestSkillTagsResponseObject, error) {
	body, ok, err := h.suggest(ctx, domain.KindSkill, req.Params.Q, req.Params.Limit)
	switch {
	case err != nil:
		return nil, err
	case !ok:
		return gen.SuggestSkillTags400JSONResponse(invalidLimit), nil
	default:
		return gen.SuggestSkillTags200JSONResponse(body), nil
	}
}

func tagBody(t domain.Tag) (gen.Tag, error) {
	var id openapi_types.UUID
	if err := id.UnmarshalText([]byte(t.ID)); err != nil {
		return gen.Tag{}, fmt.Errorf("tag id: %w", err)
	}
	return gen.Tag{Id: id, Name: t.Name}, nil
}

// tagBodies converts tags, never returning a nil slice.
func tagBodies(tags []domain.Tag) ([]gen.Tag, error) {
	out := make([]gen.Tag, 0, len(tags))
	for _, t := range tags {
		b, err := tagBody(t)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}
