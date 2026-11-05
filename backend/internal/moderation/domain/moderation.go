// Package domain holds the moderation module's entities and the interfaces its usecase needs:
// warnings, messaging restrictions, suspensions and their audit trail (FR-15.5 to FR-15.10).
// It imports only the standard library.
package domain

import (
	"context"
	"errors"
	"time"
)

// ActionKind says what an audit row records. The values match moderation_actions.action.
type ActionKind string

// Action kinds.
const (
	ActionWarning           ActionKind = "warning"
	ActionRestriction       ActionKind = "restriction"
	ActionRestrictionLifted ActionKind = "restriction_lifted"
	ActionSuspension        ActionKind = "suspension"
	ActionRestoration       ActionKind = "restoration"
)

// KindMessaging is the one kind of restriction: no sending messages or message requests.
const KindMessaging = "messaging"

// Input is what every action takes. Reason is shown to the user. Note never is: it stays in
// the administrators' history. ReportID names a report to resolve as action_taken.
type Input struct {
	Reason   string
	Note     *string
	ReportID *string
}

// RestrictInput is a restriction of Kind for Days days.
type RestrictInput struct {
	Input
	Kind string
	Days int
}

// SuspendInput is a suspension until Until, or for good when it is nil.
type SuspendInput struct {
	Input
	Until *time.Time
}

// ReverseInput lifts a restriction or restores an account. It reverses an action, so it names
// no report.
type ReverseInput struct {
	Reason string
	Note   *string
}

// Account is the part of a user account an action needs. The auth module owns accounts.
type Account struct {
	ID             string
	Email          string
	FullName       string
	Admin          bool
	SuspendedAt    *time.Time
	SuspendedUntil *time.Time
}

// Suspended reports whether the account is suspended at now. A temporary suspension no longer
// counts from its end time.
func (a Account) Suspended(now time.Time) bool {
	return a.SuspendedAt != nil && (a.SuspendedUntil == nil || now.Before(*a.SuspendedUntil))
}

// LinkedReport is the report an action resolved.
type LinkedReport struct {
	ID        string
	Reason    string
	Outcome   string
	CreatedAt time.Time
}

// NewAction is an audit row to store.
type NewAction struct {
	UserID        string
	AdminID       string
	Kind          ActionKind
	Reason        string
	Note          *string
	ReportID      *string
	WarningID     *string
	RestrictionID *string
	EndsAt        *time.Time
}

// Action is an audit row as the history shows it. It holds the internal note, so it goes only
// to an administrator.
type Action struct {
	ID             string
	UserID         string
	AdminID        *string // nil once the administrator's account is deleted
	AdminName      *string
	Kind           ActionKind
	Reason         string
	Note           *string
	ReportID       *string
	Report         *LinkedReport // nil when none, or when the report has been deleted
	WarningID      *string
	RestrictionID  *string
	EndsAt         *time.Time
	AcknowledgedAt *time.Time
	LiftedAt       *time.Time
	CreatedAt      time.Time
}

// History is one page of a user's actions, newest first.
type History struct {
	Items    []Action
	Page     int
	PageSize int
	Total    int
}

// Warning is a warning as its user sees it: no note and no administrator.
type Warning struct {
	ID             string
	Reason         string
	CreatedAt      time.Time
	AcknowledgedAt *time.Time
}

// Standing is what a user may do now (decisions.md, 2026-11-05).
type Standing struct {
	Suspended        bool
	SuspensionReason *string
	SuspendedUntil   *time.Time // nil when permanent, or when not suspended
	RestrictedUntil  *time.Time // the end of the messaging restriction, nil when none
}

// StoredStanding is the row Standing is made from.
type StoredStanding struct {
	SuspendedAt      *time.Time
	SuspendedUntil   *time.Time
	SuspensionReason *string
	RestrictedUntil  *time.Time
}

// Email is one message to a user. It is built only from what the user may read.
type Email struct {
	To       string
	FullName string
	Subject  string
	Body     string
}

