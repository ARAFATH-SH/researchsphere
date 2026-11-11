package domain

import (
	"context"
	"errors"
	"time"
)

// Kind is a conversation's kind.
type Kind string

// Kinds. A direct conversation has two members; a group has a name, an owner and up to
// MaxGroupMembers members (FR-16.6).
const (
	KindDirect Kind = "direct"
	KindGroup  Kind = "group"
)

// MaxGroupMembers is the largest group, the owner included (FR-16.6).
const MaxGroupMembers = 50

// Role is a member's role in a group.
type Role string

// Roles. Only the owner renames the group and adds and removes members.
const (
	RoleOwner  Role = "owner"
	RoleMember Role = "member"
)

// MessageKind says whether a message is a member's text or a system event.
type MessageKind string

// Message kinds. A system message has no body; it cannot be edited, deleted or reported and
// never counts as unread.
const (
	KindText   MessageKind = "text"
	KindSystem MessageKind = "system"
)

// EventType is what a system message says changed in a group.
type EventType string

// Event types. The client writes its own text for each.
const (
	EventGroupCreated  EventType = "group_created"
	EventGroupRenamed  EventType = "group_renamed"
	EventMemberAdded   EventType = "member_added"
	EventMemberRemoved EventType = "member_removed"
	EventMemberLeft    EventType = "member_left"
	// EventOwnerChanged is written by the database when the owner's membership ends; it has no
	// actor, and UserIDs holds the new owner.
	EventOwnerChanged EventType = "owner_changed"
)

// SystemEvent is what a system message describes. The actor is the message's sender.
type SystemEvent struct {
	Type    EventType
	UserIDs []string // the users it is about: added, removed, the first members, the new owner
	OldName *string  // group_renamed
	NewName *string  // group_created and group_renamed
}

// GroupInfo is what a list and a conversation show of a group.
type GroupInfo struct {
	Name        string
	MemberCount int
	Avatars     []Person // up to 3 other members, those who joined earliest first
	MyRole      Role
	// Members is every current member, the owner first; filled only when one conversation is
	// read, nil in lists.
	Members []GroupMember
}

// GroupMember is one current member of a group.
type GroupMember struct {
	UserID      string
	FullName    string
	PhotoFileID *string
	Role        Role
	JoinedAt    time.Time
	Available   bool // verified and not suspended
}

// Reader is a member's read marker, for "Seen by" in a group (FR-16.3).
type Reader struct {
	UserID            string
	LastReadMessageID *string
}

// GroupInput is what a user sends to create a group.
type GroupInput struct {
	Name      string
	MemberIDs []string
	ClientID  string
}

// NewGroup is a checked group, ready to store.
type NewGroup struct {
	CreatorID string
	Name      string
	ClientID  string
	MemberIDs []string // the other members, sorted
}

// MemberChangeKind is what changed in a group, for the conversation.members event.
type MemberChangeKind string

// Member changes.
const (
	ChangeAdded        MemberChangeKind = "added"
	ChangeRemoved      MemberChangeKind = "removed"
	ChangeLeft         MemberChangeKind = "left"
	ChangeOwnerChanged MemberChangeKind = "owner_changed"
	ChangeRenamed      MemberChangeKind = "renamed"
)

// MemberChange is one change to a group's name, members or owner.
type MemberChange struct {
	Kind    MemberChangeKind
	ActorID string   // empty for owner_changed
	UserIDs []string // who was added or removed, who left, or the new owner
	Name    string   // the group's name after the change
}

// GroupStore reads and writes groups. Writes run in the transaction the context carries and
// expect the conversation's row to be held (LockConversation), so the instants of system
// messages and joining follow the order of the conversation's messages.
type GroupStore interface {
	// FindGroupByClient returns the group creatorID created with clientID; found is false when
	// there is none.
	FindGroupByClient(ctx context.Context, creatorID, clientID string) (id string, found bool, err error)
	// CreateGroup stores the group with the creator as owner and the members, and its
	// group_created message, at one instant that is also everyone's joining time. created is
	// false, and nothing is stored, when the creator already used the client ID.
	CreateGroup(ctx context.Context, g NewGroup) (created Message, ok bool, err error)
	// AddSystemMessage writes a system message from actorID (empty for none) at the
	// conversation's next instant, and moves its last activity there.
	AddSystemMessage(ctx context.Context, conversationID, actorID string, ev SystemEvent) (Message, error)
	// AddGroupMembers adds the users as members who joined at joinedAt.
	AddGroupMembers(ctx context.Context, conversationID string, userIDs []string, joinedAt time.Time) error
	// RemoveGroupMember ends the user's membership. The database then hands ownership over,
	// with its owner_changed message, or closes the group when nobody is left. removed is false
	// when the user was not a member.
	RemoveGroupMember(ctx context.Context, conversationID, userID string) (removed bool, err error)
	// RenameGroup sets the group's name.
	RenameGroup(ctx context.Context, conversationID, name string) error
	// GroupMemberIDs returns the current members' IDs.
	GroupMemberIDs(ctx context.Context, conversationID string) ([]string, error)
	// ListGroupMembers returns the current members, the owner first, then by joining time.
	ListGroupMembers(ctx context.Context, conversationID string) ([]GroupMember, error)
	// GroupAvatars returns, for each of the groups, up to 3 members other than viewerID,
	// those who joined earliest first.
	GroupAvatars(ctx context.Context, viewerID string, conversationIDs []string) (map[string][]Person, error)
	// ListReaders returns the read markers of the current members other than viewerID.
	ListReaders(ctx context.Context, conversationID, viewerID string) ([]Reader, error)
	// GroupNames returns the current names of the groups among ids that viewerID is a member
	// of now, by ID. IDs that are not UUIDs are skipped.
	GroupNames(ctx context.Context, viewerID string, ids []string) (map[string]string, error)
	// SystemMessagesAfter returns the conversation's system messages after the message ID,
	// oldest first: what the database wrote after a change, such as owner_changed.
	SystemMessagesAfter(ctx context.Context, conversationID, afterID string) ([]Message, error)
}

// GroupNotifier tells people about group changes, inside the change's transaction. The
// notification module owns notifications; cmd/api implements it with its usecase.
type GroupNotifier interface {
	// MembersAdded tells each user that ownerID added them to the group.
	MembersAdded(ctx context.Context, conversationID, ownerID string, userIDs []string) error
	// AccessLost takes back the user's notification of being added to the group.
	AccessLost(ctx context.Context, conversationID, userID string) error
}

// Errors of groups.
var (
	ErrInvalidGroupName = errors.New("messaging: the group name is empty, too long or not one line")
	ErrNotOwner         = errors.New("messaging: only the group's owner may do this")
	ErrGroupFull        = errors.New("messaging: the group would have more than 50 members")
	ErrRemoveSelf       = errors.New("messaging: the owner leaves instead of removing themselves")
)

// IneligibleError lists the users who cannot be added to a group, without saying why for any
// of them (FR-16.6).
type IneligibleError struct {
	UserIDs []string
}

func (e *IneligibleError) Error() string { return "messaging: some users cannot be added to the group" }
