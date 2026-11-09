// Package domain holds the messaging module's entities and the interfaces its usecase
// needs: direct conversations, message requests, messages, read markers and reports
// (FR-16). It imports only the standard library.
package domain

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// State is a conversation's state (FR-16.1, FR-16.2).
type State string

// States. StateRequest waits for the receiver; StateDeclined is closed for good.
const (
	StateActive   State = "active"
	StateRequest  State = "request"
	StateDeclined State = "declined"
)

// Box is one of the two lists of conversations.
type Box string

// Boxes. BoxInbox holds active conversations and the requests the user sent; BoxRequests
// holds the requests sent to the user that can still be answered.
const (
	BoxInbox    Box = "inbox"
	BoxRequests Box = "requests"
)

// Message is one stored message, as its members see it (FR-16.5).
type Message struct {
	ID             string
	ConversationID string
	// SenderID is the sender; for a system message the actor, empty when it has none (an
	// ownership hand-over, or an actor whose account is gone).
	SenderID string
	Kind     MessageKind
	Event    *SystemEvent // nil for a text message
	// Body is the latest text. It is nil for a deleted message; only DeletedAt says a message
	// was deleted, as a later message may have no text (attachments, slice 14B.3).
	Body      *string
	ClientID  string
	CreatedAt time.Time
	EditedAt  *time.Time // the latest edit, nil when never edited
	DeletedAt *time.Time // nil unless the sender deleted it
	// HasAttachments is set when the message was sent with files (FR-16.7); Attachments are
	// filled where a reader sees them, never for a deleted message.
	HasAttachments bool
	Attachments    []Attachment
}

// Deleted reports whether the sender deleted the message.
func (m Message) Deleted() bool { return m.DeletedAt != nil }

// System reports whether the message is a system event of a group.
func (m Message) System() bool { return m.Kind == KindSystem }

// Sent is the result of sending: the message and its conversation's state. Replayed is
// true when the client ID had been used before and Message is the original.
type Sent struct {
	Message  Message
	State    State
	Replayed bool
}

// Conversation is a conversation as seen by one member.
type Conversation struct {
	ID            string
	Kind          Kind
	State         State
	RequestedBy   *string // who sent the request, nil when it never was one
	LastMessageAt time.Time
	OtherID       string  // the other member of a direct conversation, empty for a group
	OtherReadID   *string // the other member's read marker
	Name          string  // a group's name, empty for a direct conversation
	MyRole        Role    // the caller's role in a group, empty for a direct conversation
	// JoinedAt is when the caller joined: they see only messages created from then on.
	JoinedAt time.Time
}

// IsGroup reports whether the conversation is a group.
func (c Conversation) IsGroup() bool { return c.Kind == KindGroup }

// Person is the other member of a conversation, with the public fields a list shows.
type Person struct {
	UserID      string
	FullName    string
	Headline    *string
	PhotoFileID *string // the profile photo (FR-4.6), nil when not set
	Available   bool    // verified and not suspended; otherwise the conversation is read-only
}

// Preview is the start of a conversation's latest message.
type Preview struct {
	SenderID  string // empty for a system message with no actor
	Kind      MessageKind
	Event     *EventType // set for a system message
	Body      *string    // the first 120 characters of the latest text, nil when deleted or a system message
	Deleted   bool
	CreatedAt time.Time
	// AttachmentCount is how many files the message carries, 0 when deleted (FR-16.7).
	AttachmentCount int
}

// Item is one conversation of a list, as its member sees it.
type Item struct {
	Conversation
	Other       Person     // the other member of a direct conversation
	Group       *GroupInfo // nil for a direct conversation
	LastMessage *Preview
	UnreadCount int
	BlockedByMe bool // the caller has blocked the other member
	BlockedMe   bool // the other member has blocked the caller; never shown to the caller
}

