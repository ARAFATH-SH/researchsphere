package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// setSuspended sets the account's suspended time, as an administrator will (FR-15.3);
// nil lifts the suspension.
func (s *fakeStore) setSuspended(t *testing.T, email string, at *time.Time) {
	t.Helper()
	u, ok := s.users[email]
	if !ok {
		t.Fatalf("no user %s", email)
	}
	u.SuspendedAt = at
	s.users[email] = u
}

func TestLoginSuspended(t *testing.T) {
	tests := []struct {
		name         string
		verified     bool
		password     string
		want         error
		wantFailures int
	}{
		{"right password", true, loginPassword, domain.ErrAccountSuspended, 0},
		// A wrong password gets the usual answer, so a guess cannot reveal a suspension.
		{"wrong password", true, "wrong-password", domain.ErrInvalidCredentials, 1},
		// Suspension is checked before verification.
		{"unverified, right password", false, loginPassword, domain.ErrAccountSuspended, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			if tt.verified {
				verifiedUser(t, e, "ada@example.org")
			} else {
				registered(t, e, "ada@example.org")
			}
			at := start
			e.store.setSuspended(t, "ada@example.org", &at)

			_, err := e.svc.Login(context.Background(), LoginInput{Email: "ada@example.org", Password: tt.password})
			if !errors.Is(err, tt.want) {
				t.Fatalf("Login() error = %v, want %v", err, tt.want)
			}
			if len(e.store.sessions) != 0 {
				t.Errorf("sessions stored = %d, want 0", len(e.store.sessions))
			}
			if got := e.attempts.failures["ada@example.org"].count; got != tt.wantFailures {
				t.Errorf("failed logins counted = %d, want %d", got, tt.wantFailures)
			}
		})
	}
}

func TestLoginAfterSuspensionLifted(t *testing.T) {
	e := newEnv()
	verifiedUser(t, e, "ada@example.org")
	at := start
	e.store.setSuspended(t, "ada@example.org", &at)
	if err := login(e, "ada@example.org"); !errors.Is(err, domain.ErrAccountSuspended) {
		t.Fatalf("while suspended: error = %v, want ErrAccountSuspended", err)
	}

	e.store.setSuspended(t, "ada@example.org", nil)
	if err := login(e, "ada@example.org"); err != nil {
		t.Fatalf("after the suspension is lifted: error = %v, want nil", err)
	}
	if len(e.store.sessions) != 1 {
		t.Errorf("sessions stored = %d, want 1", len(e.store.sessions))
	}
}

func TestRefreshSuspendedEndsTheSession(t *testing.T) {
	e := newEnv()
	ctx := context.Background()
	verifiedUser(t, e, "ada@example.org")
	verifiedUser(t, e, "bob@example.org")
	ada := loggedIn(t, e, "ada@example.org")
	bob := loggedIn(t, e, "bob@example.org")

	e.clock.t = start.Add(10 * time.Minute)
	at := e.clock.t
	e.store.setSuspended(t, "ada@example.org", &at)

	if _, err := e.svc.Refresh(ctx, ada); !errors.Is(err, domain.ErrSessionInvalid) {
		t.Fatalf("Refresh() error = %v, want ErrSessionInvalid", err)
	}
	sess := e.store.session(t, ada)
	if sess.RevokedAt == nil || !sess.RevokedAt.Equal(e.clock.t) {
		t.Errorf("RevokedAt = %v, want %v: the suspended user's session must end", sess.RevokedAt, e.clock.t)
	}
	// Lifting the suspension does not bring the ended session back.
	e.store.setSuspended(t, "ada@example.org", nil)
	if _, err := e.svc.Refresh(ctx, ada); !errors.Is(err, domain.ErrSessionInvalid) {
		t.Errorf("Refresh() after the suspension is lifted: error = %v, want ErrSessionInvalid", err)
	}

	// Other users are not touched.
	if _, err := e.svc.Refresh(ctx, bob); err != nil {
		t.Errorf("another user's Refresh() error = %v, want nil", err)
	}
}

func TestRequestPasswordResetSuspendedSendsNothing(t *testing.T) {
	e := newEnv()
	id := verifiedUser(t, e, "ada@example.org")
	at := start
	e.store.setSuspended(t, "ada@example.org", &at)

	if err := e.svc.RequestPasswordReset(context.Background(), "ada@example.org"); err != nil {
		t.Fatalf("RequestPasswordReset() error = %v, want nil: the answer must not change (FR-2.4)", err)
	}
	if len(e.mailer.resets) != 0 {
		t.Errorf("reset emails sent = %d, want 0", len(e.mailer.resets))
	}
	for _, tok := range e.store.tokensFor(id) {
		if tok.purpose == domain.PurposePasswordReset {
			t.Error("a reset token was stored for a suspended account")
		}
	}
}

func TestResetPasswordSuspendedRefused(t *testing.T) {
	e := newEnv()
	ctx := context.Background()
	id := verifiedUser(t, e, "ada@example.org")
	refresh := loggedIn(t, e, "ada@example.org")
	token := resetRequested(t, e, "ada@example.org") // asked for before the suspension
	at := start
	e.store.setSuspended(t, "ada@example.org", &at)

	if err := e.svc.ResetPassword(ctx, token, newPassword); !errors.Is(err, domain.ErrAccountSuspended) {
		t.Fatalf("ResetPassword() error = %v, want ErrAccountSuspended", err)
	}

	// Nothing changed: the token is unused, the session lives and the old password works.
	for _, tok := range e.store.tokensFor(id) {
		if tok.purpose == domain.PurposePasswordReset && tok.UsedAt != nil {
			t.Error("the reset token was marked used")
		}
	}
	if e.store.session(t, refresh).RevokedAt != nil {
		t.Error("the session was revoked")
	}
	e.store.setSuspended(t, "ada@example.org", nil)
	if err := login(e, "ada@example.org"); err != nil {
		t.Errorf("login with the old password: error = %v, want nil", err)
	}
}
