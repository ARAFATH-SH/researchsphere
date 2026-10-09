package usecase

import (
	"context"
	"errors"
	"log/slog"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// ResendVerification emails a new verification link when email belongs to an
// unverified account, and makes the older unused links stop working (FR-1.5). So that
// the reply never shows whether an address is registered, it returns nil for unknown
// and verified addresses and when anything fails; failures are logged. Only a
// malformed address gives ErrInvalidEmail.
func (s *Service) ResendVerification(ctx context.Context, email string) error {
	email, err := normalizeEmail(email)
	if err != nil {
		return err
	}

	user, err := s.store.FindUserByEmail(ctx, email)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		s.logger.ErrorContext(ctx, "resend verification: find user", slog.Any("error", err))
		return nil
	}
	if user.Verified() {
		return nil
	}

	token, tokenHash := newToken()
	err = s.store.RunInTx(ctx, func(tx domain.Tx) error {
		if err := tx.DeleteUnusedTokens(ctx, user.ID, domain.PurposeEmailVerification); err != nil {
			return err
		}
		return tx.CreateToken(ctx, domain.NewToken{
			UserID:    user.ID,
			Purpose:   domain.PurposeEmailVerification,
			Hash:      tokenHash,
			ExpiresAt: s.now().Add(VerificationTTL),
		})
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "resend verification: store token", slog.String("user_id", user.ID), slog.Any("error", err))
		return nil
	}

	s.sendVerification(ctx, user.ID, user.Email, user.FullName, token)
	return nil
}
