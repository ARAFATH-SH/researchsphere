package domain

import (
	"context"
	"errors"
	"io"
	"time"
)

// Message attachments (FR-16.7, NFR-13; slice 14B, chunk 14B.3): checking a file's type by its
// content, re-encoding images, and the malware scan of every stored file still pending.

// PreparedFile is an uploaded file the API accepts, ready to store.
type PreparedFile struct {
	Data        []byte // what is stored: an image encoded again, a document as uploaded
	ContentType string // from the allow-list, found from the content
	// Extensions are the file name endings of the type, the usual one first.
	Extensions []string
	// Width and Height are set for an image only.
	Width, Height int
}

// ScanStatus is a stored file's scan outcome. The values match stored_files.scan_status.
type ScanStatus string

// Scan outcomes.
const (
	ScanPending  ScanStatus = "pending"
	ScanClean    ScanStatus = "clean"
	ScanInfected ScanStatus = "infected"
	ScanFailed   ScanStatus = "failed"
)

// Scan error codes kept in stored_files.scan_error. Never the scanner's own text.
const (
	ScanErrorScanner = "scanner_error"  // the scanner answered with an error about this file
	ScanErrorMissing = "object_missing" // the object was not in the bucket
)

// ScanVerdict is what the scanner found in one file.
type ScanVerdict struct {
	Infected  bool
	Signature string // the malware's name when infected; logged, never shown to users
}

// ErrScannerUnavailable says the scanner could not be reached or broke off: the file is not to
// blame, and the try does not count.
var ErrScannerUnavailable = errors.New("media: scanner unavailable")

// ErrObjectMissing says no object has the key.
var ErrObjectMissing = errors.New("media: object missing")

// ScanFileError says the scanner read the file and answered with an error about it (for
// example a size limit). It counts as one of the file's tries.
type ScanFileError struct {
	Reason string // the scanner's text, for the log only
}

func (e *ScanFileError) Error() string { return "media: the scanner refused the file: " + e.Reason }

// VirusScanner is the malware scanner (ClamAV).
type VirusScanner interface {
	// Ping reports whether the scanner answers; an error wraps ErrScannerUnavailable.
	Ping(ctx context.Context) error
	// Scan streams r to the scanner. Errors: ErrScannerUnavailable (wrapped) when it cannot
	// be reached or breaks off, *ScanFileError when it answers with an error about the file,
	// and r's own read error, wrapped.
	Scan(ctx context.Context, r io.Reader) (ScanVerdict, error)
}

// ObjectReader reads objects from the bucket.
type ObjectReader interface {
	// Get opens the object; ErrObjectMissing when no object has the key. Close the reader.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

// ScanJob is one stored file a worker has claimed for scanning.
type ScanJob struct {
	FileID string
	// OwnerID is the user who owns the file; empty for a file a project owns (a figure).
	OwnerID   string
	ObjectKey string
	Attempts  int    // tries that ended with a ScanFileError so far
	Token     string // the lease: a result is written only with it
}

// ScanQueue is the pending stored files. Every write names the job's lease token and reports
// false, changing nothing, when the lease was lost (it expired and another worker claimed the
// file). Methods run in the transaction the context carries, if any.
type ScanQueue interface {
	// ClaimScans leases up to limit pending files that are due and that no other worker
	// holds, for lease, and returns them.
	ClaimScans(ctx context.Context, limit int, lease time.Duration) ([]ScanJob, error)
	// MarkClean ends the scan as clean.
	MarkClean(ctx context.Context, job ScanJob) (bool, error)
	// MarkInfected ends the scan as infected and queues the object for deletion; the row
	// stays as a record.
	MarkInfected(ctx context.Context, job ScanJob) (bool, error)
	// MarkFailed ends the scan as failed with an error code.
	MarkFailed(ctx context.Context, job ScanJob, code string) (bool, error)
	// RetryLater gives the lease back and makes the file due again after delay. With
	// counted, the try counts and code is kept as the last error.
	RetryLater(ctx context.Context, job ScanJob, delay time.Duration, counted bool, code string) (bool, error)
}

// ScanListener hears that a scan finished. cmd/api implements it with the messaging and
// notification modules.
type ScanListener interface {
	// Infected runs in the transaction that marks the file infected: it tells the owner. An
	// error rolls the transaction back and the file is scanned again later.
	Infected(ctx context.Context, fileID, ownerID string) error
	// Scanned hears every final outcome. It cannot fail the scan; the implementation acts
	// after the commit.
	Scanned(ctx context.Context, fileID string, status ScanStatus)
}

// Transactor runs fn in one database transaction; it commits when fn returns nil and rolls
// back otherwise.
type Transactor interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}
