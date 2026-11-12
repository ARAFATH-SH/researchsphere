//go:build integration

package main

import (
	"bytes"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/argon2"
	authpg "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/postgres"
	authredis "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/redis"
	authusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
)

// TestPurgeUnverifiedAccounts runs the FR-17.4 purge against the database: only accounts
// still unverified 7 days after sign-up go, through the account deletion (their profile goes
// with them), and outside production the test email domains stay (FR-17.9).
func TestPurgeUnverifiedAccounts(t *testing.T) {
	pool := testenv.DB(t)
	rdb := testenv.Redis(t)
	ctx := t.Context()
	attempts := authredis.New(rdb)
	t.Cleanup(func() { _ = attempts.Close() })

	newAuth := func(testDomains []string) (*authusecase.Service, *bytes.Buffer) {
		var logs bytes.Buffer
		return authusecase.New(authpg.NewStore(pool), argon2.New(argon2.DefaultParams), noAuthMail{},
			accesstoken.New([]byte(testSecret), accessTTL), attempts, authusecase.Config{
				AppBaseURL: "http://localhost:3000", TestEmailDomains: testDomains, UnverifiedAccountAge: 7 * 24 * time.Hour,
			}, time.Now, slog.New(slog.NewTextHandler(&logs, nil))), &logs
	}
	setup, _ := newAuth(nil)

	// signUp registers an account that signed up age ago, verified or not.
	signUp := func(email string, age time.Duration, verified bool) string {
		t.Helper()
		if _, err := setup.Register(ctx, authusecase.RegisterInput{FullName: "Purge Test", Email: email, Password: "correct horse battery"}); err != nil {
			t.Fatal(err)
		}
		var id string
		err := pool.QueryRow(ctx, `UPDATE users SET created_at = now() - $2::interval,
			email_verified_at = CASE WHEN $3 THEN now() - $2::interval + interval '1 minute' END
			WHERE email = $1 RETURNING id::text`, email, age.String(), verified).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	day := 24 * time.Hour
	oldUnverified := signUp(testenv.Email(t), 8*day, false)
	newUnverified := signUp(testenv.Email(t), 6*day, false)
	oldVerified := signUp(testenv.Email(t), 30*day, true)
	testDomain := signUp("purge-"+strings.ToLower(testenv.Word(t))+"@devtest.example", 30*day, false)

	ids := func() []string {
		rows, err := pool.Query(ctx, `SELECT u.id::text FROM users u JOIN profiles p ON p.user_id = u.id ORDER BY 1`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			out = append(out, id)
		}
		return out
	}
	sorted := func(s ...string) []string { slices.Sort(s); return s }

	// Development: the test domain is kept.
	dev, logs := newAuth([]string{"devtest.example"})
	n, err := dev.PurgeUnverified(ctx)
	if err != nil || n != 1 {
		t.Fatalf("development: PurgeUnverified = %d, %v; want 1", n, err)
	}
	if got, want := ids(), sorted(newUnverified, oldVerified, testDomain); !slices.Equal(got, want) {
		t.Errorf("development: users left = %v, want %v", got, want)
	}
	if !strings.Contains(logs.String(), oldUnverified) || !strings.Contains(logs.String(), "unverified accounts purged") {
		t.Errorf("log lacks the purge of %s: %s", oldUnverified, logs.String())
	}
	var profiles int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM profiles WHERE user_id = $1`, oldUnverified).Scan(&profiles); err != nil || profiles != 0 {
		t.Errorf("profile of the purged account: %d rows, %v; want 0", profiles, err)
	}

	// Production passes no test domains: the test-domain account goes too.
	prod, _ := newAuth(nil)
	if n, err := prod.PurgeUnverified(ctx); err != nil || n != 1 {
		t.Fatalf("production: PurgeUnverified = %d, %v; want 1", n, err)
	}
	if got, want := ids(), sorted(newUnverified, oldVerified); !slices.Equal(got, want) {
		t.Errorf("production: users left = %v, want %v", got, want)
	}
}
