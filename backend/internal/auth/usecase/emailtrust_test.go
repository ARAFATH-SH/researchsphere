package usecase

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// withConfig rebuilds e's service with cfg (the base URL stays the one newEnv sets).
func withConfig(e *env, cfg Config) *env {
	cfg.AppBaseURL = "https://rs.example/"
	e.svc = New(e.store, e.hasher, e.mailer, fakeSigner{}, e.attempts, cfg, e.clock.now, slog.New(slog.NewTextHandler(e.logs, nil)))
	return e
}

func register(t *testing.T, e *env, email string) error {
	t.Helper()
	_, err := e.svc.Register(context.Background(), RegisterInput{FullName: "Ada Lovelace", Email: email, Password: "correct horse battery"})
	return err
}

// FR-17.2: a domain the checker refuses gets ErrEmailDomainNotAllowed, and nothing is
// stored or sent. The checker sees only the domain, never the local part.
func TestRegisterChecksTheEmailDomain(t *testing.T) {
	domains := &fakeDomains{refuse: map[string]bool{"mailinator.com": true, "nomail.example": true}}
	e := withConfig(newEnv(), Config{EmailDomains: domains})

	for _, email := range []string{"ada@mailinator.com", "Ada@NoMail.Example"} {
		if err := register(t, e, email); !errors.Is(err, domain.ErrEmailDomainNotAllowed) {
			t.Errorf("Register(%s) error = %v, want ErrEmailDomainNotAllowed", email, err)
		}
	}
	if len(e.store.users) != 0 || len(e.mailer.sent) != 0 {
		t.Errorf("stored %d users, sent %d emails; want none", len(e.store.users), len(e.mailer.sent))
	}
	if err := register(t, e, "ada@uni.example"); err != nil {
		t.Fatalf("Register(allowed domain) error = %v", err)
	}
	if want := []string{"mailinator.com", "nomail.example", "uni.example"}; !slices.Equal(domains.asked, want) {
		t.Errorf("checker asked %v, want %v", domains.asked, want)
	}
}

// FR-17.9: an address at a test domain skips the check. The API passes no test domains in
// production (config.Load), where the same address is checked like any other.
func TestRegisterTestDomains(t *testing.T) {
	refuseAll := func() *fakeDomains {
		return &fakeDomains{refuse: map[string]bool{"example.com": true, "compare.researchsphere.local": true}}
	}

	domains := refuseAll()
	dev := withConfig(newEnv(), Config{EmailDomains: domains, TestEmailDomains: []string{"example.com", "compare.researchsphere.local"}})
	for _, email := range []string{"test1@example.com", "Compare-Alpha@Compare.ResearchSphere.local"} {
		if err := register(t, dev, email); err != nil {
			t.Errorf("development: Register(%s) error = %v, want nil", email, err)
		}
	}
	if len(domains.asked) != 0 {
		t.Errorf("development: checker asked %v, want nothing", domains.asked)
	}

	prod := withConfig(newEnv(), Config{EmailDomains: refuseAll()}) // production: no test domains
	if err := register(t, prod, "test1@example.com"); !errors.Is(err, domain.ErrEmailDomainNotAllowed) {
		t.Errorf("production: Register(test1@example.com) error = %v, want ErrEmailDomainNotAllowed", err)
	}
}

// purgeEnv makes accounts that signed up at the given ages, verified or not.
type account struct {
	email    string
	age      time.Duration
	verified bool
}

func purgeEnv(t *testing.T, cfg Config, accounts []account) (*env, map[string]string) {
	t.Helper()
	cfg.UnverifiedAccountAge = 7 * 24 * time.Hour
	e := withConfig(newEnv(), cfg)
	ids := map[string]string{}
	for _, a := range accounts {
		if err := register(t, e, a.email); err != nil {
			t.Fatal(err)
		}
		key := strings.ToLower(a.email)
		u := e.store.users[key]
		u.CreatedAt = e.clock.now().Add(-a.age)
		if a.verified {
			at := u.CreatedAt.Add(time.Minute)
			u.EmailVerifiedAt = &at
		}
		e.store.users[key] = u
		ids[a.email] = u.ID
	}
	return e, ids
}

