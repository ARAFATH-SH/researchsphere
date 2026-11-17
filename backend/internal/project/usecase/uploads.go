package usecase

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// Figure uploads go straight from the browser to storage (FR-12.2, docs/decisions.md
// 2026-11-18): StartFigureUpload records the upload and signs a POST of exactly its size to
// a key of its own; CompleteFigureUpload reads what arrived, checks its type by content,
// strips an image's metadata losslessly into the figure's own key and records the figure,
// whose malware scan runs after. An upload never completed expires, and ExpireUploads
// deletes it; the database queues its object for deletion.
const (
	// UploadPostExpiry is how long the presigned POST works: the browser must start sending
	// by then.
	UploadPostExpiry = 15 * time.Minute
	// UploadExpiry is how long an upload may be completed after it starts.
	UploadExpiry = time.Hour
	// UploadSweepBatch is how many expired uploads one round of ExpireUploads deletes.
	UploadSweepBatch = 500
)

// UploadTicket is a started upload: where the browser POSTs the file, with which fields.
type UploadTicket struct {
	ID        string
	URL       *url.URL
	Fields    map[string]string
	ExpiresAt time.Time
}

// StartFigureUpload starts sending a figure of size bytes to path. Errors, in order:
// domain.ErrInvalidPath (also without a figure's extension); the access errors;
// *domain.RateLimitedError; *domain.FigureTooLargeError; *domain.StorageLimitError (the
// project's files, its uploads still being sent and this one); ErrPathTaken,
// ErrParentNotFolder, ErrFileLimit (checked early; again on completion).
func (s *Service) StartFigureUpload(ctx context.Context, userID, projectID, path string, size int64) (UploadTicket, error) {
	if s.d.Objects == nil {
		return UploadTicket{}, errors.New("start figure upload: no file storage configured")
	}
	p, err := CleanPath(path)
	if err != nil {
		return UploadTicket{}, err
	}
	if _, figure := figureType(p); !figure {
		return UploadTicket{}, domain.ErrInvalidPath
	}
	if err := s.fileAccess(ctx, userID, projectID, domain.ActEditFiles, lockNone); err != nil {
		return UploadTicket{}, err
	}
	if err := s.underLimit(ctx, s.d.FigureLimiter, userID, FiguresPerWindow, "figure upload"); err != nil {
		return UploadTicket{}, err
	}
	if size > s.d.FigureMaxBytes {
		return UploadTicket{}, &domain.FigureTooLargeError{LimitBytes: s.d.FigureMaxBytes}
	}
	if size < 1 {
		return UploadTicket{}, domain.ErrUnsupportedType
	}
	now := s.d.Now()
	if err := s.checkStorage(ctx, projectID, size, now.Add(-UploadPostExpiry)); err != nil {
		return UploadTicket{}, err
	}
	if err := s.pathFree(ctx, projectID, p); err != nil {
		return UploadTicket{}, err
	}

	random, err := newUUID()
	if err != nil {
		return UploadTicket{}, err
	}
	key := "projects/" + projectID + "/uploads/" + random
	post, err := s.d.Objects.PresignPost(ctx, key, size, UploadPostExpiry)
	if err != nil {
		return UploadTicket{}, fmt.Errorf("start figure upload: %w", err)
	}
	id, err := s.d.Files.InsertUpload(ctx, domain.Upload{
		ProjectID: projectID, UserID: userID, ObjectKey: key, Path: p, Size: size,
		ExpiresAt: now.Add(UploadExpiry),
	})
	if err != nil {
		return UploadTicket{}, err
	}
	return UploadTicket{ID: id, URL: post.URL, Fields: post.Fields, ExpiresAt: post.ExpiresAt}, nil
}

// checkStorage refuses size more bytes when the project's stored files, and its uploads
// started after since (none when since is zero), leave no room for them.
func (s *Service) checkStorage(ctx context.Context, projectID string, size int64, since time.Time) error {
	used, err := s.d.Files.StorageUsed(ctx, projectID)
	if err != nil {
		return err
	}
	if !since.IsZero() {
		pending, err := s.d.Files.PendingUploadBytes(ctx, projectID, since)
		if err != nil {
			return err
		}
		used += pending
	}
	if used+size > s.d.StorageMaxBytes {
		return &domain.StorageLimitError{UsedBytes: used, LimitBytes: s.d.StorageMaxBytes, NeedBytes: size}
	}
	return nil
}

