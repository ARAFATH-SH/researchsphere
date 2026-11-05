package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// ChangePasswordInput is what the change password form sends, with the logged-in user's
// ID and the refresh token of the device it came from. The passwords and the token are
// secrets: never log them.
type ChangePasswordInput struct {
	UserID          string
	RefreshToken    string // may be empty: then no session counts as current
	CurrentPassword string
	NewPassword     string
}

// ChangePassword sets a new password for a logged-in user who knows the current one
// (FR-2.6). In one transaction it stores the new password hash and ends every other
// session of the user (FR-2.5). The session whose current refresh token is
// in.RefreshToken stays logged in; when that token names no live session of this user,
// every session ends. It then clears the account's failed logins.
//
// A wrong current password counts as a failed login and returns ErrWrongCurrentPassword;
// the MaxFailedLogins-th failure locks the account (FR-2.7). A locked account returns a
// *domain.LockedError before any password is checked, so this operation cannot be used to
// guess. A new password registration would refuse returns ErrPasswordTooShort or
// ErrPasswordTooLong (FR-1.2), and one equal to the current password ErrSamePassword. A
// user that no longer exists returns ErrNotFound.
func (s *Service) ChangePassword(ctx context.Context, in ChangePasswordInput) error {
	creds, err := s.store.FindCredentialsByID(ctx, in.UserID)
	if err != nil {
		return fmt.Errorf("find user: %w", err)
	}
	lock := s.lockoutFor(creds.Email)
	if until := lock.lockedUntil(ctx); !until.IsZero() {
		return &domain.LockedError{Until: until, RetryAfter: until.Sub(s.now())}
	}
	if err := checkPassword(in.NewPassword); err != nil {
		return err
	}

	if utf8.RuneCountInString(in.CurrentPassword) > MaxPasswordLength {
		lock.recordFailure(ctx)
		return domain.ErrWrongCurrentPassword // no stored password can match
	}
	ok, err := s.hasher.Verify(in.CurrentPassword, creds.PasswordHash)
	if err != nil {
		return fmt.Errorf("check password: %w", err)
	}
	if !ok {
		lock.recordFailure(ctx)
		return domain.ErrWrongCurrentPassword
	}
	// Compared only after the current password is known to be right, so a wrong guess
	// is always counted.
	if in.NewPassword == in.CurrentPassword {
		return domain.ErrSamePassword
	}

	// Hash before the transaction: Argon2id is slow, and the session row stays locked
	// until the transaction ends.
	passwordHash, err := s.hasher.Hash(in.NewPassword)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	now := s.now()
	var kept string // the ID of the session that stays logged in, if any
	err = s.store.RunInTx(ctx, func(tx domain.Tx) error {
		if kept, err = s.currentSession(ctx, tx, creds.ID, in.RefreshToken); err != nil {
			return err
		}
		if err := tx.UpdatePasswordHash(ctx, creds.ID, passwordHash); err != nil {
			return err
		}
		if kept == "" {
			return tx.RevokeUserSessions(ctx, creds.ID, now)
		}
		return tx.RevokeOtherUserSessions(ctx, creds.ID, kept, now)
	})
	if err != nil {
		return err
	}
	s.logger.InfoContext(ctx, "password changed", slog.String("user_id", creds.ID), slog.Bool("kept_current_session", kept != ""))
	s.sessionsEnded(ctx, creds.ID)
	lock.reset(ctx)
	return nil
}

// currentSession returns the ID of the live session of userID whose current refresh
// token is token, locking it until the transaction ends so a refresh cannot rotate it
// meanwhile. It returns "" when there is no such session.
func (s *Service) currentSession(ctx context.Context, tx domain.Tx, userID, token string) (string, error) {
	if !wellFormedToken(token) {
		return "", nil
	}
	sess, err := tx.FindSessionForUpdate(ctx, hashToken(token))
	if errors.Is(err, domain.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("find session: %w", err)
	}
	if sess.UserID != userID || sess.RevokedAt != nil || !s.now().Before(sess.ExpiresAt) {
		return "", nil
	}
	return sess.ID, nil
}
