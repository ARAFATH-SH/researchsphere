// Command setrole gives an account the admin or researcher platform role. `make admin`
// and `make unadmin` run it. It is the only way to make an administrator: no API
// operation changes a role (docs/decisions.md). It reads DATABASE_URL.
//
//	go run ./cmd/setrole -email ada@example.org -role admin
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	authpg "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
)

// roleSetter changes a user's role; authpg.Store implements it.
type roleSetter interface {
	SetRoleByEmail(ctx context.Context, email, role string) error
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	open := func(ctx context.Context) (roleSetter, func(), error) {
		url := os.Getenv("DATABASE_URL")
		if url == "" {
			return nil, nil, errors.New("DATABASE_URL is not set")
		}
		pool, err := postgres.NewPool(ctx, url)
		if err != nil {
			return nil, nil, err
		}
		return authpg.NewStore(pool), pool.Close, nil
	}
	if err := run(ctx, os.Args[1:], open, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "setrole:", err)
		stop()
		os.Exit(1)
	}
}

// run parses args, opens the store and sets the role, printing the outcome to out.
func run(ctx context.Context, args []string, open func(context.Context) (roleSetter, func(), error), out io.Writer) error {
	fs := flag.NewFlagSet("setrole", flag.ContinueOnError)
	fs.SetOutput(out)
	email := fs.String("email", "", "the account's email address")
	role := fs.String("role", "", "admin or researcher")
	if err := fs.Parse(args); err != nil {
		return err
	}
	*email = strings.TrimSpace(*email)
	if *email == "" {
		return errors.New("usage: make admin email=ada@example.org (or make unadmin email=...)")
	}
	if *role != domain.RoleAdmin && *role != domain.RoleResearcher {
		return fmt.Errorf("role must be %q or %q, not %q", domain.RoleAdmin, domain.RoleResearcher, *role)
	}

	store, closeStore, err := open(ctx)
	if err != nil {
		return err
	}
	defer closeStore()

	err = store.SetRoleByEmail(ctx, *email, *role)
	if errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("no account with the email address %s; nothing changed", *email)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s now has the %s role. It takes effect at the user's next token refresh (within 15 minutes) or login.\n", *email, *role)
	return nil
}
