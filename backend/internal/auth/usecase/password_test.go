package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// changePassword changes Ada's password from the device logged in with refresh.
func changePassword(e *env, userID, refresh, current, next string) error {
	return e.svc.ChangePassword(context.Background(), ChangePasswordInput{
		UserID:          userID,
		RefreshToken:    refresh,
		CurrentPassword: current,
		NewPassword:     next,
	})
}

func TestChangePasswordRevokesOtherSessions(t *testing.T) {
	e := newEnv()
	ctx := context.Background()
	ada := verifiedUser(t, e, "ada@example.org")
	verifiedUser(t, e, "grace@example.org")
	laptop := loggedIn(t, e, "ada@example.org") // the device that changes the password
	phone := loggedIn(t, e, "ada@example.org")
	tablet := loggedIn(t, e, "ada@example.org")
	other := loggedIn(t, e, "grace@example.org")

	e.clock.t = start.Add(time.Minute)
	if err := changePassword(e, ada, laptop, loginPassword, newPassword); err != nil {
		t.Fatalf("ChangePassword() error = %v, want nil", err)
	}

	if e.store.session(t, laptop).RevokedAt != nil {
		t.Error("the current session was revoked")
	}
	if _, err := e.svc.Refresh(ctx, laptop); err != nil {
		t.Errorf("refresh on the current device: error = %v, want nil", err)
	}
	for name, token := range map[string]string{"phone": phone, "tablet": tablet} {
		sess := e.store.session(t, token)
		if sess.RevokedAt == nil || !sess.RevokedAt.Equal(e.clock.t) {
			t.Errorf("%s session: revoked at %v, want the change time", name, sess.RevokedAt)
		}
		if _, err := e.svc.Refresh(ctx, token); !errors.Is(err, domain.ErrSessionInvalid) {
			t.Errorf("%s refresh after change: error = %v, want ErrSessionInvalid", name, err)
		}
	}
	if e.store.session(t, other).RevokedAt != nil {
		t.Error("another user's session was revoked")
	}

	if _, err := e.svc.Login(ctx, LoginInput{Email: "ada@example.org", Password: newPassword}); err != nil {
		t.Errorf("login with the new password: error = %v, want nil", err)
	}
	if _, err := e.svc.Login(ctx, LoginInput{Email: "ada@example.org", Password: loginPassword}); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Errorf("login with the old password: error = %v, want ErrInvalidCredentials", err)
	}
	if strings.Contains(e.logs.String(), newPassword) || strings.Contains(e.logs.String(), loginPassword) {
		t.Error("the logs contain a password")
	}
}

// Without a refresh token that names a live session of this user, the server cannot
// tell which session is current, so every session ends (FR-2.5).
func TestChangePasswordWithoutCurrentSessionRevokesAll(t *testing.T) {
	tests := []struct {
		name    string
		refresh func(e *env) string
	}{
		{"no refresh token", func(*env) string { return "" }},
		{"unknown refresh token", func(*env) string { tok, _ := newToken(); return tok }},
		{"another user's refresh token", func(e *env) string { return loggedIn(t, e, "grace@example.org") }},
		{"revoked refresh token", func(e *env) string {
			tok := loggedIn(t, e, "ada@example.org")
			if err := e.svc.Logout(context.Background(), tok); err != nil {
				t.Fatal(err)
			}
			return tok
		}},
		{"expired refresh token", func(e *env) string {
			tok := loggedIn(t, e, "ada@example.org")
			e.clock.t = e.clock.t.Add(RefreshTTL)
			return tok
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			ada := verifiedUser(t, e, "ada@example.org")
			verifiedUser(t, e, "grace@example.org")
			phone := loggedIn(t, e, "ada@example.org")
			refresh := tt.refresh(e)

			if err := changePassword(e, ada, refresh, loginPassword, newPassword); err != nil {
				t.Fatalf("ChangePassword() error = %v, want nil", err)
			}
			if e.store.session(t, phone).RevokedAt == nil {
				t.Error("a session of the user was not revoked")
			}
			for _, sess := range e.store.sessions {
				if sess.UserID != ada && sess.RevokedAt != nil {
					t.Error("another user's session was revoked")
				}
			}
		})
	}
}

func TestChangePasswordRefused(t *testing.T) {
	long := strings.Repeat("x", MaxPasswordLength+1)
	tests := []struct {
		name          string
		current, next string
		want          error
		counted       bool // the attempt counts as a failed login
	}{
		{"wrong current password", "wrong-password", newPassword, domain.ErrWrongCurrentPassword, true},
		{"empty current password", "", newPassword, domain.ErrWrongCurrentPassword, true},
		{"new password too short", loginPassword, "1234567", domain.ErrPasswordTooShort, false},
		{"new password too long", loginPassword, long, domain.ErrPasswordTooLong, false},
		{"new password same as current", loginPassword, loginPassword, domain.ErrSamePassword, false},
		// The current password is checked before the new one is compared with it, so a
		// guess always counts, whatever the new password is.
		{"wrong current password equal to new", "guess-1234", "guess-1234", domain.ErrWrongCurrentPassword, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			ada := verifiedUser(t, e, "ada@example.org")
			laptop := loggedIn(t, e, "ada@example.org")
			phone := loggedIn(t, e, "ada@example.org")

			err := changePassword(e, ada, laptop, tt.current, tt.next)
			if !errors.Is(err, tt.want) {
				t.Fatalf("ChangePassword() error = %v, want %v", err, tt.want)
			}
			wantCount := 0
			if tt.counted {
				wantCount = 1
			}
			if got := e.attempts.failures["ada@example.org"].count; got != wantCount {
				t.Errorf("failed logins counted = %d, want %d", got, wantCount)
			}
			if e.store.session(t, phone).RevokedAt != nil || e.store.session(t, laptop).RevokedAt != nil {
				t.Error("a refused change revoked a session")
			}
			if err := login(e, "ada@example.org"); err != nil {
				t.Errorf("the old password stopped working after a refused change: %v", err)
			}
		})
	}
}

