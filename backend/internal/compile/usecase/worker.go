package usecase

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/compile/domain"
)

// The compile worker's operations (docs/compile.md). The worker reaches them only on the
// internal listener with COMPILE_SERVICE_TOKEN.

// Claim takes the oldest queued job for workerID, waiting up to ClaimWait for one; false when
// none came. Every call records that a worker is alive. *domain.FieldError ("workerId") for
// an empty or too long name.
func (s *Service) Claim(ctx context.Context, workerID string) (domain.Claimed, bool, error) {
	if workerID == "" || utf8.RuneCountInString(workerID) > maxWorkerID {
		return domain.Claimed{}, false, &domain.FieldError{Field: "workerId"}
	}
	if s.d.Workers != nil {
		if err := s.d.Workers.Seen(ctx, WorkerTTL); err != nil {
			s.d.Logger.WarnContext(ctx, "compile worker not recorded as alive", slog.Any("error", err))
		}
	}
	deadline := s.d.Now().Add(s.d.Config.ClaimWait)
	for {
		c, ok, err := s.d.Store.Claim(ctx, workerID, Lease)
		if err != nil {
			return domain.Claimed{}, false, err
		}
		if ok {
			if j, err := s.d.Store.Get(ctx, c.ProjectID, c.ID); err == nil {
				s.publish(ctx, j)
			} else {
				s.d.Logger.WarnContext(ctx, "compile event not sent", slog.Any("error", err))
			}
			return c, true, nil
		}
		if !s.d.Now().Before(deadline) {
			return domain.Claimed{}, false, nil
		}
		select {
		case <-ctx.Done():
			return domain.Claimed{}, false, nil
		case <-time.After(claimPoll):
		}
	}
}

// Prepared is what a running job's source holds: the entries to send to the sandbox.
type Prepared struct {
	JobID   string
	Entries []domain.SourceEntry
}

// PrepareSource reads the files of a running job (after the sync server stored the open
// documents), leaves out figures whose scan has not found them clean and records them on the
// job. domain.ErrNotFound unless the job runs under a lease; domain.ErrInputTooLarge when the
// files pass the input limit, after the job failed.
func (s *Service) PrepareSource(ctx context.Context, jobID string) (Prepared, error) {
	c, err := s.d.Store.Running(ctx, jobID)
	if err != nil {
		return Prepared{}, err
	}
	src, err := s.d.Sources.Source(ctx, c.ProjectID)
	if err != nil {
		return Prepared{}, err
	}
	p := Prepared{JobID: jobID}
	skipped := []domain.Skipped{}
	var total int64
	for _, e := range src.Entries {
		if e.Kind == domain.EntryFigure {
			if reason, skip := skipReason(e.Scan); skip {
				skipped = append(skipped, domain.Skipped{Path: e.Path, Reason: reason})
				continue
			}
		}
		total += e.Size
		p.Entries = append(p.Entries, e)
	}
	if total > s.d.Config.MaxInputBytes {
		if err := s.fail(ctx, jobID, domain.FailInputTooLarge); err != nil {
			return Prepared{}, err
		}
		return Prepared{}, domain.ErrInputTooLarge
	}
	if len(skipped) > 200 {
		skipped = skipped[:200]
	}
	if err := s.d.Store.SetSource(ctx, jobID, skipped, src.LiveConfirmed); err != nil {
		return Prepared{}, err
	}
	return p, nil
}

func skipReason(scan string) (string, bool) {
	switch scan {
	case "clean":
		return "", false
	case "infected":
		return domain.SkipInfected, true
	case "failed":
		return domain.SkipScanFailed, true
	default:
		return domain.SkipScanPending, true
	}
}

// WriteSource writes the prepared entries to w as a POSIX tar at their project paths: folders,
// text files and figures, regular files only, owned by nobody in particular. A figure whose
// object is gone since it was listed is left out.
func (s *Service) WriteSource(ctx context.Context, p Prepared, w io.Writer) error {
	tw := tar.NewWriter(w)
	for _, e := range p.Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		h := &tar.Header{Name: e.Path, ModTime: e.UpdatedAt.UTC(), Format: tar.FormatPAX}
		switch e.Kind {
		case domain.EntryFolder:
			h.Typeflag, h.Name, h.Mode = tar.TypeDir, e.Path+"/", 0o755
			if err := tw.WriteHeader(h); err != nil {
				return err
			}
		case domain.EntryText:
			h.Typeflag, h.Mode, h.Size = tar.TypeReg, 0o644, int64(len(e.Content))
			if err := tw.WriteHeader(h); err != nil {
				return err
			}
			if _, err := io.WriteString(tw, e.Content); err != nil {
				return err
			}
		case domain.EntryFigure:
			if err := s.writeFigure(ctx, tw, h, e); err != nil {
				return err
			}
		}
	}
	return tw.Close()
}

