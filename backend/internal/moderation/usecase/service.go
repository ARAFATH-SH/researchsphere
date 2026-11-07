// Package usecase holds the moderation module's logic: warnings (FR-15.5), messaging
// restrictions (FR-15.6), suspensions (FR-15.3, FR-15.7), the audit trail (FR-15.8), the
// history (FR-15.9) and the rule that administrators are not moderated (FR-15.10). It imports
// only the moderation domain.
package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/moderation/domain"
)

// Limits (FR-15.5 to FR-15.7).
const (
	MinReason          = 10
	MaxReason          = 1000
	MaxNote            = 1000
	MinRestrictionDays = 1
	MaxRestrictionDays = 90
	MinSuspension      = 24 * time.Hour
	MaxSuspension      = 365 * 24 * time.Hour
	// SuspensionSlack is the leeway at each end of a suspension's range, so a client that
	// sends "now + 1 day" is not refused because the request took a moment.
	SuspensionSlack = 5 * time.Minute
	// HistoryPageSize is the size of a page of the history.
	HistoryPageSize = 20
	// MaxPendingWarnings caps the list behind the banner.
	MaxPendingWarnings = 50
)

const day = 24 * time.Hour

// Service carries out moderation actions.
type Service struct {
	store    domain.Store
	tx       domain.Transactor
	accounts domain.Accounts
	reports  domain.Reports
	notifier domain.Notifier
	mailer   domain.Mailer
	now      func() time.Time
	logger   *slog.Logger
}

// New returns a Service. now is time.Now in production and a fake clock in tests.
func New(store domain.Store, tx domain.Transactor, accounts domain.Accounts, reports domain.Reports,
	notifier domain.Notifier, mailer domain.Mailer, now func() time.Time, logger *slog.Logger,
) *Service {
	return &Service{store: store, tx: tx, accounts: accounts, reports: reports, notifier: notifier, mailer: mailer, now: now, logger: logger}
}

// Warn stores a warning and its notification (FR-15.5).
func (s *Service) Warn(ctx context.Context, adminID, userID string, in domain.Input) (domain.Action, error) {
	reason, note, err := cleanTexts(in.Reason, in.Note)
	if err != nil {
		return domain.Action{}, err
	}
	var warningID string
	act, target, err := s.act(ctx, adminID, userID, in.ReportID, note, func(ctx context.Context, _ domain.Account, _ time.Time) (domain.NewAction, error) {
		var err error
		if warningID, err = s.store.AddWarning(ctx, userID, reason); err != nil {
			return domain.NewAction{}, err
		}
		if err := s.notifier.Warned(ctx, warningID, userID); err != nil {
			return domain.NewAction{}, fmt.Errorf("notify warning %s: %w", warningID, err)
		}
		return domain.NewAction{Kind: domain.ActionWarning, Reason: reason, WarningID: &warningID}, nil
	})
	if err != nil {
		return domain.Action{}, err
	}
	s.send(ctx, warningEmail(target, reason))
	return act, nil
}

// Restrict stops the user sending messages until the restriction ends (FR-15.6).
// ErrAlreadyRestricted when one is active: an administrator lifts it to change it.
func (s *Service) Restrict(ctx context.Context, adminID, userID string, in domain.RestrictInput) (domain.Action, error) {
	reason, note, err := cleanTexts(in.Reason, in.Note)
	if err != nil {
		return domain.Action{}, err
	}
	if in.Kind != domain.KindMessaging {
		return domain.Action{}, &domain.InvalidFieldError{Field: "kind"}
	}
	if in.Days < MinRestrictionDays || in.Days > MaxRestrictionDays {
		return domain.Action{}, &domain.InvalidFieldError{Field: "days"}
	}
	var ends time.Time
	act, target, err := s.act(ctx, adminID, userID, in.ReportID, note, func(ctx context.Context, _ domain.Account, now time.Time) (domain.NewAction, error) {
		active, err := s.store.HasActiveRestriction(ctx, userID, in.Kind, now)
		if err != nil {
			return domain.NewAction{}, err
		}
		if active {
			return domain.NewAction{}, domain.ErrAlreadyRestricted
		}
		ends = now.Add(time.Duration(in.Days) * day)
		id, err := s.store.AddRestriction(ctx, userID, in.Kind, now, ends)
		if err != nil {
			return domain.NewAction{}, err
		}
		return domain.NewAction{Kind: domain.ActionRestriction, Reason: reason, RestrictionID: &id, EndsAt: &ends}, nil
	})
	if err != nil {
		return domain.Action{}, err
	}
	s.send(ctx, restrictionEmail(target, reason, ends))
	return act, nil
}