// CanMessage reports whether a message from the caller would be accepted now. In a group,
// where blocks do not apply, it holds while the caller is a member, which reading the item
// already shows.
func (i Item) CanMessage() bool {
	if i.IsGroup() {
		return i.State == StateActive
	}
	return i.State == StateActive && i.Other.Available && !i.BlockedByMe && !i.BlockedMe
}

// Cursor is a position in a list of conversations: its last item's activity and ID.
type Cursor struct {
	LastMessageAt time.Time
	ID            string
}

// Page is one page of conversations. NextCursor is nil on the last page.
type Page struct {
	Items      []Item
	NextCursor *string
}

// MessagePage is one page of a conversation's messages, newest first. NextBefore is nil
// on the last page; OtherReadID is the other member's read marker (FR-16.3). Readers is set
// for a group only: every other current member's marker.
type MessagePage struct {
	Items       []Message
	NextBefore  *string
	OtherReadID *string
	Readers     []Reader
}

// UnreadCounts is what the messaging badge shows (FR-16.3).
type UnreadCounts struct {
	Total    int // unread messages in active conversations
	Requests int // pending requests to the user
}

// Partner is a conversation and its other member.
type Partner struct {
	ConversationID string
	UserID         string
}

// Store reads and writes conversations and messages. Every method runs in the transaction
// the context carries, if any. Methods that take an ID treat a value that is not a UUID as
// a missing row.
type Store interface {
	// UserVisible reports whether the user exists, is verified and is not suspended.
	UserVisible(ctx context.Context, userID string) (bool, error)
	// UsersVisible returns which of ids exist, are verified and are not suspended. IDs that are
	// not UUIDs are not.
	UsersVisible(ctx context.Context, ids []string) ([]string, error)
	// FindSent returns the message senderID sent with clientID, with its conversation's
	// state; found is false when there is none.
	FindSent(ctx context.Context, senderID, clientID string) (sent Sent, found bool, err error)
	// DirectExists reports whether a and b have a direct conversation.
	DirectExists(ctx context.Context, a, b string) (bool, error)
	// CreateDirect stores a direct conversation between a and b and its two members, with
	// requestedBy set unless state is StateActive. It returns created false, and stores
	// nothing, when the pair already has one, also one committed while it waited.
	CreateDirect(ctx context.Context, a, b string, state State, requestedBy *string) (c Conversation, created bool, err error)
	// LockDirect returns a and b's conversation and holds its row until the transaction
	// ends, so sends in one conversation run one at a time. ErrNotFound when there is none.
	LockDirect(ctx context.Context, a, b string) (Conversation, error)
	// LockConversation does the same by ID.
	LockConversation(ctx context.Context, id string) (Conversation, error)
	// ActivateRequest moves a and b's conversation from StateRequest to StateActive. It
	// changes nothing in any other state, so a declined conversation stays closed.
	ActivateRequest(ctx context.Context, a, b string) error
	// AddMessage stores m, sets the conversation's last_message_at and moves the sender's
	// read marker to m. It returns added false, and changes nothing, when the sender has
	// already used m.ClientID.
	AddMessage(ctx context.Context, m Message) (stored Message, added bool, err error)
	// FindConversation returns the conversation when memberID is a member, otherwise
	// ErrNotFound (NFR-13).
	FindConversation(ctx context.Context, id, memberID string) (Conversation, error)
	// FindItem is FindConversation with what a list shows.
	FindItem(ctx context.Context, id, memberID string) (Item, error)
	// ListItems returns memberID's conversations in box, newest activity first, then by
	// ID, after the cursor when it is not nil.
	ListItems(ctx context.Context, memberID string, box Box, after *Cursor, limit int) ([]Item, error)
	// ListMessages returns the conversation's messages viewerID may read: none unless they are
	// a member now, and only those created from the moment they joined (NFR-13). Newest first,
	// before the message ID before when it is not empty. SearchMessages shares the rule.
	ListMessages(ctx context.Context, conversationID, viewerID, before string, limit int) ([]Message, error)
	// FindOwnMessage returns the text message when senderID sent it in the conversation, deleted
	// or not, otherwise ErrNotFound (NFR-13). A system message is never anyone's own.
	FindOwnMessage(ctx context.Context, conversationID, messageID, senderID string) (Message, error)
	// LockOwnMessage is FindOwnMessage holding the message's row until the transaction ends.
	LockOwnMessage(ctx context.Context, conversationID, messageID, senderID string) (Message, error)
	// EditMessage replaces a message's text, keeps the text it replaces as a version and sets
	// edited_at. ErrNotFound when the message is deleted or unknown.
	EditMessage(ctx context.Context, messageID, body string) (Message, error)
	// DeleteMessage leaves a message in place with no body, keeps its last text as a version
	// and sets deleted_at. ErrNotFound when it is already deleted or unknown.
	DeleteMessage(ctx context.Context, messageID string) (Message, error)
	// MessageIn reports whether the message belongs to the conversation and was created at or
	// after since.
	MessageIn(ctx context.Context, conversationID, messageID string, since time.Time) (bool, error)
	// MoveReadMarker moves the member's marker to messageID when that is later than the
	// marker, and reports whether it moved.
	MoveReadMarker(ctx context.Context, conversationID, memberID, messageID string) (bool, error)
	// AnswerRequest moves a pending request to state when receiverID received it and its
	// sender is verified and not suspended. found is false otherwise. It returns the sender.
	AnswerRequest(ctx context.Context, id, receiverID string, state State) (requesterID string, found bool, err error)
	// CountUnread returns the two numbers of the badge.
	CountUnread(ctx context.Context, memberID string) (UnreadCounts, error)
	// DeclineRequests moves a and b's conversation from StateRequest to StateDeclined, and
	// changes nothing in any other state.
	DeclineRequests(ctx context.Context, a, b string) error
	// ActivePeers returns who may see the user's presence: the other member of each active
	// direct conversation, with no block either way (FR-16.4). Groups do not count.
	ActivePeers(ctx context.Context, userID string) ([]string, error)
	// VisiblePeers returns which of ids are among viewerID's ActivePeers. IDs that are not
	// UUIDs are not.
	VisiblePeers(ctx context.Context, viewerID string, ids []string) ([]string, error)
	// Partners returns every direct conversation of the user, in any state, with its other
	// member.
	Partners(ctx context.Context, userID string) ([]Partner, error)
	// DirectID returns a and b's conversation; found is false when there is none.
	DirectID(ctx context.Context, a, b string) (id string, found bool, err error)
	GroupStore
	ReportStore
	AttachmentStore
	SearchStore
}