func (s *Service) writeFigure(ctx context.Context, tw *tar.Writer, h *tar.Header, e domain.SourceEntry) error {
	r, err := s.d.Objects.Get(ctx, e.ObjectKey)
	if errors.Is(err, domain.ErrObjectMissing) {
		s.d.Logger.WarnContext(ctx, "figure missing from storage, left out of the compile", slog.String("object_key", e.ObjectKey))
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	h.Typeflag, h.Mode, h.Size = tar.TypeReg, 0o644, e.Size
	if err := tw.WriteHeader(h); err != nil {
		return err
	}
	if _, err := io.CopyN(tw, r, e.Size); err != nil {
		return fmt.Errorf("copy figure %s: %w", e.ObjectKey, err)
	}
	return nil
}

// pdfMagic starts every PDF.
var pdfMagic = []byte("%PDF-")

// StorePDF stores the PDF a running job made: at most MaxPDFBytes (domain.ErrPDFTooLarge),
// starting with %PDF- (domain.ErrNotPDF). It is spooled to a temporary file first, so the
// object is written with its size and never held in memory. domain.ErrNotFound unless the job
// runs under a lease.
func (s *Service) StorePDF(ctx context.Context, jobID string, body io.Reader) error {
	c, err := s.d.Store.Running(ctx, jobID)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp("", "compile-*.pdf")
	if err != nil {
		return fmt.Errorf("spool PDF: %w", err)
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}()
	n, err := io.Copy(f, io.LimitReader(body, s.d.Config.MaxPDFBytes+1))
	if err != nil {
		return fmt.Errorf("spool PDF: %w", err)
	}
	if n > s.d.Config.MaxPDFBytes {
		return domain.ErrPDFTooLarge
	}
	head := make([]byte, len(pdfMagic))
	if _, err := f.ReadAt(head, 0); err != nil || !bytes.Equal(head, pdfMagic) {
		return domain.ErrNotPDF
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	key := fmt.Sprintf("projects/%s/compiles/%s-%s.pdf", c.ProjectID, jobID, hex.EncodeToString(raw))
	if err := s.d.Objects.Put(ctx, key, bufio.NewReader(f), n, "application/pdf"); err != nil {
		return err
	}
	if err := s.d.Store.AttachPDF(ctx, jobID, domain.NewPDF{ObjectKey: key, Size: n}); err != nil {
		if derr := s.d.Objects.Delete(context.WithoutCancel(ctx), key); derr != nil {
			s.d.Logger.WarnContext(ctx, "unrecorded PDF not deleted; the cleanup cannot find it", slog.String("object_key", key), slog.Any("error", derr))
		}
		return err
	}
	return nil
}

// Finish records how a job ended (FR-13.1, FR-13.2, FR-13.5): the status, the errors parsed
// from the logs, the log; it keeps the project's records, log and PDF as SRS 7 says and tells
// the members. A job that already finished is left as it is (nil), except one that ended as
// worker_lost or cancelled (domain.ErrNotFound): its slot may already be another job's.
// *domain.FieldError for a result that breaks the contract.
func (s *Service) Finish(ctx context.Context, jobID string, r domain.Result) error {
	if err := validResult(r); err != nil {
		return err
	}
	var done domain.Job
	err := s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		j, err := s.d.Store.LockRunning(ctx, jobID)
		if err != nil {
			return err
		}
		if j.Status != domain.StatusRunning {
			if j.Failure != nil && (*j.Failure == domain.FailWorkerLost || *j.Failure == domain.FailQueueExpired) {
				return domain.ErrNotFound
			}
			return nil
		}
		paths, err := s.d.Store.TextPaths(ctx, j.ProjectID)
		if err != nil {
			return err
		}
		problems, pages := ParseLogs(r.Log, r.BibtexLog, paths)
		f := domain.Finish{ExitCode: r.ExitCode, DurationMs: int(r.Duration.Milliseconds()), Problems: problems}
		logText := truncateLog(r.Log, MaxLogBytes)
		f.Log = &logText
		switch r.Outcome {
		case domain.OutcomeCompleted:
			if j.HasPDF {
				f.Status, f.Pages = domain.StatusSucceeded, pages
			} else {
				f.Status, f.Failure = domain.StatusFailed, ptr(domain.FailNoPDF)
			}
		case domain.OutcomeTimedOut:
			f.Status = domain.StatusTimedOut
		default:
			f.Status, f.Failure = domain.StatusFailed, r.Failure
			if f.Failure == nil {
				f.Failure = ptr(domain.FailSandbox)
			}
		}
		done, err = s.d.Store.Finish(ctx, jobID, f)
		if err != nil {
			return err
		}
		pdfJob := ""
		if f.Status == domain.StatusSucceeded {
			pdfJob = jobID
		} else if pdfJob, err = s.d.Store.LatestPDFJob(ctx, j.ProjectID); err != nil {
			return err
		}
		if err := s.d.Store.Retain(ctx, j.ProjectID, jobID, pdfJob, KeepJobs); err != nil {
			return err
		}
		s.publish(ctx, done)
		return nil
	})
	return err
}

