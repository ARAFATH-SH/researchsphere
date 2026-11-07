package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// RefreshResult holds the new tokens of a refreshed session. Both tokens are secrets:
// never log them.
type RefreshResult struct {
	IssuedAt         time.Time
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time // the expiry set at login: refreshing does not extend it
}

// Refresh replaces the session's refresh token with a new one and issues a new access
// token (FR-2.1). The old refresh token stops working. A missing, unknown, expired or
// revoked session returns ErrSessionInvalid. So does a refresh token that was already
// replaced, which also ends its session: either the user or a thief holds a copy. So
// does a session of a suspended account, which is ended too, so the login lasts only
// until its access token expires (FR-3.4). A temporary suspension that has ended by time is
// cleared here, and the refresh goes on (FR-15.7).
func (s *Service) Refresh(ctx context.Context, token string) (RefreshResult, error) {
	if !wellFormedToken(token) {
		return RefreshResult{}, domain.ErrSessionInvalid
	}
	hash := hashToken(token)
	now := s.now()

	var (
		res       RefreshResult
		reused    *domain.Session // set when a retired token was sent; the transaction commits its revocation
		suspended *domain.Session // set when the account is suspended; likewise
	)
	err := s.store.RunInTx(ctx, func(tx domain.Tx) error {
		sess, err := tx.FindSessionForUpdate(ctx, hash)
		if errors.Is(err, domain.ErrNotFound) {
			return s.revokeIfReused(ctx, tx, hash, now, &reused)
		}
		if err != nil {
			return fmt.Errorf("find session: %w", err)
		}
		if sess.RevokedAt != nil || !now.Before(sess.ExpiresAt) {
			return domain.ErrSessionInvalid
		}
		if domain.SuspensionExpired(sess.UserSuspendedAt, sess.UserSuspendedUntil, now) {
			if err := tx.ClearExpiredSuspension(ctx, sess.UserID, now); err != nil {
				return err
			}
		} else if sess.UserSuspendedAt != nil {
			if err := tx.RevokeSession(ctx, sess.ID, now); err != nil {
				return fmt.Errorf("revoke session: %w", err)
			}
			suspended = &sess
			return nil
		}

		res = RefreshResult{IssuedAt: now, RefreshExpiresAt: sess.ExpiresAt}
		if res.AccessToken, res.AccessExpiresAt, err = s.signer.Sign(sess.UserID, sess.UserRole, now); err != nil {
			return err
		}
		var newHash string
		res.RefreshToken, newHash = newToken()
		if err := tx.RotateSession(ctx, sess.ID, hash, newHash, now); err != nil {
			return fmt.Errorf("rotate session: %w", err)
		}
		return nil
	})
	if err != nil {
		return RefreshResult{}, err
	}
	if reused != nil {
		s.logger.WarnContext(ctx, "refresh token reused; session ended",
			slog.String("session_id", reused.ID), slog.String("user_id", reused.UserID))
		s.sessionsEnded(ctx, reused.UserID)
		return RefreshResult{}, domain.ErrSessionInvalid
	}
	if suspended != nil {
		s.logger.InfoContext(ctx, "account suspended; session ended",
			slog.String("session_id", suspended.ID), slog.String("user_id", suspended.UserID))
		s.sessionsEnded(ctx, suspended.UserID)
		return RefreshResult{}, domain.ErrSessionInvalid
	}
	return res, nil
}

// revokeIfReused ends the session that once had the refresh token with hash, if any, and
// reports it through reused. It returns ErrSessionInvalid when no session ever had it.
func (s *Service) revokeIfReused(ctx context.Context, tx domain.Tx, hash string, now time.Time, reused **domain.Session) error {
	sess, err := tx.FindSessionByRetiredTokenForUpdate(ctx, hash)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrSessionInvalid
	}
	if err != nil {
		return fmt.Errorf("find session by retired token: %w", err)
	}
	if err := tx.RevokeSession(ctx, sess.ID, now); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	*reused = &sess
	return nil
}

// Logout ends the session whose current refresh token is token, so the token no longer
// works (FR-2.2). Other sessions of the user are not touched. It returns nil when there
// is no such session, so logging out twice is not an error.
func (s *Service) Logout(ctx context.Context, token string) error {
	if !wellFormedToken(token) {
		return nil
	}
	sess, err := s.store.RevokeSessionByToken(ctx, hashToken(token), s.now())
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	s.logger.InfoContext(ctx, "user logged out", slog.String("session_id", sess.ID), slog.String("user_id", sess.UserID))
	s.sessionsEnded(ctx, sess.UserID)
	return nil
}

// sessionsEnded tells the listener, if there is one.
func (s *Service) sessionsEnded(ctx context.Context, userID string) {
	if s.cfg.Sessions != nil {
		s.cfg.Sessions.SessionsEnded(ctx, userID)
	}
}
