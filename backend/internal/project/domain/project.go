// Package domain holds the project module's entities and the interfaces its usecase needs
// (FR-10, FR-15.11): research projects, their required skills, their members' roles and
// the reports users make of them. Standard library only.
package domain

import (
	"context"
	"errors"
	"time"
)

// Status is a project's status. The values match the project_status enum (FR-10.2).
type Status string

// Statuses.
const (
	StatusOpen     Status = "open"
	StatusClosed   Status = "closed"
	StatusArchived Status = "archived"
)

// Valid reports whether s is one of the three statuses.
func (s Status) Valid() bool {
	return s == StatusOpen || s == StatusClosed || s == StatusArchived
}

// Role is a user's role in one project. The values match the project_role enum; RoleNone
// stands for having no member row.
type Role string

// Roles.
const (
	RoleNone   Role = ""
	RoleViewer Role = "viewer"
	RoleEditor Role = "editor"
	RoleOwner  Role = "owner"
)

// Action is a project action of SRS 2.3 ("Project level") that this module checks. cmd/api
// maps each to its authz.ProjectAction, so the permission table stays in one place.
type Action string

// Actions.
const (
	// ActView is the row "View the project page (title, description, skills, members)".
	ActView Action = "view"
	// ActEditDetails is the row "Edit project details and status".
	ActEditDetails Action = "edit_details"
	// ActEditAbstract is the row "Compile, use the assistant, edit the abstract".
	ActEditAbstract Action = "edit_abstract"
	// ActDelete is the row "Transfer ownership, delete the project".
	ActDelete Action = "delete"
)

// Permissions answers SRS 2.3's project table (authz.CanInProject).
type Permissions interface {
	Can(role Role, action Action) bool
}

// Errors the usecase returns.
var (
	// ErrNotFound covers a project that does not exist, an ID that is not a UUID, and a
	// project the caller may not see: all three look the same (SRS 2.3).
	ErrNotFound = errors.New("project: not found")
	// ErrForbidden refuses an action the caller's project role does not allow (FR-3.2).
	ErrForbidden = errors.New("project: forbidden")
	// ErrConfirmMismatch refuses a delete whose confirmation is not the title (FR-10.6).
	ErrConfirmMismatch = errors.New("project: the confirmation does not match the title")
	// ErrCannotReportOwn refuses a report of the caller's own project.
	ErrCannotReportOwn = errors.New("project: cannot report own project")
	// ErrAlreadyReported refuses a second open report of one project by one user.
	ErrAlreadyReported = errors.New("project: already reported")
	// ErrReportNotFound covers a report that does not exist or an ID that is not a UUID.
	ErrReportNotFound = errors.New("project: report not found")
	// ErrAlreadyResolved refuses resolving a report twice.
	ErrAlreadyResolved = errors.New("project: report already resolved")
	// ErrInvalidQuery refuses a search text that is missing or not 2 to 100 characters.
	ErrInvalidQuery = errors.New("project: invalid search text")
	// ErrInvalidPage refuses a page or page size out of range.
	ErrInvalidPage = errors.New("project: invalid page")
)

// FieldError refuses one field of a request; Field is its name in the contract.
type FieldError struct {
	Field string
}

func (e *FieldError) Error() string { return "project: invalid " + e.Field }

// RateLimitedError refuses a request over a limit until RetryAfter has passed.
type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string { return "project: rate limited" }

// SuspendedError refuses a write by a user whose account was suspended after their access
// token was issued (decisions.md, 2026-11-05).
type SuspendedError struct {
	Reason *string
	Until  *time.Time // nil when permanent
}

func (e *SuspendedError) Error() string { return "project: the account is suspended" }

// Person is a user as a project shows them.
type Person struct {
	UserID      string
	FullName    string
	Headline    *string
	PhotoFileID *string
}

// Skill is a required skill: a shared skill tag (FR-4.2).
type Skill struct {
	ID   string
	Name string
}

// Project is a project with what its viewer needs to know.
type Project struct {
	ID          string
	Title       string
	Description string
	Abstract    *string
	Status      Status
	Skills      []Skill
	Owner       Person
	MyRole      Role
	MemberCount int
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// Access holds what the visibility rule reads.
	Access Access
}

// Access is what decides whether a user may see a project and what they may do in it.
type Access struct {
	Title          string
	Status         Status
	OwnerID        string
	OwnerAvailable bool // the owner's account is verified and not suspended
	BlockedByOwner bool // the owner has blocked the viewer (FR-16.9)
	Role           Role // the viewer's role
}

// Visible is the project visibility rule (SRS 2.3, docs/decisions.md 2026-11-16): a member
// sees the project whatever its status; anyone else only when it is open or closed, its
// owner's account is usable and the owner has not blocked them.
func (a Access) Visible() bool {
	if a.Role != RoleNone {
		return true
	}
	return a.Status != StatusArchived && a.OwnerAvailable && !a.BlockedByOwner
}

