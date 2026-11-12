package usecase

import (
	"context"
	"testing"
	"time"
)

// setRole changes the stored platform role of the user with email, as an UPDATE of
// users.role would.
func setRole(t *testing.T, e *env, email, role string) {
	t.Helper()
	u, ok := e.store.users[email]
	if !ok {
		t.Fatalf("no user %s", email)
	}
	u.Role = role
	e.store.users[email] = u
}

// Login signs the user's stored platform role into the access token (SRS 2.3).
func TestLoginSignsTheRole(t *testing.T) {
	e := newEnv()
	id := verifiedUser(t, e, "ada@example.org")
	setRole(t, e, "ada@example.org", "admin")

	res, err := e.svc.Login(context.Background(), LoginInput{Email: "ada@example.org", Password: loginPassword})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if want := "access-for-" + id + "-as-admin"; res.AccessToken != want {
		t.Errorf("AccessToken = %q, want %q", res.AccessToken, want)
	}
}

// A role change takes effect at the next refresh: the new access token carries the
// role stored now, not the one signed at login (docs/decisions.md).
func TestRefreshSignsTheCurrentRole(t *testing.T) {
	e := newEnv()
	id := verifiedUser(t, e, "ada@example.org")
	setRole(t, e, "ada@example.org", "admin")
	refresh := loggedIn(t, e, "ada@example.org")

	setRole(t, e, "ada@example.org", "researcher")
	e.clock.t = e.clock.t.Add(20 * time.Minute)
	res, err := e.svc.Refresh(context.Background(), refresh)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if want := "access-for-" + id + "-as-researcher"; res.AccessToken != want {
		t.Errorf("AccessToken = %q, want %q", res.AccessToken, want)
	}
}

// FR-17.3: the access token carries whether the email address is verified, read from the
// user at every refresh. Login refuses unverified accounts (FR-1.4), so this can only happen
// to a session whose user lost the verified state; ORCID sign-up (8A.3) will make it common.
func TestRefreshSignsTheCurrentVerifiedState(t *testing.T) {
	e := newEnv()
	id := verifiedUser(t, e, "ada@example.org")
	refresh := loggedIn(t, e, "ada@example.org")

	u := e.store.users["ada@example.org"]
	u.EmailVerifiedAt = nil
	e.store.users["ada@example.org"] = u
	e.clock.t = e.clock.t.Add(20 * time.Minute)
	res, err := e.svc.Refresh(context.Background(), refresh)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if want := "access-for-" + id + "-as-researcher-unverified"; res.AccessToken != want {
		t.Errorf("AccessToken = %q, want %q", res.AccessToken, want)
	}
}
