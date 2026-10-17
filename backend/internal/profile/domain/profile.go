// Package domain holds the profile module's entities, errors and the interface its
// usecase depends on. Standard library only.
package domain

import (
	"context"
	"errors"
)

// Profile is the part of a research profile a user edits (FR-4.1). FullName is the one
// place the user's name is stored; the auth module reads it from the same row. An
// optional field is nil when not set.
type Profile struct {
	UserID      string
	FullName    string
	Headline    *string
	Bio         *string
	Institution *string
	Department  *string
	JobTitle    *string
	Country     *string // ISO 3166-1 alpha-2 code in capitals
	// Interests and Skills are the user's tags sorted by name (FR-4.2), and Education
	// the user's education entries in profile order (FR-4.4). UpsertProfile ignores them;
	// the usecase fills them when it reads a profile.
	Interests []Tag
	Skills    []Tag
	Education []Education
}

// Education is one entry of a user's education (FR-4.4). An optional field is nil when
// not set; a nil EndYear means the entry is current.
type Education struct {
	ID           string
	UserID       string
	Institution  string
	Degree       *string
	FieldOfStudy *string
	StartYear    int
	EndYear      *int
}

// TagKind says which shared vocabulary a tag belongs to.
type TagKind string

// Tag kinds.
const (
	KindInterest TagKind = "interest" // research_interests, user_interests
	KindSkill    TagKind = "skill"    // skills, user_skills
)

// Tag is a research interest or a skill (FR-4.2). Tags are shared by all users; names
// that differ only in letter case are the same tag, and Name keeps the spelling the tag
// was first created with.
type Tag struct {
	ID   string
	Name string
}

// Account is a user as the profile module sees it: whether others may see the profile,
// and the profile row when there is one.
type Account struct {
	UserID    string
	Verified  bool // the email address is verified (FR-1.4)
	Suspended bool // the account is suspended (FR-3.4)
	// Profile is nil when the user has no profile row.
	Profile *Profile
}

// Store reads and writes profiles, the user's tags and education entries. Writes that must happen together
// go through RunInTx.
type Store interface {
	// RunInTx runs fn in one database transaction. It commits when fn returns nil and
	// rolls back otherwise, returning fn's error.
	RunInTx(ctx context.Context, fn func(tx Tx) error) error
	// FindAccount returns ErrNotFound when no user has the ID, or the ID is not a UUID.
	FindAccount(ctx context.Context, userID string) (Account, error)
	// UpsertProfile creates the user's profile row or replaces its edited fields, and
	// returns what was stored. It returns ErrNotFound when the user does not exist.
	UpsertProfile(ctx context.Context, p Profile) (Profile, error)
	// ListTags returns the user's tags of kind, sorted by name ignoring letter case.
	ListTags(ctx context.Context, userID string, kind TagKind) ([]Tag, error)
	// RemoveTag unlinks the tag from the user; the tag itself stays. It returns
	// ErrNotFound when the user does not have the tag, or tagID is not a UUID.
	RemoveTag(ctx context.Context, userID string, kind TagKind, tagID string) error
	// SuggestTags returns at most limit tags of kind whose name contains q, ignoring
	// letter case (FR-4.3): names that start with q first, then by how many users have
	// the tag (most first), then by name.
	SuggestTags(ctx context.Context, kind TagKind, q string, limit int) ([]Tag, error)
	// ListEducation returns the user's education entries: current entries first, then by
	// end year, latest first, then by start year, latest first.
	ListEducation(ctx context.Context, userID string) ([]Education, error)
	// DeleteEducation removes the user's entry. It returns ErrNotFound when the user has
	// no entry with the ID, including one that belongs to someone else, or the ID is not
	// a UUID.
	DeleteEducation(ctx context.Context, userID, id string) error
}

// Tx is the set of steps available inside a transaction.
type Tx interface {
	// LockUser locks the user against other tag and education changes until the
	// transaction ends, so two requests cannot both pass a limit. It returns ErrNotFound when the user
	// does not exist.
	LockUser(ctx context.Context, userID string) error
	// FindOrCreateTag returns the tag of kind named name ignoring letter case, creating
	// it with this spelling when there is none. A tag created at the same moment by
	// another transaction is found, not duplicated.
	FindOrCreateTag(ctx context.Context, kind TagKind, name string) (Tag, error)
	// HasTag reports whether the user has the tag.
	HasTag(ctx context.Context, userID string, kind TagKind, tagID string) (bool, error)
	// CountTags returns how many tags of kind the user has.
	CountTags(ctx context.Context, userID string, kind TagKind) (int, error)
	// LinkTag adds the tag to the user's profile.
	LinkTag(ctx context.Context, userID string, kind TagKind, tagID string) error
	// CountEducation returns how many education entries the user has.
	CountEducation(ctx context.Context, userID string) (int, error)
	// InsertEducation stores a new entry for e.UserID and returns it with its ID.
	InsertEducation(ctx context.Context, e Education) (Education, error)
	// FindEducationForUpdate returns the user's entry and locks it until the transaction
	// ends. It returns ErrNotFound when the user has no entry with the ID, including one
	// that belongs to someone else, or the ID is not a UUID.
	FindEducationForUpdate(ctx context.Context, userID, id string) (Education, error)
	// UpdateEducation replaces the fields of the entry e.ID that belongs to e.UserID and
	// returns what was stored. It returns ErrNotFound when there is no such entry.
	UpdateEducation(ctx context.Context, e Education) (Education, error)
}

// Errors the usecase returns. The HTTP adapter turns each into an error code.
var (
	ErrNotFound           = errors.New("profile not found")
	ErrInvalidFullName    = errors.New("full name is empty, too long or has a line break")
	ErrInvalidHeadline    = errors.New("headline is too long or has a line break")
	ErrInvalidBio         = errors.New("bio is too long")
	ErrInvalidInstitution = errors.New("institution is too long or has a line break")
	ErrInvalidDepartment  = errors.New("department is too long or has a line break")
	ErrInvalidJobTitle    = errors.New("job title is too long or has a line break")
	ErrInvalidCountry     = errors.New("country is not an ISO 3166-1 alpha-2 code")
	ErrInvalidTagName     = errors.New("tag name is empty or too long")
	ErrTagLimitReached    = errors.New("user has the most tags of this kind allowed")
	ErrInvalidLimit       = errors.New("limit is less than 1")

	ErrInvalidEducationInstitution = errors.New("education institution is empty, too long or has a line break")
	ErrInvalidDegree               = errors.New("degree is too long or has a line break")
	ErrInvalidFieldOfStudy         = errors.New("field of study is too long or has a line break")
	ErrInvalidStartYear            = errors.New("start year is missing or out of range")
	ErrInvalidEndYear              = errors.New("end year is out of range or before the start year")
	ErrEducationLimitReached       = errors.New("user has the most education entries allowed")
)
