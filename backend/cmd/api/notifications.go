package main

import (
	"context"

	messagingusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/usecase"
	notificationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/notification/usecase"
	socialdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/social/domain"
)

// connectionNotifier turns the social module's connection events into notifications
// (FR-9.1). Neither module imports the other; this is the only place that joins them. It
// runs inside the social usecase's transaction, so each notification is stored together
// with its connection change. A new connection also opens any message request between the
// two (FR-16.2), in the same transaction.
type connectionNotifier struct {
	notifications *notificationusecase.Service
	messaging     *messagingusecase.Service
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

// Blocked takes back everything the two were told about each other and declines a pending
// message request between them (FR-16.9), inside the block's transaction.
func (n connectionNotifier) Blocked(ctx context.Context, blockerID, blockedID string) error {
	if err := n.notifications.RemoveBetween(ctx, blockerID, blockedID); err != nil {
		return err
	}
	return n.messaging.DeclineRequests(ctx, blockerID, blockedID)
}
