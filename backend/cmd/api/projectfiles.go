package main

import (
	"context"
	"errors"
	"log/slog"

	mediadomain "github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
	mediausecase "github.com/ARAFATH-SH/researchsphere/backend/internal/media/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/storage"
	projectdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// Project files (FR-12.1, FR-12.2) join the project module, which decides who may read and
// write them, with media, which checks a figure's type and scans it for malware (the scan
// worker takes every pending stored file, figures included), and the bucket.

// figureIntake gives the project module the media module's figure check.
type figureIntake struct {
	media *mediausecase.Attachments
}

var _ projectdomain.FigureIntake = figureIntake{}

func (f figureIntake) PrepareFigure(ctx context.Context, data []byte) (projectdomain.PreparedFigure, error) {
	p, err := f.media.PrepareFigure(ctx, data)
	switch {
	case errors.Is(err, mediadomain.ErrFileTooLarge):
		return projectdomain.PreparedFigure{}, projectdomain.ErrFileTooLarge
	case errors.Is(err, mediadomain.ErrUnsupportedType):
		return projectdomain.PreparedFigure{}, projectdomain.ErrUnsupportedType
	case errors.Is(err, mediadomain.ErrImageTooLarge):
		return projectdomain.PreparedFigure{}, projectdomain.ErrImageTooLarge
	case errors.Is(err, mediadomain.ErrInvalidImage):
		return projectdomain.PreparedFigure{}, projectdomain.ErrInvalidImage
	case err != nil:
		return projectdomain.PreparedFigure{}, err
	}
	return projectdomain.PreparedFigure{Data: p.Data, ContentType: p.ContentType, Width: p.Width, Height: p.Height}, nil
}

// objectRemover deletes the objects of deleted figures once the transaction commits. The
// database queued each of them in storage_deletions in that transaction, so a failure here
// is only logged: the cleanup worker deletes them on its next run.
type objectRemover struct {
	files  storage.Storage
	logger *slog.Logger
}

var _ projectdomain.ObjectRemover = objectRemover{}

func (r objectRemover) RemoveAfterCommit(ctx context.Context, keys []string) {
	if len(keys) == 0 {
		return
	}
	postgres.AfterCommit(ctx, func(ctx context.Context) {
		for _, k := range keys {
			if err := r.files.Delete(ctx, k); err != nil {
				r.logger.WarnContext(ctx, "figure object not deleted; the storage cleanup will retry",
					slog.String("object_key", k), slog.Any("error", err))
			}
		}
	})
}
