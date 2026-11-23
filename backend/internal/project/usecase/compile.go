package usecase

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// Compile and archive support (FR-13.1, FR-13.7; slice 23, docs/compile.md). The compile
// module owns the compile jobs; it asks this module who may compile and what the project's
// files are, so the role rules and the files stay in one place.

// Limits of the archive and the live-text flush.
const (
	// FlushTimeout is how long a compile waits for the sync server to store open documents.
	// It saves within 4 s of the oldest unsaved change anyway (NFR-10.1).
	FlushTimeout = 5 * time.Second
	// ArchivesPerWindow ZIP archives a user may download in ArchiveWindow.
	ArchivesPerWindow = 10
	ArchiveWindow     = time.Hour
	// DefaultArchiveMaxBytes is the largest archive by default (ARCHIVE_MAX_BYTES): a project
	// at its 500 MB figure limit with 200 text files fits.
	DefaultArchiveMaxBytes = 768 << 20
	// maxArchiveName is the longest file name of an archive, before ".zip".
	maxArchiveName = 100
)

// Authorize checks that userID may do action in the project now: members only (a non-member,
// an administrator included, gets domain.ErrNotFound), the role (domain.ErrForbidden), and
// for writing actions and compiling a suspended account (*domain.SuspendedError). The role is
// read now, never cached.
func (s *Service) Authorize(ctx context.Context, userID, projectID string, action domain.Action) error {
	if s.d.Files == nil {
		return errors.New("project: files are not configured")
	}
	return s.fileAccess(ctx, userID, projectID, action, lockNone)
}

// CompileSource reads what a compile of the project needs: its main file and every entry
// with its text or object key (FR-13.1). When a text file is open on the sync server it first
// asks the sync server to store the project's documents, so the compile sees live edits
// (docs/sync.md, flush); Source.LiveConfirmed says whether it confirmed in time.
// domain.ErrNotFound when there is no project.
func (s *Service) CompileSource(ctx context.Context, projectID string) (domain.Source, error) {
	if s.d.Files == nil {
		return domain.Source{}, errors.New("project: files are not configured")
	}
	main, err := s.d.Files.MainFile(ctx, projectID)
	if err != nil {
		return domain.Source{}, err
	}
	files, err := s.d.Files.SourceFiles(ctx, projectID)
	if err != nil {
		return domain.Source{}, err
	}
	confirmed, err := s.flushLive(ctx, projectID, files)
	if err != nil {
		return domain.Source{}, err
	}
	if confirmed.flushed {
		// The sync server stored its documents: read the text it wrote.
		if files, err = s.d.Files.SourceFiles(ctx, projectID); err != nil {
			return domain.Source{}, err
		}
	}
	return domain.Source{MainFilePath: main, Files: files, LiveConfirmed: confirmed.ok}, nil
}

type flushOutcome struct {
	ok      bool // the stored text holds every live edit the sync server had
	flushed bool // the sync server confirmed a flush
}

// flushLive asks the sync server to store the project's documents when any text file is open
// there. When it cannot be told, it asks anyway.
func (s *Service) flushLive(ctx context.Context, projectID string, files []domain.StoredFile) (flushOutcome, error) {
	if s.d.Live == nil {
		return flushOutcome{ok: true}, nil
	}
	var ids []string
	for _, f := range files {
		if f.Kind == domain.KindText {
			ids = append(ids, f.ID)
		}
	}
	if len(ids) == 0 {
		return flushOutcome{ok: true}, nil
	}
	live, err := s.d.Live.AnyLive(ctx, ids)
	if err != nil {
		s.d.Logger.WarnContext(ctx, "live session check failed before a compile, asking for a flush", slog.Any("error", err))
		live = true
	}
	if !live {
		return flushOutcome{ok: true}, nil
	}
	if s.d.Flusher == nil {
		return flushOutcome{}, nil
	}
	ok, err := s.d.Flusher.FlushLive(ctx, projectID, FlushTimeout)
	if err != nil {
		if ctx.Err() != nil {
			return flushOutcome{}, ctx.Err()
		}
		s.d.Logger.WarnContext(ctx, "flush of live documents failed, compiling the stored text", slog.Any("error", err))
		return flushOutcome{}, nil
	}
	if !ok {
		s.d.Logger.WarnContext(ctx, "the sync server did not confirm the flush in time, compiling the stored text",
			slog.String("project_id", projectID))
	}
	return flushOutcome{ok: ok, flushed: ok}, nil
}

