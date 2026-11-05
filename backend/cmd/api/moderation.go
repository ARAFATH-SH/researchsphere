package main

import (
	"context"
	"errors"
	"time"

	authdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
	authusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/usecase"
	messagingdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
	messagingusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/usecase"
	moderationdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/moderation/domain"
	moderationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/moderation/usecase"
	notificationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/notification/usecase"
	socialdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/social/domain"
)

// moderationAccounts gives the moderation module the auth module's accounts. Neither imports
// the other; this is the only place that joins them.
type moderationAccounts struct {
	auth   *authusecase.Service
	events *realtimeEvents
}

var _ moderationdomain.Accounts = moderationAccounts{}

func (a moderationAccounts) Find(ctx context.Context, userID string) (moderationdomain.Account, error) {
	u, err := a.auth.CurrentUser(ctx, userID)
	return moderationAccount(u, err)
}

func (a moderationAccounts) Lock(ctx context.Context, userID string) (moderationdomain.Account, error) {
	u, err := a.auth.LockAccount(ctx, userID)
	return moderationAccount(u, err)
}

func moderationAccount(u authdomain.User, err error) (moderationdomain.Account, error) {
	if errors.Is(err, authdomain.ErrNotFound) {
		return moderationdomain.Account{}, moderationdomain.ErrNotFound
	}
	if err != nil {
		return moderationdomain.Account{}, err
	}
	return moderationdomain.Account{
		ID: u.ID, Email: u.Email, FullName: u.FullName, Admin: u.Role == authdomain.RoleAdmin,
		SuspendedAt: u.SuspendedAt, SuspendedUntil: u.SuspendedUntil,
	}, nil
}

func (a moderationAccounts) Suspend(ctx context.Context, userID string, at time.Time, until *time.Time, reason string) error {
	if err := a.auth.Suspend(ctx, userID, at, until, reason); err != nil {
		return err
	}
	a.events.accountSuspended(ctx, userID)
	return nil
}

func (a moderationAccounts) Restore(ctx context.Context, userID string) error {
	if err := a.auth.Restore(ctx, userID); err != nil {
		return err
	}
	a.events.accountRestored(ctx, userID)
	return nil
}

// moderationReports resolves the messaging module's reports for an action, in its transaction.
type moderationReports struct {
	messaging *messagingusecase.Service
}

var _ moderationdomain.Reports = moderationReports{}

func (r moderationReports) ResolveForAction(ctx context.Context, adminID, reportID, userID string, note *string) error {
	err := r.messaging.ResolveForAction(ctx, adminID, reportID, userID, note)
	switch {
	case errors.Is(err, messagingdomain.ErrNotFound):
		return moderationdomain.ErrReportNotFound
	case errors.Is(err, messagingdomain.ErrAlreadyResolved):
		return moderationdomain.ErrReportResolved
	case errors.Is(err, messagingdomain.ErrReportAboutOther):
		return &moderationdomain.InvalidFieldError{Field: "reportId"}
	}
	return err
}

// moderationNotifier stores a warning's notification in the action's transaction.
type moderationNotifier struct {
	notifications *notificationusecase.Service
}

var _ moderationdomain.Notifier = moderationNotifier{}

func (n moderationNotifier) Warned(ctx context.Context, warningID, userID string) error {
	return n.notifications.AccountWarned(ctx, warningID, userID)
}

// textMailer is the part of the SMTP adapter moderation uses.
type textMailer interface {
	SendText(ctx context.Context, toAddress, toName, subject, body string) error
}

// moderationMailer sends moderation's emails through the SMTP adapter.
type moderationMailer struct {
	mailer textMailer
}

var _ moderationdomain.Mailer = moderationMailer{}

func (m moderationMailer) Send(ctx context.Context, e moderationdomain.Email) error {
	return m.mailer.SendText(ctx, e.To, e.FullName, e.Subject, e.Body)
}

// messagingActors tells messaging what a user may do now.
type messagingActors struct {
	standings *moderationusecase.Standings
}

var _ messagingdomain.Actors = messagingActors{}

func (a messagingActors) Standing(ctx context.Context, userID string) (messagingdomain.Standing, error) {
	s, err := a.standings.Standing(ctx, userID)
	if err != nil {
		return messagingdomain.Standing{}, err
	}
	return messagingdomain.Standing{
		Suspended: s.Suspended, SuspensionReason: s.SuspensionReason,
		SuspendedUntil: s.SuspendedUntil, RestrictedUntil: s.RestrictedUntil,
	}, nil
}

// socialActors tells social what a user may do now.
type socialActors struct {
	standings *moderationusecase.Standings
}

var _ socialdomain.Actors = socialActors{}

func (a socialActors) Standing(ctx context.Context, userID string) (socialdomain.Standing, error) {
	s, err := a.standings.Standing(ctx, userID)
	if err != nil {
		return socialdomain.Standing{}, err
	}
	return socialdomain.Standing{Suspended: s.Suspended, SuspensionReason: s.SuspensionReason, SuspendedUntil: s.SuspendedUntil}, nil
}