func TestChangePasswordAcceptsBoundaryLengths(t *testing.T) {
	for _, next := range []string{"12345678", strings.Repeat("ä", MaxPasswordLength)} {
		e := newEnv()
		ada := verifiedUser(t, e, "ada@example.org")
		if err := changePassword(e, ada, loggedIn(t, e, "ada@example.org"), loginPassword, next); err != nil {
			t.Errorf("ChangePassword() to a %d-character password: error = %v, want nil", len([]rune(next)), err)
		}
	}
}

// Wrong current passwords count toward the login lockout (FR-2.7), so this operation
// cannot be used to guess the password.
func TestChangePasswordLocksOnFifthWrongPassword(t *testing.T) {
	e := newEnv()
	ada := verifiedUser(t, e, "ada@example.org")
	laptop := loggedIn(t, e, "ada@example.org")
	failLogins(t, e, "ada@example.org", 2) // failures at login and here share one counter

	for i := range MaxFailedLogins - 2 {
		if err := changePassword(e, ada, laptop, "wrong-password", newPassword); !errors.Is(err, domain.ErrWrongCurrentPassword) {
			t.Fatalf("attempt %d: error = %v, want ErrWrongCurrentPassword", i+1, err)
		}
	}

	var locked *domain.LockedError
	if err := login(e, "ada@example.org"); !errors.As(err, &locked) {
		t.Fatalf("login after 5 failures: error = %v, want a LockedError", err)
	}
	verified := e.hasher.verified
	if err := changePassword(e, ada, laptop, loginPassword, newPassword); !errors.As(err, &locked) {
		t.Fatalf("change while locked: error = %v, want a LockedError", err)
	}
	if !locked.Until.Equal(start.Add(LockoutDuration)) || locked.RetryAfter != LockoutDuration {
		t.Errorf("lock until %v (retry after %v), want 15 minutes from the 5th failure", locked.Until, locked.RetryAfter)
	}
	if e.hasher.verified != verified {
		t.Error("the password was checked while the account was locked")
	}
	if e.attempts.failures["ada@example.org"].count != MaxFailedLogins {
		t.Error("an attempt while locked was counted")
	}

	e.clock.t = start.Add(LockoutDuration)
	if err := changePassword(e, ada, laptop, loginPassword, newPassword); err != nil {
		t.Errorf("change after the lock ended: error = %v, want nil", err)
	}
}

func TestChangePasswordClearsFailedLogins(t *testing.T) {
	e := newEnv()
	ada := verifiedUser(t, e, "ada@example.org")
	laptop := loggedIn(t, e, "ada@example.org")
	failLogins(t, e, "ada@example.org", MaxFailedLogins-1)

	if err := changePassword(e, ada, laptop, loginPassword, newPassword); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.attempts.failures["ada@example.org"]; ok {
		t.Error("the failed logins were not cleared")
	}
}

func TestChangePasswordWhenCounterFails(t *testing.T) {
	e := newEnv()
	ada := verifiedUser(t, e, "ada@example.org")
	laptop := loggedIn(t, e, "ada@example.org")
	e.attempts.err = errors.New("redis down")

	if err := changePassword(e, ada, laptop, "wrong-password", newPassword); !errors.Is(err, domain.ErrWrongCurrentPassword) {
		t.Errorf("wrong password: error = %v, want ErrWrongCurrentPassword", err)
	}
	if err := changePassword(e, ada, laptop, loginPassword, newPassword); err != nil {
		t.Errorf("right password: error = %v, want nil: the counter must not fail a change", err)
	}
	if !strings.Contains(e.logs.String(), "login lockout unavailable") {
		t.Errorf("the counter failure was not logged: %s", e.logs.String())
	}
}

func TestChangePasswordRollsBackOnFailure(t *testing.T) {
	e := newEnv()
	ada := verifiedUser(t, e, "ada@example.org")
	laptop := loggedIn(t, e, "ada@example.org")
	phone := loggedIn(t, e, "ada@example.org")

	e.store.failTx = errors.New("database down")
	if err := changePassword(e, ada, laptop, loginPassword, newPassword); err == nil {
		t.Fatal("ChangePassword() error = nil, want the store's error")
	}
	e.store.failTx = nil

	if e.store.session(t, phone).RevokedAt != nil {
		t.Error("a session was revoked by a failed change")
	}
	if err := login(e, "ada@example.org"); err != nil {
		t.Errorf("the old password stopped working after a failed change: %v", err)
	}
}

func TestChangePasswordPassesOnFailures(t *testing.T) {
	t.Run("user lookup fails", func(t *testing.T) {
		e := newEnv()
		ada := verifiedUser(t, e, "ada@example.org")
		e.store.failFind = errors.New("database down")
		err := changePassword(e, ada, "", loginPassword, newPassword)
		if err == nil || errors.Is(err, domain.ErrWrongCurrentPassword) {
			t.Errorf("error = %v, want the store's error", err)
		}
	})
	t.Run("user no longer exists", func(t *testing.T) {
		e := newEnv()
		if err := changePassword(e, "id-404", "", loginPassword, newPassword); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("error = %v, want ErrNotFound", err)
		}
	})
	t.Run("hasher fails", func(t *testing.T) {
		e := newEnv()
		ada := verifiedUser(t, e, "ada@example.org")
		e.hasher.err = errors.New("out of memory")
		if err := changePassword(e, ada, "", loginPassword, newPassword); err == nil {
			t.Error("error = nil, want the hasher's error")
		}
	})
}
