package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	compilehttp "github.com/ARAFATH-SH/researchsphere/backend/internal/compile/adapter/http"
	compilepg "github.com/ARAFATH-SH/researchsphere/backend/internal/compile/adapter/postgres"
	compileredis "github.com/ARAFATH-SH/researchsphere/backend/internal/compile/adapter/redis"
	compiledomain "github.com/ARAFATH-SH/researchsphere/backend/internal/compile/domain"
	compileusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/compile/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/config"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/storage"
	projectdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
	projectusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/project/usecase"
	realtimedomain "github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/domain"
)

// compileSweepInterval is how often expired compile leases and stale queued compiles end.
const compileSweepInterval = 30 * time.Second

// newCompile wires the compile module (slice 23, docs/compile.md): the project module
// answers who may compile and what the files are, storage holds the PDFs, and the members
// hear about every change through the realtime hub.
func newCompile(pool *pgxpool.Pool, rdb *goredis.Client, cfg config.Config, projects *projectusecase.Service, files storage.Storage,
	events *realtimeEvents, logger *slog.Logger) (*compileusecase.Service, *compileredis.Client) {
	keys := compileredis.New(rdb)
	svc := compileusecase.New(compileusecase.Deps{
		Store:     compilepg.NewStore(pool),
		Tx:        postgres.NewTransactor(pool),
		Access:    compileAccess{projects: projects},
		Sources:   compileSources{projects: projects},
		Objects:   compileObjects{Storage: files},
		Workers:   keys,
		Limiter:   keys,
		Publisher: compileEvents{events: events},
		Logger:    logger,
		Config: compileusecase.Config{
			QueueMax: cfg.CompileQueueMax, MaxPDFBytes: cfg.CompileMaxPDFBytes, MaxInputBytes: cfg.CompileMaxInputBytes,
		},
	})
	return svc, keys
}

// compileAccess answers the compile module with the project module's role rules.
type compileAccess struct {
	projects *projectusecase.Service
}

var _ compiledomain.Access = compileAccess{}

func (a compileAccess) Authorize(ctx context.Context, userID, projectID string, need compiledomain.Need) error {
	action := projectdomain.ActReadFiles
	if need == compiledomain.NeedCompile {
		action = projectdomain.ActCompile
	}
	err := a.projects.Authorize(ctx, userID, projectID, action)
	var suspended *projectdomain.SuspendedError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, projectdomain.ErrNotFound):
		return compiledomain.ErrNotFound
	case errors.Is(err, projectdomain.ErrForbidden):
		return compiledomain.ErrForbidden
	case errors.As(err, &suspended):
		return &compiledomain.SuspendedError{Reason: suspended.Reason, Until: suspended.Until}
	}
	return err
}

// compileSources reads a compile's files from the project module.
type compileSources struct {
	projects *projectusecase.Service
}

var _ compiledomain.Sources = compileSources{}

func (s compileSources) Source(ctx context.Context, projectID string) (compiledomain.Source, error) {
	src, err := s.projects.CompileSource(ctx, projectID)
	if errors.Is(err, projectdomain.ErrNotFound) {
		return compiledomain.Source{}, compiledomain.ErrNotFound
	}
	if err != nil {
		return compiledomain.Source{}, err
	}
	out := compiledomain.Source{MainFilePath: src.MainFilePath, LiveConfirmed: src.LiveConfirmed,
		Entries: make([]compiledomain.SourceEntry, 0, len(src.Files))}
	for _, f := range src.Files {
		e := compiledomain.SourceEntry{Path: f.Path, Kind: compiledomain.EntryKind(f.Kind), Size: f.Size, UpdatedAt: f.UpdatedAt}
		if f.Content != nil {
			e.Content = *f.Content
		}
		if f.ObjectKey != nil {
			e.ObjectKey = *f.ObjectKey
		}
		if f.ScanStatus != nil {
			e.Scan = string(*f.ScanStatus)
		}
		out.Entries = append(out.Entries, e)
	}
	return out, nil
}

// compileObjects is the bucket as the compile module sees it.
type compileObjects struct {
	storage.Storage
}

var _ compiledomain.Objects = compileObjects{}

func (o compileObjects) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	r, err := o.Storage.Get(ctx, key)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, compiledomain.ErrObjectMissing
	}
	return r, err
}

// compileEvents sends compile.updated to the members after the commit (docs/realtime.md).
type compileEvents struct {
	events *realtimeEvents
}

var _ compiledomain.Publisher = compileEvents{}

func (e compileEvents) CompileUpdated(ctx context.Context, j compiledomain.Job, to []string) {
	if e.events == nil {
		return
	}
	postgres.AfterCommit(ctx, func(ctx context.Context) {
		body, err := compilehttp.JobBody(j)
		if err != nil {
			e.events.logger.ErrorContext(ctx, "realtime: compile event", slog.Any("error", err))
			return
		}
		ev := realtimedomain.Event{Type: realtimedomain.EventCompileUpdated, Data: gen.RealtimeCompileUpdated{ProjectId: body.ProjectId, Compile: body}}
		for _, userID := range to {
			e.events.hub.Send(ctx, userID, ev)
		}
	})
}
