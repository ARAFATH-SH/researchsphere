// Package domain holds the embedding module's entities and the interfaces its usecase needs
// (AI-1): the queue of owners whose embedding must be made, the stored embeddings and the AI
// service. Standard library only.
package domain

import (
	"context"
	"errors"
	"time"
)

// OwnerType is what an embedding belongs to. Each source slice adds one: project (AI-1.2),
// venue (AI-1.3, slice 24) and post (AI-1.3). The values match embedding_jobs.owner_type.
type OwnerType string

// Owner types.
const (
	OwnerProfile OwnerType = "profile"
	OwnerProject OwnerType = "project"
	OwnerVenue   OwnerType = "venue"
	// OwnerProjectAbstract is a project's second vector, made from its title and abstract
	// only, for venue recommendations (AI-6.1; slice 25). It is not a job type: the project
	// job makes it beside the project's own embedding, and it is stored in
	// project_abstract_embeddings. Watchers are told about it with this type.
	OwnerProjectAbstract OwnerType = "project_abstract"
)

// OwnerTypes lists every owner type, in the order the admin statistics show them.
var OwnerTypes = []OwnerType{OwnerProfile, OwnerProject, OwnerVenue}

// MaxChunks is how many chunks of at most 512 tokens one embedding averages (AI-1.1).
const MaxChunks = 8

// MaxTextChars is the longest source text sent to the AI service; it is far more than
// MaxChunks chunks hold in any language, so cutting there loses nothing that is embedded.
const MaxTextChars = 100_000

// Job is a claimed row of the queue. Generation identifies the request the worker read: a
// change made while it works raises it, so the job is kept and runs again.
type Job struct {
	OwnerType  OwnerType
	OwnerID    string
	Generation int64
	Attempts   int
	Force      bool
}

// Stored is what the worker compares with the current text (AI-1.4).
type Stored struct {
	Model       string
	ContentHash string
}

// Embedding is a vector to store with its record (AI-1.6).
type Embedding struct {
	OwnerType   OwnerType
	OwnerID     string
	Model       string // "name@revision"
	ContentHash string // hex SHA-256 of the source text
	ChunkCount  int
	Vector      []float32
}

// ProfileSource is the public part of a profile that its embedding is made from (AI-1.1),
// and whether the user may have one: verified and not suspended. It has no contact field.
type ProfileSource struct {
	Eligible     bool
	Headline     string
	Bio          string
	Interests    []string // by name
	Skills       []string // by name
	Publications []PublicationText
}

// ProjectSource is what a project's embedding is made from (AI-1.2). Every project may
// have one.
type ProjectSource struct {
	Title       string
	Description string
	Abstract    string
	Skills      []string // by name
}

// VenueSource is what a venue's embedding is made from (AI-1.3): its name, short name,
// research fields and scope. Only a venue not removed may have one.
type VenueSource struct {
	Name      string
	ShortName string
	Fields    []string // labels, in the order of the fixed list
	Scope     string
}

// PublicationText is a confirmed publication's text, newest first in ProfileSource.
type PublicationText struct {
	Title    string
	Abstract string
}

// OwnerStats is one owner type's line of the admin statistics.
type OwnerStats struct {
	OwnerType    OwnerType
	Total        int // stored embeddings
	CurrentModel int // of them, made by the current model
	Queued       int // jobs waiting, failed ones not counted
	Failed       int
}

// Errors.
var (
	// ErrNotFound means the owner is gone.
	ErrNotFound = errors.New("not found")
	// ErrUnavailable means the AI service is down, loading or answered with a server error.
	// The job is kept and nothing is counted against it.
	ErrUnavailable = errors.New("ai service unavailable")
	// ErrNotConfigured means the AI service is turned off (no token): jobs wait.
	ErrNotConfigured = errors.New("ai service is not configured")
)

// Embedder is the AI service. An error that is neither ErrUnavailable nor ErrNotConfigured is
// a problem with this text, which counts toward the job's attempts.
type Embedder interface {
	// EmbedDocument returns one unit-length vector for text, the model and the chunk count.
	EmbedDocument(ctx context.Context, text string, maxChunks int) (vector []float32, model string, chunks int, err error)
	// Model returns the model the service has loaded.
	Model(ctx context.Context) (string, error)
}

// Store is the queue, the embeddings and the source rows.
type Store interface {
	// ClaimJobs leases up to limit due jobs, user changes first, that no other worker holds,
	// until now plus lease.
	ClaimJobs(ctx context.Context, limit int, lease time.Duration) ([]Job, error)
	// FinishJob deletes the job when its generation is still job.Generation; otherwise it
	// only ends the lease, so the newer request runs.
	FinishJob(ctx context.Context, job Job) error
	// ReleaseJob ends the lease without counting an attempt (AI service down).
	ReleaseJob(ctx context.Context, job Job) error
	// RetryJob counts a failed attempt: the job runs again at next, or, when failed, stays
	// with failed_at and the error until the owner's next change. It does nothing when a
	// newer request replaced the job.
	RetryJob(ctx context.Context, job Job, lastError string, next time.Time, failed bool) error

	// ProfileSource returns ErrNotFound when the user or profile is gone.
	ProfileSource(ctx context.Context, userID string) (ProfileSource, error)
	// ProjectSource returns ErrNotFound when the project is gone.
	ProjectSource(ctx context.Context, projectID string) (ProjectSource, error)
	// VenueSource returns ErrNotFound when the venue is gone or was removed.
	VenueSource(ctx context.Context, venueID string) (VenueSource, error)
	// StoredEmbedding returns ErrNotFound when the owner has none.
	StoredEmbedding(ctx context.Context, owner OwnerType, ownerID string) (Stored, error)
	// SaveEmbedding inserts or replaces the embedding, only while the owner is still
	// eligible, holding the owner's row so a suspension waits for it or is seen by it. It
	// reports whether it stored.
	SaveEmbedding(ctx context.Context, e Embedding) (bool, error)
	// DeleteEmbedding removes the owner's embedding, if any.
	DeleteEmbedding(ctx context.Context, owner OwnerType, ownerID string) error

	// QueueStale queues, at low priority, up to limit owners whose embedding another model
	// made and that have no job yet. It returns how many it queued.
	QueueStale(ctx context.Context, model string, limit int) (int, error)
	// QueueAll queues every eligible owner at low priority, forced or not, in one statement,
	// and returns how many.
	QueueAll(ctx context.Context, force bool) (int, error)
	// Rebuild queues every eligible owner, forced, and records the administrator's action,
	// in one transaction. It returns how many it queued.
	Rebuild(ctx context.Context, adminID string) (int, error)
	// Stats counts the embeddings and jobs of each owner type; model is the current model
	// ("" when unknown: no embedding counts as current).
	Stats(ctx context.Context, model string) ([]OwnerStats, error)
}

// Watcher is told, after the write, that the worker stored or deleted an owner's embedding,
// so modules that keep lists made from embeddings can renew them (project recommendations,
// AI-3.1). It must not block.
type Watcher interface {
	EmbeddingChanged(ctx context.Context, ownerType OwnerType, ownerID string)
}
