package domain

import (
	"context"
	"errors"
	"io"
	"net/url"
	"time"
)

// Project files (FR-10.4, FR-12.1, FR-12.2, FR-12.6; slice 21, docs/decisions.md 2026-11-17).
// A project holds text files (their text in the database), figures (objects in the bucket,
// scanned for malware) and folders, at most 200 in all.

// Actions of SRS 2.3's project table for files.
const (
	// ActReadFiles is the row "Open the workspace, read files, view the PDF".
	ActReadFiles Action = "read_files"
	// ActEditFiles is the row "Edit files, upload figures".
	ActEditFiles Action = "edit_files"
)

// FileKind is what a project file is. The values match the file_kind enum.
type FileKind string

// File kinds.
const (
	KindText   FileKind = "text"
	KindBinary FileKind = "binary" // a figure (FR-12.2)
	KindFolder FileKind = "folder"
)

// ScanStatus is a figure's malware scan. The values match stored_files.scan_status.
type ScanStatus string

// Scan states.
const (
	ScanPending  ScanStatus = "pending"
	ScanClean    ScanStatus = "clean"
	ScanInfected ScanStatus = "infected"
	ScanFailed   ScanStatus = "failed"
)

// File is a file or folder of a project.
type File struct {
	ID        string
	ProjectID string
	Path      string
	Kind      FileKind
	Size      int64
	Version   int
	// ContentType and ScanStatus are set for a figure only.
	ContentType *string
	ScanStatus  *ScanStatus
	UpdatedAt   time.Time
	UpdatedBy   *Person // nil when unknown or the account is gone
}

// StoredFile is a File with what is kept beside it: a text file's content, a figure's
// object key.
type StoredFile struct {
	File
	Content   *string
	ObjectKey *string
}

// FileTree is a project's files and its main file.
type FileTree struct {
	MainFilePath string
	Files        []File // sorted by path, ignoring letter case
}

// NewFigure is an uploaded figure to record: its stored file and its project file.
type NewFigure struct {
	StoredFileID  string
	ProjectID     string
	Path          string
	ObjectKey     string
	ContentType   string
	Size          int64
	Width, Height int // 0 for a PDF
	UploadedBy    string
}

// Errors of the file operations.
var (
	// ErrFileNotFound covers a file that does not exist in the project and an ID that is not
	// a UUID.
	ErrFileNotFound = errors.New("project: file not found")
	// ErrInvalidPath refuses a path that breaks the path rules, a figure's extension on a
	// text file, or a figure's path without an extension of its type.
	ErrInvalidPath = errors.New("project: invalid file path")
	// ErrInvalidMove refuses moving a folder into itself or below itself.
	ErrInvalidMove = errors.New("project: a folder cannot move into itself")
	// ErrPathTaken refuses a path another entry has, ignoring letter case.
	ErrPathTaken = errors.New("project: the path is taken")
	// ErrParentNotFolder refuses a path through a file.
	ErrParentNotFolder = errors.New("project: a part of the path is a file")
	// ErrFileLimit refuses a 201st entry.
	ErrFileLimit = errors.New("project: too many files")
	// ErrNotTextFile refuses saving content into a folder or a figure.
	ErrNotTextFile = errors.New("project: not a text file")
	// ErrFileTooLarge refuses text over MaxTextBytes or a figure over MaxFigureBytes.
	ErrFileTooLarge = errors.New("project: file too large")
	// ErrUnsupportedType refuses a figure that is not PNG, JPEG or PDF by its content.
	ErrUnsupportedType = errors.New("project: unsupported figure type")
	// ErrTypeMismatch refuses a figure whose content is not the type its path's extension
	// names.
	ErrTypeMismatch = errors.New("project: the figure's type does not match its extension")
	// ErrImageTooLarge and ErrInvalidImage are media's image checks.
	ErrImageTooLarge = errors.New("project: image too large")
	ErrInvalidImage  = errors.New("project: invalid image")
	// ErrScanPending, ErrScanFailed and ErrFileRemoved refuse a figure's download until its
	// scan says clean.
	ErrScanPending = errors.New("project: scan pending")
	ErrScanFailed  = errors.New("project: scan failed")
	ErrFileRemoved = errors.New("project: file removed")
)

