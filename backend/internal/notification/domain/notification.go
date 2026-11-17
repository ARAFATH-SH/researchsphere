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

// Types (FR-9.1, FR-9.2). Later slices add the comment (FR-9.3) and like (FR-9.7) types:
// each is a constant here and an entry in Registry.
const (
	// TypeConnectionRequestReceived: the actor sent the recipient a connection request.
	// The subject is the request.
	TypeConnectionRequestReceived Type = "connection_request_received"
	// TypeConnectionRequestAccepted: the actor accepted the recipient's connection
	// request. The subject is the request, now a connection.
	TypeConnectionRequestAccepted Type = "connection_request_accepted"
	// TypeAccountWarning: an administrator warned the recipient (FR-15.5). A system
	// notification: it has no actor. The subject is the warning.
	TypeAccountWarning Type = "account_warning"
	// TypeGroupAdded: the actor, a group's owner, added the recipient to the group (FR-16.6).
	// The subject is the conversation.
	TypeGroupAdded Type = "group_added"
	// TypeAttachmentBlocked: the malware scan found something in a file the recipient attached,
	// and it was deleted (FR-16.7). A system notification: it has no actor. The subject is the
	// conversation the file was uploaded to.
	TypeAttachmentBlocked Type = "attachment_blocked"

	// Project membership (FR-9.2, FR-11, FR-10.5). The subject is the project; the actor is
	// who acted and the recipient who is told.
	//
	// TypeProjectJoinRequest: the actor asked to join the recipient's project.
	TypeProjectJoinRequest Type = "project_join_request"
	// TypeProjectJoinRequestAccepted, TypeProjectJoinRequestRejected: the actor, the owner,
	// decided the recipient's request.
	TypeProjectJoinRequestAccepted Type = "project_join_request_accepted"
	TypeProjectJoinRequestRejected Type = "project_join_request_rejected"
	// TypeProjectInvitation: the actor, the owner, invited the recipient.
	TypeProjectInvitation Type = "project_invitation"
	// TypeProjectInvitationAccepted, TypeProjectInvitationRejected: the actor answered the
	// recipient's invitation.
	TypeProjectInvitationAccepted Type = "project_invitation_accepted"
	TypeProjectInvitationRejected Type = "project_invitation_rejected"
	// TypeProjectMemberRoleChanged, TypeProjectMemberRemoved: the actor, the owner, changed
	// the recipient's role or removed them.
	TypeProjectMemberRoleChanged Type = "project_member_role_changed"
	TypeProjectMemberRemoved     Type = "project_member_removed"
	// TypeProjectOwnershipTransferred: the actor made the recipient the owner.
	TypeProjectOwnershipTransferred Type = "project_ownership_transferred"
)

// Subject kinds: what a notification's SubjectID names.
const (
	SubjectConnectionRequest = "connection_request"
	SubjectWarning           = "warning"
	SubjectConversation      = "conversation"
	SubjectProject           = "project"
)

// Spec describes one Type.
type Spec struct {
	// SubjectKind is what SubjectID names.
	SubjectKind string
	// Link is the web client path of the item the notification is about (FR-9.5).
	Link func(n Notification) string
	// ShowsSubject is set when the client's sentence names the subject (the group of
	// group_added): the list then carries its current title, from SubjectTitles.
	ShowsSubject bool
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
	TypeAccountWarning: {
		SubjectKind: SubjectWarning,
		Link:        func(n Notification) string { return "/warnings/" + n.SubjectID },
	},
	TypeGroupAdded: {
		SubjectKind:  SubjectConversation,
		Link:         func(n Notification) string { return "/messaging/" + n.SubjectID },
		ShowsSubject: true,
	},
	TypeAttachmentBlocked: {
		SubjectKind: SubjectConversation,
		Link:        func(n Notification) string { return "/messaging/" + n.SubjectID },
	},
	TypeProjectJoinRequest:          projectSpec,
	TypeProjectJoinRequestAccepted:  projectSpec,
	TypeProjectJoinRequestRejected:  projectSpec,
	TypeProjectInvitation:           projectSpec,
	TypeProjectInvitationAccepted:   projectSpec,
	TypeProjectInvitationRejected:   projectSpec,
	TypeProjectMemberRoleChanged:    projectSpec,
	TypeProjectMemberRemoved:        projectSpec,
	TypeProjectOwnershipTransferred: projectSpec,
}

// projectSpec is the Spec of every project type: the subject is the project, named by its
// current title, and the link its page.
var projectSpec = Spec{
	SubjectKind:  SubjectProject,
	Link:         func(n Notification) string { return "/projects/" + n.SubjectID },
	ShowsSubject: true,
}

// IsProjectType reports whether t is one of the project types.
func IsProjectType(t Type) bool {
	return Registry[t].SubjectKind == SubjectProject
}

// Notification is one stored notification.
type Notification struct {
	ID          string
	RecipientID string
	ActorID     string // who caused it; empty on a system notification, from ResearchSphere
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
	Actor *Actor // nil on a system notification
	// RequestPending is set only for TypeConnectionRequestReceived: whether the request
	// is still pending, so the client can offer Accept and Ignore.
	RequestPending *bool
	// Link is filled from Registry.
	Link string
	// Subject is the item as it is now, for a type whose Spec.ShowsSubject; nil otherwise, and
	// when the item is gone or the recipient cannot see it.
	Subject *Subject
}

// Subject is the item a notification is about, with its current title.
type Subject struct {
	Kind  string // a Subject* constant
	ID    string
	Title string
}

// SubjectTitles gives the current titles of the items notifications name, as viewerID may see
// them; another module implements it (the messaging module for conversations). An item that
// is gone or that viewerID cannot see has no entry.
type SubjectTitles interface {
	Titles(ctx context.Context, viewerID, kind string, ids []string) (map[string]string, error)
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

// Publisher hears about changes to a user's notifications, so an open page can update at once
// (FR-9.4). It may be called inside a transaction: its implementation waits for the commit
// and cannot fail the change.
type Publisher interface {
	// Created is called when notification id was stored for recipientID.
	Created(ctx context.Context, id, recipientID string)
	// UnreadChanged is called when recipientID's unread count may have changed.
	UnreadChanged(ctx context.Context, recipientID string)
}

// Store reads and writes notifications. Writes run in the transaction the context
// carries, if any, so a notification is stored together with the change it reports.
// "Visible" means the notification has no actor, or its actor is verified and not suspended;
// every read and every mark-as-read leaves the others out, so the count matches the list.
type Store interface {
	// Put stores n (ID, CreatedAt and ReadAt are ignored). Where the schema allows one
	// notification of n.Type per recipient and actor (the connection types), or per
	// recipient, actor and subject (the project types), it replaces the existing one: the
	// new subject, created now, unread.
	// It returns the stored notification's ID.
	Put(ctx context.Context, n Notification) (string, error)
	// DeleteBySubject deletes the notifications of type t about subjectID and returns
	// their recipients.
	DeleteBySubject(ctx context.Context, t Type, subjectID string) ([]string, error)
	// DeleteFor deletes recipientID's notifications of type t about subjectID and reports
	// whether there were any.
	DeleteFor(ctx context.Context, t Type, subjectID, recipientID string) (bool, error)
	// DeleteFrom deletes the notifications of type t about subjectID that actorID caused and
	// returns their recipients.
	DeleteFrom(ctx context.Context, t Type, subjectID, actorID string) ([]string, error)
	// Find returns one visible notification of recipientID, otherwise ErrNotFound.
	Find(ctx context.Context, recipientID, id string) (Item, error)
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
