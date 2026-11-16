package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/embedding/domain"
)

// WorkerConfig sets the worker's pace. DefaultWorkerConfig is what the API uses.
type WorkerConfig struct {
	// Poll is the wait between ticks when the queue is empty: a change is embedded within
	// Poll plus the time to embed (AI-1.4 asks for 60 s).
	Poll time.Duration
	// Batch is how many jobs one claim leases.
	Batch int
	// Lease is how long a claim holds its jobs; another worker takes them after it.
	Lease time.Duration
	// JobTimeout bounds one job, the AI service's retry included. A job is started only
	// when its claim's lease outlasts it.
	JobTimeout time.Duration
	// MaxAttempts is how many text errors a job may have before it is marked failed.
	MaxAttempts int
	// RetryBase is the wait after the first text error; it doubles each time, up to
	// RetryMax.
	RetryBase, RetryMax time.Duration
	// DownBase is the wait after the AI service is first found down; it doubles each time,
	// up to DownMax.
	DownBase, DownMax time.Duration
	// StaleBatch is how many owners of an old model one tick re-queues (AI-1.6).
	StaleBatch int
}

// DefaultWorkerConfig: poll every 5 s, back off to 1 min while the AI service is down.
func DefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{
		Poll:        5 * time.Second,
		Batch:       16,
		Lease:       5 * time.Minute,
		JobTimeout:  80 * time.Second,
		MaxAttempts: 5,
		RetryBase:   30 * time.Second,
		RetryMax:    10 * time.Minute,
		DownBase:    5 * time.Second,
		DownMax:     time.Minute,
		StaleBatch:  500,
	}
}

// Worker makes the embeddings the queue asks for (AI-1.5). Several workers, in one process
// or several, may run on one queue: a claim leases its jobs, so no owner is processed by two
// at once. A Worker itself is not safe for concurrent use; Run it on one goroutine.
type Worker struct {
	store    domain.Store
	embedder domain.Embedder
	cfg      WorkerConfig
	now      func() time.Time
	logger   *slog.Logger

	watcher domain.Watcher // nil: nobody is told

	model     string // the AI service's model, "" until known
	staleDone bool   // the owners of other models are all queued
	downs     int    // ticks in a row that found the AI service down
	offLogged bool   // "not configured" was logged
}

// NewWorker returns a Worker.
func NewWorker(store domain.Store, embedder domain.Embedder, cfg WorkerConfig, now func() time.Time, logger *slog.Logger) *Worker {
	return &Worker{store: store, embedder: embedder, cfg: cfg, now: now, logger: logger}
}

// Watch makes the worker tell wt about every embedding it stores or deletes. Call it before
// Run.
func (w *Worker) Watch(wt domain.Watcher) {
	w.watcher = wt
}

// changed tells the watcher that the owner's embedding was stored or deleted.
func (w *Worker) changed(ctx context.Context, job domain.Job) {
	if w.watcher != nil {
		w.watcher.EmbeddingChanged(ctx, job.OwnerType, job.OwnerID)
	}
}

