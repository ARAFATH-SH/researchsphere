package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// PasswordResetTTL is how long a password reset link works (FR-2.3).
const PasswordResetTTL = time.Hour

// RequestPasswordReset emails a reset link when email belongs to an account, verified
// or not, and makes the account's older unused reset links stop working (FR-2.3). So
// that the reply never shows whether an address is registered, it returns nil for
// unknown addresses and when anything fails; failures are logged (FR-2.4). Only a
// malformed address gives ErrInvalidEmail.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	email, err := normalizeEmail(email)
	if err != nil {
		return err
	}

	user, err := s.store.FindUserByEmail(ctx, email)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		s.logger.ErrorContext(ctx, "request password reset: find user", slog.Any("error", err))
		return nil
	}

	token, tokenHash := newToken()
	err = s.store.RunInTx(ctx, func(tx domain.Tx) error {
		if err := tx.DeleteUnusedTokens(ctx, user.ID, domain.PurposePasswordReset); err != nil {
			return err
		}
		return tx.CreateToken(ctx, domain.NewToken{
			UserID:    user.ID,
			Purpose:   domain.PurposePasswordReset,
			Hash:      tokenHash,
			ExpiresAt: s.now().Add(PasswordResetTTL),
		})
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "request password reset: store token", slog.String("user_id", user.ID), slog.Any("error", err))
		return nil
	}

	err = s.mailer.SendPasswordReset(ctx, domain.PasswordResetEmail{
		To:       user.Email,
		FullName: user.FullName,
		Link:     s.resetLink(token),
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "send password reset email", slog.String("user_id", user.ID), slog.Any("error", err))
	}
	return nil
}

// ResetPassword sets a new password with the token from a reset link (FR-2.3). In one
// transaction it marks the token used, stores the new password hash, marks the email
// address verified (the link proves the user reads that inbox) and ends every session
// of the user (FR-2.5). It then clears the account's failed logins (FR-2.7).
//
// It returns ErrTokenInvalid for an unknown, malformed or non-reset token,
// ErrTokenUsed, ErrTokenExpired, and ErrPasswordTooShort or ErrPasswordTooLong for a
// password registration would refuse (FR-1.2).
func (s *Service) ResetPassword(ctx context.Context, token, newPassword string) error {
	if !wellFormedToken(token) {
		return domain.ErrTokenInvalid
	}
	if err := checkPassword(newPassword); err != nil {
		return err
	}
	// Hash before the transaction: Argon2id is slow, and the token row stays locked
	// until the transaction ends.
	passwordHash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	hash := hashToken(token)
	now := s.now()

	var userID string
	err = s.store.RunInTx(ctx, func(tx domain.Tx) error {
		// The row lock makes two requests with the same token wait for each other,
		// so only one of them can use it.
		t, err := tx.FindTokenForUpdate(ctx, hash, domain.PurposePasswordReset)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrTokenInvalid
		}
		if err != nil {
			return err
		}
		if t.UsedAt != nil {
			return domain.ErrTokenUsed
		}
		if !now.Before(t.ExpiresAt) {
			return domain.ErrTokenExpired
		}
		if err := tx.MarkTokenUsed(ctx, t.ID, now); err != nil {
			return err
		}
		if err := tx.UpdatePasswordHash(ctx, t.UserID, passwordHash); err != nil {
			return err
		}
		if err := tx.MarkEmailVerified(ctx, t.UserID, now); err != nil {
			return err
		}
		if err := tx.RevokeUserSessions(ctx, t.UserID, now); err != nil {
			return err
		}
		userID = t.UserID
		return nil
	})
	if err != nil {
		return err
	}
	s.logger.InfoContext(ctx, "password reset", slog.String("user_id", userID))
	s.clearFailedLogins(ctx, userID)
	return nil
}

// clearFailedLogins forgets the account's failed logins after a reset. The counter is
// in Redis, outside the transaction, so this runs after the commit; like the rest of
// the lockout it fails open, and a failure is logged without failing the reset.
func (s *Service) clearFailedLogins(ctx context.Context, userID string) {
	user, err := s.store.FindUserByID(ctx, userID)
	if err == nil {
		// The same key as lockoutFor uses at login.
		err = s.attempts.Reset(ctx, strings.ToLower(user.Email))
	}
	if err != nil {
		s.logger.WarnContext(ctx, "password reset: could not clear failed logins", slog.String("user_id", userID), slog.Any("error", err))
	}
}
