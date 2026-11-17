// Command backfillmaintex gives the starting main.tex (FR-10.4) to every project that has no
// file, such as those created before slice 21. `make backfill-main-tex` runs it; run it once
// after migration 000035 in every environment (docs/deploy.md). It is idempotent: a project
// that has any file is left alone, so a second run adds nothing. It reads DATABASE_URL.
//
//	go run ./cmd/backfillmaintex
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	projectpg "github.com/ARAFATH-SH/researchsphere/backend/internal/project/adapter/postgres"
	projectusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/project/usecase"
)

// backfiller adds the missing main.tex files; the project usecase implements it.
type backfiller interface {
	BackfillMainFiles(ctx context.Context) (int, error)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	open := func(ctx context.Context) (backfiller, func(), error) {
		url := os.Getenv("DATABASE_URL")
		if url == "" {
			return nil, nil, errors.New("DATABASE_URL is not set")
		}
		pool, err := postgres.NewPool(ctx, url)
		if err != nil {
			return nil, nil, err
		}
		store := projectpg.NewStore(pool)
		svc := projectusecase.New(projectusecase.Deps{
			Store: store, Tx: postgres.NewTransactor(pool),
			FileDeps: projectusecase.FileDeps{Files: store},
		})
		return svc, pool.Close, nil
	}
	if err := run(ctx, open, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "backfillmaintex:", err)
		stop()
		os.Exit(1)
	}
}

// run opens the store, adds the missing files and prints how many.
func run(ctx context.Context, open func(context.Context) (backfiller, func(), error), out io.Writer) error {
	b, closeStore, err := open(ctx)
	if err != nil {
		return err
	}
	defer closeStore()
	n, err := b.BackfillMainFiles(ctx)
	if err != nil {
		return fmt.Errorf("after adding main.tex to %d projects: %w", n, err)
	}
	_, err = fmt.Fprintf(out, "Added main.tex to %d projects without files.\n", n)
	return err
}
