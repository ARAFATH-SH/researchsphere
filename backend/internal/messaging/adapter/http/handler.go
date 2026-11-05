// Package http serves the messaging operations in docs/openapi.yaml: sending (FR-16.1,
// FR-16.2), the lists of conversations and messages, read markers and the unread counts
// (FR-16.3), answering requests (FR-16.2), and reports and their review (FR-16.9). It turns
// the usecase's errors into the contract's codes. cmd/api guards each route with its platform action (SRS 2.3) before
// it gets here; membership is checked by the usecase (NFR-14).
package http

import (
	"context"
	"errors"
	"fmt"
	"math"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// Error codes of the messaging operations (docs/openapi.yaml).
const (
	CodeCannotMessageSelf  = "cannot_message_self"
	CodeInvalidBody        = "invalid_body"
	CodeRequestPending     = "request_pending"
	CodeConversationClosed = "conversation_closed"
	CodeRateLimited        = "rate_limited"
	CodeCannotMessage      = "cannot_message"
	CodeYouBlocked         = "you_blocked_this_user"
	CodeOwnMessage         = "cannot_report_own_message"
	CodeAlreadyResolved    = "report_already_resolved"
)

// Service is what the handler needs from the usecase.
type Service interface {
	SendDirect(ctx context.Context, senderID, recipientID, body, clientID string) (domain.Sent, error)
	Send(ctx context.Context, senderID, conversationID, body, clientID string) (domain.Sent, error)
	List(ctx context.Context, userID, box, cursor string, limit *int) (domain.Page, error)
	Get(ctx context.Context, userID, id string) (domain.Item, error)
	Messages(ctx context.Context, userID, id, before string, limit *int) (domain.MessagePage, error)
	MarkRead(ctx context.Context, userID, id, messageID string) error
	Accept(ctx context.Context, userID, id string) (domain.Item, error)
	Decline(ctx context.Context, userID, id string) (domain.Item, error)
	UnreadCounts(ctx context.Context, userID string) (domain.UnreadCounts, error)
	Report(ctx context.Context, reporterID, conversationID string, in domain.ReportInput) (domain.ReportReceipt, error)
	ListReports(ctx context.Context, status string, page int) (domain.ReportPage, error)
	ReportDetail(ctx context.Context, adminID, id, before string) (domain.ReportDetail, error)
	ResolveReport(ctx context.Context, adminID, id string, outcome domain.ReportOutcome, note *string) (domain.Report, error)
}

// Handler implements the messaging operations of gen.StrictServerInterface.
type Handler struct {
	svc Service
}

// NewHandler returns a Handler that uses svc.
func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

var (
	unauthenticated = gen.UnauthorizedJSONResponse{Code: httpx.CodeUnauthenticated, Message: "Log in to continue."}
	notFound        = gen.NotFoundJSONResponse{Code: httpx.CodeNotFound, Message: "This conversation does not exist."}
	invalidRequest  = gen.InvalidMessageJSONResponse{Code: httpx.CodeBadRequest, Message: "The request is not valid."}
)

// refusal is why a send was refused, in the terms both send operations answer with.
type refusal struct {
	invalid    *gen.InvalidMessageJSONResponse
	notFound   bool
	forbidden  *gen.MessageForbiddenJSONResponse
	conflict   *gen.MessageRefusedJSONResponse
	retryAfter int // set with limited
	limited    bool
}

// refusalOf sorts the errors of the two send operations; nil for any other error.
func refusalOf(err error) *refusal {
	var limited *domain.RateLimitedError
	switch {
	case errors.Is(err, domain.ErrSelf):
		return &refusal{invalid: &gen.InvalidMessageJSONResponse{Code: CodeCannotMessageSelf, Message: "You cannot send a message to yourself."}}
	case errors.Is(err, domain.ErrInvalidBody):
		return &refusal{invalid: &gen.InvalidMessageJSONResponse{Code: CodeInvalidBody, Message: "A message must have 1 to 5,000 characters."}}
	case errors.Is(err, domain.ErrNotFound):
		return &refusal{notFound: true}
	case errors.Is(err, domain.ErrCannotMessage):
		return &refusal{forbidden: &gen.MessageForbiddenJSONResponse{Code: CodeCannotMessage, Message: "You cannot message this person."}}
	case errors.Is(err, domain.ErrYouBlocked):
		return &refusal{conflict: &gen.MessageRefusedJSONResponse{Code: CodeYouBlocked, Message: "You have blocked this person. Unblock them to send a message."}}
	case errors.Is(err, domain.ErrRequestPending):
		return &refusal{conflict: &gen.MessageRefusedJSONResponse{Code: CodeRequestPending, Message: "This message request has not been accepted yet."}}
	case errors.Is(err, domain.ErrClosed):
		return &refusal{conflict: &gen.MessageRefusedJSONResponse{Code: CodeConversationClosed, Message: "This conversation is closed."}}
	case errors.As(err, &limited):
		return &refusal{limited: true, retryAfter: max(1, int(math.Ceil(limited.RetryAfter.Seconds())))}
	}
	return nil
}

func rateLimited(retryAfter int) gen.MessageRateLimitedJSONResponse {
	return gen.MessageRateLimitedJSONResponse{
		Body:    gen.Error{Code: CodeRateLimited, Message: "You are sending messages too quickly. Try again later."},
		Headers: gen.MessageRateLimitedResponseHeaders{RetryAfter: retryAfter},
	}
}

// SendDirectMessage sends a message to a user (FR-16.1, FR-16.2).
func (h *Handler) SendDirectMessage(ctx context.Context, req gen.SendDirectMessageRequestObject) (gen.SendDirectMessageResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.SendDirectMessage401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil || req.Body.UserId == (openapi_types.UUID{}) || req.Body.ClientId == (openapi_types.UUID{}) {
		return gen.SendDirectMessage400JSONResponse{InvalidMessageJSONResponse: invalidRequest}, nil
	}
	sent, err := h.svc.SendDirect(ctx, id, req.Body.UserId.String(), req.Body.Body, req.Body.ClientId.String())
	if r := refusalOf(err); r != nil {
		switch {
		case r.invalid != nil:
			return gen.SendDirectMessage400JSONResponse{InvalidMessageJSONResponse: *r.invalid}, nil
		case r.forbidden != nil:
			return gen.SendDirectMessage403JSONResponse{MessageForbiddenJSONResponse: *r.forbidden}, nil
		case r.notFound:
			return gen.SendDirectMessage404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{Code: httpx.CodeNotFound, Message: "This person does not exist."}}, nil
		case r.conflict != nil:
			return gen.SendDirectMessage409JSONResponse{MessageRefusedJSONResponse: *r.conflict}, nil
		default:
			return gen.SendDirectMessage429JSONResponse{MessageRateLimitedJSONResponse: rateLimited(r.retryAfter)}, nil
		}
	}
	if err != nil {
		return nil, err
	}
	b, err := sentBody(sent)
	if err != nil {
		return nil, err
	}
	if sent.Replayed {
		return gen.SendDirectMessage200JSONResponse(b), nil
	}
	return gen.SendDirectMessage201JSONResponse(b), nil
}

