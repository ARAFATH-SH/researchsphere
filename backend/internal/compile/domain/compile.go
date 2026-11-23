// Package domain holds the compile module's entities and the interfaces its usecase needs
// (FR-13.1, FR-13.2, FR-13.4 to FR-13.6, NFR-3, NFR-4; slice 23, docs/compile.md). The API
// never runs LaTeX: it queues jobs in compile_jobs, and the compile worker (compile/) claims
// them through the internal operations and runs each in a sandbox container.
package domain

import (
	"context"
	"errors"
	"io"
	"time"
)

// Status is a compile job's state. The values match compile_jobs.status.
type Status string

// Statuses.
const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded" // a PDF was made, even with errors in the log
	StatusFailed    Status = "failed"    // no PDF was made
	StatusTimedOut  Status = "timed_out" // stopped at 60 s (FR-13.5)
	StatusCancelled Status = "cancelled" // no worker took it in time
)

// Active reports whether the job still holds its project's one compile slot (FR-13.4).
func (s Status) Active() bool { return s == StatusQueued || s == StatusRunning }

// Failure says why a compile ended without a PDF when the log alone does not. The values
// match compile_jobs.failure.
type Failure string

// Failures.
const (
	FailInputTooLarge  Failure = "input_too_large"
	FailOutputTooLarge Failure = "output_too_large"
	FailOutOfMemory    Failure = "out_of_memory"
	FailNoPDF          Failure = "no_pdf"
	FailWorkerLost     Failure = "worker_lost"
	FailSandbox        Failure = "sandbox_error"
	FailQueueExpired   Failure = "queue_expired"
)

// Valid reports whether f is one of the failures.
func (f Failure) Valid() bool {
	switch f {
	case FailInputTooLarge, FailOutputTooLarge, FailOutOfMemory, FailNoPDF, FailWorkerLost, FailSandbox, FailQueueExpired:
		return true
	}
	return false
}

// EnginePDFLaTeX is the only engine for now (FR-13.1); compile_jobs.engine and
// projects.compile_engine allow only it, so XeLaTeX can be added later (decisions.md).
const EnginePDFLaTeX = "pdflatex"

// Severity of a problem in the log.
type Severity string

// Severities.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Problem is one error or warning from the log (FR-13.2). File is a project path, nil when
// the problem is not in a project file; Line is nil when the log gives none.
type Problem struct {
	File     *string  `json:"file"`
	Line     *int     `json:"line"`
	Message  string   `json:"message"`
	Severity Severity `json:"severity"`
}

// Skip reasons: a figure left out of a compile because its scan has not found it clean.
const (
	SkipScanPending = "scan_pending"
	SkipScanFailed  = "scan_failed"
	SkipInfected    = "infected"
)

// Skipped is a figure left out of a compile.
type Skipped struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Job is a compile job as members see it.
type Job struct {
	ID                string
	ProjectID         string
	RequestedBy       *string // nil when the account is gone
	Status            Status
	Engine            string
	MainFilePath      string
	QueuedAt          time.Time
	StartedAt         *time.Time
	FinishedAt        *time.Time
	DurationMs        *int
	Failure           *Failure
	Problems          []Problem
	Skipped           []Skipped
	LiveTextConfirmed bool
	Pages             *int
	HasLog            bool
	HasPDF            bool
}

// NewJob is a compile to queue.
type NewJob struct {
	ProjectID    string
	RequestedBy  string
	Engine       string
	MainFilePath string
}

// Claimed is a job a worker took: what it runs.
type Claimed struct {
	ID           string
	ProjectID    string
	MainFilePath string
	Engine       string
}

// Outcome is how the sandbox run ended, as the worker saw it.
type Outcome string

// Outcomes.
const (
	OutcomeCompleted Outcome = "completed" // LaTeX ran to its end, with or without errors
	OutcomeTimedOut  Outcome = "timed_out" // killed at 60 s
	OutcomeFailed    Outcome = "failed"    // the run could not complete; Result.Failure says why
)

// Result is what the worker reports when a run ends.
type Result struct {
	Outcome   Outcome
	Failure   *Failure
	ExitCode  *int
	Duration  time.Duration
	Log       string
	BibtexLog string
}

// Finish is what the usecase records for a finished job.
type Finish struct {
	Status     Status
	Failure    *Failure
	ExitCode   *int
	DurationMs int
	Log        *string
	Problems   []Problem
	Pages      *int
}

