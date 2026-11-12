package usecase

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// PurgeBatch is the most unverified accounts one purge pass deletes (FR-17.4). The loop
// runs every hour, so a backlog clears at this many an hour.
const PurgeBatch = 500

// isTestDomain reports whether email is at one of the test email domains (FR-17.9).
func (s *Service) isTestDomain(email string) bool {
	_, d, ok := strings.Cut(strings.ToLower(email), "@")
	return ok && slices.Contains(s.cfg.TestEmailDomains, d)
}

// checkEmailDomain refuses an address whose domain is disposable or cannot receive email
// (FR-17.2), unless it is a test domain (FR-17.9). Registration calls it; a later change of
// address must call it too.
func (s *Service) checkEmailDomain(ctx context.Context, email string) error {
	if s.cfg.EmailDomains == nil || s.isTestDomain(email) {
		return nil
	}
	_, d, _ := strings.Cut(email, "@")
	if !s.cfg.EmailDomains.AllowsEmailDomain(ctx, strings.ToLower(d)) {
		return domain.ErrEmailDomainNotAllowed
	}
	return nil
}

// PurgeUnverified deletes the accounts whose email address is still unverified
// UnverifiedAccountAge after sign-up, at most PurgeBatch of them, through the account
// deletion of FR-2.8 (FR-17.4). Accounts at a test email domain are kept (FR-17.9). Each
// account is deleted in its own transaction, which first checks again that it is still
// unverified, so one that verifies meanwhile is kept, and one that cannot be deleted (it
// owns a shared project) is skipped and logged. It returns how many it deleted.
func (s *Service) PurgeUnverified(ctx context.Context) (int, error) {
	if s.cfg.UnverifiedAccountAge <= 0 {
		return 0, nil
	}
	cutoff := s.now().Add(-s.cfg.UnverifiedAccountAge)
	ids, err := s.store.ListUnverifiedBefore(ctx, cutoff, s.cfg.TestEmailDomains, PurgeBatch)
	if err != nil {
		return 0, err
	}
	deleted := make([]string, 0, len(ids))
	for _, id := range ids {
		var d deletion
		var purge bool
		err := s.store.RunInTx(ctx, func(tx domain.Tx) error {
			var err error
			if purge, err = tx.LockUnverifiedBefore(ctx, id, cutoff); err != nil || !purge {
				return err
			}
			d, err = deleteUserTx(ctx, tx, id)
			return err
		})
		var shared *domain.SharedProjectsError
		switch {
		case errors.As(err, &shared):
			s.logger.WarnContext(ctx, "unverified account kept: it owns shared projects", slog.String("user_id", id))
			continue
		case err != nil:
			return len(deleted), err
		case !purge:
			continue
		}
		s.accountDeleted(ctx, id, d, "unverified_purge")
		deleted = append(deleted, id)
	}
	if len(ids) > 0 {
		s.logger.InfoContext(ctx, "unverified accounts purged", slog.Int("deleted", len(deleted)),
			slog.Int("found", len(ids)), slog.Time("signed_up_before", cutoff), slog.Any("user_ids", deleted))
	}
	return len(deleted), nil
}

// RunUnverifiedPurge runs PurgeUnverified now and then every interval until ctx ends. A
// failed pass is logged and tried again at the next tick.
func (s *Service) RunUnverifiedPurge(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if _, err := s.PurgeUnverified(ctx); err != nil && ctx.Err() == nil {
			s.logger.ErrorContext(ctx, "purge unverified accounts", slog.Any("error", err))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
