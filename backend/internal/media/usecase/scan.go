package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
)

// Rules of the malware scan (FR-16.7, NFR-13, docs/decisions.md 2026-11-09).
const (
	// MaxScanTries is how many scanner errors about one file end its scan as failed.
	MaxScanTries = 5
	// ScanRetryBase is the wait after the first scanner error about a file; it doubles with
	// each further one (30 s, 1 min, 2 min, 4 min).
	ScanRetryBase = 30 * time.Second
	// ScanTimeout bounds one file's scan; ScanLease is longer, so a lease never runs out while
	// its worker still scans.
	ScanTimeout = 2 * time.Minute
	ScanLease   = 5 * time.Minute
	// ScanBatch is how many files one claim takes.
	ScanBatch = 4
	// ScanPoll is the wait when nothing is due.
	ScanPoll = 2 * time.Second
	// UnreachableBackoffMin and UnreachableBackoffMax bound the wait while the scanner does not
	// answer (clamd down or still loading its signatures). Such waits never use up a file's tries.
	UnreachableBackoffMin = 2 * time.Second
	UnreachableBackoffMax = time.Minute
)

// ScanWorker scans the stored files whose scan is pending (FR-16.7). Several workers, in one
// API process or many, may run at once: a file is leased by one of them at a time
// (domain.ScanQueue), so it is never scanned twice at once, and a result is written only by the
// worker that holds the lease.
type ScanWorker struct {
	queue    domain.ScanQueue
	tx       domain.Transactor
	objects  domain.ObjectReader
	deleter  domain.ObjectDeleter
	scanner  domain.VirusScanner
	listener domain.ScanListener
	logger   *slog.Logger

	// down is true while the scanner does not answer; backoff is the next wait then. Only
	// Step touches them, from one goroutine.
	down    bool
	backoff time.Duration
}

// NewScanWorker returns a ScanWorker.
func NewScanWorker(queue domain.ScanQueue, tx domain.Transactor, objects domain.ObjectReader, deleter domain.ObjectDeleter,
	scanner domain.VirusScanner, listener domain.ScanListener, logger *slog.Logger,
) *ScanWorker {
	return &ScanWorker{queue: queue, tx: tx, objects: objects, deleter: deleter, scanner: scanner, listener: listener, logger: logger}
}

