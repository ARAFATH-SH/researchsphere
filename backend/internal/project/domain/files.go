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
	Storage      Storage
}

// Storage is a project's figure storage and its limits (FR-12.2).
type Storage struct {
	UsedBytes      int64 // the project's stored files, except those removed as malware
	LimitBytes     int64 // PROJECT_STORAGE_MAX_BYTES
	FigureMaxBytes int64 // FIGURE_MAX_BYTES
}

// Upload is a figure the browser sends straight to storage, until it is completed
// (docs/decisions.md 2026-11-18).
type Upload struct {
	ID        string
	ProjectID string
	UserID    string
	ObjectKey string // where the browser sends it: projects/{projectId}/uploads/{random}
	Path      string // where the figure goes
	Size      int64  // exactly what storage accepts
	ExpiresAt time.Time
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
	// ErrFileTooLarge refuses text over MaxTextBytes; a figure over the figure limit is a
	// *FigureTooLargeError, which matches it too.
	ErrFileTooLarge = errors.New("project: file too large")
	// ErrUnsupportedType refuses a figure that is not PNG, JPEG or PDF by its content.
	ErrUnsupportedType = errors.New("project: unsupported figure type")
	// ErrTypeMismatch refuses a figure whose content is not the type its path's extension
	// names.
	ErrTypeMismatch = errors.New("project: the figure's type does not match its extension")
	// ErrInvalidImage is media's figure check: an image whose structure cannot be read.
	ErrInvalidImage = errors.New("project: invalid image")
	// ErrScanPending, ErrScanFailed and ErrFileRemoved refuse a figure's download until its
	// scan says clean.
	ErrScanPending = errors.New("project: scan pending")
	ErrScanFailed  = errors.New("project: scan failed")
	ErrFileRemoved = errors.New("project: file removed")
	// ErrUploadNotFound covers an upload that does not exist, expired, was completed, or is
	// another user's or another project's.
	ErrUploadNotFound = errors.New("project: upload not found")
	// ErrUploadIncomplete refuses completing an upload whose file is not in storage yet.
	ErrUploadIncomplete = errors.New("project: the upload has not reached storage")
	// ErrMainFileInvalid refuses a main file that is not an existing .tex text file (FR-13.1).
	ErrMainFileInvalid = errors.New("project: the main file must be an existing .tex text file")
	// ErrArchiveTooLarge refuses a ZIP archive over the archive limit (FR-13.7).
	ErrArchiveTooLarge = errors.New("project: the archive would be too large")
)

// Source is what a compile reads (FR-13.1; slice 23): every entry of the project, text with
// its content and figures with their object key and scan status, sorted by path.
// LiveConfirmed is false when files were open on the sync server and it did not confirm in
// time that it stored them, so edits of the last seconds may be missing.
type Source struct {
	MainFilePath  string
	Files         []StoredFile
	LiveConfirmed bool
}

// Archive is a ZIP archive to write (FR-13.7): its file name and the entries in it.
type Archive struct {
	Name    string
	Files   []StoredFile // folders, text, and clean figures, sorted by path
	Skipped int          // figures left out because their scan has not found them clean
}

// LiveFlusher asks the sync server to store the project's open documents now and says
// whether it confirmed within timeout (docs/sync.md, flush). false, nil when it did not
// answer in time.
type LiveFlusher interface {
	FlushLive(ctx context.Context, projectID string, timeout time.Duration) (bool, error)
}

// FigureTooLargeError refuses a figure over the largest figure (FR-12.2).
type FigureTooLargeError struct {
	LimitBytes int64
}

func (e *FigureTooLargeError) Error() string { return "project: figure too large" }

// Is makes errors.Is(err, ErrFileTooLarge) hold.
func (e *FigureTooLargeError) Is(target error) bool { return target == ErrFileTooLarge }

