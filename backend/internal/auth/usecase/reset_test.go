package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

const newPassword = "new password 1"

// resetRequested asks for a reset link for email and returns the token it carries.
func resetRequested(t *testing.T, e *env, email string) string {
	t.Helper()
	before := len(e.mailer.resets)
	if err := e.svc.RequestPasswordReset(context.Background(), email); err != nil {
		t.Fatalf("RequestPasswordReset() error = %v", err)
	}
	if len(e.mailer.resets) != before+1 {
		t.Fatalf("reset emails sent = %d, want %d", len(e.mailer.resets), before+1)
	}
	return tokenFromLink(e.mailer.resets[before].Link)
}

func TestRequestPasswordResetUnknownEmail(t *testing.T) {
	e := newEnv()
	if err := e.svc.RequestPasswordReset(context.Background(), "nobody@example.org"); err != nil {
		t.Fatalf("RequestPasswordReset() error = %v, want nil, as for a registered address", err)
	}
	if len(e.mailer.resets) != 0 || len(e.store.tokens) != 0 {
		t.Error("sent an email or stored a token for an unknown address")
	}
}

func TestRequestPasswordResetMalformedEmail(t *testing.T) {
	e := newEnv()
	if err := e.svc.RequestPasswordReset(context.Background(), "not an address"); !errors.Is(err, domain.ErrInvalidEmail) {
		t.Fatalf("RequestPasswordReset() error = %v, want ErrInvalidEmail", err)
	}
}

func TestRequestPasswordResetSendsLink(t *testing.T) {
	for _, verified := range []bool{true, false} {
		name := map[bool]string{true: "verified account", false: "unverified account"}[verified]
		t.Run(name, func(t *testing.T) {
			e := newEnv()
			var userID string
			if verified {
				userID = verifiedUser(t, e, "ada@example.org")
			} else {
				registered(t, e, "ada@example.org")
				u, _ := e.store.FindUserByEmail(context.Background(), "ada@example.org")
				userID = u.ID
			}

			token := resetRequested(t, e, "  ADA@example.org ")
			msg := e.mailer.resets[0]
			if msg.To != "ada@example.org" || msg.FullName != "Ada" {
				t.Errorf("sent to %q (%q), want the registered address and name", msg.To, msg.FullName)
			}
			if !strings.HasPrefix(msg.Link, "https://rs.example/reset-password#token=") || !wellFormedToken(token) {
				t.Errorf("link = %q, want https://rs.example/reset-password#token=<token>", msg.Link)
			}

			var resets []fakeToken
			for _, tok := range e.store.tokensFor(userID) {
				if tok.purpose == domain.PurposePasswordReset {
					resets = append(resets, tok)
				}
			}
			if len(resets) != 1 {
				t.Fatalf("stored reset tokens = %d, want 1", len(resets))
			}
			if resets[0].hash != hashToken(token) {
				t.Error("the stored value is not the token's hash")
			}
			if !resets[0].ExpiresAt.Equal(start.Add(time.Hour)) {
				t.Errorf("expires at %v, want 1 hour after the request", resets[0].ExpiresAt)
			}
			if strings.Contains(e.logs.String(), token) {
				t.Error("the logs contain the token")
			}
		})
	}
}

func TestRequestPasswordResetFailuresLookTheSame(t *testing.T) {
	t.Run("store lookup fails", func(t *testing.T) {
		e := newEnv()
		e.store.failFind = errors.New("database down")
		if err := e.svc.RequestPasswordReset(context.Background(), "ada@example.org"); err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
	})
	t.Run("storing the token fails", func(t *testing.T) {
		e := newEnv()
		verifiedUser(t, e, "ada@example.org")
		e.store.failTx = errors.New("database down")
		if err := e.svc.RequestPasswordReset(context.Background(), "ada@example.org"); err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if len(e.mailer.resets) != 0 {
			t.Error("sent a link whose token was not stored")
		}
	})
	t.Run("sending fails", func(t *testing.T) {
		e := newEnv()
		verifiedUser(t, e, "ada@example.org")
		e.mailer.err = errors.New("smtp down")
		if err := e.svc.RequestPasswordReset(context.Background(), "ada@example.org"); err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if !strings.Contains(e.logs.String(), "send password reset email") {
			t.Errorf("the failure was not logged: %s", e.logs.String())
		}
	})
}

