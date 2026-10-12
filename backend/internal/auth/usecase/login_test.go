package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

const loginPassword = "password1" // the password registered() uses

// verifiedUser registers and verifies email, and returns the user's ID.
func verifiedUser(t *testing.T, e *env, email string) string {
	t.Helper()
	if err := e.svc.VerifyEmail(context.Background(), registered(t, e, email)); err != nil {
		t.Fatal(err)
	}
	u, err := e.store.FindUserByEmail(context.Background(), email)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func TestLoginSuccess(t *testing.T) {
	e := newEnv()
	id := verifiedUser(t, e, "ada@example.org")
	e.clock.t = start.Add(time.Hour)

	res, err := e.svc.Login(context.Background(), LoginInput{Email: "  ADA@example.org ", Password: loginPassword})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if res.User.ID != id || res.User.Email != "ada@example.org" || res.User.FullName != "Ada" {
		t.Errorf("User = %+v, want the registered user", res.User)
	}
	if res.AccessToken != "access-for-"+id {
		t.Errorf("AccessToken = %q, want the signer's token for the user", res.AccessToken)
	}
	if want := e.clock.t.Add(15 * time.Minute); !res.AccessExpiresAt.Equal(want) {
		t.Errorf("AccessExpiresAt = %v, want %v", res.AccessExpiresAt, want)
	}
	if !wellFormedToken(res.RefreshToken) {
		t.Errorf("RefreshToken %q is not 32 random bytes in base64url", res.RefreshToken)
	}
	if want := e.clock.t.Add(RefreshTTL); !res.RefreshExpiresAt.Equal(want) {
		t.Errorf("RefreshExpiresAt = %v, want %v (30 days)", res.RefreshExpiresAt, want)
	}

	if len(e.store.sessions) != 1 {
		t.Fatalf("sessions stored = %d, want 1", len(e.store.sessions))
	}
	s := e.store.sessions[0]
	if s.UserID != id || !s.ExpiresAt.Equal(res.RefreshExpiresAt) {
		t.Errorf("session = %+v, want the user's, expiring with the refresh token", s)
	}
	if s.TokenHash == res.RefreshToken || s.TokenHash != hashToken(res.RefreshToken) {
		t.Error("the session must store the SHA-256 of the refresh token, never the token")
	}

	// Each login is a new session with a new refresh token.
	res2, err := e.svc.Login(context.Background(), LoginInput{Email: "ada@example.org", Password: loginPassword})
	if err != nil {
		t.Fatal(err)
	}
	if len(e.store.sessions) != 2 || res2.RefreshToken == res.RefreshToken {
		t.Error("a second login must store a second session with another refresh token")
	}

	for _, secret := range []string{loginPassword, res.RefreshToken, res.AccessToken, s.TokenHash} {
		if strings.Contains(e.logs.String(), secret) {
			t.Errorf("logs contain a secret: %s", e.logs)
		}
	}
}

func TestLoginRefused(t *testing.T) {
	tests := []struct {
		name         string
		verified     bool
		email        string
		password     string
		wantErr      error
		wantVerified bool // a password hash was checked, so timing does not reveal the account
	}{
		{name: "wrong password", verified: true, email: "ada@example.org", password: "wrong-password", wantErr: domain.ErrInvalidCredentials, wantVerified: true},
		{name: "unknown email", verified: true, email: "nobody@example.org", password: loginPassword, wantErr: domain.ErrInvalidCredentials, wantVerified: true},
		{name: "unverified account", verified: false, email: "ada@example.org", password: loginPassword, wantErr: domain.ErrEmailNotVerified, wantVerified: true},
		{name: "unverified account with wrong password", verified: false, email: "ada@example.org", password: "wrong-password", wantErr: domain.ErrInvalidCredentials, wantVerified: true},
		{name: "empty password", verified: true, email: "ada@example.org", password: "", wantErr: domain.ErrInvalidCredentials, wantVerified: true},
		{name: "malformed email", verified: true, email: "not an email", password: loginPassword, wantErr: domain.ErrInvalidCredentials},
		{name: "password too long to be one", verified: true, email: "ada@example.org", password: strings.Repeat("p", MaxPasswordLength+1), wantErr: domain.ErrInvalidCredentials},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			if tt.verified {
				verifiedUser(t, e, "ada@example.org")
			} else {
				registered(t, e, "ada@example.org")
			}
			e.hasher.verified = 0

			_, err := e.svc.Login(context.Background(), LoginInput{Email: tt.email, Password: tt.password})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Login() error = %v, want %v", err, tt.wantErr)
			}
			if len(e.store.sessions) != 0 {
				t.Error("a refused login stored a session")
			}
			if got := e.hasher.verified > 0; got != tt.wantVerified {
				t.Errorf("password hash checked = %v, want %v", got, tt.wantVerified)
			}
			if tt.password != "" && strings.Contains(e.logs.String(), tt.password) {
				t.Errorf("logs contain the password: %s", e.logs)
			}
		})
	}
}

func TestLoginPassesOnFailures(t *testing.T) {
	boom := errors.New("database down")
	tests := []struct {
		name  string
		fail func(e *env)
	}{
		{"finding the user", func(e *env) { e.store.failFind = boom }},
		{"checking the password", func(e *env) { e.hasher.err = boom }},
		{"storing the session", func(e *env) { e.store.failTx = boom }},
		{"signing the access token", func(e *env) { e.svc.signer = fakeSigner{err: boom} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			verifiedUser(t, e, "ada@example.org")
			tt.fail(e)
			if _, err := e.svc.Login(context.Background(), LoginInput{Email: "ada@example.org", Password: loginPassword}); !errors.Is(err, boom) {
				t.Errorf("Login() error = %v, want it to wrap %v", err, boom)
			}
			if len(e.store.sessions) != 0 {
				t.Error("a failed login left a session")
			}
		})
	}
}

func TestCurrentUser(t *testing.T) {
	e := newEnv()
	id := verifiedUser(t, e, "ada@example.org")

	u, err := e.svc.CurrentUser(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != id || u.Email != "ada@example.org" || u.FullName != "Ada" {
		t.Errorf("CurrentUser() = %+v", u)
	}
	if _, err := e.svc.CurrentUser(context.Background(), "gone"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown user: error = %v, want ErrNotFound", err)
	}
}
