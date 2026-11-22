package usecase

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// Limits of the file operations (docs/decisions.md 2026-11-17).
const (
	// FiguresPerWindow figures a user may upload in FigureWindow; refused files do not count.
	FiguresPerWindow = 30
	FigureWindow     = time.Hour
	// SavesPerWindow content saves a user may send in SaveWindow: room for autosave from
	// several tabs (FR-12.6).
	SavesPerWindow = 120
	SaveWindow     = time.Minute
	// FigureDownloadExpiry is how long a figure's download URL works (NFR-13).
	FigureDownloadExpiry = 5 * time.Minute
)

// FileDeps is what the file operations need besides Deps. Files is required; without a
// FigureIntake or FileObjects figures cannot be uploaded or downloaded; without a Remover the
// storage cleanup alone deletes the objects of deleted figures; without limiters nothing is
// counted. FigureMaxBytes and StorageMaxBytes are FIGURE_MAX_BYTES and
// PROJECT_STORAGE_MAX_BYTES (FR-12.2); 0 means their defaults. Now is the clock, time.Now
// when nil.
type FileDeps struct {
	Files           domain.FileStore
	Figures         domain.FigureIntake
	Objects         domain.FileObjects
	Remover         domain.ObjectRemover
	FigureLimiter   domain.Limiter
	SaveLimiter     domain.Limiter
	FigureMaxBytes  int64
	StorageMaxBytes int64
	Now             func() time.Time

	// Real-time editing (slice 22). Without CollabFiles and Tickets the sync server's
	// operations cannot run; without Live a REST save never checks for a live session;
	// without CollabEvents the sync server learns of deletions at its next check; without a
	// TicketLimiter nothing is counted.
	CollabFiles   domain.CollabStore
	Tickets       domain.TicketStore
	Live          domain.LiveSessions
	CollabEvents  domain.CollabEvents
	TicketLimiter domain.Limiter
}

