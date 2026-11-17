// Package usecase holds the notification module's logic: creating and removing the
// notifications other modules report (FR-9.1), listing them (FR-9.5), counting the unread
// ones (FR-9.4) and marking them read (FR-9.6). It imports only the notification domain.
package usecase

import (
	"context"
	"encoding/base64"
	"fmt"
	"maps"
	"slices"
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
	store     domain.Store
	publisher domain.Publisher
	titles    domain.SubjectTitles
}

type noTitles struct{}

func (noTitles) Titles(context.Context, string, string, []string) (map[string]string, error) {
	return nil, nil
}

type nopPublisher struct{}

func (nopPublisher) Created(context.Context, string, string) {}

func (nopPublisher) UnreadChanged(context.Context, string) {}

// Option changes a Service.
type Option func(*Service)

// WithPublisher tells p about changes. Without it nobody is told.
func WithPublisher(p domain.Publisher) Option {
	return func(s *Service) { s.publisher = p }
}

// WithSubjectTitles reads the titles of the items notifications name from t. Without it no
// notification carries its subject.
func WithSubjectTitles(t domain.SubjectTitles) Option {
	return func(s *Service) { s.titles = t }
}

// New returns a Service.
func New(store domain.Store, opts ...Option) *Service {
	s := &Service{store: store, publisher: nopPublisher{}, titles: noTitles{}}
	for _, o := range opts {
		o(s)
	}
	return s
}

// put stores n and reports it.
func (s *Service) put(ctx context.Context, n domain.Notification) error {
	id, err := s.store.Put(ctx, n)
	if err != nil {
		return err
	}
	s.publisher.Created(ctx, id, n.RecipientID)
	s.publisher.UnreadChanged(ctx, n.RecipientID)
	return nil
}

// ConnectionRequestSent tells receiverID that requesterID sent them request requestID
// (FR-9.1). A second request between the same two users replaces the first notification.
func (s *Service) ConnectionRequestSent(ctx context.Context, requestID, requesterID, receiverID string) error {
	return s.put(ctx, domain.Notification{
		RecipientID: receiverID, ActorID: requesterID,
		Type: domain.TypeConnectionRequestReceived, SubjectID: requestID,
	})
}

// ConnectionRequestAccepted tells requesterID, who sent request requestID, that
// receiverID accepted it (FR-9.1).
func (s *Service) ConnectionRequestAccepted(ctx context.Context, requestID, requesterID, receiverID string) error {
	return s.put(ctx, domain.Notification{
		RecipientID: requesterID, ActorID: receiverID,
		Type: domain.TypeConnectionRequestAccepted, SubjectID: requestID,
	})
}

// AccountWarned tells userID that an administrator warned them (FR-15.5). It is a system
// notification: the recipient never learns which administrator it was.
func (s *Service) AccountWarned(ctx context.Context, warningID, userID string) error {
	return s.put(ctx, domain.Notification{RecipientID: userID, Type: domain.TypeAccountWarning, SubjectID: warningID})
}

// AttachmentBlocked tells userID that a file they attached in conversationID was deleted
// because the malware scan found something in it (FR-16.7). A system notification.
func (s *Service) AttachmentBlocked(ctx context.Context, conversationID, userID string) error {
	return s.put(ctx, domain.Notification{RecipientID: userID, Type: domain.TypeAttachmentBlocked, SubjectID: conversationID})
}

// GroupMembersAdded tells each user that ownerID added them to group conversationID (FR-16.6).
func (s *Service) GroupMembersAdded(ctx context.Context, conversationID, ownerID string, userIDs []string) error {
	for _, u := range userIDs {
		if err := s.put(ctx, domain.Notification{RecipientID: u, ActorID: ownerID, Type: domain.TypeGroupAdded, SubjectID: conversationID}); err != nil {
			return err
		}
	}
	return nil
}

// GroupAccessLost takes back userID's notification of being added to group conversationID
// once they left or were removed: its link would lead to a 404.
func (s *Service) GroupAccessLost(ctx context.Context, conversationID, userID string) error {
	deleted, err := s.store.DeleteFor(ctx, domain.TypeGroupAdded, conversationID, userID)
	if err != nil {
		return err
	}
	if deleted {
		s.publisher.UnreadChanged(ctx, userID)
	}
	return nil
}

// ProjectEvent tells recipientID that actorID did t on project projectID (FR-9.2, FR-11,
// FR-10.5). t must be a project type. Telling the same thing again replaces the earlier
// notification.
func (s *Service) ProjectEvent(ctx context.Context, t domain.Type, projectID, recipientID, actorID string) error {
	if !domain.IsProjectType(t) {
		return fmt.Errorf("notification type %q is not a project type", t)
	}
	return s.put(ctx, domain.Notification{RecipientID: recipientID, ActorID: actorID, Type: t, SubjectID: projectID})
}

