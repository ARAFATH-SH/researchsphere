package domain

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"time"
)

// Errors of the profile image operations (FR-4.6).
var (
	// ErrFileTooLarge: the upload is over the purpose's size limit.
	ErrFileTooLarge = errors.New("media: file too large")
	// ErrUnsupportedType: the file's first bytes are not a JPEG, PNG or still WebP image.
	ErrUnsupportedType = errors.New("media: unsupported image type")
	// ErrImageTooLarge: the image's header states more pixels on a side than the API decodes.
	ErrImageTooLarge = errors.New("media: image dimensions too large")
	// ErrInvalidImage: the first bytes name a supported type but the image cannot be read.
	ErrInvalidImage = errors.New("media: invalid image")
	// ErrNoProfile: the user has no profile row to put the image on.
	ErrNoProfile = errors.New("media: no profile")
	// ErrNotFound: no stored file has the ID, or the caller may not see it.
	ErrNotFound = errors.New("media: not found")
)

// RateLimitedError says the user has used up the uploads of the current window.
type RateLimitedError struct {
	RetryAfter time.Duration // until the window ends
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("media: upload rate limited, retry after %s", e.RetryAfter)
}

// Purpose says what a stored file is for. The values match stored_files.purpose.
type Purpose string

// Purposes of stored files. Only the profile images are served for now.
const (
	PurposePhoto Purpose = "photo"
	PurposeCover Purpose = "cover"
)

// Format is an image format the API accepts, read from the file's first bytes.
type Format string

// Accepted image formats.
const (
	FormatJPEG Format = "jpeg"
	FormatPNG  Format = "png"
	FormatWebP Format = "webp"
)

// ImageSize is an image's width and height in pixels, as its header states them.
type ImageSize struct {
	Width, Height int
}

// ImageCodec reads image headers and re-encodes images.
type ImageCodec interface {
	// Size reads only the header of an image in format f, without decoding the pixels.
	// It returns ErrInvalidImage when the header cannot be read.
	Size(r io.Reader, f Format) (ImageSize, error)
	// Render decodes an image in format f, turns it upright when its metadata says it is
	// rotated, crops the largest centred width:height rectangle, scales it to width x height
	// and encodes it as a JPEG of the given quality. Nothing of the original file but its
	// pixels is kept. It returns ErrInvalidImage when the image cannot be decoded.
	Render(r io.Reader, f Format, width, height, quality int) ([]byte, error)
}

// ImageReencoder encodes an image again at its own size (message attachments, FR-16.7).
type ImageReencoder interface {
	// Reencode decodes an image in format f, turns it upright when its metadata says it is
	// rotated, and encodes it again at its own size: a JPEG as a JPEG of the given quality, a
	// PNG as a PNG, and a WebP as a PNG when it has transparent pixels, else as a JPEG. It
	// returns the bytes and their format. Nothing of the original file but its pixels is kept.
	// It returns ErrInvalidImage when the image cannot be decoded.
	Reencode(r io.Reader, f Format, quality int) ([]byte, Format, error)
}

// NewFile is a stored_files row to insert for an image the API has re-encoded.
type NewFile struct {
	OwnerID     string
	Purpose     Purpose
	ObjectKey   string
	ContentType string
	Size        int64
	Width       int
	Height      int
}

// StoredFile is a stored_files row with what is needed to decide who may see it.
type StoredFile struct {
	ID             string
	OwnerID        string
	Purpose        Purpose
	ObjectKey      string
	ScanStatus     string // pending, clean or rejected
	OwnerVerified  bool
	OwnerSuspended bool
}

// ProfileImages keeps the profile photo and cover image: the stored_files rows and the
// profile columns that point at them.
type ProfileImages interface {
	// ReplaceProfileImage, in one transaction, inserts f with scan status clean, points
	// the profile column of f.Purpose at it and deletes the row it pointed at before
	// (whose object the database queues for deletion). It returns the new row's ID, or
	// ErrNoProfile when the owner has no profile row.
	ReplaceProfileImage(ctx context.Context, f NewFile) (string, error)
	// ClearProfileImage empties the profile column of purpose and deletes the row it
	// pointed at. Nothing set, or no profile row, is not an error.
	ClearProfileImage(ctx context.Context, userID string, purpose Purpose) error
	// FindFile returns ErrNotFound when no stored file has the ID, or it is not a UUID.
	FindFile(ctx context.Context, fileID string) (StoredFile, error)
}

// ObjectStore is the bucket, as the profile image operations use it.
type ObjectStore interface {
	ObjectDeleter
	// Put stores size bytes from r under key with the given content type.
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	// PresignGet returns a URL that reads the object without credentials until expiry.
	PresignGet(ctx context.Context, key string, expiry time.Duration) (*url.URL, error)
}

// UploadLimiter counts a user's successful uploads in a fixed window.
type UploadLimiter interface {
	// Used returns the uploads counted in the user's current window and the time left in
	// it; 0 and 0 when no window is open.
	Used(ctx context.Context, userID string) (int, time.Duration, error)
	// Record counts one upload. The first upload of a window opens it for window.
	Record(ctx context.Context, userID string, window time.Duration) error
}
