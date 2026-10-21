// Package usecase holds the media module's logic. It imports only the media domain.
package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
)

// BatchSize is how many queued objects one transaction claims.
const BatchSize = 100

// Cleaner deletes the objects whose stored_files rows are gone (FR-2.8, NFR-13).
type Cleaner struct {
	queue   domain.DeletionQueue
	objects domain.ObjectDeleter
	logger  *slog.Logger
}

// NewCleaner returns a Cleaner that drains queue by deleting from objects.
func NewCleaner(queue domain.DeletionQueue, objects domain.ObjectDeleter, logger *slog.Logger) *Cleaner {
	return &Cleaner{queue: queue, objects: objects, logger: logger}
}

// RunOnce drains the queue: batch after batch while every object of a full batch was
// deleted, so a large backlog clears in one pass but failing objects are not retried in a
// loop. It returns the totals.
func (c *Cleaner) RunOnce(ctx context.Context) (domain.DrainResult, error) {
	var total domain.DrainResult
	for {
		res, err := c.queue.Drain(ctx, BatchSize, func(ctx context.Context, d domain.PendingDeletion) error {
			return c.objects.Delete(ctx, d.ObjectKey)
		})
		total.Claimed += res.Claimed
		total.Deleted += res.Deleted
		total.Failed += res.Failed
		if err != nil {
			return total, fmt.Errorf("drain storage deletions: %w", err)
		}
		if res.Claimed < BatchSize || res.Failed > 0 {
			return total, nil
		}
	}
}

// Run calls RunOnce now and then every interval until ctx is cancelled, logging what each
// pass did. Errors are logged, not returned: the next pass tries again.
func (c *Cleaner) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		res, err := c.RunOnce(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			c.logger.ErrorContext(ctx, "storage cleanup failed", slog.Any("error", err))
		case res.Failed > 0:
			c.logger.WarnContext(ctx, "storage cleanup left objects for the next pass",
				slog.Int("deleted", res.Deleted), slog.Int("failed", res.Failed))
		case res.Deleted > 0:
			c.logger.InfoContext(ctx, "storage cleanup deleted objects", slog.Int("deleted", res.Deleted))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