func ptr[T any](v T) *T { return &v }

func validResult(r domain.Result) error {
	switch r.Outcome {
	case domain.OutcomeCompleted, domain.OutcomeTimedOut:
		if r.Failure != nil {
			return &domain.FieldError{Field: "failure"}
		}
	case domain.OutcomeFailed:
		if r.Failure != nil {
			switch *r.Failure {
			case domain.FailOutputTooLarge, domain.FailOutOfMemory, domain.FailSandbox:
			default:
				return &domain.FieldError{Field: "failure"}
			}
		}
	default:
		return &domain.FieldError{Field: "outcome"}
	}
	switch {
	case r.Duration < 0:
		return &domain.FieldError{Field: "durationMs"}
	case len(r.Log) > MaxLogBytes+4096:
		return &domain.FieldError{Field: "log"}
	case len(r.BibtexLog) > MaxBibtexLogBytes+4096:
		return &domain.FieldError{Field: "bibtexLog"}
	}
	return nil
}

// cutNote ends a log that was cut at its limit.
const cutNote = "\n[ResearchSphere: the log was cut here at 2 MiB.]\n"

// truncateLog keeps the beginning of a log within max bytes of valid UTF-8 (the database's
// compile_jobs_log_size), ending with a note when it was cut.
func truncateLog(s string, maxBytes int) string {
	s = strings.ToValidUTF8(s, "�")
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes - len(cutNote)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + cutNote
}

// fail ends a running job without a PDF for a reason the worker never saw (input too large).
func (s *Service) fail(ctx context.Context, jobID string, why domain.Failure) error {
	return s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		j, err := s.d.Store.LockRunning(ctx, jobID)
		if err != nil {
			return err
		}
		if j.Status != domain.StatusRunning {
			return domain.ErrNotFound
		}
		done, err := s.d.Store.Finish(ctx, jobID, domain.Finish{Status: domain.StatusFailed, Failure: &why})
		if err != nil {
			return err
		}
		s.publish(ctx, done)
		return nil
	})
}

// Sweep fails running jobs whose lease ended (worker_lost) and cancels jobs queued longer than
// QueueExpiry, so a stopped worker never holds a project's compile slot; it tells the members.
func (s *Service) Sweep(ctx context.Context) (int, error) {
	lost, err := s.d.Store.ExpireLeases(ctx)
	if err != nil {
		return 0, err
	}
	expired, err := s.d.Store.ExpireQueued(ctx, s.d.Now().Add(-QueueExpiry))
	if err != nil {
		return len(lost), err
	}
	for _, j := range append(lost, expired...) {
		s.publish(ctx, j)
	}
	return len(lost) + len(expired), nil
}

// RunSweep runs Sweep every interval until ctx ends.
func (s *Service) RunSweep(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := s.Sweep(ctx); err != nil {
				s.d.Logger.ErrorContext(ctx, "compile sweep failed", slog.Any("error", err))
			} else if n > 0 {
				s.d.Logger.WarnContext(ctx, "compile jobs ended by the sweep", slog.Int("jobs", n))
			}
		}
	}
}
