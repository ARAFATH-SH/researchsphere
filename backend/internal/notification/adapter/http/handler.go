// Package http serves the notification operations in docs/openapi.yaml: the list (FR-9.5),
// the unread count (FR-9.4) and marking one or all read (FR-9.6). It turns the usecase's
// errors into the contract's codes. cmd/api guards each route with its platform action
// (SRS 2.3) before it gets here.
package http

import (
	"context"
	"errors"
	"fmt"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/notification/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// Service is what the handler needs from the usecase.
type Service interface {
	List(ctx context.Context, recipientID, cursor string, limit *int) (domain.Page, error)
	UnreadCount(ctx context.Context, recipientID string) (int, error)
	MarkRead(ctx context.Context, recipientID, id string) error
	MarkAllRead(ctx context.Context, recipientID string) (int, error)
}

// Handler implements the notification operations of gen.StrictServerInterface.
type Handler struct {
	svc Service
}

// NewHandler returns a Handler that uses svc.
func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

var (
	unauthenticated = gen.UnauthorizedJSONResponse{Code: httpx.CodeUnauthenticated, Message: "Log in to continue."}
	notFound        = gen.NotFoundJSONResponse{Code: httpx.CodeNotFound, Message: "This notification does not exist."}
)

// ListNotifications lists the caller's notifications, newest first (FR-9.5).
func (h *Handler) ListNotifications(ctx context.Context, req gen.ListNotificationsRequestObject) (gen.ListNotificationsResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ListNotifications401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	var cursor string
	if req.Params.Cursor != nil {
		cursor = *req.Params.Cursor
	}
	p, err := h.svc.List(ctx, id, cursor, req.Params.Limit)
	switch {
	case errors.Is(err, domain.ErrInvalidCursor):
		return gen.ListNotifications400JSONResponse{Code: httpx.CodeBadRequest, Message: "The cursor is not valid. Start again from the first page."}, nil
	case errors.Is(err, domain.ErrInvalidLimit):
		return gen.ListNotifications400JSONResponse{Code: httpx.CodeBadRequest, Message: "Limit must be a whole number of at least 1."}, nil
	case err != nil:
		return nil, err
	}
	items := make([]gen.Notification, len(p.Items))
	for i, it := range p.Items {
		n, err := NotificationBody(it)
		if err != nil {
			return nil, err
		}
		items[i] = n
	}
	return gen.ListNotifications200JSONResponse{Items: items, NextCursor: p.NextCursor}, nil
}

// GetUnreadNotificationCount counts the caller's unread notifications (FR-9.4).
func (h *Handler) GetUnreadNotificationCount(ctx context.Context, _ gen.GetUnreadNotificationCountRequestObject) (gen.GetUnreadNotificationCountResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.GetUnreadNotificationCount401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	n, err := h.svc.UnreadCount(ctx, id)
	if err != nil {
		return nil, err
	}
	return gen.GetUnreadNotificationCount200JSONResponse{Count: n}, nil
}

// MarkNotificationRead marks one of the caller's notifications read (FR-9.6).
func (h *Handler) MarkNotificationRead(ctx context.Context, req gen.MarkNotificationReadRequestObject) (gen.MarkNotificationReadResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.MarkNotificationRead401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	err := h.svc.MarkRead(ctx, id, req.NotificationId)
	if errors.Is(err, domain.ErrNotFound) {
		return gen.MarkNotificationRead404JSONResponse{NotFoundJSONResponse: notFound}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.MarkNotificationRead204Response{}, nil
}

// MarkAllNotificationsRead marks all of the caller's notifications read (FR-9.6).
func (h *Handler) MarkAllNotificationsRead(ctx context.Context, _ gen.MarkAllNotificationsReadRequestObject) (gen.MarkAllNotificationsReadResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.MarkAllNotificationsRead401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	n, err := h.svc.MarkAllRead(ctx, id)
	if err != nil {
		return nil, err
	}
	return gen.MarkAllNotificationsRead200JSONResponse{Marked: n}, nil
}

// NotificationBody is a notification as the API shows it, also in a real-time event.
func NotificationBody(it domain.Item) (gen.Notification, error) {
	nid, err := uuid(it.ID)
	if err != nil {
		return gen.Notification{}, err
	}
	n := gen.Notification{Id: nid, Type: gen.NotificationType(it.Type), CreatedAt: it.CreatedAt, Read: it.ReadAt != nil, Link: it.Link}
	if it.Actor != nil {
		actor, err := uuid(it.Actor.UserID)
		if err != nil {
			return gen.Notification{}, err
		}
		n.Actor = &gen.NotificationActor{UserId: actor, FullName: it.Actor.FullName, PhotoUrl: mediaURL(it.Actor.PhotoFileID)}
	}
	if it.Subject != nil {
		sid, err := uuid(it.Subject.ID)
		if err != nil {
			return gen.Notification{}, err
		}
		n.Subject = &gen.NotificationSubject{Type: gen.NotificationSubjectType(it.Subject.Kind), Id: sid, Title: it.Subject.Title}
	}
	if it.Type == domain.TypeConnectionRequestReceived {
		rid, err := uuid(it.SubjectID)
		if err != nil {
			return gen.Notification{}, err
		}
		pending := it.RequestPending != nil && *it.RequestPending
		n.RequestId, n.RequestPending = &rid, &pending
	}
	return n, nil
}

func uuid(s string) (openapi_types.UUID, error) {
	var id openapi_types.UUID
	if err := id.UnmarshalText([]byte(s)); err != nil {
		return id, fmt.Errorf("notification id %q: %w", s, err)
	}
	return id, nil
}

// mediaURL is where the client reads a profile photo (FR-4.6), nil when none is set.
func mediaURL(fileID *string) *string {
	if fileID == nil {
		return nil
	}
	u := httpx.MediaURL(*fileID)
	return &u
}
