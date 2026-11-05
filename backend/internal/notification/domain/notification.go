// Package domain holds the notification module's entities, its type registry and the
// interfaces its usecase needs (FR-9). It imports only the standard library.
package domain

import (
	"context"
	"errors"
	"time"
)

// Type says what happened. The values are those of the notification_type enum.
type Type string

// Types (FR-9.1). Later slices add the project (FR-9.2), comment (FR-9.3) and like
// (FR-9.7) types: each is a constant here and an entry in Registry.
const (
	// TypeConnectionRequestReceived: the actor sent the recipient a connection request.
	// The subject is the request.
	TypeConnectionRequestReceived Type = "connection_request_received"
	// TypeConnectionRequestAccepted: the actor accepted the recipient's connection
	// request. The subject is the request, now a connection.
	TypeConnectionRequestAccepted Type = "connection_request_accepted"
)

// Subject kinds: what a notification's SubjectID names.
const (
	SubjectConnectionRequest = "connection_request"
)

// Spec describes one Type.
type Spec struct {
	// SubjectKind is what SubjectID names.
	SubjectKind string
	// Link is the web client path of the item the notification is about (FR-9.5).
	Link func(n Notification) string
}

// Registry maps every Type to its Spec. A stored type without an entry is an error when
// listed, so a missing entry cannot go unnoticed.
var Registry = map[Type]Spec{
	TypeConnectionRequestReceived: {
		SubjectKind: SubjectConnectionRequest,
		Link:        func(Notification) string { return "/network" },
	},
	TypeConnectionRequestAccepted: {
		SubjectKind: SubjectConnectionRequest,
		Link:        func(n Notification) string { return "/profile/" + n.ActorID },
	},
}

// Notification is one stored notification.
type Notification struct {
	ID          string
	RecipientID string
	ActorID     string // who caused it
	Type        Type
	SubjectID   string // the item it is about; Registry gives its kind
	CreatedAt   time.Time
	ReadAt      *time.Time // nil while unread
}

// Actor is the public face of the user who caused a notification.
type Actor struct {
	UserID      string
	FullName    string
	PhotoFileID *string // the profile photo (FR-4.6), nil when not set
}

// Item is one notification as the list shows it.
type Item struct {
	Notification
	Actor Actor
	// RequestPending is set only for TypeConnectionRequestReceived: whether the request
	// is still pending, so the client can offer Accept and Ignore.
	RequestPending *bool
	// Link is filled from Registry.
	Link string
}

// Cursor is the position after the last item of a page: the list is ordered by
// CreatedAt, then ID, both descending.
type Cursor struct {
	CreatedAt time.Time
	ID        string
}

// Page is one page of a user's notifications. NextCursor is nil on the last page.
type Page struct {
	Items      []Item
	NextCursor *string
}

// Store reads and writes notifications. Writes run in the transaction the context
// carries, if any, so a notification is stored together with the change it reports.
// "Visible" means the actor is verified and not suspended; every read and every
// mark-as-read leaves the others out, so the count matches the list.
type Store interface {
	// Put stores n (ID, CreatedAt and ReadAt are ignored). Where the schema allows one
	// notification of n.Type per recipient and actor, it replaces the existing one: the
	// new subject, created now, unread.
	Put(ctx context.Context, n Notification) error
	// DeleteBySubject deletes the notifications of type t about subjectID.
	DeleteBySubject(ctx context.Context, t Type, subjectID string) error
	// DeleteBetween deletes everything a and b were told about each other, of any type.
	DeleteBetween(ctx context.Context, a, b string) error
	// List returns up to limit visible notifications of recipientID, newest first,
	// after the cursor when it is not nil.
	List(ctx context.Context, recipientID string, after *Cursor, limit int) ([]Item, error)
	// CountUnread counts the visible unread notifications of recipientID.
	CountUnread(ctx context.Context, recipientID string) (int, error)
	// MarkRead marks the visible notification id of recipientID read, keeping an earlier
	// read time. found is false when there is no such notification, also for an id that
	// is not a UUID.
	MarkRead(ctx context.Context, id, recipientID string) (found bool, err error)
	// MarkAllRead marks every visible unread notification of recipientID read and
	// returns how many there were.
	MarkAllRead(ctx context.Context, recipientID string) (int, error)
}

// Errors of the notification module.
var (
	ErrNotFound      = errors.New("notification: not found")
	ErrInvalidCursor = errors.New("notification: cursor is not valid")
	ErrInvalidLimit  = errors.New("notification: limit is below 1")
)
