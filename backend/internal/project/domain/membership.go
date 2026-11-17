package domain

import (
	"context"
	"errors"
	"time"
)

// Project membership (FR-11.1 to FR-11.5, FR-10.5): join requests, invitations, members and
// ownership. A join request and an invitation are both rows of project_join_requests, told
// apart by Kind; one row per user and project may be pending, of either kind.

// RequestKind says who started a pending membership: the user (a join request, FR-11.1) or
// the owner (an invitation, FR-11.3). The values match the join_request_kind enum.
type RequestKind string

// Kinds.
const (
	KindRequest    RequestKind = "request"
	KindInvitation RequestKind = "invitation"
)

// RequestStatus is a join request's or invitation's state. The values match the
// join_request_status enum; a cancelled one is deleted.
type RequestStatus string

// Statuses of a join request or invitation.
const (
	RequestPending  RequestStatus = "pending"
	RequestAccepted RequestStatus = "accepted"
	RequestRejected RequestStatus = "rejected"
)

// MemberRoleValid reports whether r is a role the owner can give: editor or viewer.
// Ownership moves only by transfer (FR-10.5).
func MemberRoleValid(r Role) bool {
	return r == RoleEditor || r == RoleViewer
}

// Membership errors the usecase returns.
var (
	// ErrRequestNotFound covers a join request or invitation that does not exist in the
	// project, is not the caller's to act on, or whose other side is no longer reachable.
	ErrRequestNotFound = errors.New("project: join request or invitation not found")
	// ErrUserNotFound covers an invited user who does not exist, is unverified or suspended,
	// or is blocked either way with the owner: all look the same.
	ErrUserNotFound = errors.New("project: user not found")
	// ErrMemberNotFound covers a user who is not a member of the project.
	ErrMemberNotFound = errors.New("project: member not found")
	// ErrAlreadyMember refuses a request or invitation for a member.
	ErrAlreadyMember = errors.New("project: already a member")
	// ErrProjectNotOpen refuses a new request or invitation for a closed or archived project.
	ErrProjectNotOpen = errors.New("project: not open")
	// ErrRequestPending refuses a request, or an invitation, while the user has a pending
	// request to join.
	ErrRequestPending = errors.New("project: a join request is pending")
	// ErrInvitationPending refuses a request while the user is invited.
	ErrInvitationPending = errors.New("project: an invitation is pending")
	// ErrAlreadyInvited refuses a second invitation of the same user.
	ErrAlreadyInvited = errors.New("project: already invited")
	// ErrRequestNotPending refuses deciding a join request that is already decided.
	ErrRequestNotPending = errors.New("project: the join request is not pending")
	// ErrInvitationNotPending refuses acting on an invitation that is already decided.
	ErrInvitationNotPending = errors.New("project: the invitation is not pending")
	// ErrUseTransfer refuses changing the owner's role: ownership moves by transfer.
	ErrUseTransfer = errors.New("project: use an ownership transfer")
	// ErrCannotRemoveOwner refuses removing the owner.
	ErrCannotRemoveOwner = errors.New("project: the owner cannot be removed")
	// ErrOwnerMustTransfer refuses the owner leaving (FR-11.5).
	ErrOwnerMustTransfer = errors.New("project: the owner cannot leave")
	// ErrNotAMember refuses a transfer to a user who is not a member.
	ErrNotAMember = errors.New("project: not a member")
	// ErrAlreadyOwner refuses a transfer to the owner.
	ErrAlreadyOwner = errors.New("project: already the owner")
	// ErrMemberUnavailable refuses a transfer to a member who is unverified or suspended.
	ErrMemberUnavailable = errors.New("project: the member is unavailable")
)

// JoinRequest is a join request or an invitation.
type JoinRequest struct {
	ID        string
	ProjectID string
	Kind      RequestKind
	Status    RequestStatus
	Message   *string // requests only
	Role      Role    // invitations only: the role offered
	UserID    string  // the sender of a request, the invitee of an invitation
	CreatedAt time.Time
	DecidedAt *time.Time
}

// RequestItem is a join request or invitation in the owner's list, with the user.
type RequestItem struct {
	JoinRequest
	User Person
}

// MembershipProject is the project a request or invitation is about, for its user.
type MembershipProject struct {
	ID     string
	Title  string
	Status Status
	Owner  Person
}

// MyRequestItem is a join request or invitation in its user's own list, with the project.
type MyRequestItem struct {
	JoinRequest
	Project MembershipProject
}

// Member is a project member.
type Member struct {
	Person
	Role     Role
	JoinedAt time.Time
}

// Relation is what decides whether a user may ask to join a project or be invited to it:
// the user's account, their role, a block either way with the owner and a pending request
// or invitation.
type Relation struct {
	Exists    bool // the user exists
	Available bool // verified and not suspended
	Blocked   bool // a block either way between the user and the project's owner
	Role      Role
	Pending   *JoinRequest // the pending request or invitation, if any
}

// NewJoinRequest is a request or invitation to store, pending.
type NewJoinRequest struct {
	ProjectID string
	UserID    string
	Kind      RequestKind
	Message   *string
	Role      Role // invitations only
}

