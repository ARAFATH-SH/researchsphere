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

// Project matching (AI-3.1, AI-2.3; slice 19, chunk 19.2).

// ProjectCard is the public summary of a recommended project.
type ProjectCard struct {
	ID          string
	Title       string
	Description string   // the first 300 characters
	Skills      []string // up to CardSkills required skills, by name ignoring case
	Owner       ProjectOwner
	MemberCount int // shown only; never read by the ranking (AI-2.4)
}

// CardSkills is how many required skills a ProjectCard carries.
const CardSkills = 5

// ProjectOwner is a project's owner as a card shows them.
type ProjectOwner struct {
	UserID      string
	FullName    string
	Headline    *string
	PhotoFileID *string
}

// ProjectCandidate is a ranked project: the cosine similarity of its embedding to the
// caller's profile embedding and its required skills, which only the shared list uses. Like
// Candidate it carries no count of any kind (AI-2.4).
type ProjectCandidate struct {
	ProjectID string
	Cosine    float64
	Skills    []string // every required skill, by name ignoring case
}

// ProjectRecommendation is one item of a user's project list (AI-3.1).
type ProjectRecommendation struct {
	Project ProjectCard
	Score   int      // 0 to 100
	Shared  []string // up to 3 of the project's required skills the caller has
}

// KeptProjects is a user's project list as the cache holds it: the stamp of the caller's
// embedding and the projects generation it was made at.
type KeptProjects struct {
	Stamp      Stamp
	Generation int64
	Items      []ProjectRecommendation
}

// ProjectSelf is what the usecase reads about a project for its researcher list (AI-2.3).
type ProjectSelf struct {
	OwnerID   string
	Skills    []string // by name ignoring case
	Embedding *Stamp   // nil when the project has no embedding yet
}

// ProjectStore reads project embeddings and what leaves projects and people out.
type ProjectStore interface {
	// RankProjects returns at most limit open projects ordered by the cosine similarity of
	// their embedding to userID's profile embedding, most similar first, and nothing else
	// (AI-2.4). It leaves out projects userID is a member of or has a pending request to
	// join, projects whose owner is unverified or suspended, a block either way between
	// userID and the owner, and embeddings of another model (AI-3.1).
	RankProjects(ctx context.Context, userID string, limit int) ([]ProjectCandidate, error)
	// ProjectCards returns the cards of ids, by ID; a missing project is left out.
	ProjectCards(ctx context.Context, ids []string) (map[string]ProjectCard, error)
	// ProjectsStillEligible returns those of ids that RankProjects could still return.
	ProjectsStillEligible(ctx context.Context, userID string, ids []string) (map[string]bool, error)
	// ProjectSelf reads the project; ErrProjectNotFound when it does not exist.
	ProjectSelf(ctx context.Context, projectID string) (ProjectSelf, error)
	// RankResearchers returns at most limit users ordered by the cosine similarity of their
	// profile embedding to the project's, most similar first, and nothing else (AI-2.4). It
	// leaves out the project's members, unverified and suspended users, a block either way
	// with the owner, and embeddings of another model (AI-2.3).
	RankResearchers(ctx context.Context, projectID string, limit int) ([]Candidate, error)
	// ResearchersStillEligible returns those of ids that RankResearchers could still return.
	ResearchersStillEligible(ctx context.Context, projectID string, ids []string) (map[string]bool, error)
	// OwnedProjects returns the IDs of the projects the users own.
	OwnedProjects(ctx context.Context, userIDs ...string) ([]string, error)
}

// ProjectCache keeps the project lists for a while. Errors are reported, never fatal.
type ProjectCache interface {
	GetProjects(ctx context.Context, userID string) (kept KeptProjects, found bool, err error)
	SetProjects(ctx context.Context, userID string, kept KeptProjects, ttl time.Duration) error
	// DeleteProjects drops the project lists of every user named.
	DeleteProjects(ctx context.Context, userIDs ...string) error
	// GetResearchers returns a project's kept researcher list.
	GetResearchers(ctx context.Context, projectID string) (kept Kept, found bool, err error)
	SetResearchers(ctx context.Context, projectID string, kept Kept, ttl time.Duration) error
	// DeleteResearchers drops the researcher lists of every project named.
	DeleteResearchers(ctx context.Context, projectIDs ...string) error
	// Generation is the projects generation: it grows whenever a project's embedding or
	// status changes or a project is deleted, so every user's kept project list is then
	// made again. 0 when it was never raised.
	Generation(ctx context.Context) (int64, error)
	// NextGeneration raises it.
	NextGeneration(ctx context.Context) error
}

// ProjectAccess checks that a user may see a project's recommended researchers: nil, or
// ErrProjectNotFound when the user cannot see the project, or ErrNotProjectOwner when the
// project role lacks the right (AI-2.3). The project module owns the rule; cmd/api
// implements it.
type ProjectAccess interface {
	CanSeeRecommendedResearchers(ctx context.Context, userID, projectID string) error
}

// Errors of the project lists.
var (
	// ErrProjectNotFound covers a missing project and one the caller may not see.
	ErrProjectNotFound = errors.New("recommendation: project not found")
	// ErrNotProjectOwner refuses a visible project's researcher list to a non-owner.
	ErrNotProjectOwner = errors.New("recommendation: only the project's owner sees its recommended researchers")
)