// Lift ends a restriction early (FR-15.6). ErrNotFound for an unknown restriction,
// ErrRestrictionNotActive for one that has ended or been lifted.
func (s *Service) Lift(ctx context.Context, adminID, restrictionID string, in domain.ReverseInput) error {
	reason, note, err := cleanTexts(in.Reason, in.Note)
	if err != nil {
		return err
	}
	userID, err := s.store.RestrictionUser(ctx, restrictionID)
	if err != nil {
		return err
	}
	_, target, err := s.act(ctx, adminID, userID, nil, note, func(ctx context.Context, _ domain.Account, now time.Time) (domain.NewAction, error) {
		lifted, err := s.store.LiftRestriction(ctx, restrictionID, adminID, now)
		if err != nil {
			return domain.NewAction{}, err
		}
		if !lifted {
			return domain.NewAction{}, domain.ErrRestrictionNotActive
		}
		return domain.NewAction{Kind: domain.ActionRestrictionLifted, Reason: reason, RestrictionID: &restrictionID}, nil
	})
	if err != nil {
		return err
	}
	s.send(ctx, liftEmail(target, reason))
	return nil
}

// Suspend suspends the account and ends its sessions (FR-15.3, FR-15.7). ErrAlreadySuspended
// when it is suspended now: an administrator restores it to change the suspension. A suspension
// that has ended by time does not count, and this one replaces it.
func (s *Service) Suspend(ctx context.Context, adminID, userID string, in domain.SuspendInput) (domain.Action, error) {
	reason, note, err := cleanTexts(in.Reason, in.Note)
	if err != nil {
		return domain.Action{}, err
	}
	if in.Until != nil {
		now := s.now()
		if in.Until.Before(now.Add(MinSuspension-SuspensionSlack)) || in.Until.After(now.Add(MaxSuspension+SuspensionSlack)) {
			return domain.Action{}, &domain.InvalidFieldError{Field: "until"}
		}
	}
	act, target, err := s.act(ctx, adminID, userID, in.ReportID, note, func(ctx context.Context, a domain.Account, now time.Time) (domain.NewAction, error) {
		if a.Suspended(now) {
			return domain.NewAction{}, domain.ErrAlreadySuspended
		}
		if err := s.accounts.Suspend(ctx, userID, now, in.Until, reason); err != nil {
			return domain.NewAction{}, err
		}
		return domain.NewAction{Kind: domain.ActionSuspension, Reason: reason, EndsAt: in.Until}, nil
	})
	if err != nil {
		return domain.Action{}, err
	}
	s.send(ctx, suspensionEmail(target, reason, in.Until))
	return act, nil
}

// Restore ends a suspension (FR-15.3). ErrNotSuspended when the account is not suspended now,
// also when its suspension has ended by time.
func (s *Service) Restore(ctx context.Context, adminID, userID string, in domain.ReverseInput) (domain.Action, error) {
	reason, note, err := cleanTexts(in.Reason, in.Note)
	if err != nil {
		return domain.Action{}, err
	}
	act, target, err := s.act(ctx, adminID, userID, nil, note, func(ctx context.Context, a domain.Account, now time.Time) (domain.NewAction, error) {
		if !a.Suspended(now) {
			return domain.NewAction{}, domain.ErrNotSuspended
		}
		if err := s.accounts.Restore(ctx, userID); err != nil {
			return domain.NewAction{}, err
		}
		return domain.NewAction{Kind: domain.ActionRestoration, Reason: reason}, nil
	})
	if err != nil {
		return domain.Action{}, err
	}
	s.send(ctx, restoreEmail(target, reason))
	return act, nil
}

// act runs one action in one transaction: it locks the target, refuses an administrator
// (FR-15.10), runs do, resolves the report if one is given and writes the audit row (FR-15.8).
// Any error undoes all of it.
func (s *Service) act(ctx context.Context, adminID, userID string, reportID, note *string,
	do func(ctx context.Context, target domain.Account, now time.Time) (domain.NewAction, error),
) (domain.Action, domain.Account, error) {
	now := s.now()
	var (
		act    domain.Action
		target domain.Account
	)
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		if target, err = s.accounts.Lock(ctx, userID); err != nil {
			return err
		}
		if target.Admin || target.ID == adminID {
			return domain.ErrCannotModerateAdmin
		}
		row, err := do(ctx, target, now)
		if err != nil {
			return err
		}
		if reportID != nil {
			if err := s.reports.ResolveForAction(ctx, adminID, *reportID, userID, note); err != nil {
				return err
			}
		}
		row.UserID, row.AdminID, row.Note, row.ReportID = userID, adminID, note, reportID
		id, err := s.store.AddAction(ctx, row)
		if err != nil {
			return err
		}
		act, err = s.store.FindAction(ctx, id)
		return err
	})
	if err != nil {
		return domain.Action{}, domain.Account{}, err
	}
	s.logger.InfoContext(ctx, "moderation action", slog.String("action_id", act.ID), slog.String("kind", string(act.Kind)),
		slog.String("user_id", userID), slog.String("admin_id", adminID))
	return act, target, nil
}