func (d *FileDeps) defaults() {
	if d.FigureMaxBytes <= 0 {
		d.FigureMaxBytes = DefaultFigureMaxBytes
	}
	if d.StorageMaxBytes <= 0 {
		d.StorageMaxBytes = DefaultStorageMaxBytes
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Remover == nil {
		d.Remover = noRemover{}
	}
	if d.FigureLimiter == nil {
		d.FigureLimiter = noLimit{}
	}
	if d.SaveLimiter == nil {
		d.SaveLimiter = noLimit{}
	}
	if d.TicketLimiter == nil {
		d.TicketLimiter = noLimit{}
	}
	if d.CollabEvents == nil {
		d.CollabEvents = noCollabEvents{}
	}
}

type noCollabEvents struct{}

func (noCollabEvents) FilesDeleted(context.Context, string, []string) {}
func (noCollabEvents) ProjectDeleted(context.Context, string)         {}

type noRemover struct{}

func (noRemover) RemoveAfterCommit(context.Context, []string) {}

// lockMode is how a file operation holds the project row while it checks the caller's role.
type lockMode int

const (
	// lockNone reads the role only: reads.
	lockNone lockMode = iota
	// lockShared holds the project row shared: content saves, which may run side by side but
	// wait for a membership change, so a removed member's save never lands after it.
	lockShared
	// lockExclusive holds the project row: changes to the tree, one at a time per project,
	// so the 200-entry limit and folder moves are judged on a stable tree.
	lockExclusive
)

// fileAccess checks that userID may do action in the project's workspace: a member (else
// domain.ErrNotFound, whatever the project's visibility: the workspace is for members only)
// whose role allows it (domain.ErrForbidden), and, for an edit, whose account is not
// suspended (*domain.SuspendedError).
func (s *Service) fileAccess(ctx context.Context, userID, projectID string, action domain.Action, lock lockMode) error {
	var (
		role domain.Role
		err  error
	)
	switch lock {
	case lockExclusive:
		var a domain.Access
		a, err = s.d.Store.Lock(ctx, projectID, userID)
		role = a.Role
	case lockShared:
		role, err = s.d.Files.LockShared(ctx, projectID, userID)
	default:
		role, err = s.d.Store.Role(ctx, projectID, userID)
	}
	if err != nil {
		return err
	}
	if role == domain.RoleNone {
		return domain.ErrNotFound
	}
	if !s.d.Permissions.Can(role, action) {
		return domain.ErrForbidden
	}
	if action == domain.ActEditFiles {
		return s.refuseSuspended(ctx, userID)
	}
	return nil
}

// Files lists the project's files for a member (FR-12.1), with its storage (FR-12.2).
func (s *Service) Files(ctx context.Context, userID, projectID string) (domain.FileTree, error) {
	if err := s.fileAccess(ctx, userID, projectID, domain.ActReadFiles, lockNone); err != nil {
		return domain.FileTree{}, err
	}
	t, err := s.d.Files.Tree(ctx, projectID)
	if err != nil {
		return domain.FileTree{}, err
	}
	used, err := s.d.Files.StorageUsed(ctx, projectID)
	if err != nil {
		return domain.FileTree{}, err
	}
	t.Storage = domain.Storage{UsedBytes: used, LimitBytes: s.d.StorageMaxBytes, FigureMaxBytes: s.d.FigureMaxBytes}
	return t, nil
}

// FileContent is one file with its text or, for a clean figure, a download URL.
type FileContent struct {
	File      domain.File
	Content   *string
	URL       *url.URL
	ExpiresAt time.Time
}

// File reads one file for a member. A figure gets a URL valid for FigureDownloadExpiry once
// its scan says clean; before, domain.ErrScanPending, ErrScanFailed or ErrFileRemoved.
func (s *Service) File(ctx context.Context, userID, projectID, fileID string) (FileContent, error) {
	if err := s.fileAccess(ctx, userID, projectID, domain.ActReadFiles, lockNone); err != nil {
		return FileContent{}, err
	}
	f, err := s.d.Files.File(ctx, projectID, fileID)
	if err != nil {
		return FileContent{}, err
	}
	out := FileContent{File: f.File}
	switch f.Kind {
	case domain.KindText:
		out.Content = f.Content
	case domain.KindBinary:
		if err := scanAllows(f.ScanStatus); err != nil {
			return FileContent{}, err
		}
		if s.d.Objects == nil || f.ObjectKey == nil || f.ContentType == nil {
			return FileContent{}, errors.New("read figure: no file storage configured")
		}
		expires := time.Now().Add(FigureDownloadExpiry)
		u, err := s.d.Objects.PresignDownload(ctx, *f.ObjectKey, FigureDownloadExpiry, baseName(f.Path), *f.ContentType)
		if err != nil {
			return FileContent{}, fmt.Errorf("presign figure: %w", err)
		}
		out.URL, out.ExpiresAt = u, expires
	}
	return out, nil
}

func scanAllows(st *domain.ScanStatus) error {
	if st == nil {
		return domain.ErrScanPending
	}
	switch *st {
	case domain.ScanClean:
		return nil
	case domain.ScanFailed:
		return domain.ErrScanFailed
	case domain.ScanInfected:
		return domain.ErrFileRemoved
	}
	return domain.ErrScanPending
}

// CreateFileInput is a new text file or folder as the caller sends it.
type CreateFileInput struct {
	Path    string
	Kind    domain.FileKind // text or folder
	Content *string
}

// CreateFile adds a text file or a folder, and the folders on the way (FR-12.1). Errors, in
// order: domain.ErrInvalidPath and *domain.FieldError ("kind", "content"); the access errors
// of fileAccess; domain.ErrFileTooLarge; ErrPathTaken, ErrParentNotFolder, ErrFileLimit.
func (s *Service) CreateFile(ctx context.Context, userID, projectID string, in CreateFileInput) (domain.File, error) {
	p, err := CleanPath(in.Path)
	if err != nil {
		return domain.File{}, err
	}
	content := ""
	switch in.Kind {
	case domain.KindText:
		if _, figure := figureType(p); figure {
			return domain.File{}, domain.ErrInvalidPath
		}
		if in.Content != nil {
			content = *in.Content
		}
		if err := checkText(content); err != nil && !errors.Is(err, domain.ErrFileTooLarge) {
			return domain.File{}, err
		}
	case domain.KindFolder:
		if in.Content != nil {
			return domain.File{}, &domain.FieldError{Field: "content"}
		}
	default:
		return domain.File{}, &domain.FieldError{Field: "kind"}
	}
	var id string
	err = s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.fileAccess(ctx, userID, projectID, domain.ActEditFiles, lockExclusive); err != nil {
			return err
		}
		if len(content) > MaxTextBytes {
			return domain.ErrFileTooLarge
		}
		if err := s.makeRoom(ctx, projectID, p, "", userID); err != nil {
			return err
		}
		var err error
		if in.Kind == domain.KindFolder {
			id, err = s.d.Files.InsertFolder(ctx, projectID, p, userID)
		} else {
			id, err = s.d.Files.InsertText(ctx, projectID, p, content, userID)
		}
		return err
	})
	if err != nil {
		return domain.File{}, err
	}
	return s.fileByID(ctx, projectID, id)
}

