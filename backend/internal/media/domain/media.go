// Package domain holds the media module's entities and the interfaces its usecase
// depends on. Standard library only.
package domain

import "context"

// PendingDeletion is an object whose stored_files row is gone and that is still in the
// bucket, as far as the queue knows (docs/decisions.md, 2026-10-20).
type PendingDeletion struct {
	ID        string
	ObjectKey string
	Attempts  int // failed deletions so far
}

// DrainResult counts what one pass over the queue did.
type DrainResult struct {
	Claimed int // rows taken from the queue
	Deleted int // objects deleted and their rows removed
	Failed  int // objects that could not be deleted; their rows stay for the next pass
}

// ObjectDeleter removes an object from the bucket. A key with no object is not an error.
type ObjectDeleter interface {
	Delete(ctx context.Context, key string) error
}

// DeletionQueue is the storage_deletions table. Drain claims up to limit rows that no
// other worker holds, calls fn for each, removes the rows fn succeeded for, and records
// the attempt and error on the others, all in one transaction.
type DeletionQueue interface {
	Drain(ctx context.Context, limit int, fn func(context.Context, PendingDeletion) error) (DrainResult, error)
}
