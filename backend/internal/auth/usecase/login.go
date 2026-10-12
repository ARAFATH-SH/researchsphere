package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// RefreshTTL is how long a refresh token, and so a session, lasts (FR-2.1). The access
// token's lifetime is set by the AccessTokenSigner.
const RefreshTTL = 30 * 24 * time.Hour

// LoginInput is what the login form sends.
type LoginInput struct {
	Email    string
	Password string
}

// LoginResult holds the tokens of a new session. Both tokens are secrets: never log them.
type LoginResult struct {
	User             domain.User
	IssuedAt         time.Time
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
}

// Login checks the email address and password and starts a new session (FR-2.1).
// An unknown address and a wrong password both return ErrInvalidCredentials. An
// account whose address is not verified returns ErrEmailNotVerified, but only after
// the right password, so that answer cannot reveal an account either (FR-1.4).
func (s *Service) Login(ctx context.Context, in LoginInput) (LoginResult, error) {
	email, err := normalizeEmail(in.Email)
	if err != nil || utf8.RuneCountInString(in.Password) > MaxPasswordLength {
		return LoginResult{}, domain.ErrInvalidCredentials // no account can match
	}

	creds, err := s.store.FindCredentialsByEmail(ctx, email)
	if errors.Is(err, domain.ErrNotFound) {
		// Check the password against a dummy hash anyway, so an unknown address takes
		// as long to refuse as a known one.
		if err := s.checkDummyPassword(in.Password); err != nil {
			return LoginResult{}, err
		}
		return LoginResult{}, domain.ErrInvalidCredentials
	}
	if err != nil {
		return LoginResult{}, fmt.Errorf("find user: %w", err)
	}

	ok, err := s.hasher.Verify(in.Password, creds.PasswordHash)
	if err != nil {
		return LoginResult{}, fmt.Errorf("check password: %w", err)
	}
	if !ok {
		return LoginResult{}, domain.ErrInvalidCredentials
	}
	if !creds.Verified() {
		return LoginResult{}, domain.ErrEmailNotVerified
	}

	now := s.now()
	refresh, hash := newToken()
	res := LoginResult{User: creds.User, IssuedAt: now, RefreshToken: refresh, RefreshExpiresAt: now.Add(RefreshTTL)}
	// Sign first, so a failure leaves no session behind.
	if res.AccessToken, res.AccessExpiresAt, err = s.signer.Sign(creds.ID, now); err != nil {
		return LoginResult{}, err
	}
	if err := s.store.CreateSession(ctx, domain.NewSession{UserID: creds.ID, TokenHash: hash, ExpiresAt: res.RefreshExpiresAt}); err != nil {
		return LoginResult{}, fmt.Errorf("create session: %w", err)
	}
	s.logger.InfoContext(ctx, "user logged in", slog.String("user_id", creds.ID))
	return res, nil
}

// checkDummyPassword runs one password check that always fails. The dummy hash is made
// on first use with the real hasher, so it costs the same as a stored hash.
func (s *Service) checkDummyPassword(password string) error {
	s.dummyOnce.Do(func() {
		s.dummyHash, s.dummyErr = s.hasher.Hash("dummy password, never a real one")
	})
	if s.dummyErr != nil {
		return fmt.Errorf("make dummy hash: %w", s.dummyErr)
	}
	if _, err := s.hasher.Verify(password, s.dummyHash); err != nil {
		return fmt.Errorf("check password: %w", err)
	}
	return nil
}

// CurrentUser returns the user with id, or ErrNotFound when the account no longer exists.
func (s *Service) CurrentUser(ctx context.Context, id string) (domain.User, error) {
	return s.store.FindUserByID(ctx, id)
}
