// Command seedcompare fills the development database with the accounts and conversations
// `npm run ui:compare` needs to compare the messaging pages. `make seed-ui-compare` runs
// it. It reads DATABASE_URL, UI_COMPARE_PASSWORD and APP_ENV, and refuses anything but a
// local development database (see checkTarget). Running it again starts the conversations
// over, so their dates stay current; it touches only the @compare.researchsphere.local
// accounts.
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
		fmt.Fprintln(os.Stderr, "seedcompare:", err)
		stop()
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	databaseURL := os.Getenv("DATABASE_URL")
	if err := checkTarget(databaseURL, os.Getenv("APP_ENV")); err != nil {
		return err
	}
	password := os.Getenv("UI_COMPARE_PASSWORD")
	if len(password) < authusecase.MinPasswordLength {
		return fmt.Errorf("set UI_COMPARE_PASSWORD in .env, at least %d characters", authusecase.MinPasswordLength)
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
	fmt.Printf("Seeded %s, %s, %s and %s; sign in as %s, or as %s for the admin pages.\n",
		alpha.email, beta.email, gamma.email, admin.email, alpha.email, admin.email)
	return nil
}

// checkTarget refuses every database but a local development one: a test database, which
// the tests empty, a production environment, and any host other than this machine.
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
