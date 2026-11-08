package http

import (
	"context"
	"errors"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// CodeEditWindowClosed refuses an edit of a message older than the edit window (FR-16.5).
const CodeEditWindowClosed = "edit_window_closed"

var messageNotFound = gen.NotFoundJSONResponse{Code: httpx.CodeNotFound, Message: "This message does not exist."}

// EditMessage replaces the text of the caller's own message (FR-16.5).
func (h *Handler) EditMessage(ctx context.Context, req gen.EditMessageRequestObject) (gen.EditMessageResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.EditMessage401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.EditMessage400JSONResponse{InvalidMessageJSONResponse: invalidRequest}, nil
	}
	m, err := h.svc.Edit(ctx, id, req.ConversationId, req.MessageId, req.Body.Body)
	if errors.Is(err, domain.ErrEditWindowClosed) {
		return gen.EditMessage409JSONResponse{MessageEditRefusedJSONResponse: gen.MessageEditRefusedJSONResponse{
			Code: CodeEditWindowClosed, Message: "This message can no longer be edited.",
		}}, nil
	}
	if r := refusalOf(err); r != nil {
		switch {
		case r.invalid != nil:
			return gen.EditMessage400JSONResponse{InvalidMessageJSONResponse: *r.invalid}, nil
		case r.forbidden != nil:
			return gen.EditMessage403JSONResponse{MessageForbiddenJSONResponse: *r.forbidden}, nil
		case r.notFound:
			return gen.EditMessage404JSONResponse{NotFoundJSONResponse: messageNotFound}, nil
		case r.conflict != nil:
			return gen.EditMessage409JSONResponse{MessageEditRefusedJSONResponse: gen.MessageEditRefusedJSONResponse(*r.conflict)}, nil
		default:
			return gen.EditMessage429JSONResponse{MessageRateLimitedJSONResponse: rateLimited(r.retryAfter)}, nil
		}
	}
	if err != nil {
		return nil, err
	}
	b, err := MessageBody(m)
	if err != nil {
		return nil, err
	}
	return gen.EditMessage200JSONResponse(b), nil
}

// DeleteMessage leaves the caller's own message in place with no text (FR-16.5).
func (h *Handler) DeleteMessage(ctx context.Context, req gen.DeleteMessageRequestObject) (gen.DeleteMessageResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.DeleteMessage401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	err := h.svc.Delete(ctx, id, req.ConversationId, req.MessageId)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return gen.DeleteMessage404JSONResponse{NotFoundJSONResponse: messageNotFound}, nil
	case err != nil:
		return nil, err
	}
	return gen.DeleteMessage204Response{}, nil
}