// makeRoom readies path p for an entry: it must be free, ignoring letter case, unless the
// entry there is self (a rename that changes only letter case), and the folders on the way
// are created when missing (domain.ErrParentNotFolder when one is a file). Call it with the
// project locked.
func (s *Service) makeRoom(ctx context.Context, projectID, p, self, userID string) error {
	if f, found, err := s.d.Files.FileAt(ctx, projectID, p); err != nil {
		return err
	} else if found && f.ID != self {
		return domain.ErrPathTaken
	}
	for _, parent := range parentPaths(p) {
		f, found, err := s.d.Files.FileAt(ctx, projectID, parent)
		switch {
		case err != nil:
			return err
		case found && f.Kind != domain.KindFolder:
			return domain.ErrParentNotFolder
		case found:
			continue
		}
		if _, err := s.d.Files.InsertFolder(ctx, projectID, parent, userID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) fileByID(ctx context.Context, projectID, fileID string) (domain.File, error) {
	f, err := s.d.Files.File(ctx, projectID, fileID)
	if err != nil {
		return domain.File{}, err
	}
	return f.File, nil
}

// MoveFile renames or moves a file or folder; a folder moves with everything in it, and the
// project's main file follows (FR-12.1). Errors: domain.ErrInvalidPath, the access errors,
// domain.ErrFileNotFound, ErrInvalidMove, ErrPathTaken, ErrParentNotFolder, ErrFileLimit (a
// folder created on the way).
func (s *Service) MoveFile(ctx context.Context, userID, projectID, fileID, path string) (domain.File, error) {
	p, err := CleanPath(path)
	if err != nil {
		return domain.File{}, err
	}
	err = s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.fileAccess(ctx, userID, projectID, domain.ActEditFiles, lockExclusive); err != nil {
			return err
		}
		f, err := s.d.Files.LockFile(ctx, projectID, fileID)
		if err != nil {
			return err
		}
		if p == f.Path {
			return nil
		}
		if err := movable(f.File, p); err != nil {
			return err
		}
		if err := s.makeRoom(ctx, projectID, p, f.ID, userID); err != nil {
			return err
		}
		return s.d.Files.Move(ctx, projectID, f.Path, p, userID)
	})
	if err != nil {
		return domain.File{}, err
	}
	return s.fileByID(ctx, projectID, fileID)
}

// movable checks that f may go to p: a folder not into itself, a text file not to a figure's
// extension, a figure only to an extension of its own type.
func movable(f domain.File, p string) error {
	ct, figure := figureType(p)
	switch f.Kind {
	case domain.KindFolder:
		if !strings.EqualFold(p, f.Path) && under(p, f.Path) {
			return domain.ErrInvalidMove
		}
	case domain.KindText:
		if figure {
			return domain.ErrInvalidPath
		}
	case domain.KindBinary:
		if !figure || f.ContentType == nil || ct != *f.ContentType {
			return domain.ErrInvalidPath
		}
	}
	return nil
}

// DeleteFile deletes a file, or a folder with everything in it (FR-12.1). The figures'
// objects are deleted after the commit. Errors: the access errors, domain.ErrFileNotFound.
func (s *Service) DeleteFile(ctx context.Context, userID, projectID, fileID string) error {
	return s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.fileAccess(ctx, userID, projectID, domain.ActEditFiles, lockExclusive); err != nil {
			return err
		}
		f, err := s.d.Files.LockFile(ctx, projectID, fileID)
		if err != nil {
			return err
		}
		keys, err := s.d.Files.ObjectKeysUnder(ctx, projectID, f.Path)
		if err != nil {
			return err
		}
		var texts []string
		if s.d.CollabFiles != nil {
			if texts, err = s.d.CollabFiles.TextFileIDsUnder(ctx, projectID, f.Path); err != nil {
				return err
			}
		}
		if err := s.d.Files.DeleteTree(ctx, projectID, f.Path); err != nil {
			return err
		}
		s.d.Remover.RemoveAfterCommit(ctx, keys)
		if len(texts) > 0 {
			// Open documents of these files close on the sync server (docs/sync.md, 4404).
			s.d.CollabEvents.FilesDeleted(ctx, projectID, texts)
		}
		return nil
	})
}

