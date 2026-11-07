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

// suspendFor stores a suspension as the moderation module does (FR-15.7).
func suspendFor(t *testing.T, e *env, id string, until *time.Time, reason string) {
	t.Helper()
	if err := e.svc.Suspend(context.Background(), id, start, until, reason); err != nil {
		t.Fatal(err)
	}
}

func TestLoginSuspendedSaysWhyAndUntilWhen(t *testing.T) {
	end := start.Add(48 * time.Hour)
	tests := []struct {
		name  string
		until *time.Time
	}{
		{"temporary", &end},
		{"permanent", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			id := verifiedUser(t, e, "ada@example.org")
			suspendFor(t, e, id, tt.until, "Repeated spam in messages")

			err := login(e, "ada@example.org")
			var refused *domain.SuspendedError
			if !errors.As(err, &refused) || !errors.Is(err, domain.ErrAccountSuspended) {
				t.Fatalf("Login() error = %v, want *SuspendedError", err)
			}
			if refused.Reason == nil || *refused.Reason != "Repeated spam in messages" {
				t.Errorf("Reason = %v", refused.Reason)
			}
			if (refused.Until == nil) != (tt.until == nil) || (tt.until != nil && !refused.Until.Equal(*tt.until)) {
				t.Errorf("Until = %v, want %v", refused.Until, tt.until)
			}
		})
	}
}

// A wrong password must not reveal a suspension or its reason (FR-3.4).
func TestLoginSuspendedWrongPasswordRevealsNothing(t *testing.T) {
	e := newEnv()
	id := verifiedUser(t, e, "ada@example.org")
	suspendFor(t, e, id, nil, "Repeated spam in messages")

	_, err := e.svc.Login(context.Background(), LoginInput{Email: "ada@example.org", Password: "wrong-password"})
	var refused *domain.SuspendedError
	if errors.As(err, &refused) || !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v, want ErrInvalidCredentials", err)
	}
}

// A temporary suspension ends at its exact end time, checked at login, which clears it
// (FR-15.7).
func TestLoginTemporarySuspensionEndsByItself(t *testing.T) {
	e := newEnv()
	id := verifiedUser(t, e, "ada@example.org")
	end := start.Add(24 * time.Hour)
	suspendFor(t, e, id, &end, "Repeated spam in messages")

	e.clock.t = end.Add(-time.Nanosecond)
	if err := login(e, "ada@example.org"); !errors.Is(err, domain.ErrAccountSuspended) {
		t.Fatalf("just before the end: error = %v, want ErrAccountSuspended", err)
	}
	e.clock.t = end
	if err := login(e, "ada@example.org"); err != nil {
		t.Fatalf("at the end: error = %v, want nil", err)
	}
	u := e.store.users["ada@example.org"]
	if u.SuspendedAt != nil || u.SuspendedUntil != nil || u.suspensionReason != nil {
		t.Errorf("suspension not cleared: %+v", u)
	}
}

// A session that outlived a temporary suspension (one set by hand revokes nothing) refreshes
// once it has ended, and the refresh clears it (FR-15.7).
func TestRefreshTemporarySuspensionEndsByItself(t *testing.T) {
	e := newEnv()
	ctx := context.Background()
	verifiedUser(t, e, "ada@example.org")
	ada := loggedIn(t, e, "ada@example.org")
	u := e.store.users["ada@example.org"]
	at, end := start, start.Add(time.Hour)
	u.SuspendedAt, u.SuspendedUntil = &at, &end
	e.store.users["ada@example.org"] = u

	e.clock.t = end
	if _, err := e.svc.Refresh(ctx, ada); err != nil {
		t.Fatalf("Refresh() error = %v, want nil", err)
	}
	if got := e.store.users["ada@example.org"]; got.SuspendedAt != nil {
		t.Errorf("suspension not cleared: %+v", got)
	}
}

func TestRefreshDuringTemporarySuspensionEndsTheSession(t *testing.T) {
	e := newEnv()
	verifiedUser(t, e, "ada@example.org")
	ada := loggedIn(t, e, "ada@example.org")
	u := e.store.users["ada@example.org"]
	at, end := start, start.Add(time.Hour)
	u.SuspendedAt, u.SuspendedUntil = &at, &end
	e.store.users["ada@example.org"] = u

	e.clock.t = end.Add(-time.Second)
	if _, err := e.svc.Refresh(context.Background(), ada); !errors.Is(err, domain.ErrSessionInvalid) {
		t.Fatalf("Refresh() error = %v, want ErrSessionInvalid", err)
	}
}

// A suspension that has ended by time must not stop the user getting back in by a reset, even
// before a login clears it (FR-15.7).
func TestPasswordResetAfterSuspensionEnded(t *testing.T) {
	e := newEnv()
	id := verifiedUser(t, e, "ada@example.org")
	end := start.Add(time.Hour)
	suspendFor(t, e, id, &end, "Repeated spam in messages")
	before := len(e.mailer.resets)

	if err := e.svc.RequestPasswordReset(context.Background(), "ada@example.org"); err != nil {
		t.Fatal(err)
	}
	if got := len(e.mailer.resets); got != before {
		t.Fatalf("during the suspension: %d emails sent, want none", got-before)
	}
	e.clock.t = end
	if err := e.svc.RequestPasswordReset(context.Background(), "ada@example.org"); err != nil {
		t.Fatal(err)
	}
	if got := len(e.mailer.resets); got != before+1 {
		t.Errorf("after the end: %d emails sent, want 1", got-before)
	}
}

func TestUserSuspendedByTime(t *testing.T) {
	at, end := start, start.Add(time.Hour)
	tests := []struct {
		name      string
		user      domain.User
		now       time.Time
		suspended bool
		expired   bool
	}{
		{"never", domain.User{}, end, false, false},
		{"permanent", domain.User{SuspendedAt: &at}, end.Add(1000 * time.Hour), true, false},
		{"before the end", domain.User{SuspendedAt: &at, SuspendedUntil: &end}, end.Add(-time.Second), true, false},
		{"at the end", domain.User{SuspendedAt: &at, SuspendedUntil: &end}, end, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.user.Suspended(tt.now); got != tt.suspended {
				t.Errorf("Suspended = %v, want %v", got, tt.suspended)
			}
			if got := tt.user.SuspensionExpired(tt.now); got != tt.expired {
				t.Errorf("SuspensionExpired = %v, want %v", got, tt.expired)
			}
		})
	}
}
