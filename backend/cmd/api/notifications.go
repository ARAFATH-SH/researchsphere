package main

import (
	"context"

	notificationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/notification/usecase"
	socialdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/social/domain"
)

// connectionNotifier turns the social module's connection events into notifications
// (FR-9.1). Neither module imports the other; this is the only place that joins them. It
// runs inside the social usecase's transaction, so each notification is stored together
// with its connection change.
type connectionNotifier struct {
	notifications *notificationusecase.Service
}

var _ socialdomain.Notifier = connectionNotifier{}

// RequestSent notifies the receiver.
func (n connectionNotifier) RequestSent(ctx context.Context, r socialdomain.Request) error {
	return n.notifications.ConnectionRequestSent(ctx, r.ID, r.RequesterID, r.ReceiverID)
}

// RequestAccepted notifies the original sender.
func (n connectionNotifier) RequestAccepted(ctx context.Context, r socialdomain.Request) error {
	return n.notifications.ConnectionRequestAccepted(ctx, r.ID, r.RequesterID, r.ReceiverID)
}

// RequestWithdrawn removes the receiver's notification of a cancelled or rejected request.
func (n connectionNotifier) RequestWithdrawn(ctx context.Context, requestID string) error {
	return n.notifications.ConnectionRequestWithdrawn(ctx, requestID)
}
