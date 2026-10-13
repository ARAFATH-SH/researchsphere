package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// failLogins makes n logins with a wrong password and checks that each is refused as
// invalid credentials.
func failLogins(t *testing.T, e *env, email string, n int) {
	t.Helper()
	for i := range n {
		_, err := e.svc.Login(context.Background(), LoginInput{Email: email, Password: "wrong-password"})
		if !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatalf("failure %d: Login() error = %v, want ErrInvalidCredentials", i+1, err)
		}
	}
}

func login(e *env, email string) error {
	_, err := e.svc.Login(context.Background(), LoginInput{Email: email, Password: loginPassword})
	return err
}

func TestLoginAllowedAfterFourFailures(t *testing.T) {
	e := newEnv()
	verifiedUser(t, e, "ada@example.org")

	failLogins(t, e, "ada@example.org", MaxFailedLogins-1)
	if err := login(e, "ada@example.org"); err != nil {
		t.Fatalf("Login() after %d failures: error = %v, want nil", MaxFailedLogins-1, err)
	}
}

func TestLoginLocksOnFifthFailure(t *testing.T) {
	e := newEnv()
	verifiedUser(t, e, "ada@example.org")
	e.clock.t = start.Add(time.Hour)

	failLogins(t, e, "ada@example.org", MaxFailedLogins)

	// The 5th failure itself is answered as a wrong password; the lock starts with it.
	want := e.clock.t.Add(LockoutDuration)
	if got := e.attempts.locks["ada@example.org"]; !got.Equal(want) {
		t.Fatalf("lock ends at %v, want %v (15 minutes after the 5th failure)", got, want)
	}
}

func TestLoginRefusedWhileLocked(t *testing.T) {
	e := newEnv()
	verifiedUser(t, e, "ada@example.org")
	e.clock.t = start.Add(time.Hour)
	failLogins(t, e, "ada@example.org", MaxFailedLogins)
	lockedAt := e.clock.t
	e.hasher.verified = 0

	for _, tt := range []struct {
		name, email, password string
	}{
		{"correct password", "ada@example.org", loginPassword},
		{"correct password, other letter case", "  ADA@Example.org ", loginPassword},
		{"wrong password", "ada@example.org", "wrong-password"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e.clock.t = lockedAt.Add(LockoutDuration - time.Second)
			_, err := e.svc.Login(context.Background(), LoginInput{Email: tt.email, Password: tt.password})
			if !errors.Is(err, domain.ErrAccountLocked) {
				t.Fatalf("Login() error = %v, want ErrAccountLocked", err)
			}
			var locked *domain.LockedError
			if !errors.As(err, &locked) || !locked.Until.Equal(lockedAt.Add(LockoutDuration)) || locked.RetryAfter != time.Second {
				t.Errorf("error = %#v, want a LockedError ending 15 minutes after the 5th failure, 1s from now", err)
			}
		})
	}
	if len(e.store.sessions) != 0 {
		t.Error("a login while locked stored a session")
	}
	if e.hasher.verified != 0 {
		t.Errorf("password checked %d times while locked, want 0", e.hasher.verified)
	}
	// Attempts while locked do not move the end of the lock.
	if got, want := e.attempts.locks["ada@example.org"], lockedAt.Add(LockoutDuration); !got.Equal(want) {
		t.Errorf("lock ends at %v, want %v", got, want)
	}
}

func TestLoginLockEndsAfterFifteenMinutes(t *testing.T) {
	e := newEnv()
	verifiedUser(t, e, "ada@example.org")
	e.clock.t = start.Add(time.Hour)
	failLogins(t, e, "ada@example.org", MaxFailedLogins)

	e.clock.t = e.clock.t.Add(LockoutDuration)
	if err := login(e, "ada@example.org"); err != nil {
		t.Fatalf("Login() 15 minutes after the lock: error = %v, want nil", err)
	}
}

func TestLoginSuccessResetsFailures(t *testing.T) {
	e := newEnv()
	verifiedUser(t, e, "ada@example.org")

	failLogins(t, e, "ada@example.org", MaxFailedLogins-1)
	if err := login(e, "ada@example.org"); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.attempts.failures["ada@example.org"]; ok {
		t.Error("a successful login kept the failure count")
	}
	// Without the reset, the next failure would be the 5th and lock.
	failLogins(t, e, "ada@example.org", MaxFailedLogins-1)
	if err := login(e, "ada@example.org"); err != nil {
		t.Fatalf("Login() after a reset and %d failures: error = %v, want nil", MaxFailedLogins-1, err)
	}
}