func TestRequestPasswordResetInvalidatesOldToken(t *testing.T) {
	e := newEnv()
	ctx := context.Background()
	verifiedUser(t, e, "ada@example.org")
	oldToken := resetRequested(t, e, "ada@example.org")
	newTok := resetRequested(t, e, "ada@example.org")
	if oldToken == newTok {
		t.Fatal("the new link has the old token")
	}

	if err := e.svc.ResetPassword(ctx, oldToken, newPassword); !errors.Is(err, domain.ErrTokenInvalid) {
		t.Errorf("old token: error = %v, want ErrTokenInvalid", err)
	}
	if err := e.svc.ResetPassword(ctx, newTok, newPassword); err != nil {
		t.Errorf("new token: error = %v, want nil", err)
	}
}

func TestResetPasswordWorksOnce(t *testing.T) {
	e := newEnv()
	ctx := context.Background()
	verifiedUser(t, e, "ada@example.org")
	token := resetRequested(t, e, "ada@example.org")

	if err := e.svc.ResetPassword(ctx, token, newPassword); err != nil {
		t.Fatalf("first use: error = %v, want nil", err)
	}
	if err := e.svc.ResetPassword(ctx, token, "another password"); !errors.Is(err, domain.ErrTokenUsed) {
		t.Fatalf("second use: error = %v, want ErrTokenUsed", err)
	}

	// The first reset's password is the one that counts.
	if _, err := e.svc.Login(ctx, LoginInput{Email: "ada@example.org", Password: newPassword}); err != nil {
		t.Errorf("login with the new password: error = %v, want nil", err)
	}
	if _, err := e.svc.Login(ctx, LoginInput{Email: "ada@example.org", Password: loginPassword}); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Errorf("login with the old password: error = %v, want ErrInvalidCredentials", err)
	}
	if strings.Contains(e.logs.String(), token) || strings.Contains(e.logs.String(), newPassword) {
		t.Error("the logs contain the token or the password")
	}
}