// Connections says whether two users are connected. The social module owns connections;
// cmd/api implements this with its usecase.
type Connections interface {
	Connected(ctx context.Context, a, b string) (bool, error)
	// ConnectedAmong returns which of ids are connected with userID.
	ConnectedAmong(ctx context.Context, userID string, ids []string) ([]string, error)
}

// BlockState says who has blocked whom between a user and another (FR-16.9).
type BlockState struct {
	ByMe   bool // the user has blocked the other
	ByThem bool // the other has blocked the user
}

// Blocks reads blocks. The social module owns them; cmd/api implements this with its usecase.
type Blocks interface {
	// Lock holds a lock on the pair a, b until the transaction ends, so a send and a block
	// between them run one at a time.
	Lock(ctx context.Context, a, b string) error
	// Between reads who has blocked whom between me and other.
	Between(ctx context.Context, me, other string) (BlockState, error)
	// BlockedAmong returns which of ids are in a block with userID, either way.
	BlockedAmong(ctx context.Context, userID string, ids []string) ([]string, error)
}

// Standing is what a user may do now (FR-15.6, FR-15.7).
type Standing struct {
	Suspended        bool
	SuspensionReason *string
	SuspendedUntil   *time.Time // nil when permanent, or when not suspended
	RestrictedUntil  *time.Time // the end of a messaging restriction, nil when none
}