func TestLoginLocksUnknownEmail(t *testing.T) {
	e := newEnv()

	failLogins(t, e, "nobody@example.org", MaxFailedLogins)
	_, err := e.svc.Login(context.Background(), LoginInput{Email: "nobody@example.org", Password: loginPassword})
	if !errors.Is(err, domain.ErrAccountLocked) {
		t.Fatalf("Login() for an unknown email after %d failures: error = %v, want ErrAccountLocked", MaxFailedLogins, err)
	}
}

func TestLoginFailuresWindow(t *testing.T) {
	e := newEnv()
	verifiedUser(t, e, "ada@example.org")
	e.clock.t = start.Add(time.Hour)

	// 4 failures, then the window ends: the 5th failure starts a new window and does not lock.
	failLogins(t, e, "ada@example.org", MaxFailedLogins-1)
	e.clock.t = e.clock.t.Add(LockoutWindow)
	failLogins(t, e, "ada@example.org", 1)
	if err := login(e, "ada@example.org"); err != nil {
		t.Fatalf("Login() error = %v, want nil: failures in two windows must not lock", err)
	}
}

func TestLoginLockoutCountsOnlyWrongCredentials(t *testing.T) {
	e := newEnv()
	registered(t, e, "ada@example.org") // not verified

	// The right password on an unverified account is neither a failure nor a success.
	for range MaxFailedLogins {
		if err := login(e, "ada@example.org"); !errors.Is(err, domain.ErrEmailNotVerified) {
			t.Fatalf("Login() error = %v, want ErrEmailNotVerified", err)
		}
	}
	failLogins(t, e, "ada@example.org", MaxFailedLogins-1)
	if err := login(e, "ada@example.org"); !errors.Is(err, domain.ErrEmailNotVerified) {
		t.Fatalf("Login() error = %v, want ErrEmailNotVerified", err)
	}
	if got := e.attempts.failures["ada@example.org"].count; got != MaxFailedLogins-1 {
		t.Errorf("failures = %d, want %d: a right password on an unverified account must not reset them", got, MaxFailedLogins-1)
	}

	// A malformed address names no account, so it has no counter.
	failLogins(t, e, "not an email", MaxFailedLogins)
	if len(e.attempts.failures) != 1 || len(e.attempts.locks) != 0 {
		t.Errorf("failures = %v, locks = %v; a malformed address must not be counted", e.attempts.failures, e.attempts.locks)
	}
}

func TestLoginAllowedWhenCounterFails(t *testing.T) {
	e := newEnv()
	verifiedUser(t, e, "ada@example.org")
	e.attempts.err = errors.New("redis: connection refused")

	logins := []struct {
		name     string
		password string
		wantErr  error
	}{
		{"wrong password", "wrong-password", domain.ErrInvalidCredentials},
		{"right password", loginPassword, nil},
	}
	for range MaxFailedLogins {
		logins = append(logins, logins[0])
	}
	logins = append(logins, logins[1])

	for _, l := range logins {
		e.logs.Reset()
		e.attempts.calls = 0
		_, err := e.svc.Login(context.Background(), LoginInput{Email: "ada@example.org", Password: l.password})
		if !errors.Is(err, l.wantErr) {
			t.Fatalf("%s with the counter down: Login() error = %v, want %v", l.name, err, l.wantErr)
		}
		// The first counter error turns the counter off for the rest of the login.
		if e.attempts.calls != 1 {
			t.Errorf("%s: counter calls = %d, want 1", l.name, e.attempts.calls)
		}
		logs := e.logs.String()
		if n := strings.Count(logs, "level=WARN"); n != 1 || !strings.Contains(logs, "redis: connection refused") {
			t.Errorf("%s: logs = %s, want the counter's error logged once at WARN", l.name, logs)
		}
		if strings.Contains(logs, "level=ERROR") {
			t.Errorf("%s: logs = %s, want no ERROR", l.name, logs)
		}
		if strings.Contains(logs, loginPassword) || strings.Contains(logs, "ada@example.org") {
			t.Errorf("%s: logs contain the password or the address: %s", l.name, logs)
		}
	}
}