func remaining(e *env) []string {
	var out []string
	for k := range e.store.users {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// FR-17.4: only accounts still unverified 7 days after sign-up are deleted; verified ones,
// newer ones and, outside production, test-domain ones stay.
func TestPurgeUnverified(t *testing.T) {
	accounts := []account{
		{"old-unverified@uni.example", 8 * 24 * time.Hour, false},
		{"older-unverified@uni.example", 30 * 24 * time.Hour, false},
		{"new-unverified@uni.example", 6 * 24 * time.Hour, false},
		{"old-verified@uni.example", 30 * 24 * time.Hour, true},
		{"test1@example.com", 30 * 24 * time.Hour, false},
	}

	t.Run("development keeps test domains", func(t *testing.T) {
		e, ids := purgeEnv(t, Config{TestEmailDomains: []string{"example.com"}}, accounts)
		n, err := e.svc.PurgeUnverified(context.Background())
		if err != nil || n != 2 {
			t.Fatalf("PurgeUnverified = %d, %v; want 2, nil", n, err)
		}
		want := []string{"new-unverified@uni.example", "old-verified@uni.example", "test1@example.com"}
		if got := remaining(e); !slices.Equal(got, want) {
			t.Errorf("remaining = %v, want %v", got, want)
		}
		logs := e.logs.String()
		if !strings.Contains(logs, "unverified accounts purged") || !strings.Contains(logs, ids["old-unverified@uni.example"]) {
			t.Errorf("log lacks the purge or the deleted ID: %s", logs)
		}
		if strings.Contains(logs, "old-unverified@uni.example") {
			t.Errorf("log contains an email address: %s", logs)
		}

		// A second pass finds nothing and logs nothing new.
		before := e.logs.Len()
		if n, err := e.svc.PurgeUnverified(context.Background()); err != nil || n != 0 {
			t.Errorf("second pass = %d, %v; want 0", n, err)
		}
		if e.logs.Len() != before {
			t.Errorf("second pass logged: %s", e.logs.String()[before:])
		}
	})

	t.Run("production deletes test-domain accounts too", func(t *testing.T) {
		e, _ := purgeEnv(t, Config{}, accounts)
		if n, err := e.svc.PurgeUnverified(context.Background()); err != nil || n != 3 {
			t.Fatalf("PurgeUnverified = %d, %v; want 3", n, err)
		}
		want := []string{"new-unverified@uni.example", "old-verified@uni.example"}
		if got := remaining(e); !slices.Equal(got, want) {
			t.Errorf("remaining = %v, want %v", got, want)
		}
	})

	t.Run("deleted through the account deletion: sessions end, publications go", func(t *testing.T) {
		listener := &endedSessions{}
		e, ids := purgeEnv(t, Config{Sessions: listener}, accounts[:1])
		id := ids["old-unverified@uni.example"]
		e.store.pubs["pub-1"] = []string{id}
		if _, err := e.svc.PurgeUnverified(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, ok := e.store.pubs["pub-1"]; ok {
			t.Error("the account's only-author publication was kept")
		}
		if !slices.Contains(listener.users, id) {
			t.Errorf("sessions ended for %v, want %s", listener.users, id)
		}
	})

	t.Run("an account that owns a shared project is kept and logged", func(t *testing.T) {
		e, ids := purgeEnv(t, Config{}, accounts[:1])
		e.store.addProject("Shared", ids["old-unverified@uni.example"], "someone-else")
		if n, err := e.svc.PurgeUnverified(context.Background()); err != nil || n != 0 {
			t.Fatalf("PurgeUnverified = %d, %v; want 0, nil", n, err)
		}
		if len(e.store.users) != 1 || !strings.Contains(e.logs.String(), "owns shared projects") {
			t.Errorf("users = %d, logs %s", len(e.store.users), e.logs)
		}
	})

	t.Run("off when the age is zero", func(t *testing.T) {
		e, _ := purgeEnv(t, Config{}, accounts)
		e = withConfig(e, Config{})
		if n, err := e.svc.PurgeUnverified(context.Background()); err != nil || n != 0 || len(e.store.users) != len(accounts) {
			t.Errorf("PurgeUnverified = %d, %v with %d users left; want nothing deleted", n, err, len(e.store.users))
		}
	})

	t.Run("a store failure is returned", func(t *testing.T) {
		e, _ := purgeEnv(t, Config{}, accounts)
		e.store.failFind = errors.New("db down")
		if _, err := e.svc.PurgeUnverified(context.Background()); err == nil {
			t.Error("error = nil, want the store's")
		}
	})
}

// RunUnverifiedPurge runs a pass at once, before the first tick, and stops when its
// context ends. The context is already cancelled here, so it returns right after that pass.
func TestRunUnverifiedPurge(t *testing.T) {
	e, _ := purgeEnv(t, Config{}, []account{{"old@uni.example", 8 * 24 * time.Hour, false}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		e.svc.RunUnverifiedPurge(ctx, time.Hour)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunUnverifiedPurge did not stop")
	}
	if len(e.store.users) != 0 || !strings.Contains(e.logs.String(), "unverified accounts purged") {
		t.Errorf("first pass did not run: %d users left, logs %s", len(e.store.users), e.logs)
	}
}