// Run ticks until ctx ends.
func (w *Worker) Run(ctx context.Context) {
	for {
		wait := w.Tick(ctx)
		if wait <= 0 {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

// Tick does one round: learn the model when it is not known, re-queue a batch of owners of
// another model, then claim and process a batch of jobs. It returns how long to wait before
// the next tick: 0 when a full batch was processed and more may wait.
func (w *Worker) Tick(ctx context.Context) time.Duration {
	if w.model == "" {
		m, err := w.embedder.Model(ctx)
		if err != nil {
			return w.unavailable(err)
		}
		w.setModel(m)
	}
	w.recovered()

	if !w.staleDone {
		n, err := w.store.QueueStale(ctx, w.model, w.cfg.StaleBatch)
		if err != nil {
			w.logger.ErrorContext(ctx, "re-queue embeddings of an old model", slog.Any("error", err))
		} else {
			if n > 0 {
				w.logger.InfoContext(ctx, "re-queued embeddings of an old model", slog.Int("owners", n), slog.String("model", w.model))
			}
			w.staleDone = n < w.cfg.StaleBatch
		}
	}

	claimed := w.now()
	jobs, err := w.store.ClaimJobs(ctx, w.cfg.Batch, w.cfg.Lease)
	if err != nil {
		if ctx.Err() == nil {
			w.logger.ErrorContext(ctx, "claim embedding jobs", slog.Any("error", err))
		}
		return w.cfg.Poll
	}
	deadline := claimed.Add(w.cfg.Lease)
	for i, job := range jobs {
		if ctx.Err() != nil || deadline.Sub(w.now()) < w.cfg.JobTimeout {
			w.release(ctx, jobs[i:])
			return w.cfg.Poll
		}
		if err := w.process(ctx, job); err != nil {
			w.release(ctx, jobs[i:])
			if errors.Is(err, domain.ErrUnavailable) || errors.Is(err, domain.ErrNotConfigured) {
				w.model = "" // read it again once the service is back: it may have changed
				return w.unavailable(err)
			}
			if ctx.Err() == nil {
				w.logger.ErrorContext(ctx, "embedding job", slog.String("owner_type", string(job.OwnerType)),
					slog.String("owner_id", job.OwnerID), slog.Any("error", err))
			}
			return w.cfg.Poll
		}
	}
	if len(jobs) == w.cfg.Batch {
		return 0
	}
	return w.cfg.Poll
}

// process makes, keeps or removes one owner's embedding. It returns ErrUnavailable or
// ErrNotConfigured when the AI service cannot be used, so the caller keeps the job, and a
// database error as it is. A text error is recorded on the job and is not returned.
func (w *Worker) process(ctx context.Context, job domain.Job) error {
	ctx, cancel := context.WithTimeout(ctx, w.cfg.JobTimeout)
	defer cancel()

	text, found, err := w.sourceText(ctx, job)
	if err != nil {
		return err
	}
	if !found || text == "" {
		// Gone, not eligible or nothing filled in: no embedding (AI-1.1).
		if err := w.store.DeleteEmbedding(ctx, job.OwnerType, job.OwnerID); err != nil {
			return err
		}
		w.changed(ctx, job)
		return w.store.FinishJob(ctx, job)
	}

	hash := hashOf(job.OwnerType, text)
	stored, err := w.store.StoredEmbedding(ctx, job.OwnerType, job.OwnerID)
	switch {
	case err == nil:
		if !job.Force && stored.ContentHash == hash && stored.Model == w.model {
			return w.store.FinishJob(ctx, job) // unchanged (AI-1.4)
		}
	case !errors.Is(err, domain.ErrNotFound):
		return err
	}

	vector, model, chunks, err := w.embedder.EmbedDocument(ctx, cutText(text), domain.MaxChunks)
	if errors.Is(err, domain.ErrUnavailable) || errors.Is(err, domain.ErrNotConfigured) {
		return err
	}
	if err != nil {
		return w.retry(ctx, job, err)
	}
	if model != w.model {
		w.setModel(model)
	}
	if _, err := w.store.SaveEmbedding(ctx, domain.Embedding{
		OwnerType: job.OwnerType, OwnerID: job.OwnerID, Model: model, ContentHash: hash, ChunkCount: chunks, Vector: vector,
	}); err != nil {
		return err
	}
	w.changed(ctx, job)
	return w.store.FinishJob(ctx, job)
}

// sourceText returns the owner's text, and false when the owner is gone or may not have an
// embedding. Each owner type adds its case.
func (w *Worker) sourceText(ctx context.Context, job domain.Job) (string, bool, error) {
	switch job.OwnerType {
	case domain.OwnerProfile:
		src, err := w.store.ProfileSource(ctx, job.OwnerID)
		if errors.Is(err, domain.ErrNotFound) {
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}
		return ProfileText(src), src.Eligible, nil
	case domain.OwnerProject:
		src, err := w.store.ProjectSource(ctx, job.OwnerID)
		if errors.Is(err, domain.ErrNotFound) {
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}
		return ProjectText(src), true, nil
	default:
		return "", false, fmt.Errorf("unknown owner type %q", job.OwnerType)
	}
}

// retry counts a text error against the job: it runs again later, or after MaxAttempts it
// is marked failed with the error.
func (w *Worker) retry(ctx context.Context, job domain.Job, cause error) error {
	attempts := job.Attempts + 1
	failed := attempts >= w.cfg.MaxAttempts
	wait := w.cfg.RetryBase << min(attempts-1, 20)
	if wait > w.cfg.RetryMax || wait <= 0 {
		wait = w.cfg.RetryMax
	}
	msg := cause.Error()
	if len(msg) > 500 {
		msg = msg[:500]
	}
	level := slog.LevelWarn
	if failed {
		level = slog.LevelError
	}
	w.logger.Log(ctx, level, "embedding failed", slog.String("owner_type", string(job.OwnerType)),
		slog.String("owner_id", job.OwnerID), slog.Int("attempts", attempts), slog.Bool("gave_up", failed), slog.Any("error", cause))
	return w.store.RetryJob(ctx, job, msg, w.now().Add(wait), failed)
}

// release ends the leases of jobs not processed, with a context that outlives a shutdown.
func (w *Worker) release(ctx context.Context, jobs []domain.Job) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for _, j := range jobs {
		if err := w.store.ReleaseJob(ctx, j); err != nil {
			// The lease ends by itself.
			w.logger.WarnContext(ctx, "release embedding job", slog.Any("error", err))
			return
		}
	}
}

func (w *Worker) setModel(m string) {
	if m != w.model {
		w.model, w.staleDone = m, false
	}
}

// unavailable logs once that the AI service is down or turned off, and returns the wait:
// doubling from DownBase up to DownMax while down, Poll while turned off.
func (w *Worker) unavailable(err error) time.Duration {
	if errors.Is(err, domain.ErrNotConfigured) {
		if !w.offLogged {
			w.offLogged = true
			w.logger.Info("the AI service is not configured; embedding jobs wait until it is")
		}
		return w.cfg.DownMax
	}
	w.downs++
	if w.downs == 1 {
		w.logger.Warn("the AI service is unavailable; embedding jobs wait", slog.Any("error", err))
	}
	wait := w.cfg.DownBase << min(w.downs-1, 20)
	if wait > w.cfg.DownMax || wait <= 0 {
		wait = w.cfg.DownMax
	}
	return wait
}

// recovered logs once that the AI service answers again.
func (w *Worker) recovered() {
	if w.downs > 0 {
		w.logger.Info("the AI service is available again; embedding resumes", slog.String("model", w.model))
	}
	w.downs, w.offLogged = 0, false
}