// EntryKind is what a source entry is.
type EntryKind string

// Entry kinds; they match the project module's file kinds.
const (
	EntryText   EntryKind = "text"
	EntryFigure EntryKind = "binary"
	EntryFolder EntryKind = "folder"
)

// SourceEntry is one entry of a project as a compile reads it. A figure has ObjectKey and
// Scan (pending, clean, infected or failed); a text file has Content.
type SourceEntry struct {
	Path      string
	Kind      EntryKind
	Size      int64
	Content   string
	ObjectKey string
	Scan      string
	UpdatedAt time.Time
}

// Source is what a compile reads, from the project module.
type Source struct {
	MainFilePath  string
	Entries       []SourceEntry // sorted by path
	LiveConfirmed bool
}

// PDF is the project's latest PDF (FR-13.6).
type PDF struct {
	CompileID  string
	ObjectKey  string
	Size       int64
	FinishedAt time.Time
	Title      string // the project's, for the file name
}

// NewPDF is a PDF a running job made, to record.
type NewPDF struct {
	StoredFileID string
	ObjectKey    string
	Size         int64
}

// Errors.
var (
	// ErrNotFound covers a project the caller may not see (non-members, an administrator
	// included) and a job that does not exist, belongs to another project or is not in the
	// state the operation needs.
	ErrNotFound = errors.New("compile: not found")
	// ErrForbidden refuses a member whose role does not allow the action.
	ErrForbidden = errors.New("compile: forbidden")
	// ErrMainFileMissing refuses a compile when no text file is at the main file's path.
	ErrMainFileMissing = errors.New("compile: the main file does not exist")
	// ErrUnavailable refuses a compile when no worker asked for work lately (NFR-10.2).
	ErrUnavailable = errors.New("compile: the compile service is unavailable")
	// ErrBusy refuses a compile when too many are queued.
	ErrBusy = errors.New("compile: too many compiles are waiting")
	// ErrNoCompile: the project was never compiled.
	ErrNoCompile = errors.New("compile: no compile")
	// ErrNoLog: the compile's log is not kept (SRS 7, Retention) or it has not finished.
	ErrNoLog = errors.New("compile: no log")
	// ErrNoPDF: the project has no PDF.
	ErrNoPDF = errors.New("compile: no PDF")
	// ErrInputTooLarge: the project's files pass the compile's input limit; the job failed.
	ErrInputTooLarge = errors.New("compile: the files are too large to compile")
	// ErrPDFTooLarge refuses a PDF over the PDF limit.
	ErrPDFTooLarge = errors.New("compile: the PDF is too large")
	// ErrNotPDF refuses a body that does not start like a PDF.
	ErrNotPDF = errors.New("compile: not a PDF")
)

// RunningError refuses a compile while another of the project is queued or running
// (FR-13.4); ID names it.
type RunningError struct {
	ID string
}

func (e *RunningError) Error() string { return "compile: a compile of this project is running" }

// RateLimitedError refuses a compile over the user's limit.
type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string { return "compile: rate limited" }

// SuspendedError refuses a suspended account.
type SuspendedError struct {
	Reason *string
	Until  *time.Time
}

func (e *SuspendedError) Error() string { return "compile: account suspended" }

// FieldError names a body field the worker sent wrong.
type FieldError struct {
	Field string
}

func (e *FieldError) Error() string { return "compile: invalid " + e.Field }

