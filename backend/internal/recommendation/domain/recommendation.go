// Package domain holds the recommendation module's entities and the interfaces its usecase
// needs: collaborator recommendations from profile embeddings (AI-2.1, AI-2.2, AI-2.4,
// AI-4). Standard library only.
package domain

import (
	"context"
	"errors"
	"time"
)

// State says what a user's recommendations are (AI-4.2).
type State string

// States.
const (
	// StateReady: the list is made (it may be empty).
	StateReady State = "ready"
	// StateNeedsProfile: the profile has no interests, skills or bio; the client asks for
	// them in place of an empty list (AI-4.2).
	StateNeedsProfile State = "needs_profile"
	// StateProcessing: the profile has text but no embedding yet (AI-1.5).
	StateProcessing State = "processing"
)

// Field names a profile field a needs_profile answer asks for.
type Field string

// Fields, in the order they are listed.
const (
	FieldInterests Field = "interests"
	FieldSkills    Field = "skills"
	FieldBio       Field = "bio"
)

// ConnectionStatus is the caller's link with a recommended user, as the profile and search
// show it (FR-7). The values are the contract's ConnectionStatus.
type ConnectionStatus string

// Connection statuses.
const (
	StatusNone            ConnectionStatus = "none"
	StatusPendingSent     ConnectionStatus = "pending_sent"
	StatusPendingReceived ConnectionStatus = "pending_received"
	StatusConnected       ConnectionStatus = "connected"
	StatusSelf            ConnectionStatus = "self"
)

// Stamp identifies one version of a user's own embedding. A kept list is used only while
// the embedding it was made from is still the stored one.
type Stamp struct {
	Model       string
	ContentHash string
	UpdatedAt   time.Time
}

// Equal reports whether s and o name the same embedding.
func (s Stamp) Equal(o Stamp) bool {
	return s.Model == o.Model && s.ContentHash == o.ContentHash && s.UpdatedAt.Equal(o.UpdatedAt)
}

// Self is what the usecase reads about the caller: the fields AI-4.2 checks, the tags the
// shared list compares and the stamp of the stored embedding (nil when there is none).
type Self struct {
	Interests []string // by name, in the profile's order
	Skills    []string // by name, in the profile's order
	HasBio    bool
	Embedding *Stamp
}

// Person is the public summary of a recommended user.
type Person struct {
	UserID      string
	FullName    string
	Headline    *string
	Institution *string
	PhotoFileID *string
}

// Candidate is a ranked user: the cosine similarity of the two profile embeddings and the
// candidate's tags, which only the shared list uses. It carries no count of any kind.
type Candidate struct {
	Person
	Cosine    float64
	Interests []string
	Skills    []string
}

// Recommendation is one item of the list.
type Recommendation struct {
	Person
	Score      int      // 0 to 100 (AI-2.2)
	Shared     []string // up to 3 interests or skills both have (AI-2.2)
	Connection ConnectionStatus
}

// Kept is a user's list as the cache holds it: the items without their connection status,
// and the stamp of the embedding they were made from.
type Kept struct {
	Stamp Stamp
	Items []Recommendation
}

// Store reads the embeddings and profiles.
type Store interface {
	// Self returns the caller's fields and embedding stamp. A user without a profile row
	// reads as an empty profile.
	Self(ctx context.Context, userID string) (Self, error)
	// Rank returns at most limit users ordered by the cosine similarity of their profile
	// embedding to userID's, most similar first, and nothing else (AI-2.4). It leaves out
	// userID, users connected to userID or with a pending request either way, blocks either
	// way, unverified and suspended users, and users whose embedding is missing or made by
	// another model than userID's (AI-2.1).
	Rank(ctx context.Context, userID string, limit int) ([]Candidate, error)
	// StillEligible returns those of ids that Rank could still return for userID now.
	StillEligible(ctx context.Context, userID string, ids []string) (map[string]bool, error)
}

// Cache keeps each user's list for a while. Errors are reported, never fatal: the usecase
// goes on without the cache.
type Cache interface {
	// Get returns the kept list; found is false when there is none.
	Get(ctx context.Context, userID string) (kept Kept, found bool, err error)
	Set(ctx context.Context, userID string, kept Kept, ttl time.Duration) error
	// Delete drops the lists of every user named.
	Delete(ctx context.Context, userIDs ...string) error
}

// Relations gives the caller's connection status with each user, from the social module.
type Relations interface {
	Statuses(ctx context.Context, viewerID string, userIDs []string) (map[string]ConnectionStatus, error)
}

// ErrInvalidLimit means the limit is below 1.
var ErrInvalidLimit = errors.New("recommendation: limit must be at least 1")