// PrepareArchive checks that a member may download the project's files as a ZIP archive
// (FR-13.7) and lists what it will hold: folders, text files and figures whose scan found them
// clean. Errors, in order: *domain.RateLimitedError after ArchivesPerWindow in ArchiveWindow;
// the access errors of reading files; domain.ErrArchiveTooLarge over ArchiveMaxBytes. Only an
// archive that will be written counts against the limit.
func (s *Service) PrepareArchive(ctx context.Context, userID, projectID string) (domain.Archive, error) {
	if s.d.Files == nil {
		return domain.Archive{}, errors.New("project: files are not configured")
	}
	if err := s.underLimit(ctx, s.d.ArchiveLimiter, userID, ArchivesPerWindow, "archive"); err != nil {
		return domain.Archive{}, err
	}
	if err := s.fileAccess(ctx, userID, projectID, domain.ActReadFiles, lockNone); err != nil {
		return domain.Archive{}, err
	}
	title, err := s.d.Files.ProjectTitle(ctx, projectID)
	if err != nil {
		return domain.Archive{}, err
	}
	all, err := s.d.Files.SourceFiles(ctx, projectID)
	if err != nil {
		return domain.Archive{}, err
	}
	a := domain.Archive{Name: ArchiveName(title) + ".zip"}
	var total int64
	for _, f := range all {
		if f.Kind == domain.KindBinary && (f.ScanStatus == nil || *f.ScanStatus != domain.ScanClean || f.ObjectKey == nil) {
			a.Skipped++
			continue
		}
		total += f.Size
		a.Files = append(a.Files, f)
	}
	if total > s.d.ArchiveMaxBytes {
		return domain.Archive{}, domain.ErrArchiveTooLarge
	}
	if err := s.d.ArchiveLimiter.Record(ctx, userID, ArchiveWindow); err != nil {
		s.d.Logger.WarnContext(ctx, "archive limit not recorded", slog.Any("error", err))
	}
	return a, nil
}

// WriteArchive writes the archive's entries to w as a ZIP: text compressed, figures stored as
// they are (PNG, JPEG and PDF are compressed already), each with its last change time. A
// figure whose object is gone since it was listed is left out. The error of w ends it.
func (s *Service) WriteArchive(ctx context.Context, a domain.Archive, w io.Writer) error {
	zw := zip.NewWriter(w)
	for _, f := range a.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		h := &zip.FileHeader{Name: f.Path, Modified: f.UpdatedAt.UTC(), Method: zip.Store}
		switch f.Kind {
		case domain.KindFolder:
			h.Name += "/"
			if _, err := zw.CreateHeader(h); err != nil {
				return err
			}
		case domain.KindText:
			h.Method = zip.Deflate
			fw, err := zw.CreateHeader(h)
			if err != nil {
				return err
			}
			if f.Content != nil {
				if _, err := io.WriteString(fw, *f.Content); err != nil {
					return err
				}
			}
		case domain.KindBinary:
			if err := s.copyFigure(ctx, zw, h, *f.ObjectKey); err != nil {
				return err
			}
		}
	}
	return zw.Close()
}

func (s *Service) copyFigure(ctx context.Context, zw *zip.Writer, h *zip.FileHeader, key string) error {
	r, err := s.d.Objects.Get(ctx, key)
	if errors.Is(err, domain.ErrObjectMissing) {
		s.d.Logger.WarnContext(ctx, "figure missing from storage, left out of the archive", slog.String("object_key", key))
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	fw, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	if _, err := io.Copy(fw, r); err != nil {
		return fmt.Errorf("copy figure %s: %w", key, err)
	}
	return nil
}

// ArchiveName makes a file name from a project's title: control characters and the
// characters file systems refuse become "_", runs of space one space, at most 100
// characters; "project" when nothing is left.
func ArchiveName(title string) string {
	var b strings.Builder
	space := false
	n := 0
	for _, r := range strings.TrimSpace(title) {
		if n == maxArchiveName {
			break
		}
		switch {
		case unicode.IsSpace(r):
			if space {
				continue
			}
			space = true
			b.WriteRune(' ')
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || strings.ContainsRune(`/\:*?"<>|`, r):
			space = false
			b.WriteRune('_')
		default:
			space = false
			b.WriteRune(r)
		}
		n++
	}
	name := strings.Trim(b.String(), " .")
	if name == "" {
		return "project"
	}
	return name
}
