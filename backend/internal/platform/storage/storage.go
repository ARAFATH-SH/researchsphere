// Package storage keeps files in S3-compatible object storage (NFR-13). The rest of the
// code depends on the Storage interface; S3 implements it with minio-go.
package storage

import (
	"context"
	"errors"
	"io"
	"net/url"
	"time"
)

// MaxPresignExpiry is the longest a presigned URL may stay valid. Callers use a few
// minutes (docs/decisions.md): the bucket is private, and a URL is issued only after a
// permission check.
const MaxPresignExpiry = time.Hour

// ErrNotFound is returned by Stat and Get when no object has the key.
var ErrNotFound = errors.New("storage: object not found")

// ErrChanged is returned by GetIfMatch and CopyIfMatch when the object is no longer the
// version its ETag named: something wrote it again in between.
var ErrChanged = errors.New("storage: object changed")

// PresignedPost is a form upload straight to the bucket (docs/decisions.md, 2026-11-18): the
// client POSTs Fields, then the file as the last part named "file", to URL before ExpiresAt.
type PresignedPost struct {
	URL       *url.URL
	Fields    map[string]string
	ExpiresAt time.Time
}

// ObjectInfo describes a stored object.
type ObjectInfo struct {
	Key          string
	Size         int64
	ContentType  string
	ETag         string
	LastModified time.Time
}

// Storage is the bucket the API keeps files in.
type Storage interface {
	// Put stores size bytes from r under key with the given content type.
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	// Delete removes the object. A key with no object is not an error.
	Delete(ctx context.Context, key string) error
	// PresignGet returns a URL that reads the object without credentials until expiry
	// passes. expiry must be more than 0 and at most MaxPresignExpiry.
	PresignGet(ctx context.Context, key string, expiry time.Duration) (*url.URL, error)
	// PresignDownload is PresignGet for a file to save, never to show (NFR-13, FR-16.7): the
	// object's answer carries Content-Disposition: attachment with filename and the given
	// content type, whatever was stored with the object.
	PresignDownload(ctx context.Context, key string, expiry time.Duration, filename, contentType string) (*url.URL, error)
	// PresignPost returns a form upload that stores exactly size bytes under key, typed
	// application/octet-stream, until expiry passes; storage refuses any other size or key.
	// expiry must be more than 0 and at most MaxPresignExpiry.
	PresignPost(ctx context.Context, key string, size int64, expiry time.Duration) (PresignedPost, error)
	// Get opens the object for reading, or returns ErrNotFound. Close the reader.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// GetIfMatch is Get of the object's version with this ETag: ErrChanged when it was
	// written again since.
	GetIfMatch(ctx context.Context, key, etag string) (io.ReadCloser, error)
	// CopyIfMatch copies the object at src, if it still has this ETag (else ErrChanged), to
	// dst with the given content type, inside storage.
	CopyIfMatch(ctx context.Context, src, dst, etag, contentType string) error
	// Stat returns the object's details, or ErrNotFound.
	Stat(ctx context.Context, key string) (ObjectInfo, error)
	// Ping reports whether the bucket can be reached (the health check).
	Ping(ctx context.Context) error
}