// Store reads and writes moderation's tables. Every method runs in the transaction the context
// carries, if any. Methods that take an ID treat a value that is not a UUID as a missing row.
type Store interface {
	AddWarning(ctx context.Context, userID, reason string) (string, error)
	PendingWarnings(ctx context.Context, userID string, limit int) ([]Warning, error)
	// FindWarning returns ErrNotFound for an unknown ID and for another user's warning.
	FindWarning(ctx context.Context, id, userID string) (Warning, error)
	// AcknowledgeWarning keeps an earlier time. found is false for an unknown ID and for
	// another user's warning.
	AcknowledgeWarning(ctx context.Context, id, userID string, at time.Time) (found bool, err error)
	AddRestriction(ctx context.Context, userID, kind string, startsAt, endsAt time.Time) (string, error)
	// HasActiveRestriction reports whether the user has a restriction of kind, not lifted,
	// that ends after now.
	HasActiveRestriction(ctx context.Context, userID, kind string, now time.Time) (bool, error)
	// RestrictionUser returns who the restriction is on, or ErrNotFound.
	RestrictionUser(ctx context.Context, id string) (string, error)
	// LiftRestriction ends an active restriction early. lifted is false when it has ended or
	// been lifted already.
	LiftRestriction(ctx context.Context, id, adminID string, now time.Time) (lifted bool, err error)
	AddAction(ctx context.Context, a NewAction) (string, error)
	FindAction(ctx context.Context, id string) (Action, error)
	// ListActions returns one page of the user's actions, newest first, and how many exist.
	ListActions(ctx context.Context, userID string, limit, offset int) ([]Action, int, error)
	// Standing reads the user's row FOR SHARE, so that inside a transaction no moderation
	// action on the user can commit until it ends. A missing user has the zero value.
	Standing(ctx context.Context, userID string, now time.Time) (StoredStanding, error)
}

// Accounts reads and changes accounts. The auth module owns them; cmd/api implements this with
// its usecase. Each method runs in the context's transaction.
type Accounts interface {
	// Find returns ErrNotFound when there is no such user.
	Find(ctx context.Context, userID string) (Account, error)
	// Lock locks the user's row until the transaction ends. ErrNotFound when there is none.
	Lock(ctx context.Context, userID string) (Account, error)
	// Suspend also ends every session of the user.
	Suspend(ctx context.Context, userID string, at time.Time, until *time.Time, reason string) error
	Restore(ctx context.Context, userID string) error
}

// Reports resolves message reports. The messaging module owns them; cmd/api implements this
// with its usecase, in the context's transaction.
type Reports interface {
	// ResolveForAction resolves the report as action_taken. It returns ErrReportNotFound,
	// ErrReportResolved, or an *InvalidFieldError for a report about someone else.
	ResolveForAction(ctx context.Context, adminID, reportID, userID string, note *string) error
}

// Notifier tells a user in the app. cmd/api implements it with the notification usecase, in
// the context's transaction.
type Notifier interface {
	Warned(ctx context.Context, warningID, userID string) error
}

// Mailer sends email. It is called after the action has committed and cannot undo it.
type Mailer interface {
	Send(ctx context.Context, e Email) error
}

// Transactor runs fn in one database transaction; it commits when fn returns nil and rolls back
// otherwise.
type Transactor interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Errors of the moderation module.
var (
	ErrNotFound             = errors.New("moderation: not found")
	ErrCannotModerateAdmin  = errors.New("moderation: the target is an administrator")
	ErrAlreadyRestricted    = errors.New("moderation: the user already has an active restriction")
	ErrAlreadySuspended     = errors.New("moderation: the account is already suspended")
	ErrNotSuspended         = errors.New("moderation: the account is not suspended")
	ErrRestrictionNotActive = errors.New("moderation: the restriction has ended")
	ErrInvalidPage          = errors.New("moderation: page is not a whole number of at least 1")
	ErrReportNotFound       = errors.New("moderation: report not found")
	ErrReportResolved       = errors.New("moderation: report already resolved")
)

// InvalidFieldError names the request field that broke its rule.
type InvalidFieldError struct {
	Field string
}

func (e *InvalidFieldError) Error() string { return "moderation: invalid " + e.Field }
