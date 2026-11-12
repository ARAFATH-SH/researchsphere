// Command seeddev creates or repairs the development accounts (FR-17.9): test1@example.com
// to test4@example.com (researchers) and admin@example.com (an administrator), all verified,
// with the password in DEV_SEED_PASSWORD. `make seed-dev` runs it. Running it again resets
// their password, verified state, role and suspension, and touches no other account. It
// reads DATABASE_URL, DEV_SEED_PASSWORD and APP_ENV, and refuses anything but a local
// development database (see checkTarget). example.com cannot receive email, so the API
// accepts these addresses only while TEST_EMAIL_DOMAINS names it, which production ignores.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/argon2"
	authusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "seeddev:", err)
		stop()
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	databaseURL := os.Getenv("DATABASE_URL")
	if err := checkTarget(databaseURL, os.Getenv("APP_ENV")); err != nil {
		return err
	}
	password := os.Getenv("DEV_SEED_PASSWORD")
	if err := checkPassword(password); err != nil {
		return err
	}
	hash, err := argon2.New(argon2.DefaultParams).Hash(password)
	if err != nil {
		return err
	}
	pool, err := postgres.NewPool(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := seed(ctx, pool, hash); err != nil {
		return err
	}
	emails := make([]string, len(accounts))
	for i, a := range accounts {
		emails[i] = a.email
	}
	fmt.Printf("Seeded %s; the password is DEV_SEED_PASSWORD from .env.\n", strings.Join(emails, ", "))
	return nil
}

// checkPassword wants a password the API would accept at login.
func checkPassword(p string) error {
	if len([]rune(p)) < authusecase.MinPasswordLength || len([]rune(p)) > authusecase.MaxPasswordLength {
		return fmt.Errorf("set DEV_SEED_PASSWORD in .env, %d to %d characters", authusecase.MinPasswordLength, authusecase.MaxPasswordLength)
	}
	return nil
}

// checkTarget refuses every database but a local development one: a test database, which
// the tests empty, a production environment, and any host other than this machine. It is
// the check of cmd/seedcompare, which stays as it is.
func checkTarget(databaseURL, appEnv string) error {
	if databaseURL == "" {
		return errors.New("DATABASE_URL is not set")
	}
	if strings.EqualFold(strings.TrimSpace(appEnv), "production") {
		return errors.New("refusing to run with APP_ENV=production")
	}
	u, err := url.Parse(databaseURL)
	if err != nil {
		return errors.New("DATABASE_URL is not a valid URL") // the error would repeat the password
	}
	name := strings.TrimPrefix(u.Path, "/")
	if name == "" {
		return errors.New("DATABASE_URL names no database")
	}
	if strings.HasSuffix(name, "_test") {
		return fmt.Errorf("refusing the test database %s", name)
	}
	if host := u.Hostname(); host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return fmt.Errorf("refusing the database on %s: only a local database can be seeded", host)
	}
	return nil
}