// ProjectRequestHandled takes back the notification of senderID's request to join
// projectID, once it was cancelled or decided: whoever owned the project then.
func (s *Service) ProjectRequestHandled(ctx context.Context, projectID, senderID string) error {
	recipients, err := s.store.DeleteFrom(ctx, domain.TypeProjectJoinRequest, projectID, senderID)
	if err != nil {
		return err
	}
	for _, r := range recipients {
		s.publisher.UnreadChanged(ctx, r)
	}
	return nil
}

// ProjectInvitationHandled takes back inviteeID's notification of an invitation to
// projectID, once it was cancelled or answered.
func (s *Service) ProjectInvitationHandled(ctx context.Context, projectID, inviteeID string) error {
	deleted, err := s.store.DeleteFor(ctx, domain.TypeProjectInvitation, projectID, inviteeID)
	if err != nil {
		return err
	}
	if deleted {
		s.publisher.UnreadChanged(ctx, inviteeID)
	}
	return nil
}

// ConnectionRequestWithdrawn removes the notification of request requestID after the
// sender cancelled it or the receiver rejected it, so no stale Accept remains. Nobody is
// told about a rejection (FR-7.6).
func (s *Service) ConnectionRequestWithdrawn(ctx context.Context, requestID string) error {
	recipients, err := s.store.DeleteBySubject(ctx, domain.TypeConnectionRequestReceived, requestID)
	if err != nil {
		return err
	}
	for _, r := range recipients {
		s.publisher.UnreadChanged(ctx, r)
	}
	return nil
}

// RemoveBetween removes everything a and b were told about each other, once one has blocked
// the other (FR-16.9): pending requests, and accepted ones, which name a person the
// recipient can no longer open.
func (s *Service) RemoveBetween(ctx context.Context, a, b string) error {
	if err := s.store.DeleteBetween(ctx, a, b); err != nil {
		return err
	}
	s.publisher.UnreadChanged(ctx, a)
	s.publisher.UnreadChanged(ctx, b)
	return nil
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
		if err := complete(&items[i]); err != nil {
			return domain.Page{}, err
		}
	}
	if err := s.withSubjects(ctx, recipientID, items); err != nil {
		return domain.Page{}, err
	}
	return domain.Page{Items: items, NextCursor: next}, nil
}

// withSubjects gives each item whose type shows its subject the subject's current title,
// asking once per kind of subject for the whole page.
func (s *Service) withSubjects(ctx context.Context, recipientID string, items []domain.Item) error {
	byKind := map[string][]string{}
	for _, it := range items {
		if spec := domain.Registry[it.Type]; spec.ShowsSubject && !slices.Contains(byKind[spec.SubjectKind], it.SubjectID) {
			byKind[spec.SubjectKind] = append(byKind[spec.SubjectKind], it.SubjectID)
		}
	}
	kinds := slices.Sorted(maps.Keys(byKind))
	for _, kind := range kinds {
		titles, err := s.titles.Titles(ctx, recipientID, kind, byKind[kind])
		if err != nil {
			return err
		}
		for i := range items {
			spec := domain.Registry[items[i].Type]
			if title, ok := titles[items[i].SubjectID]; ok && spec.ShowsSubject && spec.SubjectKind == kind {
				items[i].Subject = &domain.Subject{Kind: kind, ID: items[i].SubjectID, Title: title}
			}
		}
	}
	return nil
}

// complete fills in what the store does not know.
func complete(it *domain.Item) error {
	spec, ok := domain.Registry[it.Type]
	if !ok {
		return fmt.Errorf("notification %s: type %q has no registry entry", it.ID, it.Type)
	}
	it.Link = spec.Link(it.Notification)
	if it.Type != domain.TypeConnectionRequestReceived {
		it.RequestPending = nil
	}
	return nil
}

// Get returns one of recipientID's notifications, as the list shows it. ErrNotFound for
// another user's, a hidden one or an unknown ID.
func (s *Service) Get(ctx context.Context, recipientID, id string) (domain.Item, error) {
	it, err := s.store.Find(ctx, recipientID, id)
	if err != nil {
		return domain.Item{}, err
	}
	if err := complete(&it); err != nil {
		return domain.Item{}, err
	}
	one := []domain.Item{it}
	if err := s.withSubjects(ctx, recipientID, one); err != nil {
		return domain.Item{}, err
	}
	return one[0], nil
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
	s.publisher.UnreadChanged(ctx, recipientID)
	return nil
}

// MarkAllRead marks all of recipientID's unread notifications read and returns how many
// there were (FR-9.6).
func (s *Service) MarkAllRead(ctx context.Context, recipientID string) (int, error) {
	n, err := s.store.MarkAllRead(ctx, recipientID)
	if err != nil {
		return 0, err
	}
	s.publisher.UnreadChanged(ctx, recipientID)
	return n, nil
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
