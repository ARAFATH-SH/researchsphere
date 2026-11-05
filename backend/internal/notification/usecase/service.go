// Package usecase holds the notification module's logic: creating and removing the
// notifications other modules report (FR-9.1), listing them (FR-9.5), counting the unread
// ones (FR-9.4) and marking them read (FR-9.6). It imports only the notification domain.
package usecase

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/notification/domain"
)

// Page sizes (docs/decisions.md).
const (
	DefaultLimit = 20
	MaxLimit     = 50
)

// Service creates and reads notifications.
type Service struct {
	store domain.Store
}

// New returns a Service.
func New(store domain.Store) *Service {
	return &Service{store: store}
}

// ConnectionRequestSent tells receiverID that requesterID sent them request requestID
// (FR-9.1). A second request between the same two users replaces the first notification.
func (s *Service) ConnectionRequestSent(ctx context.Context, requestID, requesterID, receiverID string) error {
	return s.store.Put(ctx, domain.Notification{
		RecipientID: receiverID, ActorID: requesterID,
		Type: domain.TypeConnectionRequestReceived, SubjectID: requestID,
	})
}

// ConnectionRequestAccepted tells requesterID, who sent request requestID, that
// receiverID accepted it (FR-9.1).
func (s *Service) ConnectionRequestAccepted(ctx context.Context, requestID, requesterID, receiverID string) error {
	return s.store.Put(ctx, domain.Notification{
		RecipientID: requesterID, ActorID: receiverID,
		Type: domain.TypeConnectionRequestAccepted, SubjectID: requestID,
	})
}

// AccountWarned tells userID that an administrator warned them (FR-15.5). It is a system
// notification: the recipient never learns which administrator it was.
func (s *Service) AccountWarned(ctx context.Context, warningID, userID string) error {
	return s.store.Put(ctx, domain.Notification{RecipientID: userID, Type: domain.TypeAccountWarning, SubjectID: warningID})
}

// ConnectionRequestWithdrawn removes the notification of request requestID after the
// sender cancelled it or the receiver rejected it, so no stale Accept remains. Nobody is
// told about a rejection (FR-7.6).
func (s *Service) ConnectionRequestWithdrawn(ctx context.Context, requestID string) error {
	return s.store.DeleteBySubject(ctx, domain.TypeConnectionRequestReceived, requestID)
}

// RemoveBetween removes everything a and b were told about each other, once one has blocked
// the other (FR-16.9): pending requests, and accepted ones, which name a person the
// recipient can no longer open.
func (s *Service) RemoveBetween(ctx context.Context, a, b string) error {
	return s.store.DeleteBetween(ctx, a, b)
}

// List returns one page of recipientID's notifications, newest first (FR-9.5). A nil
// limit means DefaultLimit; above MaxLimit it is reduced to MaxLimit; below 1 it is
// ErrInvalidLimit. cursor is the previous page's NextCursor, or empty for the first page;
// one the server did not issue is ErrInvalidCursor.
func (s *Service) List(ctx context.Context, recipientID, cursor string, limitParam *int) (domain.Page, error) {
	limit := DefaultLimit
	if limitParam != nil {
		limit = min(*limitParam, MaxLimit)
	}
	if limit < 1 {
		return domain.Page{}, domain.ErrInvalidLimit
	}
	var after *domain.Cursor
	if cursor != "" {
		c, err := DecodeCursor(cursor)
		if err != nil {
			return domain.Page{}, err
		}
		after = &c
	}
	items, err := s.store.List(ctx, recipientID, after, limit+1)
	if err != nil {
		return domain.Page{}, err
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1]
		c := EncodeCursor(domain.Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
		next = &c
	}
	for i := range items {
		spec, ok := domain.Registry[items[i].Type]
		if !ok {
			return domain.Page{}, fmt.Errorf("notification %s: type %q has no registry entry", items[i].ID, items[i].Type)
		}
		items[i].Link = spec.Link(items[i].Notification)
		if items[i].Type != domain.TypeConnectionRequestReceived {
			items[i].RequestPending = nil
		}
	}
	return domain.Page{Items: items, NextCursor: next}, nil
}

// UnreadCount returns how many of recipientID's notifications are unread (FR-9.4).
func (s *Service) UnreadCount(ctx context.Context, recipientID string) (int, error) {
	return s.store.CountUnread(ctx, recipientID)
}

// MarkRead marks one of recipientID's notifications read (FR-9.6). It succeeds again for
// one already read; another user's, a hidden one or an unknown ID is ErrNotFound.
func (s *Service) MarkRead(ctx context.Context, recipientID, id string) error {
	found, err := s.store.MarkRead(ctx, id, recipientID)
	if err != nil {
		return err
	}
	if !found {
		return domain.ErrNotFound
	}
	return nil
}

// MarkAllRead marks all of recipientID's unread notifications read and returns how many
// there were (FR-9.6).
func (s *Service) MarkAllRead(ctx context.Context, recipientID string) (int, error) {
	return s.store.MarkAllRead(ctx, recipientID)
}

// EncodeCursor turns a position into the opaque value the client sends back.
func EncodeCursor(c domain.Cursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID))
}

// DecodeCursor reads a value made by EncodeCursor; anything else is ErrInvalidCursor.
// The ID is checked only for its shape; the store treats it as a UUID.
func DecodeCursor(s string) (domain.Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return domain.Cursor{}, domain.ErrInvalidCursor
	}
	at, id, ok := strings.Cut(string(raw), "|")
	if !ok || !isUUID(id) {
		return domain.Cursor{}, domain.ErrInvalidCursor
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return domain.Cursor{}, domain.ErrInvalidCursor
	}
	return domain.Cursor{CreatedAt: t, ID: id}, nil
}

// isUUID reports whether s is a UUID in its canonical 36-character form.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				return false
			}
		}
	}
	return true
}
