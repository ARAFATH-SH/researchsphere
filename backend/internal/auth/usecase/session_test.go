package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// loggedIn logs a verified user in and returns the refresh token of the new session.
func loggedIn(t *testing.T, e *env, email string) string {
	t.Helper()
	res, err := e.svc.Login(context.Background(), LoginInput{Email: email, Password: loginPassword})
	if err != nil {
		t.Fatal(err)
	}
	return res.RefreshToken
}

// session returns the stored session whose current refresh token is token.
func (s *fakeStore) session(t *testing.T, token string) fakeSession {
	t.Helper()
	for _, sess := range s.sessions {
		if sess.TokenHash == hashToken(token) {
			return sess
		}
	}
	t.Fatalf("no session has the refresh token")
	return fakeSession{}
}

func TestRefreshRotatesTheToken(t *testing.T) {
	e := newEnv()
	id := verifiedUser(t, e, "ada@example.org")
	old := loggedIn(t, e, "ada@example.org")
	loginExpiry := e.store.session(t, old).ExpiresAt
	e.clock.t = start.Add(20 * time.Minute)

	res, err := e.svc.Refresh(context.Background(), old)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if res.AccessToken != "access-for-"+id+"-as-researcher" {
		t.Errorf("AccessToken = %q, want the signer's token for the user", res.AccessToken)
	}
	if !res.IssuedAt.Equal(e.clock.t) || !res.AccessExpiresAt.Equal(e.clock.t.Add(15*time.Minute)) {
		t.Errorf("IssuedAt, AccessExpiresAt = %v, %v; want now and 15 minutes later", res.IssuedAt, res.AccessExpiresAt)
	}
	if !wellFormedToken(res.RefreshToken) || res.RefreshToken == old {
		t.Errorf("RefreshToken %q must be a new 32-byte token", res.RefreshToken)
	}
	if !res.RefreshExpiresAt.Equal(loginExpiry) {
		t.Errorf("RefreshExpiresAt = %v, want the expiry set at login %v", res.RefreshExpiresAt, loginExpiry)
	}
	if len(e.store.sessions) != 1 {
		t.Fatalf("sessions = %d, want the same one session", len(e.store.sessions))
	}
	if got := e.store.sessions[0].TokenHash; got != hashToken(res.RefreshToken) {
		t.Error("the session must now hold the SHA-256 of the new refresh token")
	}

	// The old token stops working; the new one works.
	if _, err := e.svc.Refresh(context.Background(), old); !errors.Is(err, domain.ErrSessionInvalid) {
		t.Errorf("Refresh(old token) error = %v, want %v", err, domain.ErrSessionInvalid)
	}

	for _, secret := range []string{old, res.RefreshToken, hashToken(old), hashToken(res.RefreshToken)} {
		if strings.Contains(e.logs.String(), secret) {
			t.Errorf("logs contain a secret: %s", e.logs)
		}
	}
}

func TestRefreshWithTheNewTokenWorks(t *testing.T) {
	e := newEnv()
	verifiedUser(t, e, "ada@example.org")
	token := loggedIn(t, e, "ada@example.org")

	for i := range 3 {
		res, err := e.svc.Refresh(context.Background(), token)
		if err != nil {
			t.Fatalf("refresh %d: error = %v", i+1, err)
		}
		token = res.RefreshToken
	}
}

func TestRefreshRefused(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, e *env, token string) string // returns the token to send
	}{
		{"expired session", func(_ *testing.T, e *env, token string) string {
			e.clock.t = start.Add(RefreshTTL)
			return token
		}},
		{"revoked session", func(t *testing.T, e *env, token string) string {
			if err := e.svc.Logout(context.Background(), token); err != nil {
				t.Fatal(err)
			}
			return token
		}},
		{"unknown token", func(*testing.T, *env, string) string { tok, _ := newToken(); return tok }},
		{"empty token", func(*testing.T, *env, string) string { return "" }},
		{"malformed token", func(*testing.T, *env, string) string { return "not-a-token" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			verifiedUser(t, e, "ada@example.org")
			token := tt.setup(t, e, loggedIn(t, e, "ada@example.org"))
			hashBefore := e.store.sessions[0].TokenHash

			if _, err := e.svc.Refresh(context.Background(), token); !errors.Is(err, domain.ErrSessionInvalid) {
				t.Fatalf("Refresh() error = %v, want %v", err, domain.ErrSessionInvalid)
			}
			if e.store.sessions[0].TokenHash != hashBefore || len(e.store.retired) != 0 {
				t.Error("a refused refresh rotated the session")
			}
		})
	}
}

func TestRefreshWithAReusedTokenEndsTheSession(t *testing.T) {
	e := newEnv()
	verifiedUser(t, e, "ada@example.org")
	stolen := loggedIn(t, e, "ada@example.org")
	other := loggedIn(t, e, "ada@example.org") // another device
	res, err := e.svc.Refresh(context.Background(), stolen)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := e.svc.Refresh(context.Background(), stolen); !errors.Is(err, domain.ErrSessionInvalid) {
		t.Fatalf("Refresh(reused token) error = %v, want %v", err, domain.ErrSessionInvalid)
	}
	if e.store.session(t, res.RefreshToken).RevokedAt == nil {
		t.Fatal("reusing a retired token must revoke its session")
	}
	if _, err := e.svc.Refresh(context.Background(), res.RefreshToken); !errors.Is(err, domain.ErrSessionInvalid) {
		t.Errorf("Refresh(newest token after reuse) error = %v, want %v", err, domain.ErrSessionInvalid)
	}
	if _, err := e.svc.Refresh(context.Background(), other); err != nil {
		t.Errorf("another device's session must survive the reuse; Refresh() error = %v", err)
	}
	if !strings.Contains(e.logs.String(), "refresh token reused") {
		t.Errorf("logs = %s, want a warning about the reuse", e.logs)
	}
}

