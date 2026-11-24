package domain

import (
	"context"
	"time"
)

// Venue recommendations (AI-6.1 to AI-6.4; slice 25, chunk 25.1).

// StateNeedsAbstract: the project has no abstract; the client asks for one in place of the
// list, even when the project has a title and description (AI-6.3).
const StateNeedsAbstract State = "needs_abstract"

// VenueNotice is the fixed sentence AI-6.4 asks to show beside the list. A client may
// restyle it but not drop it.
const VenueNotice = "Matched on topic only. This says nothing about a venue's quality or ranking."

// VenueField is one of a venue's research fields.
type VenueField struct {
	Key   string
	Label string
}

// VenueCard is what a recommended venue shows (AI-6.2). Deadline and TimeZone are nil for a
// rolling journal.
type VenueCard struct {
	ID          string
	Name        string
	ShortName   *string
	Type        string
	Fields      []VenueField // in the order of the fixed list
	TimeZone    *string      // the label the deadline was given in: AoE, UTC or an IANA name
	Deadline    *time.Time   // the submission deadline as an instant
	OfficialURL string
	LinkKind    string
}

// VenueCandidate is a ranked venue: the cosine similarity of its embedding to the project's
// title-and-abstract embedding, and nothing else (AI-2.4).
type VenueCandidate struct {
	VenueCard
	Cosine float64
}

// VenueRecommendation is one item of a project's venue or journal list.
type VenueRecommendation struct {
	VenueCard
	Score int // 0 to 100 (AI-6.2)
	// DaysLeft is the whole days until the deadline, rounded down, worked out when the list
	// is read (never kept); nil for a rolling journal.
	DaysLeft *int
}

// KeptVenues is a project's lists as the cache holds them: the stamp of the project's
// title-and-abstract embedding and the venues generation they were made at.
type KeptVenues struct {
	Stamp      Stamp
	Generation int64
	Venues     []VenueRecommendation
	Journals   []VenueRecommendation
}

// VenueSelf is what the usecase reads about a project for its venue lists.
type VenueSelf struct {
	HasAbstract bool
	Embedding   *Stamp // the title-and-abstract embedding; nil when not made yet
}

// VenueStore reads the venue embeddings and the project's title-and-abstract embedding.
type VenueStore interface {
	// VenueSelf reads the project; ErrProjectNotFound when it does not exist.
	VenueSelf(ctx context.Context, projectID string) (VenueSelf, error)
	// RankVenues returns at most limit venues ordered by the cosine similarity of their
	// embedding to the project's title-and-abstract embedding, most similar first, and
	// nothing else (AI-2.4). Only venues not removed, not rolling, whose submission deadline
	// is after now, with an embedding of the project's model (AI-6.1).
	RankVenues(ctx context.Context, projectID string, limit int) ([]VenueCandidate, error)
	// RankJournals does the same for rolling journals not removed.
	RankJournals(ctx context.Context, projectID string, limit int) ([]VenueCandidate, error)
	// VenuesStillEligible returns those of ids that are not removed and either rolling or
	// with a submission deadline after now.
	VenuesStillEligible(ctx context.Context, ids []string) (map[string]bool, error)
}

// VenueCache keeps each project's venue lists for a while. Errors are reported, never fatal.
type VenueCache interface {
	GetVenues(ctx context.Context, projectID string) (kept KeptVenues, found bool, err error)
	SetVenues(ctx context.Context, projectID string, kept KeptVenues, ttl time.Duration) error
	// DeleteVenues drops the venue lists of every project named.
	DeleteVenues(ctx context.Context, projectIDs ...string) error
	// VenueGeneration grows whenever any venue or venue embedding changes, so every kept
	// venue list is then made again. 0 when it was never raised.
	VenueGeneration(ctx context.Context) (int64, error)
	// NextVenueGeneration raises it.
	NextVenueGeneration(ctx context.Context) error
}

// VenueAccess checks that a user may see a project's venue recommendations: nil, or
// ErrProjectNotFound for anyone who is not a member, also when they can see the project
// (SRS 2.3 "See venue recommendations for the project": viewer, editor, owner). The project
// module owns the rule; cmd/api implements it.
type VenueAccess interface {
	CanSeeRecommendedVenues(ctx context.Context, userID, projectID string) error
}
