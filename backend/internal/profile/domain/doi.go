package domain

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// WorkMetadata is what an outside service knows about the work with a DOI (FR-5.2). An
// optional field is nil when the service has no value. A WorkSource returns the text as
// the service gave it; the usecase trims and cuts it.
type WorkMetadata struct {
	DOI      string // the bare, lower-case DOI that was asked for
	Title    string
	Year     *int
	Venue    *string
	Abstract *string
	URL      *string // the landing page
}

// WorkSource fetches the metadata of a work by its DOI (OpenAlex; Crossref could be
// another).
type WorkSource interface {
	// FetchWork returns ErrWorkNotFound when the service has no work with the DOI, and
	// ErrMetadataUnavailable when it cannot be reached or gives no usable answer.
	FetchWork(ctx context.Context, doi string) (WorkMetadata, error)
}

// WorkCache keeps successful lookups for a while, by DOI.
type WorkCache interface {
	// GetWork returns the cached metadata and true, or false when nothing is cached.
	GetWork(ctx context.Context, doi string) (WorkMetadata, bool, error)
	// PutWork caches w under w.DOI for ttl.
	PutWork(ctx context.Context, w WorkMetadata, ttl time.Duration) error
}

// LookupLimiter counts a user's DOI lookups that reach the outside service, in a fixed
// window.
type LookupLimiter interface {
	// Used returns the lookups counted in the user's current window and the time left in
	// it; 0 and 0 when no window is open.
	Used(ctx context.Context, userID string) (int, time.Duration, error)
	// Record counts one lookup. The first lookup of a window opens it for window.
	Record(ctx context.Context, userID string, window time.Duration) error
}

// Errors of DOI lookups (FR-5.2, FR-5.6).
var (
	ErrWorkNotFound        = errors.New("no work with this DOI")
	ErrMetadataUnavailable = errors.New("publication metadata service unavailable")
)

// RateLimitedError says the user has used up the DOI lookups of the current window.
type RateLimitedError struct {
	RetryAfter time.Duration // until the window ends
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("profile: DOI lookup rate limited, retry after %s", e.RetryAfter)
}
