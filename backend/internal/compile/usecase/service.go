// Package usecase holds the compile module's business logic (FR-13.1, FR-13.2, FR-13.4 to
// FR-13.6; slice 23, docs/compile.md): members queue compiles and read their results; the
// compile worker claims jobs, reads their files, stores the PDF and reports how they ended.
package usecase

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/compile/domain"
)

// Limits and timings (docs/compile.md).
const (
	// CompilesPerWindow compiles a user may start in CompileWindow.
	CompilesPerWindow = 10
	CompileWindow     = time.Minute
	// Lease is how long a claimed job may run before it fails as worker_lost: the 60 s hard
	// limit (FR-13.5), reading the files and storing the PDF.
	Lease = 120 * time.Second
	// ClaimWait is how long a claim waits for a queued job; the internal listener's write
	// timeout is 15 s.
	ClaimWait = 10 * time.Second
	// claimPoll is how often a waiting claim looks again.
	claimPoll = 500 * time.Millisecond
	// WorkerTTL: a compile is refused as unavailable when no worker asked for work this long.
	WorkerTTL = 30 * time.Second
	// QueueExpiry cancels a job no worker took in this time, so a stopped worker never
	// holds a project's one compile slot for good.
	QueueExpiry = 5 * time.Minute
	// KeepJobs is how many compile records a project keeps (SRS 7, Retention).
	KeepJobs = 10
	// DefaultQueueMax is how many compiles may wait at once (COMPILE_QUEUE_MAX).
	DefaultQueueMax = 50
	// DefaultMaxPDFBytes is the largest PDF (COMPILE_MAX_PDF_BYTES); a single figure may be
	// 50 MB (FR-12.2). MaxPDFCeiling is the database's (stored_files_pdf_size).
	DefaultMaxPDFBytes = 100 << 20
	MaxPDFCeiling      = 200 << 20
	// DefaultMaxInputBytes is the most a compile reads (COMPILE_MAX_INPUT_BYTES); it lives in
	// the sandbox's memory-backed /work (docs/compile.md).
	DefaultMaxInputBytes = 384 << 20
	// MaxLogBytes and MaxBibtexLogBytes cap the logs the worker reports.
	MaxLogBytes       = 2 << 20
	MaxBibtexLogBytes = 256 << 10
	maxWorkerID       = 100
)

// Config holds the settings; a zero field takes its default.
type Config struct {
	QueueMax      int
	MaxPDFBytes   int64
	MaxInputBytes int64
	ClaimWait     time.Duration // tests shorten it; 0 means ClaimWait, below 0 no wait
}

// Deps is what the Service needs. Workers, Limiter and Publisher may be nil: then every
// compile is accepted as if a worker were alive, nothing is counted and nobody is told.
type Deps struct {
	Store     domain.Store
	Tx        domain.Transactor
	Access    domain.Access
	Sources   domain.Sources
	Objects   domain.Objects
	Workers   domain.Workers
	Limiter   domain.Limiter
	Publisher domain.Publisher
	Logger    *slog.Logger
	Now       func() time.Time
	Config    Config
}

// Service runs the compile operations.
type Service struct {
	d Deps
}

// New returns a Service.
func New(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	c := &d.Config
	if c.QueueMax <= 0 {
		c.QueueMax = DefaultQueueMax
	}
	if c.MaxPDFBytes <= 0 {
		c.MaxPDFBytes = DefaultMaxPDFBytes
	}
	if c.MaxInputBytes <= 0 {
		c.MaxInputBytes = DefaultMaxInputBytes
	}
	switch {
	case c.ClaimWait == 0:
		c.ClaimWait = ClaimWait
	case c.ClaimWait < 0:
		c.ClaimWait = 0
	}
	return &Service{d: d}
}