// SendMessage sends a message in a conversation (FR-16.1, FR-16.2).
func (h *Handler) SendMessage(ctx context.Context, req gen.SendMessageRequestObject) (gen.SendMessageResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.SendMessage401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil || req.Body.ClientId == (openapi_types.UUID{}) {
		return gen.SendMessage400JSONResponse{InvalidMessageJSONResponse: invalidRequest}, nil
	}
	sent, err := h.svc.Send(ctx, id, req.ConversationId, req.Body.Body, req.Body.ClientId.String())
	if r := refusalOf(err); r != nil {
		switch {
		case r.invalid != nil:
			return gen.SendMessage400JSONResponse{InvalidMessageJSONResponse: *r.invalid}, nil
		case r.forbidden != nil:
			return gen.SendMessage403JSONResponse{MessageForbiddenJSONResponse: *r.forbidden}, nil
		case r.notFound:
			return gen.SendMessage404JSONResponse{NotFoundJSONResponse: notFound}, nil
		case r.conflict != nil:
			return gen.SendMessage409JSONResponse{MessageRefusedJSONResponse: *r.conflict}, nil
		default:
			return gen.SendMessage429JSONResponse{MessageRateLimitedJSONResponse: rateLimited(r.retryAfter)}, nil
		}
	}
	if err != nil {
		return nil, err
	}
	b, err := sentBody(sent)
	if err != nil {
		return nil, err
	}
	if sent.Replayed {
		return gen.SendMessage200JSONResponse(b), nil
	}
	return gen.SendMessage201JSONResponse(b), nil
}

