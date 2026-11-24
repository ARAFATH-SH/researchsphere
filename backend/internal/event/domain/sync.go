package domain

import (
	"context"
	"errors"
	"time"
)

// The automatic venue import (FR-14.5; docs/venues-sync.md): conference editions from open
// deadline repositories, journals from OpenAlex.

// RepoState is what a repository feed last read: the commit, and the ETag of the request
// that found it, for a conditional request next time.
type RepoState struct {
	CommitSHA string
	ETag      string
}

// RepoFile is one data file of a repository at a commit.
type RepoFile struct {
	Path    string
	Content string
}

// RepoFetch is what a repository feed found. When Changed is false the commit is the one in
// the state given, and the stored files are still current. Otherwise Full says whether Files
// is every data file (replace the stored set) or only the files changed since the commit
// given, with the paths of the files that were deleted.
type RepoFetch struct {
	State   RepoState
	Changed bool
	Full    bool
	Files   []RepoFile
	Deleted []string
}

// Round is one submission round of a conference edition: local times as the source states
// them ("YYYY-MM-DD HH:MM:SS" or "TBD").
type Round struct {
	Deadline         string
	AbstractDeadline string
}

// Edition is one edition of a conference as a repository lists it, before mapping.
type Edition struct {
	File     string // the data file it came from
	ID       string // the source's own ID of the edition, such as sigmod26
	Title    string // the short name, such as SIGMOD
	FullName string
	Year     int
	Link     string
	Areas    []string // the source's area codes, such as AI or HCI
	Ranks    string   // such as "CCF A, CORE A*"
	Rounds   []Round
	TimeZone string // as the source states it: AoE, UTC-12, PT, an IANA name
	Place    string
	StartsOn string // YYYY-MM-DD, or ""
	EndsOn   string
	Note     string
}

// FileError is a data file that could not be read.
type FileError struct {
	Path    string
	Message string
}

// RepoFeed reads conference editions from one repository.
type RepoFeed interface {
	// Fetch reads what changed since prev (zero for the first run).
	Fetch(ctx context.Context, prev RepoState) (RepoFetch, error)
	// Parse turns data files into editions, with an error for each file it cannot read.
	Parse(files []RepoFile) ([]Edition, []FileError)
}

// JournalTopic is one of a journal's main topics, with its place in OpenAlex's hierarchy.
type JournalTopic struct {
	Name       string
	SubfieldID int
	FieldID    int
}

// Journal is a journal as OpenAlex describes it, before mapping.
type Journal struct {
	ID          string // such as S137773608
	Name        string
	HomepageURL string
	ISSNL       string
	Publisher   string
	WorksCount  int
	Topics      []JournalTopic // most associated first
}

// JournalFeed reads the journals the import considers.
type JournalFeed interface {
	Journals(ctx context.Context) ([]Journal, error)
}

// LinkCheck is the result of checking that a link answers over https.
type LinkCheck struct {
	OK          bool
	FinalStatus int // the last HTTP status, or 0 when no answer came
	CheckedAt   time.Time
}

// LinkChecker checks that an https link answers.
type LinkChecker interface {
	Check(ctx context.Context, url string) LinkCheck
}

// SyncTrigger says what started a run.
type SyncTrigger string

// Triggers.
const (
	TriggerSchedule SyncTrigger = "schedule"
	TriggerOnDemand SyncTrigger = "on_demand"
	TriggerCommand  SyncTrigger = "command"
)

// SyncStatus is the state of a run.
type SyncStatus string

// Statuses.
const (
	SyncRunning   SyncStatus = "running"
	SyncSucceeded SyncStatus = "succeeded"
	SyncFailed    SyncStatus = "failed"
)

// SyncReject is an entry a run rejected, with every reason.
type SyncReject struct {
	SourceKey string
	Name      string
	Problems  []Problem
}

// SyncRun is one run of one source.
type SyncRun struct {
	ID          string
	Source      Source
	DryRun      bool
	Trigger     SyncTrigger
	RequestedBy string
	StartedAt   time.Time
	FinishedAt  time.Time
	Status      SyncStatus
	CommitSHA   string
	Created     int
	Updated     int
	Unchanged   int
	Skipped     int
	Rejected    int
	Errors      []string
	Rejects     []SyncReject
}

// SourceState is the stored state of one source.
type SourceState struct {
	Source          Source
	Repo            RepoState
	LeaseUntil      time.Time
	LastScheduledAt time.Time
	LastRunAt       time.Time // when the last real (not dry) run started
}

// Errors of starting a run.
var (
	ErrSyncRunning = errors.New("a run of this source is in progress")
	ErrSyncTooSoon = errors.New("the last real run of this source was too recent")
	ErrSyncNotDue  = errors.New("the scheduled run of this source is not due")
)

// SyncStore keeps the import's own state. Every method runs on the transaction the context
// carries, if any.
type SyncStore interface {
	// LockSource returns a source's state, locked until the transaction ends.
	LockSource(ctx context.Context, source Source) (SourceState, error)
	// SaveSource stores the lease, schedule and last run times of a source.
	SaveSource(ctx context.Context, s SourceState) error
	// SaveRepoState stores what a repository feed last read.
	SaveRepoState(ctx context.Context, source Source, s RepoState) error
	// Sources returns every source's state.
	Sources(ctx context.Context) ([]SourceState, error)
	// Files returns the stored data files of a source.
	Files(ctx context.Context, source Source) ([]RepoFile, error)
	// SaveFiles stores data files: with full, they replace every stored file of the source;
	// otherwise files are added or replaced and the deleted paths removed.
	SaveFiles(ctx context.Context, source Source, full bool, files []RepoFile, deleted []string) error
	// StartRun stores a new run, running, and returns its ID.
	StartRun(ctx context.Context, r SyncRun) (string, error)
	// FinishRun stores a run's outcome and keeps only the last runs of its source.
	FinishRun(ctx context.Context, r SyncRun) error
	// FailStaleRuns marks the runs of a source still running, whose lease has expired, failed.
	FailStaleRuns(ctx context.Context, source Source, at time.Time) error
	// LatestRun returns the latest run of a source, real or dry; found is false when none.
	LatestRun(ctx context.Context, source Source, dryRun bool) (r SyncRun, found bool, err error)
	// LinkChecks returns the stored checks of these links.
	LinkChecks(ctx context.Context, urls []string) (map[string]LinkCheck, error)
	// SaveLinkChecks stores checks.
	SaveLinkChecks(ctx context.Context, checks map[string]LinkCheck) error
}