// Start queues a compile of the project's main file (FR-13.1, FR-13.4). Errors, in order:
// *domain.RateLimitedError after CompilesPerWindow in CompileWindow; the access errors
// (domain.ErrNotFound for a non-member, domain.ErrForbidden for a viewer,
// *domain.SuspendedError); domain.ErrMainFileMissing; domain.ErrUnavailable when no worker
// asked for work in WorkerTTL; domain.ErrBusy over the queue limit; *domain.RunningError
// while another compile of the project is queued or running. Only a queued compile counts
// against the limit.
func (s *Service) Start(ctx context.Context, userID, projectID string) (domain.Job, error) {
	if s.d.Limiter != nil {
		used, left, err := s.d.Limiter.Used(ctx, userID)
		switch {
		case err != nil:
			s.d.Logger.WarnContext(ctx, "compile limit unavailable, allowing it", slog.Any("error", err))
		case used >= CompilesPerWindow:
			return domain.Job{}, &domain.RateLimitedError{RetryAfter: left}
		}
	}
	if err := s.d.Access.Authorize(ctx, userID, projectID, domain.NeedCompile); err != nil {
		return domain.Job{}, err
	}
	main, engine, exists, err := s.d.Store.Project(ctx, projectID)
	if err != nil {
		return domain.Job{}, err
	}
	if !exists {
		return domain.Job{}, domain.ErrMainFileMissing
	}
	if s.d.Workers != nil {
		alive, err := s.d.Workers.Alive(ctx)
		switch {
		case err != nil:
			s.d.Logger.WarnContext(ctx, "compile worker check failed, queueing anyway", slog.Any("error", err))
		case !alive:
			return domain.Job{}, domain.ErrUnavailable
		}
	}
	n, err := s.d.Store.QueuedCount(ctx)
	if err != nil {
		return domain.Job{}, err
	}
	if n >= s.d.Config.QueueMax {
		return domain.Job{}, domain.ErrBusy
	}
	j, err := s.d.Store.Insert(ctx, domain.NewJob{ProjectID: projectID, RequestedBy: userID, Engine: engine, MainFilePath: main})
	if err != nil {
		return domain.Job{}, err
	}
	if s.d.Limiter != nil {
		if err := s.d.Limiter.Record(ctx, userID, CompileWindow); err != nil {
			s.d.Logger.WarnContext(ctx, "compile limit not recorded", slog.Any("error", err))
		}
	}
	s.publish(ctx, j)
	return j, nil
}

// Latest is the project's newest compile, for a member (FR-13.2). domain.ErrNotFound for
// others, domain.ErrNoCompile when there is none.
func (s *Service) Latest(ctx context.Context, userID, projectID string) (domain.Job, error) {
	if err := s.d.Access.Authorize(ctx, userID, projectID, domain.NeedRead); err != nil {
		return domain.Job{}, err
	}
	return s.d.Store.Latest(ctx, projectID)
}

// Get is one compile of the project, for a member. domain.ErrNotFound for others and for a
// compile that is not kept or not this project's.
func (s *Service) Get(ctx context.Context, userID, projectID, jobID string) (domain.Job, error) {
	if err := s.d.Access.Authorize(ctx, userID, projectID, domain.NeedRead); err != nil {
		return domain.Job{}, err
	}
	return s.d.Store.Get(ctx, projectID, jobID)
}

// Log is a compile's log, for a member (FR-13.2). domain.ErrNoLog when it is not kept.
func (s *Service) Log(ctx context.Context, userID, projectID, jobID string) (string, error) {
	if err := s.d.Access.Authorize(ctx, userID, projectID, domain.NeedRead); err != nil {
		return "", err
	}
	return s.d.Store.Log(ctx, projectID, jobID)
}

// OpenPDF opens the project's latest PDF for a member (FR-13.6). Close the reader.
// domain.ErrNoPDF when there is none (or its object is gone).
func (s *Service) OpenPDF(ctx context.Context, userID, projectID string) (domain.PDF, io.ReadCloser, error) {
	if err := s.d.Access.Authorize(ctx, userID, projectID, domain.NeedRead); err != nil {
		return domain.PDF{}, nil, err
	}
	p, err := s.d.Store.LatestPDF(ctx, projectID)
	if err != nil {
		return domain.PDF{}, nil, err
	}
	r, err := s.d.Objects.Get(ctx, p.ObjectKey)
	if errors.Is(err, domain.ErrObjectMissing) {
		s.d.Logger.ErrorContext(ctx, "compiled PDF missing from storage", slog.String("compile_id", p.CompileID))
		return domain.PDF{}, nil, domain.ErrNoPDF
	}
	if err != nil {
		return domain.PDF{}, nil, err
	}
	return p, r, nil
}

// publish tells the project's members about the job, after the commit of the transaction the
// context carries.
func (s *Service) publish(ctx context.Context, j domain.Job) {
	if s.d.Publisher == nil {
		return
	}
	ids, err := s.d.Store.MemberIDs(ctx, j.ProjectID)
	if err != nil {
		s.d.Logger.WarnContext(ctx, "compile event not sent: members unknown", slog.Any("error", err))
		return
	}
	s.d.Publisher.CompileUpdated(ctx, j, ids)
}
