// Package usecase holds the auth module's logic: registration, email verification and
// new verification links (FR-1). It imports only the auth domain.
package usecase

import (
	"log/slog"
	"strings"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// VerificationTTL is how long a verification link works (FR-1.3).
const VerificationTTL = 24 * time.Hour

// Config holds the settings the usecase needs.
type Config struct {
	// AppBaseURL is where the web client runs, such as https://researchsphere.example.
	// Verification links point to its /verify-email page.
	AppBaseURL string
}

// Service registers users and verifies their email addresses.
type Service struct {
	store  domain.Store
	hasher domain.PasswordHasher
	mailer domain.Mailer
	cfg    Config
	now    func() time.Time
	logger *slog.Logger
}

// New returns a Service. now is time.Now in production and a fixed clock in tests.
func New(store domain.Store, hasher domain.PasswordHasher, mailer domain.Mailer, cfg Config, now func() time.Time, logger *slog.Logger) *Service {
	cfg.AppBaseURL = strings.TrimRight(cfg.AppBaseURL, "/")
	return &Service{store: store, hasher: hasher, mailer: mailer, cfg: cfg, now: now, logger: logger}
}

// verificationLink builds the link sent by email. The token is in the fragment, which
// browsers never send to a server, so it cannot appear in access logs or Referer headers.
func (s *Service) verificationLink(token string) string {
	return s.cfg.AppBaseURL + "/verify-email#token=" + token
}
