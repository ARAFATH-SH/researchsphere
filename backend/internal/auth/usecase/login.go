package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// RefreshTTL is how long a refresh token, and so a session, lasts (FR-2.1). The access
// token's lifetime is set by the AccessTokenSigner.
const RefreshTTL = 30 * 24 * time.Hour

// Login lockout (FR-2.7): MaxFailedLogins failures within LockoutWindow lock the account
// for LockoutDuration from the last of them.
const (
	MaxFailedLogins = 5
	LockoutWindow   = 15 * time.Minute
	LockoutDuration = 15 * time.Minute
)

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
//
// A locked address returns a *domain.LockedError before the password is checked, so
// even the right password is refused (FR-2.7). An unknown address and a wrong password
// count as failures; only a login that starts a session clears them.
func (s *Service) Login(ctx context.Context, in LoginInput) (LoginResult, error) {
	email, err := normalizeEmail(in.Email)
	if err != nil {
		return LoginResult{}, domain.ErrInvalidCredentials // no account can match, so nothing to count
	}
	lock := s.lockoutFor(email)
	if until := lock.lockedUntil(ctx); !until.IsZero() {
		return LoginResult{}, &domain.LockedError{Until: until, RetryAfter: until.Sub(s.now())}
	}
	if utf8.RuneCountInString(in.Password) > MaxPasswordLength {
		lock.recordFailure(ctx)
		return LoginResult{}, domain.ErrInvalidCredentials // no password can match
	}

	creds, err := s.store.FindCredentialsByEmail(ctx, email)
	if errors.Is(err, domain.ErrNotFound) {
		// Check the password against a dummy hash anyway, so an unknown address takes
		// as long to refuse as a known one.
		if err := s.checkDummyPassword(in.Password); err != nil {
			return LoginResult{}, err
		}
		lock.recordFailure(ctx)
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
		lock.recordFailure(ctx)
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
	lock.reset(ctx)
	s.logger.InfoContext(ctx, "user logged in", slog.String("user_id", creds.ID))
	return res, nil
}

// lockout is the failed-login counter for one login attempt (FR-2.7). The first counter
// error is logged once, at warn level, and the rest of the login skips the counter and
// goes on unlocked, so an outage of the counter neither locks everyone out nor makes a
// login wait on it more than once (docs/decisions.md).
type lockout struct {
	s    *Service
	key  string
	down bool // a counter call has failed during this login
}

// lockoutFor returns the counter for email. The key ignores letter case, as the database
// does when it looks up an address, so ADA@example.org cannot get round the lock on
// ada@example.org.
func (s *Service) lockoutFor(email string) *lockout {
	return &lockout{s: s, key: strings.ToLower(email)}
}

// failed logs the counter's error once and turns the counter off for this login.
func (l *lockout) failed(ctx context.Context, err error) {
	l.down = true
	l.s.logger.WarnContext(ctx, "login lockout unavailable, allowing the login", slog.Any("error", err))
}

// lockedUntil returns when the address's lock ends, or the zero time when it is not
// locked or the counter fails.
func (l *lockout) lockedUntil(ctx context.Context) time.Time {
	if l.down {
		return time.Time{}
	}
	until, err := l.s.attempts.LockedUntil(ctx, l.key)
	if err != nil {
		l.failed(ctx, err)
		return time.Time{}
	}
	if !until.After(l.s.now()) {
		return time.Time{}
	}
	return until
}

// recordFailure counts a failed login and locks the address on the MaxFailedLogins-th
// failure within LockoutWindow.
func (l *lockout) recordFailure(ctx context.Context) {
	if l.down {
		return
	}
	n, err := l.s.attempts.RecordFailure(ctx, l.key, LockoutWindow)
	if err != nil {
		l.failed(ctx, err)
		return
	}
	if n < MaxFailedLogins {
		return
	}
	if err := l.s.attempts.Lock(ctx, l.key, l.s.now().Add(LockoutDuration)); err != nil {
		l.failed(ctx, err)
		return
	}
	l.s.logger.WarnContext(ctx, "account locked after failed logins", slog.Int("failures", n))
}

// reset clears the failed logins after a successful login.
func (l *lockout) reset(ctx context.Context) {
	if l.down {
		return
	}
	if err := l.s.attempts.Reset(ctx, l.key); err != nil {
		l.failed(ctx, err)
	}
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