// Actors tells what a user may do now. The moderation module owns this; cmd/api implements it
// with its usecase. Inside a transaction it holds the user's row until the transaction ends, so
// a suspension or restriction cannot commit between the check and the write.
type Actors interface {
	Standing(ctx context.Context, userID string) (Standing, error)
}

// Limiter counts a user's sends in fixed windows, one counter per window name.
type Limiter interface {
	// Used returns the sends counted in the user's current window and the time left in
	// it; 0 and 0 when no window is open.
	Used(ctx context.Context, name, userID string) (int, time.Duration, error)
	// Record counts n sends. The first of a window opens it for length.
	Record(ctx context.Context, name, userID string, n int, length time.Duration) error
}

// Publisher hears about changes other members should see. It cannot fail the change, and
// nobody hears before the commit: most calls come after it, and PairChanged comes inside
// another module's transaction, where the implementation waits for the commit. The to lists
// name the members to tell. cmd/api implements it with the realtime module.
type Publisher interface {
	// MessageSent tells the to list, and for a text message the sender's own tabs. A system
	// message goes only to the to list.
	MessageSent(ctx context.Context, sent Sent, to []string)
	// MessageEdited and MessageDeleted tell the to list and the sender's own tabs.
	MessageEdited(ctx context.Context, m Message, to []string)
	MessageDeleted(ctx context.Context, m Message, to []string)
	ReadMoved(ctx context.Context, conversationID, readerID, messageID string, to []string)
	RequestAccepted(ctx context.Context, conversationID string, to []string)
	RequestDeclined(ctx context.Context, conversationID string, to []string)
	// PairChanged says what a and b's conversation allows may have changed: they connected,
	// or one blocked or unblocked the other. It does nothing when they have no conversation.
	PairChanged(ctx context.Context, a, b string)
	// MembersChanged tells the to list, the group's current members, what changed (FR-16.6).
	MembersChanged(ctx context.Context, conversationID string, change MemberChange, to []string)
	// AccessLost tells each user, who left or was removed, that the group is gone for them.
	AccessLost(ctx context.Context, conversationID string, userIDs []string)
	// AttachmentUpdated tells the to list that an attachment's scan finished (FR-16.7).
	AttachmentUpdated(ctx context.Context, u AttachmentUpdate, to []string)
}

// Transactor runs fn in one database transaction; it commits when fn returns nil and
// rolls back otherwise.
type Transactor interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Errors of the messaging module.
var (
	ErrNotFound       = errors.New("messaging: not found")
	ErrSelf           = errors.New("messaging: cannot message oneself")
	ErrInvalidBody    = errors.New("messaging: body is empty, too long or not text")
	ErrRequestPending = errors.New("messaging: the request has not been accepted")
	ErrClosed         = errors.New("messaging: the conversation is closed")
	ErrInvalidBox     = errors.New("messaging: unknown box")
	ErrInvalidCursor  = errors.New("messaging: invalid cursor")
	ErrInvalidLimit   = errors.New("messaging: limit is not a whole number of at least 1")
	ErrCannotMessage  = errors.New("messaging: this person cannot be messaged")
	ErrYouBlocked     = errors.New("messaging: the caller has blocked this user")
	// ErrEditWindowClosed refuses an edit of a message older than the edit window (FR-16.5).
	ErrEditWindowClosed = errors.New("messaging: the message can no longer be edited")
)

// SuspendedError refuses a write by a user whose account was suspended after their access token
// was issued (decisions.md, 2026-11-05).
type SuspendedError struct {
	Reason *string
	Until  *time.Time // nil when permanent
}

func (e *SuspendedError) Error() string { return "messaging: the account is suspended" }

// RestrictedError refuses a send during a messaging restriction (FR-15.6).
type RestrictedError struct {
	Until time.Time
}

func (e *RestrictedError) Error() string { return "messaging: the user may not send messages" }

// RateLimitedError says the user has used up a window (FR-16.10).
type RateLimitedError struct {
	RetryAfter time.Duration // until sending is accepted again
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("messaging: rate limited, retry after %s", e.RetryAfter)
}
