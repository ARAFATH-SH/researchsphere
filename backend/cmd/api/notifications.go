package main

import (
	"context"

	messagingdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
	messagingusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/usecase"
	notificationdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/notification/domain"
	notificationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/notification/usecase"
	projectdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
	projectusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/project/usecase"
	socialdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/social/domain"
)

// groupNotifier turns the messaging module's group changes into notifications (FR-16.6), inside
// the change's transaction. Neither module imports the other; this is the only place that
// joins them.
type groupNotifier struct {
	notifications *notificationusecase.Service
}

var _ messagingdomain.GroupNotifier = groupNotifier{}

// MembersAdded notifies each new member; the owner is the actor.
func (n groupNotifier) MembersAdded(ctx context.Context, conversationID, ownerID string, userIDs []string) error {
	return n.notifications.GroupMembersAdded(ctx, conversationID, ownerID, userIDs)
}

// AccessLost takes back the notification of a member who left or was removed.
func (n groupNotifier) AccessLost(ctx context.Context, conversationID, userID string) error {
	return n.notifications.GroupAccessLost(ctx, conversationID, userID)
}

// subjectTitles gives notifications the current titles of the items they name (FR-9.5) from
// the module that owns each kind: the messaging module's group names for conversations, the
// project module's titles for projects (only those the viewer may see). The usecases are set
// once built, as they are built after the notification usecase.
type subjectTitles struct {
	messaging *messagingusecase.Service
	projects  *projectusecase.Service
}

var _ notificationdomain.SubjectTitles = (*subjectTitles)(nil)

// Titles reads the titles of one kind in one query; a kind without an owner has none.
func (t *subjectTitles) Titles(ctx context.Context, viewerID, kind string, ids []string) (map[string]string, error) {
	switch {
	case kind == notificationdomain.SubjectConversation && t.messaging != nil:
		return t.messaging.GroupNames(ctx, viewerID, ids)
	case kind == notificationdomain.SubjectProject && t.projects != nil:
		return t.projects.Titles(ctx, viewerID, ids)
	}
	return nil, nil
}

// projectNotifier turns the project module's membership events into notifications (FR-9.2),
// inside the change's transaction. Neither module imports the other; this is the only place
// that joins them. The notice kinds are the notification types' names.
type projectNotifier struct {
	notifications *notificationusecase.Service
}

var _ projectdomain.Notifier = projectNotifier{}

func (n projectNotifier) Notify(ctx context.Context, kind projectdomain.NoticeKind, projectID, recipientID, actorID string) error {
	return n.notifications.ProjectEvent(ctx, notificationdomain.Type(kind), projectID, recipientID, actorID)
}

func (n projectNotifier) RequestHandled(ctx context.Context, projectID, senderID string) error {
	return n.notifications.ProjectRequestHandled(ctx, projectID, senderID)
}

func (n projectNotifier) InvitationHandled(ctx context.Context, projectID, inviteeID string) error {
	return n.notifications.ProjectInvitationHandled(ctx, projectID, inviteeID)
}

// projectMemberships lets a block withdraw the pending join requests and invitations between
// the two users (FR-16.9, decisions.md 2026-11-17). The project usecase is set once built,
// after the social usecase.
type projectMemberships struct {
	projects *projectusecase.Service
}

func (m *projectMemberships) withdrawBetween(ctx context.Context, a, b string) error {
	if m == nil || m.projects == nil {
		return nil
	}
	return m.projects.WithdrawBetween(ctx, a, b)
}

// connectionNotifier turns the social module's connection events into notifications
// (FR-9.1). Neither module imports the other; this is the only place that joins them. It
// runs inside the social usecase's transaction, so each notification is stored together
// with its connection change. A new connection also opens any message request between the
// two (FR-16.2), in the same transaction.
type connectionNotifier struct {
	notifications *notificationusecase.Service
	messaging     *messagingusecase.Service
	events        *realtimeEvents
	memberships   *projectMemberships
}

var _ socialdomain.Notifier = connectionNotifier{}

// RequestSent notifies the receiver.
func (n connectionNotifier) RequestSent(ctx context.Context, r socialdomain.Request) error {
	return n.notifications.ConnectionRequestSent(ctx, r.ID, r.RequesterID, r.ReceiverID)
}

// RequestAccepted notifies the original sender and activates a pending message request.
func (n connectionNotifier) RequestAccepted(ctx context.Context, r socialdomain.Request) error {
	if err := n.notifications.ConnectionRequestAccepted(ctx, r.ID, r.RequesterID, r.ReceiverID); err != nil {
		return err
	}
	return n.messaging.ConnectionMade(ctx, r.RequesterID, r.ReceiverID)
}

// RequestWithdrawn removes the receiver's notification of a cancelled or rejected request.
func (n connectionNotifier) RequestWithdrawn(ctx context.Context, requestID string) error {
	return n.notifications.ConnectionRequestWithdrawn(ctx, requestID)
}

// Blocked takes back everything the two were told about each other, declines a pending
// message request between them (FR-16.9) and deletes the pending project join requests and
// invitations between them, leaving memberships alone, inside the block's transaction.
func (n connectionNotifier) Blocked(ctx context.Context, blockerID, blockedID string) error {
	if err := n.notifications.RemoveBetween(ctx, blockerID, blockedID); err != nil {
		return err
	}
	if err := n.messaging.DeclineRequests(ctx, blockerID, blockedID); err != nil {
		return err
	}
	if err := n.memberships.withdrawBetween(ctx, blockerID, blockedID); err != nil {
		return err
	}
	n.events.hidePresence(ctx, blockerID, blockedID)
	return nil
}

// Unblocked reports that the two may be able to message each other again.
func (n connectionNotifier) Unblocked(ctx context.Context, blockerID, blockedID string) {
	n.messaging.Unblocked(ctx, blockerID, blockedID)
}