// ListConversations lists the caller's conversations in one box.
func (h *Handler) ListConversations(ctx context.Context, req gen.ListConversationsRequestObject) (gen.ListConversationsResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ListConversations401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	p, err := h.svc.List(ctx, id, text(req.Params.Box), text(req.Params.Cursor), req.Params.Limit)
	switch {
	case errors.Is(err, domain.ErrInvalidBox):
		return gen.ListConversations400JSONResponse{Code: httpx.CodeBadRequest, Message: "Box must be inbox or requests."}, nil
	case errors.Is(err, domain.ErrInvalidCursor):
		return gen.ListConversations400JSONResponse{Code: httpx.CodeBadRequest, Message: "The cursor is not valid. Start again from the first page."}, nil
	case errors.Is(err, domain.ErrInvalidLimit):
		return gen.ListConversations400JSONResponse{Code: httpx.CodeBadRequest, Message: "Limit must be a whole number of at least 1."}, nil
	case err != nil:
		return nil, err
	}
	items := make([]gen.Conversation, len(p.Items))
	for i, it := range p.Items {
		if items[i], err = conversationBody(it); err != nil {
			return nil, err
		}
	}
	return gen.ListConversations200JSONResponse{Items: items, NextCursor: p.NextCursor}, nil
}

// GetConversation returns one of the caller's conversations.
func (h *Handler) GetConversation(ctx context.Context, req gen.GetConversationRequestObject) (gen.GetConversationResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.GetConversation401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	it, err := h.svc.Get(ctx, id, req.ConversationId)
	if errors.Is(err, domain.ErrNotFound) {
		return gen.GetConversation404JSONResponse{NotFoundJSONResponse: notFound}, nil
	}
	if err != nil {
		return nil, err
	}
	b, err := conversationBody(it)
	if err != nil {
		return nil, err
	}
	return gen.GetConversation200JSONResponse(b), nil
}

// ListMessages lists a conversation's messages, newest first, with the other member's
// read marker (FR-16.3).
func (h *Handler) ListMessages(ctx context.Context, req gen.ListMessagesRequestObject) (gen.ListMessagesResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ListMessages401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	p, err := h.svc.Messages(ctx, id, req.ConversationId, text(req.Params.Before), req.Params.Limit)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return gen.ListMessages404JSONResponse{NotFoundJSONResponse: notFound}, nil
	case errors.Is(err, domain.ErrInvalidCursor):
		return gen.ListMessages400JSONResponse{Code: httpx.CodeBadRequest, Message: "Before must be a message ID."}, nil
	case errors.Is(err, domain.ErrInvalidLimit):
		return gen.ListMessages400JSONResponse{Code: httpx.CodeBadRequest, Message: "Limit must be a whole number of at least 1."}, nil
	case err != nil:
		return nil, err
	}
	items := make([]gen.Message, len(p.Items))
	for i, m := range p.Items {
		if items[i], err = messageBody(m); err != nil {
			return nil, err
		}
	}
	read, err := optionalUUID(p.OtherReadID)
	if err != nil {
		return nil, err
	}
	return gen.ListMessages200JSONResponse{Items: items, NextBefore: p.NextBefore, OtherLastReadMessageId: read}, nil
}

