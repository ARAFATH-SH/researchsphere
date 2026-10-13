// Package usecase holds the auth module's logic: registration, email verification and
// new verification links (FR-1), login (FR-2.1), refreshing and ending sessions
// (FR-2.2), password reset (FR-2.3 to FR-2.5), and locking an account after failed
// logins (FR-2.7). It imports only the auth domain.
package usecase

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// VerificationTTL is how long a verification link works (FR-1.3).
const VerificationTTL = 24 * time.Hour

// Config holds the settings the usecase needs.
type Config struct {
	// AppBaseURL is where the web client runs, such as https://researchsphere.example.
	// Verification links point to its /verify-email page, reset links to /reset-password.
	AppBaseURL string
}

// Service registers users, verifies their email addresses and logs them in.
type Service struct {
	store  domain.Store
	hasher domain.PasswordHasher
	mailer domain.Mailer
	signer domain.AccessTokenSigner
	// attempts counts failed logins (FR-2.7). When it fails, logins go on unlocked.
	attempts domain.LoginAttempts
	cfg      Config
	now      func() time.Time
	logger   *slog.Logger

	// The hash checked when a login names an unknown address (login.go).
	dummyOnce sync.Once
	dummyHash string
	dummyErr  error
}

// New returns a Service. now is time.Now in production and a fixed clock in tests.
func New(store domain.Store, hasher domain.PasswordHasher, mailer domain.Mailer, signer domain.AccessTokenSigner, attempts domain.LoginAttempts, cfg Config, now func() time.Time, logger *slog.Logger) *Service {
	cfg.AppBaseURL = strings.TrimRight(cfg.AppBaseURL, "/")
	return &Service{store: store, hasher: hasher, mailer: mailer, signer: signer, attempts: attempts, cfg: cfg, now: now, logger: logger}
}

// verificationLink builds the link sent by email. The token is in the fragment, which
// browsers never send to a server, so it cannot appear in access logs or Referer headers.
func (s *Service) verificationLink(token string) string {
	return s.cfg.AppBaseURL + "/verify-email#token=" + token
}

// resetLink builds the password reset link, with the token in the fragment for the same
// reason as verificationLink.
func (s *Service) resetLink(token string) string {
	return s.cfg.AppBaseURL + "/reset-password#token=" + token
}
