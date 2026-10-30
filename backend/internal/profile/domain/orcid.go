package domain

import (
	"context"
	"errors"
	"time"
)

// ORCIDWork is one public work of an ORCID record (FR-5.3). An optional field is nil when
// ORCID has no value. An ORCIDSource returns the text as ORCID gave it; the usecase
// normalizes it.
type ORCIDWork struct {
	PutCode string // ORCID's identifier of the work within the record
	Title   string
	Year    *int
	Venue   *string // the journal title
	DOI     *string
	Type    *string // ORCID's work type, such as journal-article
	URL     *string
}

// ORCIDSource lists the public works of an ORCID iD (the ORCID public API).
type ORCIDSource interface {
	// ListWorks returns ErrORCIDNotFound when ORCID has no record with the iD, and
	// ErrMetadataUnavailable when it cannot be reached or gives no usable answer.
	ListWorks(ctx context.Context, orcidID string) ([]ORCIDWork, error)
}

// ORCIDWorksCache keeps listed works for a while, by ORCID iD.
type ORCIDWorksCache interface {
	// GetORCIDWorks returns the cached works and true, or false when nothing is cached.
	GetORCIDWorks(ctx context.Context, orcidID string) ([]ORCIDWork, bool, error)
	// PutORCIDWorks caches works under orcidID for ttl.
	PutORCIDWorks(ctx context.Context, orcidID string, works []ORCIDWork, ttl time.Duration) error
}

// DOIMatch is a stored publication with a given DOI and how one user is linked to it.
type DOIMatch struct {
	PublicationID string
	State         LinkState
}

// Errors of the ORCID iD and of ORCID imports (FR-5.3, FR-5.6).
var (
	ErrInvalidORCID   = errors.New("ORCID iD is not of the form 0000-0000-0000-000X with a valid check digit")
	ErrORCIDTaken     = errors.New("another profile has this ORCID iD")
	ErrORCIDNotSet    = errors.New("profile has no ORCID iD")
	ErrORCIDNotFound  = errors.New("ORCID has no record with this iD")
	ErrInvalidPutCode = errors.New("put-codes are empty, too many, repeated or not in the ORCID list")
)