// Summary is a project in a list.
type Summary struct {
	ID          string
	Title       string
	Description string // the first 300 characters
	Status      Status
	Skills      []string // up to SummarySkills names
	Owner       Person
	MyRole      Role
	MatchedOn   string // search only: title, skill or description
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SummarySkills is how many skill names a Summary carries.
const SummarySkills = 5

// NewProject is a project to create.
type NewProject struct {
	OwnerID     string
	Title       string
	Description string
	Abstract    *string
	Skills      []string // normalized, distinct ignoring letter case
}

// Changes is a validated edit. A nil field keeps its value; SetAbstract with a nil Abstract
// clears it.
type Changes struct {
	Title       *string
	Description *string
	Skills      *[]string
	SetAbstract bool
	Abstract    *string
	Status      *Status
}

// SearchQuery is a validated project search.
type SearchQuery struct {
	ViewerID string
	Q        string
	Limit    int
	Offset   int
}

// SearchResult is one page of a project search.
type SearchResult struct {
	Items          []Summary
	Total          int
	Fuzzy          bool
	CorrectedQuery *string
}

// Report reasons and outcomes, as in message reports.
var (
	ReportReasons  = []string{"spam", "harassment", "inappropriate", "other"}
	ReportOutcomes = []string{"dismissed", "action_taken"}
)

// NewReport is a report to store.
type NewReport struct {
	ProjectID  string
	ReporterID string
	Reason     string
	Details    *string
}

// ReportReceipt is a stored report's ID and moment.
type ReportReceipt struct {
	ID        string
	CreatedAt time.Time
}

// ReportPerson is a person in a report as an administrator sees them.
type ReportPerson struct {
	Person
	Role string // platform role
}

// Report is a project report as an administrator sees it.
type Report struct {
	ID           string
	ProjectID    string
	ProjectTitle string
	Reason       string
	Details      *string
	Reporter     ReportPerson
	Owner        ReportPerson
	CreatedAt    time.Time
	Outcome      *string
	Note         *string
	ResolvedAt   *time.Time
}

// ReportedProject is the reported project as it is now, for an administrator.
type ReportedProject struct {
	ID          string
	Title       string
	Description string
	Abstract    *string
	Status      Status
	Skills      []Skill
	MemberCount int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Store keeps projects. Every method runs on the transaction the context carries, if any.
type Store interface {
	// Create stores the project and its owner's member row; Skills sets its skills after.
	Create(ctx context.Context, p NewProject) (string, error)
	// SetSkills makes names (distinct ignoring case) the project's skills, creating the
	// tags that do not exist.
	SetSkills(ctx context.Context, projectID string, names []string) error
	// Get reads the project as viewerID sees it; ErrNotFound when there is none.
	Get(ctx context.Context, projectID, viewerID string) (Project, error)
	// Lock reads the access fields and locks the project row until the transaction ends.
	Lock(ctx context.Context, projectID, viewerID string) (Access, error)
	Update(ctx context.Context, projectID string, c Changes) error
	Delete(ctx context.Context, projectID string) error
	// Role is the user's role in the project, RoleNone without a member row.
	Role(ctx context.Context, projectID, userID string) (Role, error)
	ListMine(ctx context.Context, userID string, limit, offset int) ([]Summary, int, error)
	// Search runs in one read-only snapshot of its own.
	Search(ctx context.Context, q SearchQuery) (SearchResult, error)

	// CreateReport returns ErrAlreadyReported for a second open report.
	CreateReport(ctx context.Context, r NewReport) (ReportReceipt, error)
	ListReports(ctx context.Context, open bool, limit, offset int) ([]Report, int, error)
	GetReport(ctx context.Context, reportID string) (Report, ReportedProject, error)
	// ResolveReport returns ErrReportNotFound or ErrAlreadyResolved.
	ResolveReport(ctx context.Context, reportID, adminID, outcome string, note *string) (Report, error)
}

// Transactor runs fn in one transaction that the stores read from the context.
type Transactor interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Limiter counts a user's actions in a fixed window (project creation, search).
type Limiter interface {
	// Used returns the actions counted in the user's current window and the time left in
	// it; 0 and 0 when no window is open.
	Used(ctx context.Context, userID string) (int, time.Duration, error)
	// Record counts one action. The first action of a window opens it for window.
	Record(ctx context.Context, userID string, window time.Duration) error
}

// Standing is what a user may do now. The zero value means no suspension.
type Standing struct {
	Suspended        bool
	SuspensionReason *string
	SuspendedUntil   *time.Time
}

// Actors tells what a user may do now. The moderation module owns this; cmd/api implements
// it. Inside a transaction it holds the user's row until the transaction ends.
type Actors interface {
	Standing(ctx context.Context, userID string) (Standing, error)
}