// Withdrawn is a pending request or invitation a block deleted.
type Withdrawn struct {
	ProjectID string
	UserID    string
}

// MemberStore keeps join requests, invitations and members. Every method runs on the
// transaction the context carries, if any; the writes expect the caller to hold the project
// lock (Store.Lock).
type MemberStore interface {
	// Relation reads userID's relation to the project and its owner; Exists is false for an
	// ID that is not a UUID.
	Relation(ctx context.Context, projectID, userID string) (Relation, error)
	// CreateRequest stores a pending request or invitation. A pending one for the same
	// user and project is ErrRequestPending (the unique index; Relation sees it first).
	CreateRequest(ctx context.Context, r NewJoinRequest) (JoinRequest, error)
	// LockRequest reads and locks the request or invitation id of kind in the project,
	// ErrRequestNotFound otherwise (also for an ID that is not a UUID).
	LockRequest(ctx context.Context, projectID, id string, kind RequestKind) (JoinRequest, error)
	// DecideRequest sets a pending row's status, now.
	DecideRequest(ctx context.Context, id string, status RequestStatus) error
	// DeleteRequest deletes a row (a cancel).
	DeleteRequest(ctx context.Context, id string) error
	// AddMember adds userID with role; an existing member is ErrAlreadyMember.
	AddMember(ctx context.Context, projectID, userID string, role Role) (Member, error)
	// Person reads a user as a project shows them.
	Person(ctx context.Context, userID string) (Person, error)
	// Member reads one member, ErrMemberNotFound when the user is not one.
	Member(ctx context.Context, projectID, userID string) (Member, error)
	// SetRole changes a member's role.
	SetRole(ctx context.Context, projectID, userID string, role Role) error
	// RemoveMember deletes a member row.
	RemoveMember(ctx context.Context, projectID, userID string) error
	// Members lists the project's members, the owner first, then by joining time.
	Members(ctx context.Context, projectID string, limit, offset int) ([]Member, int, error)
	// ProjectRequests lists one kind of the project's rows whose user is verified and not
	// suspended: pending first (oldest first), then decided (most recently decided first).
	ProjectRequests(ctx context.Context, projectID string, kind RequestKind, limit, offset int) ([]RequestItem, int, error)
	// MyRequests lists userID's join requests, newest first.
	MyRequests(ctx context.Context, userID string, limit, offset int) ([]MyRequestItem, int, error)
	// MyInvitations lists userID's pending invitations whose project's owner is verified,
	// not suspended and not blocked either way with userID, newest first.
	MyInvitations(ctx context.Context, userID string, limit, offset int) ([]MyRequestItem, int, error)
	// WithdrawBetween deletes the pending requests and invitations between a and b: those
	// of either user on a project the other owns.
	WithdrawBetween(ctx context.Context, a, b string) ([]Withdrawn, error)
	// VisibleTitles returns the titles of the projects in ids that viewerID may see
	// (Access.Visible).
	VisibleTitles(ctx context.Context, viewerID string, ids []string) (map[string]string, error)
}

// NoticeKind is a membership event another user is told about (FR-9.2). The values are the
// notification types' names.
type NoticeKind string

// Notices. The actor is who acted, the recipient who is told; the subject is the project.
const (
	// NoticeJoinRequest: the actor asked to join; the owner is told.
	NoticeJoinRequest NoticeKind = "project_join_request"
	// NoticeRequestAccepted and NoticeRequestRejected: the owner decided; the sender is told.
	NoticeRequestAccepted NoticeKind = "project_join_request_accepted"
	NoticeRequestRejected NoticeKind = "project_join_request_rejected"
	// NoticeInvitation: the owner invited the recipient.
	NoticeInvitation NoticeKind = "project_invitation"
	// NoticeInvitationAccepted and NoticeInvitationRejected: the invitee answered; the
	// owner is told.
	NoticeInvitationAccepted NoticeKind = "project_invitation_accepted"
	NoticeInvitationRejected NoticeKind = "project_invitation_rejected"
	// NoticeRoleChanged, NoticeRemoved: the owner changed the member's role or removed them.
	NoticeRoleChanged NoticeKind = "project_member_role_changed"
	NoticeRemoved     NoticeKind = "project_member_removed"
	// NoticeOwnership: the owner made the recipient the owner (FR-10.5).
	NoticeOwnership NoticeKind = "project_ownership_transferred"
)

// Notifier tells users about membership events (FR-9.2). The notification module implements
// it in cmd/api; it runs inside the change's transaction, so a notice is stored with its
// change.
type Notifier interface {
	// Notify tells recipientID that actorID did kind on the project.
	Notify(ctx context.Context, kind NoticeKind, projectID, recipientID, actorID string) error
	// RequestHandled takes back the owner's notice of senderID's request to join the
	// project, once it was cancelled or decided.
	RequestHandled(ctx context.Context, projectID, senderID string) error
	// InvitationHandled takes back inviteeID's notice of an invitation to the project,
	// once it was cancelled or answered.
	InvitationHandled(ctx context.Context, projectID, inviteeID string) error
}
