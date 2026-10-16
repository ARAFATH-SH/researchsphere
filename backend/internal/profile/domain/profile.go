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

// Store reads and writes profiles.
type Store interface {
	// FindAccount returns ErrNotFound when no user has the ID, or the ID is not a UUID.
	FindAccount(ctx context.Context, userID string) (Account, error)
	// UpsertProfile creates the user's profile row or replaces its edited fields, and
	// returns what was stored. It returns ErrNotFound when the user does not exist.
	UpsertProfile(ctx context.Context, p Profile) (Profile, error)
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
)