// SaveContent stores a text file's content when version is still the file's version
// (FR-12.6); otherwise *domain.VersionConflictError with what the file holds now. Errors, in
// order: *domain.FieldError ("content", "version"); *domain.RateLimitedError after
// SavesPerWindow saves in SaveWindow; the access errors; domain.ErrFileNotFound,
// ErrNotTextFile, ErrFileTooLarge; domain.ErrLiveSessionOpen or ErrLiveCheckUnavailable
// while the file is, or may be, open on the sync server (slice 22).
func (s *Service) SaveContent(ctx context.Context, userID, projectID, fileID, content string, version int) (domain.File, error) {
	textErr := checkText(content)
	if textErr != nil && !errors.Is(textErr, domain.ErrFileTooLarge) {
		return domain.File{}, textErr
	}
	if version < 1 {
		return domain.File{}, &domain.FieldError{Field: "version"}
	}
	if err := s.underLimit(ctx, s.d.SaveLimiter, userID, SavesPerWindow, "file save"); err != nil {
		return domain.File{}, err
	}
	err := s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.fileAccess(ctx, userID, projectID, domain.ActEditFiles, lockShared); err != nil {
			return err
		}
		f, err := s.d.Files.LockFile(ctx, projectID, fileID)
		if err != nil {
			return err
		}
		if f.Kind != domain.KindText {
			return domain.ErrNotTextFile
		}
		if textErr != nil {
			return textErr
		}
		if err := s.refuseLive(ctx, f.ID); err != nil {
			return err
		}
		if f.Version != version {
			c := &domain.VersionConflictError{Version: f.Version, UpdatedAt: f.UpdatedAt}
			if f.Content != nil {
				c.Content = *f.Content
			}
			return c
		}
		_, err = s.d.Files.SaveContent(ctx, f.ID, content, userID)
		return err
	})
	if err != nil {
		return domain.File{}, err
	}
	if err := s.d.SaveLimiter.Record(ctx, userID, SaveWindow); err != nil {
		s.d.Logger.WarnContext(ctx, "file save limit not recorded", slog.Any("error", err))
	}
	return s.fileByID(ctx, projectID, fileID)
}

// addMainFile writes the starting main.tex of a project (FR-10.4), with the owner's profile
// name as the author, in the caller's transaction.
func (s *Service) addMainFile(ctx context.Context, projectID, title, ownerID string) error {
	name, err := s.d.Files.FullName(ctx, ownerID)
	if err != nil {
		return err
	}
	_, err = s.d.Files.InsertText(ctx, projectID, MainFileName, MainTex(title, name), ownerID)
	return err
}

// BackfillBatch is how many projects one round of BackfillMainFiles reads.
const BackfillBatch = 100

// BackfillMainFiles gives main.tex to every project that has no file (FR-10.4 for projects
// made before slice 21), one transaction per project, and returns how many it gave one. A
// project that gained a file meanwhile is skipped, so running it again changes nothing.
func (s *Service) BackfillMainFiles(ctx context.Context) (int, error) {
	added, after := 0, ""
	for {
		seeds, err := s.d.Files.ProjectsWithoutFiles(ctx, after, BackfillBatch)
		if err != nil {
			return added, err
		}
		for _, id := range seeds {
			done := false
			err := s.d.Tx.InTx(ctx, func(ctx context.Context) error {
				a, err := s.d.Store.Lock(ctx, id, "")
				if errors.Is(err, domain.ErrNotFound) {
					return nil // deleted meanwhile
				}
				if err != nil {
					return err
				}
				n, err := s.d.Files.FileCount(ctx, id)
				if err != nil || n > 0 {
					return err
				}
				done = true
				return s.addMainFile(ctx, id, a.Title, a.OwnerID)
			})
			if err != nil {
				return added, fmt.Errorf("main.tex for project %s: %w", id, err)
			}
			if done {
				added++
			}
			after = id
		}
		if len(seeds) < BackfillBatch {
			return added, nil
		}
	}
}

// newUUID returns a random (version 4) UUID.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("new ID: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
