package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// registered registers a user and returns the token from the emailed link.
func registered(t *testing.T, e *env, email string) string {
	t.Helper()
	if _, err := e.svc.Register(context.Background(), RegisterInput{FullName: "Ada", Email: email, Password: "password1"}); err != nil {
		t.Fatal(err)
	}
	return tokenFromLink(e.mailer.sent[len(e.mailer.sent)-1].Link)
}

func TestVerifyEmail(t *testing.T) {
	otherToken := func() string { tok, _ := newToken(); return tok }
	tests := []struct {
		name    string
		after   time.Duration // time between registering and verifying
		token   func(registered string) string
		twice   bool // verify once before the checked call
		wantErr error
	}{
		{name: "fresh token", token: same, wantErr: nil},
		{name: "one second before expiry", after: VerificationTTL - time.Second, token: same, wantErr: nil},
		{name: "exactly at expiry", after: VerificationTTL, token: same, wantErr: domain.ErrTokenExpired},
		{name: "a day after expiry", after: 2 * VerificationTTL, token: same, wantErr: domain.ErrTokenExpired},
		{name: "used token", token: same, twice: true, wantErr: domain.ErrTokenUsed},
		{name: "unknown token", token: func(string) string { return otherToken() }, wantErr: domain.ErrTokenInvalid},
		{name: "malformed token", token: func(string) string { return "not-a-token" }, wantErr: domain.ErrTokenInvalid},
		{name: "empty token", token: func(string) string { return "" }, wantErr: domain.ErrTokenInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			token := registered(t, e, "ada@example.org")
			if tt.twice {
				if err := e.svc.VerifyEmail(context.Background(), token); err != nil {
					t.Fatal(err)
				}
			}
			e.clock.t = start.Add(tt.after)

			err := e.svc.VerifyEmail(context.Background(), tt.token(token))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("VerifyEmail() error = %v, want %v", err, tt.wantErr)
			}

			u := e.store.users["ada@example.org"]
			switch {
			case tt.wantErr == nil && (u.EmailVerifiedAt == nil || !u.EmailVerifiedAt.Equal(e.clock.t)):
				t.Errorf("email_verified_at = %v, want %v", u.EmailVerifiedAt, e.clock.t)
			case tt.wantErr != nil && !tt.twice && u.Verified():
				t.Error("a refused token verified the user")
			}
		})
	}
}

func same(s string) string { return s }

func TestVerifyEmailMarksTokenUsed(t *testing.T) {
	e := newEnv()
	token := registered(t, e, "ada@example.org")
	if err := e.svc.VerifyEmail(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	u := e.store.users["ada@example.org"]
	for _, tok := range e.store.tokensFor(u.ID) {
		if tok.UsedAt == nil || !tok.UsedAt.Equal(start) {
			t.Errorf("used_at = %v, want %v", tok.UsedAt, start)
		}
	}
}
