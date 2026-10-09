package usecase

import (
	"context"
	"errors"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// VerifyEmail marks the token's user as verified (FR-1.3). The token works once and
// for VerificationTTL. It returns ErrTokenInvalid for an unknown or malformed token,
// ErrTokenUsed and ErrTokenExpired otherwise.
func (s *Service) VerifyEmail(ctx context.Context, token string) error {
	if !wellFormedToken(token) {
		return domain.ErrTokenInvalid
	}
	hash := hashToken(token)
	now := s.now()

	return s.store.RunInTx(ctx, func(tx domain.Tx) error {
		// The row lock makes two requests with the same token wait for each other,
		// so only one of them can use it.
		t, err := tx.FindTokenForUpdate(ctx, hash, domain.PurposeEmailVerification)
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
		return tx.MarkEmailVerified(ctx, t.UserID, now)
	})
}
