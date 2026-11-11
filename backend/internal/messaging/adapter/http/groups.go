package http

import (
	"context"
	"errors"
	"math"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// Error codes of the group operations (docs/openapi.yaml).
const (
	CodeInvalidGroupName  = "invalid_group_name"
	CodeIneligibleMembers = "ineligible_members"
	CodeNotGroupOwner     = "not_group_owner"
	CodeGroupFull         = "group_full"
	CodeCannotRemoveSelf  = "cannot_remove_self"
)

// groupRefusal is why a group change was refused, in the terms the group operations answer with.
type groupRefusal struct {
	invalid    *gen.Error
	forbidden  *gen.Error
	notFound   bool
	full       bool
	limited    bool
	retryAfter int
}

// groupRefusalOf sorts the errors of the group operations; nil for any other error.
func groupRefusalOf(err error) *groupRefusal {
	var (
		field      *domain.InvalidFieldError
		ineligible *domain.IneligibleError
		suspended  *domain.SuspendedError
		limited    *domain.RateLimitedError
	)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrInvalidGroupName):
		f := "name"
		return &groupRefusal{invalid: &gen.Error{Code: CodeInvalidGroupName, Message: "A group name must have 1 to 100 characters on one line.", Field: &f}}
	case errors.As(err, &field):
		f := field.Field
		return &groupRefusal{invalid: &gen.Error{Code: httpx.CodeBadRequest, Message: "List 1 to 49 other people.", Field: &f}}
	case errors.As(err, &ineligible):
		ids, convErr := uuids(ineligible.UserIDs)
		if convErr != nil {
			return nil
		}
		return &groupRefusal{invalid: &gen.Error{Code: CodeIneligibleMembers, Message: "Some of these people cannot be added to the group.", UserIds: &ids}}
	case errors.Is(err, domain.ErrRemoveSelf):
		return &groupRefusal{invalid: &gen.Error{Code: CodeCannotRemoveSelf, Message: "Leave the group instead of removing yourself."}}
	case errors.As(err, &suspended):
		b := suspendedBody(suspended)
		return &groupRefusal{forbidden: &b}
	case errors.Is(err, domain.ErrNotOwner):
		return &groupRefusal{forbidden: &gen.Error{Code: CodeNotGroupOwner, Message: "Only the group's owner can do this."}}
	case errors.Is(err, domain.ErrNotFound):
		return &groupRefusal{notFound: true}
	case errors.Is(err, domain.ErrGroupFull):
		return &groupRefusal{full: true}
	case errors.As(err, &limited):
		return &groupRefusal{limited: true, retryAfter: max(1, int(math.Ceil(limited.RetryAfter.Seconds())))}
	}
	return nil
}

func groupRateLimited(retryAfter int) gen.GroupRateLimitedJSONResponse {
	return gen.GroupRateLimitedJSONResponse{
		Body:    gen.Error{Code: CodeRateLimited, Message: "You have created too many groups or added too many people today. Try again later."},
		Headers: gen.GroupRateLimitedResponseHeaders{RetryAfter: retryAfter},
	}
}

var groupFull = gen.Error{Code: CodeGroupFull, Message: "A group can have at most 50 members."}

