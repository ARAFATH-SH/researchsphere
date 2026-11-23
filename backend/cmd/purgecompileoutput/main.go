// Command purgecompileoutput deletes the PDFs and logs of the compiles that started before a
// cutoff, from the database and from object storage, so no output a broken sandbox made stays
// downloadable. Run it once in every environment after a sandbox fix, with the time the fixed
// sandbox went live (docs/deploy.md). Without -apply it only lists what it would delete.
// The compile records, their status and their errors stay; a project whose PDF went shows "no
// PDF" until it compiles again. It reads the API's settings (DATABASE_URL, S3_*).
//
//	go run ./cmd/purgecompileoutput -before 2026-11-23T21:10:27+06:00 [-apply]
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	compilepg "github.com/ARAFATH-SH/researchsphere/backend/internal/compile/adapter/postgres"
	compiledomain "github.com/ARAFATH-SH/researchsphere/backend/internal/compile/domain"
	compileusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/compile/usecase"
	mediapg "github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/postgres"
	mediadomain "github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
	mediausecase "github.com/ARAFATH-SH/researchsphere/backend/internal/media/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/config"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/logging"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/storage"
)

// purger is what the command needs: the compile usecase's purge and the storage cleanup.
type purger interface {
	PurgeOutputBefore(ctx context.Context, before time.Time, apply bool) ([]compiledomain.Output, error)
	DeleteQueuedObjects(ctx context.Context) (mediadomain.DrainResult, error)
}

type services struct {
	compile *compileusecase.Service
	cleaner *mediausecase.Cleaner
}

func (s services) PurgeOutputBefore(ctx context.Context, before time.Time, apply bool) ([]compiledomain.Output, error) {
	return s.compile.PurgeOutputBefore(ctx, before, apply)
}

func (s services) DeleteQueuedObjects(ctx context.Context) (mediadomain.DrainResult, error) {
	return s.cleaner.RunOnce(ctx)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	open := func(ctx context.Context) (purger, func(), error) {
		cfg, err := config.Load(os.Getenv)
		if err != nil {
			return nil, nil, err
		}
		pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
		if err != nil {
			return nil, nil, err
		}
		files, err := storage.NewS3(cfg.Storage)
		if err != nil {
			pool.Close()
			return nil, nil, err
		}
		logger := logging.New(os.Stderr, cfg.LogLevel)
		svc := services{
			compile: compileusecase.New(compileusecase.Deps{Store: compilepg.NewStore(pool), Tx: postgres.NewTransactor(pool), Logger: logger}),
			cleaner: mediausecase.NewCleaner(mediapg.NewStore(pool), files, logger),
		}
		return svc, pool.Close, nil
	}
	if err := run(ctx, os.Args[1:], open, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "purgecompileoutput:", err)
		stop()
		os.Exit(1)
	}
}

// run reads the flags, lists or clears the output, then deletes the queued objects.
func run(ctx context.Context, args []string, open func(context.Context) (purger, func(), error), out io.Writer) error {
	fs := flag.NewFlagSet("purgecompileoutput", flag.ContinueOnError)
	fs.SetOutput(out)
	beforeText := fs.String("before", "", "delete the output of compiles that started before this time (RFC 3339, with its zone)")
	apply := fs.Bool("apply", false, "delete; without it, only list what would be deleted")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *beforeText == "" {
		return errors.New("-before is required: the time the fixed sandbox went live, such as 2026-11-23T21:10:27+06:00")
	}
	before, err := time.Parse(time.RFC3339, *beforeText)
	if err != nil {
		return fmt.Errorf("-before %q is not an RFC 3339 time with a zone", *beforeText)
	}

	p, closeAll, err := open(ctx)
	if err != nil {
		return err
	}
	defer closeAll()

	jobs, err := p.PurgeOutputBefore(ctx, before, *apply)
	if err != nil {
		return err
	}
	pdfs, logs := 0, 0
	for _, j := range jobs {
		finished := "-"
		if j.FinishedAt != nil {
			finished = j.FinishedAt.Format(time.RFC3339)
		}
		var what []string
		if j.HasPDF {
			pdfs++
			what = append(what, "PDF")
		}
		if j.HasLog {
			logs++
			what = append(what, "log")
		}
		if _, err := fmt.Fprintf(out, "compile %s  project %s  finished %s  %v\n", j.JobID, j.ProjectID, finished, what); err != nil {
			return err
		}
	}
	if !*apply {
		_, err = fmt.Fprintf(out, "Dry run: %d compiles before %s hold %d PDFs and %d logs. Add -apply to delete them.\n",
			len(jobs), before.Format(time.RFC3339), pdfs, logs)
		return err
	}
	if _, err := fmt.Fprintf(out, "Deleted %d PDFs and %d logs of %d compiles before %s.\n", pdfs, logs, len(jobs), before.Format(time.RFC3339)); err != nil {
		return err
	}
	// The PDFs' objects are queued (storage_deletions); delete them now rather than at the
	// API's next cleanup pass.
	res, err := p.DeleteQueuedObjects(ctx)
	if err != nil {
		return fmt.Errorf("the database is cleared, but deleting the objects failed (the API's cleanup retries them): %w", err)
	}
	if res.Failed > 0 {
		_, err = fmt.Fprintf(out, "Deleted %d objects from storage; %d could not be deleted yet (the API's cleanup retries them).\n", res.Deleted, res.Failed)
		return err
	}
	_, err = fmt.Fprintf(out, "Deleted %d objects from storage.\n", res.Deleted)
	return err
}