// pathFree checks, without changing anything, that a new file could go to p: no entry has
// it and no file stands where a folder on the way must be. The project's entry limit counts
// a folder still to be made.
func (s *Service) pathFree(ctx context.Context, projectID, p string) error {
	if _, found, err := s.d.Files.FileAt(ctx, projectID, p); err != nil {
		return err
	} else if found {
		return domain.ErrPathTaken
	}
	missing := 1
	for _, parent := range parentPaths(p) {
		f, found, err := s.d.Files.FileAt(ctx, projectID, parent)
		switch {
		case err != nil:
			return err
		case found && f.Kind != domain.KindFolder:
			return domain.ErrParentNotFolder
		case !found:
			missing++
		}
	}
	n, err := s.d.Files.FileCount(ctx, projectID)
	if err != nil {
		return err
	}
	if n+missing > MaxFiles {
		return domain.ErrFileLimit
	}
	return nil
}

// CompleteFigureUpload records the figure the browser sent for an upload the user started.
// Errors, in order: the access errors; domain.ErrUploadNotFound (unknown, expired, completed,
// or another user's); domain.ErrUploadIncomplete (nothing in storage yet);
// *domain.FigureTooLargeError; ErrUnsupportedType, ErrInvalidImage; ErrTypeMismatch; then,
// under the project's lock, the access errors again, *domain.StorageLimitError,
// ErrPathTaken, ErrParentNotFolder, ErrFileLimit.
func (s *Service) CompleteFigureUpload(ctx context.Context, userID, projectID, uploadID string) (domain.File, error) {
	if s.d.Figures == nil || s.d.Objects == nil {
		return domain.File{}, errors.New("complete figure upload: no file storage configured")
	}
	if err := s.fileAccess(ctx, userID, projectID, domain.ActEditFiles, lockNone); err != nil {
		return domain.File{}, err
	}
	up, err := s.d.Files.Upload(ctx, projectID, uploadID, userID)
	if err != nil {
		return domain.File{}, err
	}
	wantType, figure := figureType(up.Path)
	if !figure {
		return domain.File{}, domain.ErrInvalidPath
	}
	obj, err := s.d.Objects.Stat(ctx, up.ObjectKey)
	if errors.Is(err, domain.ErrObjectMissing) {
		return domain.File{}, domain.ErrUploadIncomplete
	}
	if err != nil {
		return domain.File{}, fmt.Errorf("complete figure upload: %w", err)
	}
	if obj.Size > s.d.FigureMaxBytes || obj.Size > up.Size {
		return domain.File{}, &domain.FigureTooLargeError{LimitBytes: s.d.FigureMaxBytes}
	}

	// First pass: check the type and count what stripping keeps. Every read names the ETag,
	// so a second POST to the upload's key in between is noticed, never mixed in.
	var counted countWriter
	info, err := s.stripObject(ctx, up.ObjectKey, obj.ETag, &counted)
	if err != nil {
		return domain.File{}, err
	}
	if info.ContentType != wantType {
		return domain.File{}, domain.ErrTypeMismatch
	}

	storedID, err := newUUID()
	if err != nil {
		return domain.File{}, err
	}
	key := "projects/" + projectID + "/files/" + storedID
	size := counted.n
	if err := s.storeFigure(ctx, up.ObjectKey, key, obj, info, size); err != nil {
		return domain.File{}, err
	}

	var id string
	err = s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.fileAccess(ctx, userID, projectID, domain.ActEditFiles, lockExclusive); err != nil {
			return err
		}
		claimed, err := s.d.Files.ClaimUpload(ctx, up.ID)
		if err != nil {
			return err
		}
		if !claimed {
			return domain.ErrUploadNotFound // completed by another request, or just expired
		}
		if err := s.checkStorage(ctx, projectID, size, time.Time{}); err != nil {
			return err
		}
		if err := s.makeRoom(ctx, projectID, up.Path, "", userID); err != nil {
			return err
		}
		id, err = s.d.Files.InsertFigure(ctx, domain.NewFigure{
			StoredFileID: storedID, ProjectID: projectID, Path: up.Path, ObjectKey: key,
			ContentType: info.ContentType, Size: size, Width: info.Width, Height: info.Height,
			UploadedBy: userID,
		})
		if err != nil {
			return err
		}
		// The claim queued the upload's object for deletion; delete it now, after the commit.
		s.d.Remover.RemoveAfterCommit(ctx, []string{up.ObjectKey})
		return nil
	})
	if err != nil {
		// No row points at the figure's object, so the cleanup queue does not know it. The
		// upload's own object stays while the upload does, for another try.
		if derr := s.d.Objects.Delete(context.WithoutCancel(ctx), key); derr != nil {
			s.d.Logger.ErrorContext(ctx, "orphaned object after a failed figure upload",
				slog.String("object_key", key), slog.Any("error", derr))
		}
		return domain.File{}, err
	}
	if err := s.d.FigureLimiter.Record(ctx, userID, FigureWindow); err != nil {
		s.d.Logger.WarnContext(ctx, "figure upload limit not recorded", slog.Any("error", err))
	}
	return s.fileByID(ctx, projectID, id)
}