func idStrings(ids []openapi_types.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

// CreateGroup creates a group owned by the caller (FR-16.6).
func (h *Handler) CreateGroup(ctx context.Context, req gen.CreateGroupRequestObject) (gen.CreateGroupResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.CreateGroup401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil || req.Body.ClientId == (openapi_types.UUID{}) {
		f := "clientId"
		return gen.CreateGroup400JSONResponse{GroupInvalidJSONResponse: gen.GroupInvalidJSONResponse{
			Code: httpx.CodeBadRequest, Message: "The request is not valid.", Field: &f}}, nil
	}
	it, replayed, err := h.svc.CreateGroup(ctx, id, domain.GroupInput{
		Name: req.Body.Name, MemberIDs: idStrings(req.Body.MemberIds), ClientID: req.Body.ClientId.String(),
	})
	if r := groupRefusalOf(err); r != nil {
		switch {
		case r.invalid != nil:
			return gen.CreateGroup400JSONResponse{GroupInvalidJSONResponse: gen.GroupInvalidJSONResponse(*r.invalid)}, nil
		case r.forbidden != nil:
			return gen.CreateGroup403JSONResponse{AccountSuspendedOrForbiddenJSONResponse: gen.AccountSuspendedOrForbiddenJSONResponse(*r.forbidden)}, nil
		case r.notFound:
			return gen.CreateGroup404JSONResponse{NotFoundJSONResponse: notFound}, nil
		case r.limited:
			return gen.CreateGroup429JSONResponse{GroupRateLimitedJSONResponse: groupRateLimited(r.retryAfter)}, nil
		}
	}
	if err != nil {
		return nil, err
	}
	b, err := conversationBody(it)
	if err != nil {
		return nil, err
	}
	if replayed {
		return gen.CreateGroup200JSONResponse(b), nil
	}
	return gen.CreateGroup201JSONResponse(b), nil
}

// RenameGroup renames a group; only its owner may (FR-16.6).
func (h *Handler) RenameGroup(ctx context.Context, req gen.RenameGroupRequestObject) (gen.RenameGroupResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.RenameGroup401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.RenameGroup400JSONResponse{GroupInvalidJSONResponse: gen.GroupInvalidJSONResponse{Code: httpx.CodeBadRequest, Message: "The request is not valid."}}, nil
	}
	it, err := h.svc.RenameGroup(ctx, id, req.ConversationId, req.Body.Name)
	if r := groupRefusalOf(err); r != nil {
		switch {
		case r.invalid != nil:
			return gen.RenameGroup400JSONResponse{GroupInvalidJSONResponse: gen.GroupInvalidJSONResponse(*r.invalid)}, nil
		case r.forbidden != nil:
			return gen.RenameGroup403JSONResponse{GroupForbiddenJSONResponse: gen.GroupForbiddenJSONResponse(*r.forbidden)}, nil
		case r.notFound:
			return gen.RenameGroup404JSONResponse{NotFoundJSONResponse: notFound}, nil
		}
	}
	if err != nil {
		return nil, err
	}
	b, err := conversationBody(it)
	if err != nil {
		return nil, err
	}
	return gen.RenameGroup200JSONResponse(b), nil
}

// AddGroupMembers adds members to a group; only its owner may (FR-16.6).
func (h *Handler) AddGroupMembers(ctx context.Context, req gen.AddGroupMembersRequestObject) (gen.AddGroupMembersResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.AddGroupMembers401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.AddGroupMembers400JSONResponse{GroupInvalidJSONResponse: gen.GroupInvalidJSONResponse{Code: httpx.CodeBadRequest, Message: "The request is not valid."}}, nil
	}
	it, err := h.svc.AddMembers(ctx, id, req.ConversationId, idStrings(req.Body.UserIds))
	if r := groupRefusalOf(err); r != nil {
		switch {
		case r.invalid != nil:
			return gen.AddGroupMembers400JSONResponse{GroupInvalidJSONResponse: gen.GroupInvalidJSONResponse(*r.invalid)}, nil
		case r.forbidden != nil:
			return gen.AddGroupMembers403JSONResponse{GroupForbiddenJSONResponse: gen.GroupForbiddenJSONResponse(*r.forbidden)}, nil
		case r.notFound:
			return gen.AddGroupMembers404JSONResponse{NotFoundJSONResponse: notFound}, nil
		case r.full:
			return gen.AddGroupMembers409JSONResponse(groupFull), nil
		case r.limited:
			return gen.AddGroupMembers429JSONResponse{GroupRateLimitedJSONResponse: groupRateLimited(r.retryAfter)}, nil
		}
	}
	if err != nil {
		return nil, err
	}
	b, err := conversationBody(it)
	if err != nil {
		return nil, err
	}
	return gen.AddGroupMembers200JSONResponse(b), nil
}

// RemoveGroupMember removes a member from a group; only its owner may (FR-16.6).
func (h *Handler) RemoveGroupMember(ctx context.Context, req gen.RemoveGroupMemberRequestObject) (gen.RemoveGroupMemberResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.RemoveGroupMember401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	err := h.svc.RemoveMember(ctx, id, req.ConversationId, req.UserId)
	if r := groupRefusalOf(err); r != nil {
		switch {
		case r.invalid != nil:
			return gen.RemoveGroupMember400JSONResponse(*r.invalid), nil
		case r.forbidden != nil:
			return gen.RemoveGroupMember403JSONResponse{GroupForbiddenJSONResponse: gen.GroupForbiddenJSONResponse(*r.forbidden)}, nil
		case r.notFound:
			return gen.RemoveGroupMember404JSONResponse{NotFoundJSONResponse: notFound}, nil
		}
	}
	if err != nil {
		return nil, err
	}
	return gen.RemoveGroupMember204Response{}, nil
}

// LeaveGroup ends the caller's membership of a group (FR-16.6).
func (h *Handler) LeaveGroup(ctx context.Context, req gen.LeaveGroupRequestObject) (gen.LeaveGroupResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.LeaveGroup401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	err := h.svc.Leave(ctx, id, req.ConversationId)
	if errors.Is(err, domain.ErrNotFound) {
		return gen.LeaveGroup404JSONResponse{NotFoundJSONResponse: notFound}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.LeaveGroup204Response{}, nil
}
