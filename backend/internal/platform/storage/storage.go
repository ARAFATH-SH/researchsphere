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

// ErrNotFound is returned by Stat when no object has the key.
var ErrNotFound = errors.New("storage: object not found")

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
	// Stat returns the object's details, or ErrNotFound.
	Stat(ctx context.Context, key string) (ObjectInfo, error)
	// Ping reports whether the bucket can be reached (the health check).
	Ping(ctx context.Context) error
}
