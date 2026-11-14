package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// DeleteAccountInput is what the delete account form sends, with the logged-in user's ID.
// The password is a secret: never log it.
type DeleteAccountInput struct {
	UserID   string
	Password string
}

// DeleteAccount deletes a logged-in user who confirms with the password (FR-2.8). While
// the user owns a project that has other members it returns a *domain.SharedProjectsError
// listing them and deletes nothing (FR-2.9). Otherwise, in one transaction, it deletes
// the projects where the user is the only member, the user's publication links and the
// publications left with no author, and the user, whose other rows the database deletes
// with it. It then clears the account's failed logins.
//
// A wrong password counts as a failed login and returns ErrWrongPassword; a locked
// account returns a *domain.LockedError before the password is checked (FR-2.7). A user
// that no longer exists returns ErrNotFound.
func (s *Service) DeleteAccount(ctx context.Context, in DeleteAccountInput) error {
	creds, err := s.store.FindCredentialsByID(ctx, in.UserID)
	if err != nil {
		return fmt.Errorf("find user: %w", err)
	}
	lock := s.lockoutFor(creds.Email)
	if until := lock.lockedUntil(ctx); !until.IsZero() {
		return &domain.LockedError{Until: until, RetryAfter: until.Sub(s.now())}
	}
	if utf8.RuneCountInString(in.Password) > MaxPasswordLength {
		lock.recordFailure(ctx)
		return domain.ErrWrongPassword // no stored password can match
	}
	ok, err := s.hasher.Verify(in.Password, creds.PasswordHash)
	if err != nil {
		return fmt.Errorf("check password: %w", err)
	}
	if !ok {
		lock.recordFailure(ctx)
		return domain.ErrWrongPassword
	}

	var d deletion
	err = s.store.RunInTx(ctx, func(tx domain.Tx) error {
		var err error
		d, err = deleteUserTx(ctx, tx, creds.ID)
		return err
	})
	if err != nil {
		return err
	}
	s.accountDeleted(ctx, creds.ID, d, "user")
	lock.reset(ctx)
	return nil
}

// DeleteAccountByID deletes the account userID through the same transaction as DeleteAccount
// (FR-2.8, FR-2.9), without a password and without touching the failed-login counter. It is
// for maintenance commands only, never a route: cmd/seedsample's sample purge calls it with
// by "sample_purge". TestNoRouteDeletesAccountsByID (cmd/api) fails when anything under
// internal/ or cmd/api calls it. A user who owns a project with other members returns a
// *domain.SharedProjectsError and keeps everything; a missing user returns ErrNotFound.
func (s *Service) DeleteAccountByID(ctx context.Context, userID, by string) error {
	var d deletion
	err := s.store.RunInTx(ctx, func(tx domain.Tx) error {
		var err error
		d, err = deleteUserTx(ctx, tx, userID)
		return err
	})
	if err != nil {
		return err
	}
	s.accountDeleted(ctx, userID, d, by)
	return nil
}

// deletion counts what deleteUserTx removed besides the user.
type deletion struct{ projects, publications int64 }

// deleteUserTx is the account deletion of FR-2.8 inside tx, shared by DeleteAccount and the
// purge of unverified accounts (FR-17.4). While the user owns a project that has other
// members it returns a *domain.SharedProjectsError and deletes nothing (FR-2.9). Otherwise
// it deletes the projects where the user is the only member, the user's publication links
// and the publications left with no author, and the user, whose other rows the database
// deletes with it.
func deleteUserTx(ctx context.Context, tx domain.Tx, userID string) (deletion, error) {
	var d deletion
	// Lock first, then look: a member who joined while the lock was awaited is seen.
	if err := tx.LockOwnedProjects(ctx, userID); err != nil {
		return d, err
	}
	shared, err := tx.FindSharedOwnedProjects(ctx, userID)
	if err != nil {
		return d, err
	}
	if len(shared) > 0 {
		return d, &domain.SharedProjectsError{Projects: shared}
	}
	if d.projects, err = tx.DeleteSoloProjects(ctx, userID); err != nil {
		return d, err
	}
	if d.publications, err = tx.DeleteUserPublications(ctx, userID); err != nil {
		return d, err
	}
	return d, tx.DeleteUser(ctx, userID)
}

// accountDeleted logs a deletion after its commit and ends the user's sessions. by says who
// deleted it: the user, the purge of unverified accounts or a maintenance command.
func (s *Service) accountDeleted(ctx context.Context, userID string, d deletion, by string) {
	s.logger.InfoContext(ctx, "account deleted", slog.String("user_id", userID), slog.String("by", by),
		slog.Int64("projects_deleted", d.projects), slog.Int64("publications_deleted", d.publications))
	s.sessionsEnded(ctx, userID)
}
