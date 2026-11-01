package domain

import (
	"context"
	"errors"
	"time"
)

// MatchField names the profile field of a search result's best match (FR-6.1).
type MatchField string

// Searched fields. Only public profile fields are searched; contact info never is.
const (
	MatchName        MatchField = "name"
	MatchInstitution MatchField = "institution"
	MatchInterest    MatchField = "interest"
	MatchHeadline    MatchField = "headline"
)

// PeopleQuery is a validated researcher search (FR-6.1, FR-6.3).
type PeopleQuery struct {
	Q           string   // trimmed, 2 to 100 characters
	Institution *string  // keep only this institution, ignoring letter case; nil for any
	Interests   []string // normalized tag names the result must all have, no duplicates
	Page        int      // from 1
	PageSize    int
}

// PersonHit is one search result: the public fields the results list shows.
type PersonHit struct {
	UserID      string
	FullName    string
	Headline    *string
	Institution *string
	PhotoFileID *string
	Interests   []string // up to 3
	MatchedOn   MatchField
	// Connection is the searcher's link with this person (FR-7); the usecase fills it.
	Connection ConnectionRelation
}

// Facet is one option of a search filter and the number of results it leaves.
type Facet struct {
	Name  string
	Count int
}

// PeopleResults is one page of a researcher search.
type PeopleResults struct {
	Hits  []PersonHit
	Total int // results on all pages, every filter applied
	// Fuzzy is true when no visible name contains the text and the name results are
	// similar names instead (FR-6.2); CorrectedQuery is then the most similar one.
	Fuzzy          bool
	CorrectedQuery *string
	// Institutions is counted without the institution filter, Interests with every
	// filter, and lists the selected interests first (FR-6.3).
	Institutions []Facet
	Interests    []Facet
}

// PeopleSearcher runs researcher searches over verified accounts that are not suspended.
type PeopleSearcher interface {
	// SearchPeople returns the page of results, the total and the facets of q, with at
	// most maxFacets options in each facet besides the selected interests.
	SearchPeople(ctx context.Context, q PeopleQuery, maxFacets int) (PeopleResults, error)
}

// SearchLimiter counts a user's searches in a fixed window.
type SearchLimiter interface {
	// Used returns the searches counted in the user's current window and the time left in
	// it; 0 and 0 when no window is open.
	Used(ctx context.Context, userID string) (int, time.Duration, error)
	// Record counts one search. The first search of a window opens it for window.
	Record(ctx context.Context, userID string, window time.Duration) error
}

// Errors of researcher search. A used-up search window gives *RateLimitedError.
var (
	ErrInvalidQuery        = errors.New("search text is not 2 to 100 characters")
	ErrInvalidSearchFilter = errors.New("search filter is not valid")
)