// MarkConversationRead moves the caller's read marker forward (FR-16.3).
func (h *Handler) MarkConversationRead(ctx context.Context, req gen.MarkConversationReadRequestObject) (gen.MarkConversationReadResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.MarkConversationRead401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil || req.Body.MessageId == (openapi_types.UUID{}) {
		return gen.MarkConversationRead400JSONResponse{Code: httpx.CodeBadRequest, Message: "The request is not valid."}, nil
	}
	err := h.svc.MarkRead(ctx, id, req.ConversationId, req.Body.MessageId.String())
	if errors.Is(err, domain.ErrNotFound) {
		return gen.MarkConversationRead404JSONResponse{NotFoundJSONResponse: notFound}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.MarkConversationRead204Response{}, nil
}

// AcceptMessageRequest accepts a message request (FR-16.2).
func (h *Handler) AcceptMessageRequest(ctx context.Context, req gen.AcceptMessageRequestRequestObject) (gen.AcceptMessageRequestResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.AcceptMessageRequest401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	it, err := h.svc.Accept(ctx, id, req.ConversationId)
	if errors.Is(err, domain.ErrNotFound) {
		return gen.AcceptMessageRequest404JSONResponse{NotFoundJSONResponse: notFound}, nil
	}
	if err != nil {
		return nil, err
	}
	b, err := conversationBody(it)
	if err != nil {
		return nil, err
	}
	return gen.AcceptMessageRequest200JSONResponse(b), nil
}

// DeclineMessageRequest declines a message request (FR-16.2).
func (h *Handler) DeclineMessageRequest(ctx context.Context, req gen.DeclineMessageRequestRequestObject) (gen.DeclineMessageRequestResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.DeclineMessageRequest401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	it, err := h.svc.Decline(ctx, id, req.ConversationId)
	if errors.Is(err, domain.ErrNotFound) {
		return gen.DeclineMessageRequest404JSONResponse{NotFoundJSONResponse: notFound}, nil
	}
	if err != nil {
		return nil, err
	}
	b, err := conversationBody(it)
	if err != nil {
		return nil, err
	}
	return gen.DeclineMessageRequest200JSONResponse(b), nil
}

// GetMessageUnreadCount returns the numbers of the messaging badge (FR-16.3).
func (h *Handler) GetMessageUnreadCount(ctx context.Context, _ gen.GetMessageUnreadCountRequestObject) (gen.GetMessageUnreadCountResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.GetMessageUnreadCount401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	n, err := h.svc.UnreadCounts(ctx, id)
	if err != nil {
		return nil, err
	}
	return gen.GetMessageUnreadCount200JSONResponse{Total: n.Total, Requests: n.Requests}, nil
}

func sentBody(s domain.Sent) (gen.SentMessage, error) {
	m, err := messageBody(s.Message)
	if err != nil {
		return gen.SentMessage{}, err
	}
	return gen.SentMessage{Message: m, ConversationState: gen.ConversationState(s.State)}, nil
}

func messageBody(m domain.Message) (gen.Message, error) {
	id, err := uuid(m.ID)
	if err != nil {
		return gen.Message{}, err
	}
	conversation, err := uuid(m.ConversationID)
	if err != nil {
		return gen.Message{}, err
	}
	sender, err := uuid(m.SenderID)
	if err != nil {
		return gen.Message{}, err
	}
	return gen.Message{Id: id, ConversationId: conversation, SenderId: sender, Body: m.Body, CreatedAt: m.CreatedAt}, nil
}

func conversationBody(it domain.Item) (gen.Conversation, error) {
	id, err := uuid(it.ID)
	if err != nil {
		return gen.Conversation{}, err
	}
	other, err := uuid(it.Other.UserID)
	if err != nil {
		return gen.Conversation{}, err
	}
	requester, err := optionalUUID(it.RequestedBy)
	if err != nil {
		return gen.Conversation{}, err
	}
	c := gen.Conversation{
		Id: id, State: gen.ConversationState(it.State), RequestedBy: requester,
		LastMessageAt: it.LastMessageAt, UnreadCount: it.UnreadCount,
		CanMessage: it.CanMessage(), BlockedByMe: it.BlockedByMe,
		OtherMember: gen.ConversationMember{
			UserId: other, FullName: it.Other.FullName, Headline: it.Other.Headline,
			PhotoUrl: mediaURL(it.Other.PhotoFileID), Available: it.Other.Available,
		},
	}
	if it.LastMessage != nil {
		sender, err := uuid(it.LastMessage.SenderID)
		if err != nil {
			return gen.Conversation{}, err
		}
		c.LastMessage = &gen.MessagePreview{SenderId: sender, Body: it.LastMessage.Body, CreatedAt: it.LastMessage.CreatedAt}
	}
	return c, nil
}

func uuid(s string) (openapi_types.UUID, error) {
	var id openapi_types.UUID
	if err := id.UnmarshalText([]byte(s)); err != nil {
		return id, fmt.Errorf("messaging id %q: %w", s, err)
	}
	return id, nil
}

func optionalUUID(s *string) (*openapi_types.UUID, error) {
	if s == nil {
		return nil, nil
	}
	id, err := uuid(*s)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// text is the value of an optional query parameter, empty when left out.
func text(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// mediaURL is where the client reads a profile photo (FR-4.6), nil when none is set.
func mediaURL(fileID *string) *string {
	if fileID == nil {
		return nil
	}
	u := httpx.MediaURL(*fileID)
	return &u
}