func TestRefreshPassesOnFailures(t *testing.T) {
	boom := errors.New("database down")
	tests := []struct {
		name string
		fail func(e *env)
	}{
		{"signing the access token", func(e *env) { e.svc.signer = fakeSigner{err: boom} }},
		{"rotating the session", func(e *env) { e.store.failTx = boom }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			verifiedUser(t, e, "ada@example.org")
			token := loggedIn(t, e, "ada@example.org")
			tt.fail(e)

			if _, err := e.svc.Refresh(context.Background(), token); !errors.Is(err, boom) {
				t.Fatalf("Refresh() error = %v, want it to wrap %v", err, boom)
			}
			if e.store.sessions[0].TokenHash != hashToken(token) || len(e.store.retired) != 0 {
				t.Error("a failed refresh must leave the session as it was")
			}
		})
	}
}

func TestLogoutThenRefreshIsRefused(t *testing.T) {
	e := newEnv()
	id := verifiedUser(t, e, "ada@example.org")
	token := loggedIn(t, e, "ada@example.org")
	e.clock.t = start.Add(time.Hour)

	if err := e.svc.Logout(context.Background(), token); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if got := e.store.sessions[0].RevokedAt; got == nil || !got.Equal(e.clock.t) {
		t.Errorf("RevokedAt = %v, want %v", got, e.clock.t)
	}
	if _, err := e.svc.Refresh(context.Background(), token); !errors.Is(err, domain.ErrSessionInvalid) {
		t.Errorf("Refresh() after logout error = %v, want %v", err, domain.ErrSessionInvalid)
	}
	if !strings.Contains(e.logs.String(), id) || strings.Contains(e.logs.String(), token) {
		t.Errorf("logs = %s, want the logout logged without the token", e.logs)
	}
}

func TestLogoutEndsOnlyThatDevice(t *testing.T) {
	e := newEnv()
	verifiedUser(t, e, "ada@example.org")
	laptop := loggedIn(t, e, "ada@example.org")
	phone := loggedIn(t, e, "ada@example.org")

	if err := e.svc.Logout(context.Background(), laptop); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Refresh(context.Background(), phone); err != nil {
		t.Errorf("Refresh() on the other device error = %v, want nil", err)
	}
	if _, err := e.svc.Refresh(context.Background(), laptop); !errors.Is(err, domain.ErrSessionInvalid) {
		t.Errorf("Refresh() on the logged-out device error = %v, want %v", err, domain.ErrSessionInvalid)
	}
}

func TestLogoutWithoutASession(t *testing.T) {
	tests := []struct {
		name  string
		token func(t *testing.T, e *env) string
	}{
		{"empty token", func(*testing.T, *env) string { return "" }},
		{"malformed token", func(*testing.T, *env) string { return "not-a-token" }},
		{"unknown token", func(*testing.T, *env) string { tok, _ := newToken(); return tok }},
		{"already logged out", func(t *testing.T, e *env) string {
			tok := loggedIn(t, e, "ada@example.org")
			if err := e.svc.Logout(context.Background(), tok); err != nil {
				t.Fatal(err)
			}
			return tok
		}},
		{"retired token", func(t *testing.T, e *env) string {
			tok := loggedIn(t, e, "ada@example.org")
			if _, err := e.svc.Refresh(context.Background(), tok); err != nil {
				t.Fatal(err)
			}
			return tok
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			verifiedUser(t, e, "ada@example.org")
			token := tt.token(t, e)
			before := make([]fakeSession, len(e.store.sessions))
			copy(before, e.store.sessions)
			e.clock.t = start.Add(time.Hour)

			if err := e.svc.Logout(context.Background(), token); err != nil {
				t.Fatalf("Logout() error = %v, want nil", err)
			}
			for i, sess := range e.store.sessions {
				if sess.RevokedAt != before[i].RevokedAt {
					t.Error("a logout without a current session changed a session")
				}
			}
		})
	}
}

func TestLogoutPassesOnFailure(t *testing.T) {
	boom := errors.New("database down")
	e := newEnv()
	verifiedUser(t, e, "ada@example.org")
	token := loggedIn(t, e, "ada@example.org")
	e.store.failTx = boom

	if err := e.svc.Logout(context.Background(), token); !errors.Is(err, boom) {
		t.Errorf("Logout() error = %v, want it to wrap %v", err, boom)
	}
}

// endedSessions records who the listener heard about.
type endedSessions struct{ users []string }

func (l *endedSessions) SessionsEnded(_ context.Context, userID string) {
	l.users = append(l.users, userID)
}

// Open sockets close whenever a session ends. Only the session that was found counts: a
// logout that matches nothing tells nobody.
func TestSessionListener(t *testing.T) {
	e := newEnv()
	l := &endedSessions{}
	e.svc.cfg.Sessions = l
	id := verifiedUser(t, e, "ada@example.org")
	token := loggedIn(t, e, "ada@example.org")

	if err := e.svc.Logout(context.Background(), strings.Repeat("a", 43)); err != nil {
		t.Fatal(err)
	}
	if len(l.users) != 0 {
		t.Fatalf("heard %v for an unknown session", l.users)
	}
	if err := e.svc.Logout(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if len(l.users) != 1 || l.users[0] != id {
		t.Errorf("heard %v, want [%s]", l.users, id)
	}
}