// stripObject runs the figure check over the object's version with this ETag, writing what
// is kept to w.
func (s *Service) stripObject(ctx context.Context, key, etag string, w io.Writer) (domain.FigureInfo, error) {
	r, err := s.d.Objects.Open(ctx, key, etag)
	if err != nil {
		return domain.FigureInfo{}, objectError(err)
	}
	defer func() { _ = r.Close() }()
	return s.d.Figures.StripFigure(r, w)
}

// storeFigure puts the figure at key: a copy inside storage when stripping keeps every byte
// (a PDF, or an image with no metadata), else the stripped image, streamed from the upload
// and checked to be the one the first pass saw.
func (s *Service) storeFigure(ctx context.Context, src, key string, obj domain.ObjectVersion, info domain.FigureInfo, size int64) error {
	if size == obj.Size {
		if err := s.d.Objects.Copy(ctx, src, key, obj.ETag, info.ContentType); err != nil {
			return objectError(err)
		}
		return nil
	}
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		again, err := s.stripObject(ctx, src, obj.ETag, pw)
		if err == nil && again != info {
			err = domain.ErrUploadIncomplete
		}
		_ = pw.CloseWithError(err)
		done <- err
	}()
	putErr := s.d.Objects.Put(ctx, key, pr, size, info.ContentType)
	_ = pr.CloseWithError(errors.New("put ended")) // let the stripping goroutine finish
	stripErr := <-done
	if stripErr != nil {
		_ = s.d.Objects.Delete(context.WithoutCancel(ctx), key)
		return stripErr
	}
	if putErr != nil {
		_ = s.d.Objects.Delete(context.WithoutCancel(ctx), key)
		return fmt.Errorf("store figure: %w", putErr)
	}
	return nil
}

// objectError maps the bucket's answers: a missing upload has not arrived; one written again
// since it was checked must be completed again, so it counts as not arrived either.
func objectError(err error) error {
	if errors.Is(err, domain.ErrObjectMissing) || errors.Is(err, domain.ErrObjectChanged) {
		return domain.ErrUploadIncomplete
	}
	return err
}

// countWriter counts bytes and keeps none.
type countWriter struct{ n int64 }

func (c *countWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

// ExpireUploads deletes the uploads that expired without being completed, a batch at a
// time, and says how many; the database queues their objects for the storage cleanup.
func (s *Service) ExpireUploads(ctx context.Context) (int, error) {
	total := 0
	for {
		n, err := s.d.Files.DeleteExpiredUploads(ctx, UploadSweepBatch)
		total += n
		if err != nil || n < UploadSweepBatch {
			return total, err
		}
	}
}

// RunUploadSweep calls ExpireUploads every interval until ctx ends, logging what it did.
func (s *Service) RunUploadSweep(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		n, err := s.ExpireUploads(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			s.d.Logger.ErrorContext(ctx, "expired figure uploads not deleted", slog.Any("error", err))
		case n > 0:
			s.d.Logger.InfoContext(ctx, "expired figure uploads deleted", slog.Int("count", n))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
