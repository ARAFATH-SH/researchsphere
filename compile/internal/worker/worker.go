// Package worker is the compile worker's loop (docs/compile.md): claim a job from the API,
// stream its files into a sandbox, send the PDF back and report how the run ended. It holds
// no database, storage or Redis credentials: only the compile service's token.
package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/ARAFATH-SH/researchsphere/compile/internal/apiclient"
	"github.com/ARAFATH-SH/researchsphere/compile/internal/sandbox"
)

// API is the part of the internal API the worker calls.
type API interface {
	Claim(ctx context.Context, workerID string) (*apiclient.Job, error)
	Source(ctx context.Context, jobID string) (io.ReadCloser, error)
	StorePDF(ctx context.Context, jobID string, pdf io.Reader, size int64) error
	Finish(ctx context.Context, jobID string, r apiclient.Result) error
}

// Runner runs one compile in a sandbox.
type Runner interface {
	Run(ctx context.Context, job sandbox.Job) (sandbox.Result, error)
	Sweep(ctx context.Context, age time.Duration) (int, error)
}

// Timings of the loop.
const (
	sweepEvery = time.Minute
	// sweepAge: a compile container older than this is a leftover (a run lasts at most 60 s).
	sweepAge     = 3 * time.Minute
	claimBackoff = 2 * time.Second
	finishTries  = 3
)

// Worker runs compiles in parallel slots.
type Worker struct {
	api    API
	runner Runner
	id     string
	slots  int
	logger *slog.Logger
	// stop ends Run with a cause; set by Run.
	stop context.CancelCauseFunc
}

// New returns a Worker named id with slots parallel compiles.
func New(api API, runner Runner, id string, slots int, logger *slog.Logger) *Worker {
	if slots < 1 {
		slots = 1
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Worker{api: api, runner: runner, id: id, slots: slots, logger: logger}
}

// Run claims and runs jobs until ctx ends, then lets the running ones finish and returns.
// It also ends, returning sandbox.ErrUnavailable, when the sandbox says it cannot enforce its
// file rules: the worker fails closed, so the API answers compile_unavailable.
func (w *Worker) Run(ctx context.Context) error {
	ctx, w.stop = context.WithCancelCause(ctx)
	defer w.stop(nil)
	w.sweep(ctx)
	var wg sync.WaitGroup
	wg.Go(func() {
		t := time.NewTicker(sweepEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				w.sweep(ctx)
			}
		}
	})
	for range w.slots {
		wg.Go(func() { w.loop(ctx) })
	}
	wg.Wait()
	if err := context.Cause(ctx); errors.Is(err, sandbox.ErrUnavailable) {
		return err
	}
	return nil
}

func (w *Worker) sweep(ctx context.Context) {
	n, err := w.runner.Sweep(ctx, sweepAge)
	if err != nil {
		w.logger.Warn("sweep of leftover compile containers failed", slog.Any("error", err))
	} else if n > 0 {
		w.logger.Warn("removed leftover compile containers", slog.Int("containers", n))
	}
}

func (w *Worker) loop(ctx context.Context) {
	for ctx.Err() == nil {
		job, err := w.api.Claim(ctx, w.id)
		if err != nil {
			if ctx.Err() == nil {
				w.logger.Warn("claim failed", slog.Any("error", err))
				sleep(ctx, claimBackoff)
			}
			continue
		}
		if job == nil {
			continue // the API waited for one already
		}
		// A started compile runs to its end even when the worker is stopping.
		w.Handle(context.WithoutCancel(ctx), *job)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func ptr[T any](v T) *T { return &v }

// Handle runs one claimed job and reports it.
func (w *Worker) Handle(ctx context.Context, job apiclient.Job) {
	log := w.logger.With(slog.String("job_id", job.ID), slog.String("project_id", job.ProjectID))
	if !sandbox.ValidMain(job.MainFilePath) {
		log.Error("main file is not a .tex path")
		w.finish(ctx, log, job.ID, apiclient.Result{Outcome: "failed", Failure: ptr("sandbox_error")})
		return
	}
	src, err := w.api.Source(ctx, job.ID)
	switch {
	case errors.Is(err, apiclient.ErrInputTooLarge), errors.Is(err, apiclient.ErrGone):
		log.Info("job ended before it ran", slog.Any("reason", err))
		return
	case err != nil:
		log.Error("files not read", slog.Any("error", err))
		w.finish(ctx, log, job.ID, apiclient.Result{Outcome: "failed", Failure: ptr("sandbox_error")})
		return
	}
	res, err := w.runner.Run(ctx, sandbox.Job{ID: job.ID, MainFile: job.MainFilePath, Input: src})
	_ = src.Close()
	if err != nil {
		log.Error("sandbox did not run", slog.Any("error", err))
		w.finish(ctx, log, job.ID, apiclient.Result{Outcome: "failed", Failure: ptr("sandbox_error")})
		return
	}
	if res.Unavailable {
		log.Error("the sandbox cannot enforce its file rules; the worker stops", slog.String("stderr", res.Stderr))
		w.finish(ctx, log, job.ID, apiclient.Result{Outcome: "failed", Failure: ptr("sandbox_error"), ExitCode: res.ExitCode,
			DurationMs: int(res.Duration.Milliseconds())})
		if w.stop != nil {
			w.stop(sandbox.ErrUnavailable)
		}
		return
	}
	out := apiclient.Result{ExitCode: res.ExitCode, DurationMs: int(res.Duration.Milliseconds()), Log: res.Log, BibtexLog: res.BibtexLog}
	switch {
	case res.TimedOut:
		out.Outcome = "timed_out"
	case res.Failure != "":
		out.Outcome, out.Failure = "failed", ptr(string(res.Failure))
	default:
		out.Outcome = "completed"
	}
	if res.PDF != nil {
		defer func() {
			_ = res.PDF.Close()
			_ = os.Remove(res.PDF.Name())
		}()
		if out.Outcome == "completed" {
			if err := w.storePDF(ctx, job.ID, res.PDF, res.PDFSize); err != nil {
				if errors.Is(err, apiclient.ErrGone) {
					log.Warn("job ended while it ran")
					return
				}
				log.Error("PDF not stored", slog.Any("error", err))
				out.Outcome, out.Failure = "failed", ptr("output_too_large")
				if !errors.Is(err, apiclient.ErrPDFTooLarge) {
					out.Failure = ptr("sandbox_error")
				}
			}
		}
	}
	log.Info("compile finished", slog.String("outcome", out.Outcome), slog.Int("duration_ms", out.DurationMs))
	w.finish(ctx, log, job.ID, out)
}

func (w *Worker) storePDF(ctx context.Context, jobID string, f *os.File, size int64) error {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return w.api.StorePDF(ctx, jobID, f, size)
}

// finish reports the end, trying again on a transient error; a job the API ended meanwhile
// is left as it is.
func (w *Worker) finish(ctx context.Context, log *slog.Logger, jobID string, r apiclient.Result) {
	for i := range finishTries {
		err := w.api.Finish(ctx, jobID, r)
		if err == nil || errors.Is(err, apiclient.ErrGone) {
			return
		}
		log.Warn("finish not reported", slog.Any("error", err), slog.Int("try", i+1))
		sleep(ctx, time.Duration(i+1)*time.Second)
	}
	log.Error("finish never reported; the API's sweep ends the job as worker_lost")
}