// VersionConflictError refuses a save whose version is not the file's (FR-12.6).
type VersionConflictError struct {
	Version   int
	Content   string
	UpdatedAt time.Time
}

func (e *VersionConflictError) Error() string { return "project: the file changed since this version" }

// FileStore keeps project files. Every method runs on the transaction the context carries, if
// any. Writes map the database rules to ErrPathTaken, ErrParentNotFolder and ErrFileLimit.
type FileStore interface {
	// Tree lists the project's files.
	Tree(ctx context.Context, projectID string) (FileTree, error)
	// File reads one file of the project; ErrFileNotFound when there is none.
	File(ctx context.Context, projectID, fileID string) (StoredFile, error)
	// LockFile is File with the row locked until the transaction ends.
	LockFile(ctx context.Context, projectID, fileID string) (StoredFile, error)
	// FileAt finds the entry at path, ignoring letter case; false when there is none.
	FileAt(ctx context.Context, projectID, path string) (File, bool, error)
	// InsertFolder and InsertText add an entry and return its ID.
	InsertFolder(ctx context.Context, projectID, path, by string) (string, error)
	InsertText(ctx context.Context, projectID, path, content, by string) (string, error)
	// InsertFigure adds the stored file, owned by the project, and its project file.
	InsertFigure(ctx context.Context, f NewFigure) (string, error)
	// SaveContent stores a text file's content, raises its version, clears its Yjs state
	// and returns the new version.
	SaveContent(ctx context.Context, fileID, content, by string) (int, error)
	// Move moves the entry at from, and everything under it, to to; the project's main file
	// follows when it moves.
	Move(ctx context.Context, projectID, from, to, by string) error
	// ObjectKeysUnder returns the object keys of the figures at path and under it.
	ObjectKeysUnder(ctx context.Context, projectID, path string) ([]string, error)
	// DeleteTree deletes the entry at path and everything under it.
	DeleteTree(ctx context.Context, projectID, path string) error
	// ObjectKeys returns the object keys of all the project's figures.
	ObjectKeys(ctx context.Context, projectID string) ([]string, error)
	// FileCount counts the project's entries.
	FileCount(ctx context.Context, projectID string) (int, error)
	// FullName is the user's profile name; "" when there is none.
	FullName(ctx context.Context, userID string) (string, error)
	// ProjectsWithoutFiles returns the IDs of up to limit projects with no file, IDs after
	// after (none when empty), in ID order.
	ProjectsWithoutFiles(ctx context.Context, after string, limit int) ([]string, error)
	// LockShared is the user's role in the project, RoleNone without a member row, with the
	// project row locked shared until the transaction ends; ErrNotFound when there is no
	// project.
	LockShared(ctx context.Context, projectID, userID string) (Role, error)
}

// PreparedFigure is an upload the type check accepted, ready to store.
type PreparedFigure struct {
	Data          []byte
	ContentType   string
	Width, Height int // 0 for a PDF
}

// FigureIntake checks a figure by its content (PNG, JPEG or PDF) and encodes an image again,
// dropping EXIF and anything else it carries. The media module owns the checks; cmd/api
// implements this. Errors: ErrFileTooLarge, ErrUnsupportedType, ErrImageTooLarge,
// ErrInvalidImage.
type FigureIntake interface {
	PrepareFigure(ctx context.Context, data []byte) (PreparedFigure, error)
}

// FileObjects is the bucket, as project files use it.
type FileObjects interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	// Delete removes the object. A key with no object is not an error.
	Delete(ctx context.Context, key string) error
	// PresignDownload returns a URL that downloads the object as a file named filename, with
	// the given content type, until expiry.
	PresignDownload(ctx context.Context, key string, expiry time.Duration, filename, contentType string) (*url.URL, error)
}

// ObjectRemover deletes objects once the transaction the context carries commits, at once
// outside one, and never after a rollback. A failure is logged; the storage_deletions queue,
// filled by the database in the same transaction, tries again.
type ObjectRemover interface {
	RemoveAfterCommit(ctx context.Context, keys []string)
}
