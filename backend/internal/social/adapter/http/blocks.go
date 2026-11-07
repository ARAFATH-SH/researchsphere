package http

import (
	"context"
	"errors"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/social/domain"
)

// BlockUser blocks a user (FR-16.9).
func (h *Handler) BlockUser(ctx context.Context, req gen.BlockUserRequestObject) (gen.BlockUserResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.BlockUser401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.BlockUser400JSONResponse{Code: httpx.CodeBadRequest, Message: "The request is not valid."}, nil
	}
	err := h.svc.Block(ctx, id, req.Body.UserId.String())
	switch {
	case errors.Is(err, domain.ErrBlockSelf):
		return gen.BlockUser400JSONResponse{Code: CodeCannotBlockSelf, Message: "You cannot block yourself."}, nil
	case errors.Is(err, domain.ErrNotFound):
		return gen.BlockUser404JSONResponse{NotFoundJSONResponse: profileNotFound}, nil
	case err != nil:
		return nil, err
	}
	return gen.BlockUser204Response{}, nil
}

// UnblockUser unblocks a user (FR-16.9).
func (h *Handler) UnblockUser(ctx context.Context, req gen.UnblockUserRequestObject) (gen.UnblockUserResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.UnblockUser401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if err := h.svc.Unblock(ctx, id, req.UserId); err != nil {
		return nil, err
	}
	return gen.UnblockUser204Response{}, nil
}

// ListBlockedPeople lists the people the caller has blocked (FR-16.9).
func (h *Handler) ListBlockedPeople(ctx context.Context, req gen.ListBlockedPeopleRequestObject) (gen.ListBlockedPeopleResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ListBlockedPeople401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	res, err := h.svc.ListBlocks(ctx, id, pageParam(req.Params.Page))
	switch {
	case errors.Is(err, domain.ErrInvalidPage):
		return gen.ListBlockedPeople400JSONResponse{Code: httpx.CodeBadRequest, Message: "Page must be a whole number of at least 1."}, nil
	case err != nil:
		return nil, err
	}
	items := make([]gen.BlockedPerson, 0, len(res.Items))
	for _, b := range res.Items {
		uid, err := uuid(b.Other.UserID)
		if err != nil {
			return nil, err
		}
		items = append(items, gen.BlockedPerson{UserId: uid, FullName: b.Other.FullName, Headline: b.Other.Headline,
			PhotoUrl: mediaURL(b.Other.PhotoFileID), BlockedAt: b.BlockedAt})
	}
	return gen.ListBlockedPeople200JSONResponse{Items: items, Page: res.Page, PageSize: res.PageSize, Total: res.Total}, nil
}