// StorageLimitError refuses a figure the project's storage cannot hold (FR-12.2). UsedBytes
// counts its stored files and, when an upload starts, the uploads still being sent.
type StorageLimitError struct {
	UsedBytes, LimitBytes, NeedBytes int64
}

func (e *StorageLimitError) Error() string { return "project: storage limit reached" }

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
	// StorageUsed is the bytes of the project's stored files, except those removed as
	// malware.
	StorageUsed(ctx context.Context, projectID string) (int64, error)
	// PendingUploadBytes is the bytes of the project's uploads started after since that have
	// not expired.
	PendingUploadBytes(ctx context.Context, projectID string, since time.Time) (int64, error)
	// InsertUpload records an upload and returns its ID.
	InsertUpload(ctx context.Context, u Upload) (string, error)
	// Upload reads the user's upload in the project; ErrUploadNotFound when there is none or
	// it expired.
	Upload(ctx context.Context, projectID, uploadID, userID string) (Upload, error)
	// ClaimUpload deletes the upload, if it is still there and not expired, and says so; the
	// database queues its object for deletion.
	ClaimUpload(ctx context.Context, uploadID string) (bool, error)
	// DeleteExpiredUploads deletes up to limit expired uploads and says how many; the
	// database queues their objects for deletion.
	DeleteExpiredUploads(ctx context.Context, limit int) (int, error)
	// SourceFiles lists every entry of the project with what is kept beside it: text with
	// its content, figures with their object key and scan status; sorted by path.
	SourceFiles(ctx context.Context, projectID string) ([]StoredFile, error)
	// MainFile is the project's main file path; ErrNotFound when there is no project.
	MainFile(ctx context.Context, projectID string) (string, error)
	// SetMainFile sets the project's main file path.
	SetMainFile(ctx context.Context, projectID, path string) error
	// ProjectTitle is the project's title; ErrNotFound when there is none.
	ProjectTitle(ctx context.Context, projectID string) (string, error)
}

// FigureInfo is what the figure check found.
type FigureInfo struct {
	ContentType   string
	Width, Height int // 0 for a PDF
}

// FigureIntake checks a figure by its content (PNG, JPEG or PDF) and copies it from r to w,
// an image without its metadata, losslessly, and a PDF as it is. The media module owns the
// checks; cmd/api implements this. Errors: ErrUnsupportedType, ErrInvalidImage, or the error
// of r or w.
type FigureIntake interface {
	StripFigure(r io.Reader, w io.Writer) (FigureInfo, error)
}

// ObjectVersion is an object's size and ETag, which names this version of it.
type ObjectVersion struct {
	Size int64
	ETag string
}

// PostUpload is a presigned form upload to storage.
type PostUpload struct {
	URL       *url.URL
	Fields    map[string]string
	ExpiresAt time.Time
}

// ErrObjectMissing and ErrObjectChanged are the bucket's answers for an object that is not
// there, or no longer the version an ETag named.
var (
	ErrObjectMissing = errors.New("project: object missing")
	ErrObjectChanged = errors.New("project: object changed")
)

// FileObjects is the bucket, as project files use it.
type FileObjects interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	// Delete removes the object. A key with no object is not an error.
	Delete(ctx context.Context, key string) error
	// PresignPost returns a form upload of exactly size bytes to key, until expiry.
	PresignPost(ctx context.Context, key string, size int64, expiry time.Duration) (PostUpload, error)
	// Stat returns the object's version; ErrObjectMissing when there is none.
	Stat(ctx context.Context, key string) (ObjectVersion, error)
	// Open reads the object's version with this ETag; ErrObjectMissing, ErrObjectChanged.
	Open(ctx context.Context, key, etag string) (io.ReadCloser, error)
	// Copy copies the object's version with this ETag to dst with the content type, inside
	// storage; ErrObjectMissing, ErrObjectChanged.
	Copy(ctx context.Context, src, dst, etag, contentType string) error
	// Get reads the object; ErrObjectMissing when there is none.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
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
