package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	mediadomain "github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
	mediausecase "github.com/ARAFATH-SH/researchsphere/backend/internal/media/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/storage"
	projectdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// Project files (FR-12.1, FR-12.2) join the project module, which decides who may read and
// write them, with media, which checks a figure's type and scans it for malware (the scan
// worker takes every pending stored file, figures included), and the bucket.

// figureIntake gives the project module the media module's figure check, which strips an
// image's metadata losslessly (docs/decisions.md 2026-11-18).
type figureIntake struct{}

var _ projectdomain.FigureIntake = figureIntake{}

func (figureIntake) StripFigure(r io.Reader, w io.Writer) (projectdomain.FigureInfo, error) {
	info, err := mediausecase.StripFigure(r, w)
	switch {
	case errors.Is(err, mediadomain.ErrUnsupportedType):
		return projectdomain.FigureInfo{}, projectdomain.ErrUnsupportedType
	case errors.Is(err, mediadomain.ErrInvalidImage):
		return projectdomain.FigureInfo{}, projectdomain.ErrInvalidImage
	case err != nil:
		return projectdomain.FigureInfo{}, err
	}
	return projectdomain.FigureInfo{ContentType: info.ContentType, Width: info.Width, Height: info.Height}, nil
}

// figureObjects is the bucket as the project module sees it: storage's answers mapped to
// the project domain's.
type figureObjects struct {
	storage.Storage
}

var _ projectdomain.FileObjects = figureObjects{}

func (o figureObjects) PresignPost(ctx context.Context, key string, size int64, expiry time.Duration) (projectdomain.PostUpload, error) {
	p, err := o.Storage.PresignPost(ctx, key, size, expiry)
	if err != nil {
		return projectdomain.PostUpload{}, err
	}
	return projectdomain.PostUpload{URL: p.URL, Fields: p.Fields, ExpiresAt: p.ExpiresAt}, nil
}

func (o figureObjects) Stat(ctx context.Context, key string) (projectdomain.ObjectVersion, error) {
	info, err := o.Storage.Stat(ctx, key)
	if err != nil {
		return projectdomain.ObjectVersion{}, objectError(err)
	}
	return projectdomain.ObjectVersion{Size: info.Size, ETag: info.ETag}, nil
}

func (o figureObjects) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	r, err := o.Storage.Get(ctx, key)
	if err != nil {
		return nil, objectError(err)
	}
	return r, nil
}

func (o figureObjects) Open(ctx context.Context, key, etag string) (io.ReadCloser, error) {
	r, err := o.GetIfMatch(ctx, key, etag)
	if err != nil {
		return nil, objectError(err)
	}
	return r, nil
}

func (o figureObjects) Copy(ctx context.Context, src, dst, etag, contentType string) error {
	return objectError(o.CopyIfMatch(ctx, src, dst, etag, contentType))
}

func objectError(err error) error {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		return projectdomain.ErrObjectMissing
	case errors.Is(err, storage.ErrChanged):
		return projectdomain.ErrObjectChanged
	}
	return err
}

// figureLimits are FIGURE_MAX_BYTES and PROJECT_STORAGE_MAX_BYTES (FR-12.2).
type figureLimits struct {
	figure, storage int64
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