func TestResetPasswordExpiry(t *testing.T) {
	for _, tt := range []struct {
		name    string
		after   time.Duration
		wantErr error
	}{
		{"just before 1 hour", PasswordResetTTL - time.Second, nil},
		{"at 1 hour", PasswordResetTTL, domain.ErrTokenExpired},
		{"a day later", 24 * time.Hour, domain.ErrTokenExpired},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			verifiedUser(t, e, "ada@example.org")
			token := resetRequested(t, e, "ada@example.org")
			e.clock.t = start.Add(tt.after)
			if err := e.svc.ResetPassword(context.Background(), token, newPassword); !errors.Is(err, tt.wantErr) {
				t.Fatalf("ResetPassword() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestResetPasswordInvalidToken(t *testing.T) {
	e := newEnv()
	verificationToken := registered(t, e, "ada@example.org")
	unknown, _ := newToken()
	for _, tt := range []struct{ name, token string }{
		{"missing", ""},
		{"malformed", "not-a-token"},
		{"unknown", unknown},
		{"a verification token", verificationToken},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := e.svc.ResetPassword(context.Background(), tt.token, newPassword); !errors.Is(err, domain.ErrTokenInvalid) {
				t.Fatalf("ResetPassword() error = %v, want ErrTokenInvalid", err)
			}
		})
	}
}

func TestResetPasswordRefusesPassword(t *testing.T) {
	e := newEnv()
	ctx := context.Background()
	verifiedUser(t, e, "ada@example.org")
	token := resetRequested(t, e, "ada@example.org")
	for _, tt := range []struct {
		name, password string
		wantErr        error
	}{
		{"7 characters", "1234567", domain.ErrPasswordTooShort},
		{"empty", "", domain.ErrPasswordTooShort},
		{"129 characters", strings.Repeat("x", 129), domain.ErrPasswordTooLong},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := e.svc.ResetPassword(ctx, token, tt.password); !errors.Is(err, tt.wantErr) {
				t.Fatalf("ResetPassword() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
	// The refused attempts did not use the token.
	if err := e.svc.ResetPassword(ctx, token, "12345678"); err != nil {
		t.Fatalf("ResetPassword() with 8 characters after refusals: error = %v, want nil", err)
	}
}

func TestResetPasswordRevokesAllSessions(t *testing.T) {
	e := newEnv()
	ctx := context.Background()
	verifiedUser(t, e, "ada@example.org")
	verifiedUser(t, e, "grace@example.org")
	laptop := loggedIn(t, e, "ada@example.org")
	phone := loggedIn(t, e, "ada@example.org")
	other := loggedIn(t, e, "grace@example.org")

	e.clock.t = start.Add(time.Minute)
	if err := e.svc.ResetPassword(ctx, resetRequested(t, e, "ada@example.org"), newPassword); err != nil {
		t.Fatal(err)
	}

	for name, token := range map[string]string{"laptop": laptop, "phone": phone} {
		sess := e.store.session(t, token)
		if sess.RevokedAt == nil || !sess.RevokedAt.Equal(e.clock.t) {
			t.Errorf("%s session: revoked at %v, want the reset time", name, sess.RevokedAt)
		}
		if _, err := e.svc.Refresh(ctx, token); !errors.Is(err, domain.ErrSessionInvalid) {
			t.Errorf("%s refresh after reset: error = %v, want ErrSessionInvalid", name, err)
		}
	}
	if e.store.session(t, other).RevokedAt != nil {
		t.Error("another user's session was revoked")
	}
}

func TestResetPasswordVerifiesEmail(t *testing.T) {
	e := newEnv()
	ctx := context.Background()
	registered(t, e, "ada@example.org") // never verified
	if err := e.svc.ResetPassword(ctx, resetRequested(t, e, "ada@example.org"), newPassword); err != nil {
		t.Fatal(err)
	}
	u, _ := e.store.FindUserByEmail(ctx, "ada@example.org")
	if !u.Verified() {
		t.Error("the address is not verified after a reset")
	}
	if _, err := e.svc.Login(ctx, LoginInput{Email: "ada@example.org", Password: newPassword}); err != nil {
		t.Errorf("login after reset: error = %v, want nil", err)
	}
}

func TestResetPasswordClearsFailedLogins(t *testing.T) {
	e := newEnv()
	verifiedUser(t, e, "ada@example.org")
	failLogins(t, e, "ada@example.org", MaxFailedLogins-1)

	if err := e.svc.ResetPassword(context.Background(), resetRequested(t, e, "ada@example.org"), newPassword); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.attempts.failures["ada@example.org"]; ok {
		t.Fatal("the failed logins were not cleared")
	}
	// The count starts again: one more failure does not lock the account.
	failLogins(t, e, "ada@example.org", 1)
	if _, err := e.svc.Login(context.Background(), LoginInput{Email: "ada@example.org", Password: newPassword}); err != nil {
		t.Errorf("Login() error = %v, want nil", err)
	}
}

func TestResetPasswordWhenCounterFails(t *testing.T) {
	e := newEnv()
	verifiedUser(t, e, "ada@example.org")
	token := resetRequested(t, e, "ada@example.org")
	e.attempts.err = errors.New("redis down")

	if err := e.svc.ResetPassword(context.Background(), token, newPassword); err != nil {
		t.Fatalf("ResetPassword() error = %v, want nil: the counter must not fail a reset", err)
	}
	if !strings.Contains(e.logs.String(), "could not clear failed logins") {
		t.Errorf("the counter failure was not logged: %s", e.logs.String())
	}
	if _, err := e.svc.Login(context.Background(), LoginInput{Email: "ada@example.org", Password: newPassword}); err != nil {
		t.Errorf("login after reset: error = %v, want nil", err)
	}
}

func TestResetPasswordRollsBackOnFailure(t *testing.T) {
	e := newEnv()
	ctx := context.Background()
	verifiedUser(t, e, "ada@example.org")
	session := loggedIn(t, e, "ada@example.org")
	token := resetRequested(t, e, "ada@example.org")

	e.store.failTx = errors.New("database down")
	if err := e.svc.ResetPassword(ctx, token, newPassword); err == nil {
		t.Fatal("ResetPassword() error = nil, want the store's error")
	}
	e.store.failTx = nil

	if e.store.session(t, session).RevokedAt != nil {
		t.Error("the session was revoked by a failed reset")
	}
	if _, err := e.svc.Login(ctx, LoginInput{Email: "ada@example.org", Password: loginPassword}); err != nil {
		t.Errorf("the old password stopped working after a failed reset: %v", err)
	}
	if err := e.svc.ResetPassword(ctx, token, newPassword); err != nil {
		t.Errorf("the token was used by a failed reset: %v", err)
	}
}

func TestResetPasswordHasherFails(t *testing.T) {
	e := newEnv()
	verifiedUser(t, e, "ada@example.org")
	token := resetRequested(t, e, "ada@example.org")
	e.hasher.err = errors.New("out of memory")
	if err := e.svc.ResetPassword(context.Background(), token, newPassword); err == nil {
		t.Fatal("ResetPassword() error = nil, want the hasher's error")
	}
}
