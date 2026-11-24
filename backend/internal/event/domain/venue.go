// Package domain holds the event module's entities and the interfaces its usecase needs:
// venues (FR-14), their research fields, the administrators' changes (FR-15.1) and the file
// import (FR-14.5). Standard library only.
package domain

import (
	"context"
	"errors"
	"time"
)

// VenueType is what kind of call a venue is.
type VenueType string

// Venue types, in the order the facets list them.
const (
	TypeConference   VenueType = "conference"
	TypeJournal      VenueType = "journal"
	TypeWorkshop     VenueType = "workshop"
	TypeSpecialIssue VenueType = "special_issue"
)

// VenueTypes lists every type, in the order the facets show them.
var VenueTypes = []VenueType{TypeConference, TypeJournal, TypeWorkshop, TypeSpecialIssue}

// IsEvent says whether the type takes place somewhere: a conference or a workshop. Only an
// event has attendance, a place and event dates.
func (t VenueType) IsEvent() bool { return t == TypeConference || t == TypeWorkshop }

// Attendance is how an event takes place.
type Attendance string

// Attendance values.
const (
	AttendInPerson Attendance = "in_person"
	AttendOnline   Attendance = "online"
	AttendHybrid   Attendance = "hybrid"
)

// Source says how a venue was added.
type Source string

// Sources.
const (
	SourceManual Source = "manual"
	SourceImport Source = "import"
)

// Field is a research field of the fixed list (FR-14.3).
type Field struct {
	Key   string
	Label string
}