// send delivers an email after the action has committed. A failure is logged and changes
// nothing, as for password reset: the action stands. The log has no text of the message.
func (s *Service) send(ctx context.Context, e domain.Email) {
	if err := s.mailer.Send(ctx, e); err != nil {
		s.logger.ErrorContext(ctx, "send moderation email", slog.Any("error", err))
	}
}

// History returns one page of the user's actions, newest first (FR-15.9). ErrNotFound when
// there is no such user. Reading is allowed about anyone, an administrator included.
func (s *Service) History(ctx context.Context, userID string, page int) (domain.History, error) {
	if page < 1 || page-1 > math.MaxInt32/HistoryPageSize {
		return domain.History{}, domain.ErrInvalidPage
	}
	account, err := s.accounts.Find(ctx, userID)
	if err != nil {
		return domain.History{}, err
	}
	items, total, err := s.store.ListActions(ctx, userID, HistoryPageSize, (page-1)*HistoryPageSize)
	if err != nil {
		return domain.History{}, err
	}
	return domain.History{
		User: account, Suspended: account.Suspended(s.now()),
		Items: items, Page: page, PageSize: HistoryPageSize, Total: total,
	}, nil
}

// PendingWarnings returns the warnings userID has not acknowledged, newest first (FR-15.5).
func (s *Service) PendingWarnings(ctx context.Context, userID string) ([]domain.Warning, error) {
	return s.store.PendingWarnings(ctx, userID, MaxPendingWarnings)
}

// Warning returns one of userID's warnings. Another user's is ErrNotFound.
func (s *Service) Warning(ctx context.Context, userID, id string) (domain.Warning, error) {
	return s.store.FindWarning(ctx, id, userID)
}

// Acknowledge marks one of userID's warnings read. It succeeds again for one already
// acknowledged; another user's is ErrNotFound.
func (s *Service) Acknowledge(ctx context.Context, userID, id string) error {
	found, err := s.store.AcknowledgeWarning(ctx, id, userID, s.now())
	if err != nil {
		return err
	}
	if !found {
		return domain.ErrNotFound
	}
	return nil
}

// Standings tells other modules what a user may do now. It is apart from Service because
// messaging and social need it, and Service needs messaging.
type Standings struct {
	store domain.Store
	now   func() time.Time
}

// NewStandings returns a Standings.
func NewStandings(store domain.Store, now func() time.Time) *Standings {
	return &Standings{store: store, now: now}
}

// MessagingRestrictedUntil returns when the user's messaging restriction ends, nil when there
// is none (FR-15.6). It is for showing the restriction; a send checks Standing.
func (s *Standings) MessagingRestrictedUntil(ctx context.Context, userID string) (*time.Time, error) {
	return s.store.RestrictedUntil(ctx, userID, s.now())
}

// Standing reads the user's suspension and restriction, both judged by time, so each ends by
// itself (FR-15.6, FR-15.7). Inside a transaction it holds the user's row until the transaction
// ends, so no action can slip past the check (decisions.md, 2026-11-05).
func (s *Standings) Standing(ctx context.Context, userID string) (domain.Standing, error) {
	now := s.now()
	row, err := s.store.Standing(ctx, userID, now)
	if err != nil {
		return domain.Standing{}, err
	}
	st := domain.Standing{RestrictedUntil: row.RestrictedUntil}
	if (domain.Account{SuspendedAt: row.SuspendedAt, SuspendedUntil: row.SuspendedUntil}).Suspended(now) {
		st.Suspended, st.SuspensionReason, st.SuspendedUntil = true, row.SuspensionReason, row.SuspendedUntil
	}
	return st, nil
}

// cleanTexts trims and checks an action's reason and note; an empty note is none.
func cleanTexts(reason string, note *string) (string, *string, error) {
	text, ok := cleanText(reason)
	if n := utf8.RuneCountInString(text); !ok || n < MinReason || n > MaxReason {
		return "", nil, &domain.InvalidFieldError{Field: "reason"}
	}
	if note == nil {
		return text, nil, nil
	}
	cleaned, ok := cleanText(*note)
	if !ok || utf8.RuneCountInString(cleaned) > MaxNote {
		return "", nil, &domain.InvalidFieldError{Field: "note"}
	}
	if cleaned == "" {
		return text, nil, nil
	}
	return text, &cleaned, nil
}

// cleanText trims s; false when it is not text PostgreSQL can store.
func cleanText(s string) (string, bool) {
	if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
		return "", false
	}
	return strings.TrimSpace(s), true
}