// Store keeps compile jobs. Every method runs on the transaction the context carries, if any.
type Store interface {
	// Project reads the project's main file path and engine, and whether a text file is at
	// that path (ignoring letter case); ErrNotFound when there is no project.
	Project(ctx context.Context, projectID string) (mainFile, engine string, mainExists bool, err error)
	// QueuedCount counts the queued jobs of every project.
	QueuedCount(ctx context.Context) (int, error)
	// Insert queues a job; *RunningError when the project has an active one.
	Insert(ctx context.Context, j NewJob) (Job, error)
	// Latest is the project's newest job; ErrNoCompile when there is none.
	Latest(ctx context.Context, projectID string) (Job, error)
	// Get is a job of the project; ErrNotFound when there is none.
	Get(ctx context.Context, projectID, jobID string) (Job, error)
	// Log is the job's log; ErrNoLog when none is kept, ErrNotFound when there is no job.
	Log(ctx context.Context, projectID, jobID string) (string, error)
	// LatestPDF is the project's PDF; ErrNoPDF when there is none.
	LatestPDF(ctx context.Context, projectID string) (PDF, error)
	// Claim takes the oldest queued job: running, started now, leased to workerID until
	// lease from now. false when none is queued.
	Claim(ctx context.Context, workerID string, lease time.Duration) (Claimed, bool, error)
	// Running reads a running job whose lease has not expired; ErrNotFound otherwise.
	Running(ctx context.Context, jobID string) (Claimed, error)
	// SetSource records what the compile left out and whether live text was confirmed.
	SetSource(ctx context.Context, jobID string, skipped []Skipped, liveConfirmed bool) error
	// AttachPDF records the PDF as the project's stored file and the job's, replacing one it
	// had, while the job runs under a lease; ErrNotFound otherwise.
	AttachPDF(ctx context.Context, jobID string, p NewPDF) error
	// LockRunning locks the job for its finish: ErrNotFound when there is no such job, and
	// the job as it is (any status) otherwise.
	LockRunning(ctx context.Context, jobID string) (Job, error)
	// Finish records how the job ended and returns it.
	Finish(ctx context.Context, jobID string, f Finish) (Job, error)
	// Retain keeps the project's newest keep jobs and the one holding the PDF, the log of
	// newest only, and the PDF of pdfJob only (none when empty); the database queues the
	// objects of the PDFs it drops.
	Retain(ctx context.Context, projectID, newest, pdfJob string, keep int) error
	// LatestPDFJob is the ID of the project's job that holds its PDF, "" when none does.
	LatestPDFJob(ctx context.Context, projectID string) (string, error)
	// ExpireLeases fails running jobs whose lease ended before now as worker_lost.
	ExpireLeases(ctx context.Context) ([]Job, error)
	// ExpireQueued cancels jobs queued before before as queue_expired.
	ExpireQueued(ctx context.Context, before time.Time) ([]Job, error)
	// TextPaths lists the project's text file paths, to tell project files in the log.
	TextPaths(ctx context.Context, projectID string) ([]string, error)
	// MemberIDs lists the project's members, who are told about its compiles.
	MemberIDs(ctx context.Context, projectID string) ([]string, error)
	// OutputBefore lists the compiles that started before before and still hold a PDF or a
	// log; ClearOutputBefore drops those PDFs (their objects are queued for deletion) and
	// logs and returns what it dropped (cmd/purgecompileoutput).
	OutputBefore(ctx context.Context, before time.Time) ([]Output, error)
	ClearOutputBefore(ctx context.Context, before time.Time) ([]Output, error)
}

// Output is a compile's stored output: whether it holds a PDF and a log.
type Output struct {
	JobID      string
	ProjectID  string
	FinishedAt *time.Time
	HasPDF     bool
	HasLog     bool
}

// Need is what a caller wants to do in a project.
type Need int

// Needs, by SRS 2.3's project table.
const (
	// NeedRead is the row "Open the workspace, read files, view the PDF".
	NeedRead Need = iota
	// NeedCompile is the row "Compile, use the assistant, edit the abstract".
	NeedCompile
)

// Access asks the project module whether userID may do need in the project now. Errors:
// ErrNotFound (not a member, or no project), ErrForbidden, *SuspendedError (compiling only).
type Access interface {
	Authorize(ctx context.Context, userID, projectID string, need Need) error
}

// Sources reads what a compile of the project needs from the project module, after the
// sync server stored the open documents. ErrNotFound when there is no project.
type Sources interface {
	Source(ctx context.Context, projectID string) (Source, error)
}

// ErrObjectMissing is the bucket's answer for an object that is not there.
var ErrObjectMissing = errors.New("compile: object missing")

// Objects is the bucket.
type Objects interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	// Get reads the object; ErrObjectMissing when there is none.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// Workers records that a compile worker asked for work, and tells whether one did lately.
type Workers interface {
	Seen(ctx context.Context, ttl time.Duration) error
	Alive(ctx context.Context) (bool, error)
}

// Limiter counts a user's compiles in fixed windows.
type Limiter interface {
	Used(ctx context.Context, userID string) (used int, left time.Duration, err error)
	Record(ctx context.Context, userID string, window time.Duration) error
}

// Publisher tells the members about a compile (docs/realtime.md, compile.updated). It
// publishes after the commit of the transaction the context carries and never fails.
type Publisher interface {
	CompileUpdated(ctx context.Context, j Job, to []string)
}

// Transactor runs fn in one transaction.
type Transactor interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}