// Venue is a venue as stored. Optional text is "" when not set, an optional time or date is
// the zero time. Dates (StartsOn, EndsOn) are midnight UTC of that day. TimeZone is the label
// the deadlines are stated in (AoE, UTC or an IANA name); the times are instants.
type Venue struct {
	ID                 string
	Name               string
	ShortName          string
	Type               VenueType
	Fields             []Field // in the order of the fixed list
	Attendance         Attendance
	City               string
	Country            string
	StartsOn           time.Time
	EndsOn             time.Time
	TimeZone           string
	SubmissionDeadline time.Time
	AbstractDeadline   time.Time
	NotificationAt     time.Time
	OfficialURL        string
	Scope              string
	Source             Source
	SourceKey          string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// FieldKeys returns the keys of the venue's fields, in order.
func (v Venue) FieldKeys() []string {
	keys := make([]string, len(v.Fields))
	for i, f := range v.Fields {
		keys[i] = f.Key
	}
	return keys
}

// Past says whether the submission deadline has passed at now: at or before it.
func (v Venue) Past(now time.Time) bool { return !v.SubmissionDeadline.After(now) }

// Query is one request of the venue list (FR-14.1 to FR-14.3), already checked. Empty Types
// or Fields means any; a zero From or To means no bound. To is exclusive.
type Query struct {
	Now         time.Time
	IncludePast bool
	Types       []VenueType
	Fields      []string
	From, To    time.Time
	Q           string
	Limit       int
	Offset      int
}

// TypeCount and FieldCount are facet counts.
type (
	TypeCount struct {
		Type  VenueType
		Count int
	}
	FieldCount struct {
		Field Field
		Count int
	}
)

// Page is one page of the list with the total and the facets.
type Page struct {
	Items       []Venue
	Total       int
	TypeCounts  []TypeCount
	FieldCounts []FieldCount
}

// ActionKind is a row of the venue audit trail.
type ActionKind string

// Audit actions.
const (
	ActCreate       ActionKind = "create"
	ActUpdate       ActionKind = "update"
	ActRemove       ActionKind = "remove"
	ActImportCreate ActionKind = "import_create"
	ActImportUpdate ActionKind = "import_update"
)

// Action is one audit row (FR-15.1): who changed which venue, how, and the names of the
// fields changed, never their values.
type Action struct {
	VenueID       string
	AdminID       string
	AdminName     string
	Kind          ActionKind
	Reason        string // remove only
	ChangedFields []string
	ImportID      string // import_create and import_update only
}

// Imported is an imported venue found by its key, and whether it was removed.
type Imported struct {
	Venue   Venue
	Removed bool
}

// Errors the store and the usecase return.
var (
	ErrNotFound = errors.New("venue not found")
	// ErrFileTooLarge: an import file over MaxImportBytes.
	ErrFileTooLarge = errors.New("import file too large")
)

// Problem codes of a field that breaks a rule. The import reports them per row.
const (
	CodeRequired         = "required"
	CodeTooLong          = "too_long"
	CodeTooShort         = "too_short"
	CodeInvalid          = "invalid"
	CodeNotHTTPS         = "not_https"
	CodeUnknownField     = "unknown_field"
	CodeDuplicateField   = "duplicate_field"
	CodeTooManyFields    = "too_many_fields"
	CodeNotAllowed       = "not_allowed"
	CodeInvalidTimeZone  = "invalid_time_zone"
	CodeInvalidDate      = "invalid_date"
	CodeEndsBeforeStarts = "ends_before_starts"
	CodeAfterDeadline    = "after_deadline"
	CodeBeforeDeadline   = "before_deadline"
	CodeDuplicateInFile  = "duplicate_in_file"
	CodeVenueRemoved     = "venue_removed"
)

// Problem is one rule a field breaks.
type Problem struct {
	Field string // the API's name, such as officialUrl
	Code  string
}

// InvalidError lists the rules a request breaks, in the order of the fields.
type InvalidError struct {
	Problems []Problem
}

func (e *InvalidError) Error() string {
	if len(e.Problems) == 0 {
		return "invalid venue"
	}
	return "invalid venue: " + e.Problems[0].Field + " " + e.Problems[0].Code
}

// Invalid returns an InvalidError of one problem.
func Invalid(field, code string) *InvalidError {
	return &InvalidError{Problems: []Problem{{Field: field, Code: code}}}
}

// ImportFileError is an import file that cannot be read as a whole; Message says why, and
// never quotes the file.
type ImportFileError struct {
	Message string
}

func (e *ImportFileError) Error() string { return "invalid import file: " + e.Message }

// TooManyRowsError is an import file with more rows than allowed.
type TooManyRowsError struct {
	Max int
}

func (e *TooManyRowsError) Error() string { return "import file has too many rows" }

// Store keeps venues. Every method runs on the transaction the context carries, if any.
type Store interface {
	// Fields returns the fixed list of research fields, in order.
	Fields(ctx context.Context) ([]Field, error)
	// List returns one page of live venues that match q, with the total and the facets.
	List(ctx context.Context, q Query) (Page, error)
	// Get returns a live venue, or ErrNotFound when it does not exist or was removed.
	Get(ctx context.Context, id string) (Venue, error)
	// GetForUpdate is Get with the row locked until the transaction ends.
	GetForUpdate(ctx context.Context, id string) (Venue, error)
	// Create stores a new venue with its fields and returns its ID.
	Create(ctx context.Context, v Venue, createdBy string) (string, error)
	// Update stores v's values over the venue with v.ID, its fields included.
	Update(ctx context.Context, v Venue) error
	// Remove marks a live venue removed; ErrNotFound when it is not live.
	Remove(ctx context.Context, id, by, reason string, at time.Time) error
	// Record writes an audit row.
	Record(ctx context.Context, a Action) error
	// AdminName returns the name to keep with an audit row: the profile's full name, or the
	// email address when there is no profile.
	AdminName(ctx context.Context, userID string) (string, error)
	// LockImports makes imports take turns until the transaction ends.
	LockImports(ctx context.Context) error
	// ImportedByKeys returns the imported venues with these keys, removed ones included,
	// locked until the transaction ends.
	ImportedByKeys(ctx context.Context, keys []string) (map[string]Imported, error)
}

// Transactor runs fn in one transaction.
type Transactor interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}