// Run calls Step until ctx is cancelled, waiting as long as each Step says.
func (w *ScanWorker) Run(ctx context.Context) {
	for {
		wait := w.Step(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// Step does one round and returns how long to wait before the next. It asks the scanner first:
// while the scanner does not answer, nothing is claimed, the wait grows from
// UnreachableBackoffMin to UnreachableBackoffMax, and one warning is logged for the whole
// outage. Otherwise it scans one batch, and waits only when there was nothing to do.
func (w *ScanWorker) Step(ctx context.Context) time.Duration {
	if err := w.scanner.Ping(ctx); err != nil {
		return w.unreachable(ctx, err)
	}
	w.reachable(ctx)
	n, err := w.RunOnce(ctx)
	switch {
	case errors.Is(err, domain.ErrScannerUnavailable):
		return w.unreachable(ctx, err)
	case err != nil && ctx.Err() == nil:
		w.logger.ErrorContext(ctx, "malware scan failed", slog.Any("error", err))
		return ScanPoll
	case n == 0:
		return ScanPoll
	}
	return 0
}

func (w *ScanWorker) unreachable(ctx context.Context, err error) time.Duration {
	if !w.down {
		w.down = true
		w.backoff = UnreachableBackoffMin
		w.logger.WarnContext(ctx, "malware scanner unreachable; attachments stay pending until it answers", slog.Any("error", err))
		return w.backoff
	}
	w.backoff = min(w.backoff*2, UnreachableBackoffMax)
	return w.backoff
}

func (w *ScanWorker) reachable(ctx context.Context) {
	if w.down {
		w.down = false
		w.logger.InfoContext(ctx, "malware scanner reachable again")
	}
}

// RunOnce claims one batch and scans it. It returns how many files it claimed. When the scanner
// stops answering partway, the rest of the batch is given back without using a try and the
// error wraps domain.ErrScannerUnavailable.
func (w *ScanWorker) RunOnce(ctx context.Context) (int, error) {
	jobs, err := w.queue.ClaimScans(ctx, ScanBatch, ScanLease)
	if err != nil {
		return 0, fmt.Errorf("claim scans: %w", err)
	}
	for i, job := range jobs {
		if err := w.scan(ctx, job); err != nil {
			for _, rest := range jobs[i+1:] {
				w.giveBack(ctx, rest, 0)
			}
			return len(jobs), err
		}
	}
	return len(jobs), nil
}

// scan scans one file and records the outcome. It returns an error only when the scanner or
// the database cannot go on; an error about the file itself is recorded and is not returned.
func (w *ScanWorker) scan(ctx context.Context, job domain.ScanJob) error {
	sctx, cancel := context.WithTimeout(ctx, ScanTimeout)
	defer cancel()
	verdict, err := w.read(sctx, job)
	var fileErr *domain.ScanFileError
	switch {
	case errors.Is(err, domain.ErrObjectMissing):
		w.logger.ErrorContext(ctx, "stored file has no object; scan failed", slog.String("file_id", job.FileID))
		return w.finish(ctx, job, domain.ScanFailed, func(ctx context.Context) (bool, error) {
			return w.queue.MarkFailed(ctx, job, domain.ScanErrorMissing)
		})
	case errors.As(err, &fileErr):
		return w.fileError(ctx, job, fileErr)
	case errors.Is(err, domain.ErrScannerUnavailable):
		w.giveBack(ctx, job, 0)
		return err
	case err != nil:
		// The bucket could not be read, or the scan ran out of time: not the file's fault.
		w.logger.WarnContext(ctx, "stored file not scanned; trying again later",
			slog.String("file_id", job.FileID), slog.Any("error", err))
		w.giveBack(ctx, job, ScanPoll)
		return nil
	case verdict.Infected:
		return w.infected(ctx, job, verdict)
	}
	return w.finish(ctx, job, domain.ScanClean, func(ctx context.Context) (bool, error) {
		return w.queue.MarkClean(ctx, job)
	})
}

func (w *ScanWorker) read(ctx context.Context, job domain.ScanJob) (domain.ScanVerdict, error) {
	r, err := w.objects.Get(ctx, job.ObjectKey)
	if err != nil {
		return domain.ScanVerdict{}, err
	}
	defer func() { _ = r.Close() }()
	return w.scanner.Scan(ctx, r)
}

// fileError counts one try. The last one ends the scan as failed; the others wait
// ScanRetryBase, doubling.
func (w *ScanWorker) fileError(ctx context.Context, job domain.ScanJob, e *domain.ScanFileError) error {
	tries := job.Attempts + 1
	w.logger.WarnContext(ctx, "the malware scanner refused a file",
		slog.String("file_id", job.FileID), slog.Int("try", tries), slog.String("reason", e.Reason))
	if tries >= MaxScanTries {
		return w.finish(ctx, job, domain.ScanFailed, func(ctx context.Context) (bool, error) {
			return w.queue.MarkFailed(ctx, job, domain.ScanErrorScanner)
		})
	}
	delay := ScanRetryBase << (tries - 1)
	if _, err := w.queue.RetryLater(ctx, job, delay, true, domain.ScanErrorScanner); err != nil {
		return fmt.Errorf("record scan try: %w", err)
	}
	return nil
}

// infected marks the file and tells its owner in one transaction, then deletes the object at
// once. The deletion queue row written with the mark is the backup if that fails.
func (w *ScanWorker) infected(ctx context.Context, job domain.ScanJob, v domain.ScanVerdict) error {
	w.logger.WarnContext(ctx, "malware found in a stored file",
		slog.String("file_id", job.FileID), slog.String("signature", v.Signature))
	var marked bool
	err := w.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		if marked, err = w.queue.MarkInfected(ctx, job); err != nil || !marked {
			return err
		}
		if err := w.listener.Infected(ctx, job.FileID, job.OwnerID); err != nil {
			return fmt.Errorf("tell the owner: %w", err)
		}
		w.listener.Scanned(ctx, job.FileID, domain.ScanInfected)
		return nil
	})
	if err != nil {
		return fmt.Errorf("mark infected: %w", err)
	}
	if !marked {
		return nil
	}
	if err := w.deleter.Delete(context.WithoutCancel(ctx), job.ObjectKey); err != nil {
		w.logger.WarnContext(ctx, "infected object not deleted yet; the cleanup queue will",
			slog.String("file_id", job.FileID), slog.Any("error", err))
	}
	return nil
}

// finish writes a final outcome and tells the listener when it was written.
func (w *ScanWorker) finish(ctx context.Context, job domain.ScanJob, status domain.ScanStatus, mark func(context.Context) (bool, error)) error {
	marked, err := mark(ctx)
	if err != nil {
		return fmt.Errorf("record scan result: %w", err)
	}
	if marked {
		w.listener.Scanned(ctx, job.FileID, status)
	}
	return nil
}

// giveBack returns the lease without using a try.
func (w *ScanWorker) giveBack(ctx context.Context, job domain.ScanJob, delay time.Duration) {
	if _, err := w.queue.RetryLater(context.WithoutCancel(ctx), job, delay, false, ""); err != nil {
		w.logger.WarnContext(ctx, "scan lease not given back; it runs out by itself",
			slog.String("file_id", job.FileID), slog.Any("error", err))
	}
}
