package usecase

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
)

// Rules of the profile images (FR-4.6, docs/decisions.md).
const (
	// MaxImageSide is the most pixels an image may have on a side. It is checked from the
	// header before decoding, so a small file that would decode to gigabytes is refused.
	MaxImageSide = 8000
	// JPEGQuality is the quality every stored image is encoded with.
	JPEGQuality = 85
	// UploadsPerWindow is how many images (photo and cover together) a user may upload
	// in UploadWindow. Only successful uploads count.
	UploadsPerWindow = 5
	UploadWindow     = time.Hour
	// PresignExpiry is how long a URL from FileURL works. It is longer than the 5 minutes
	// the redirect may be cached, so a cached redirect never points to an expired URL.
	PresignExpiry = 10 * time.Minute
	// maxRenders is how many images are decoded at once. A decoded 8000x8000 image takes
	// up to 256 MB; the rest wait.
	maxRenders = 2
)

// ImageSpec is the size limit of an upload and the size it is stored at.
type ImageSpec struct {
	MaxBytes      int64
	Width, Height int
}

// Specs gives each profile image purpose its limit and stored size.
var Specs = map[domain.Purpose]ImageSpec{
	domain.PurposePhoto: {MaxBytes: 5 << 20, Width: 400, Height: 400},
	domain.PurposeCover: {MaxBytes: 8 << 20, Width: 1584, Height: 396},
}

// Images uploads, removes and serves the profile photo and cover image (FR-4.6, NFR-13).
type Images struct {
	files   domain.ProfileImages
	objects domain.ObjectStore
	codec   domain.ImageCodec
	limiter domain.UploadLimiter
	logger  *slog.Logger
	renders chan struct{} // one token per image being decoded
}

// NewImages returns an Images.
func NewImages(files domain.ProfileImages, objects domain.ObjectStore, codec domain.ImageCodec, limiter domain.UploadLimiter, logger *slog.Logger) *Images {
	return &Images{files: files, objects: objects, codec: codec, limiter: limiter, logger: logger,
		renders: make(chan struct{}, maxRenders)}
}

// Upload reads an image from r, re-encodes it as the purpose's JPEG, stores it and makes
// it the user's photo or cover in place of the previous one. It returns the new file's ID.
// A refused or failed upload does not count towards the rate limit. Errors:
// RateLimitedError, ErrFileTooLarge, ErrUnsupportedType, ErrImageTooLarge,
// ErrInvalidImage and ErrNoProfile; r's own read error is returned wrapped.
func (s *Images) Upload(ctx context.Context, userID string, purpose domain.Purpose, r io.Reader) (string, error) {
	spec, ok := Specs[purpose]
	if !ok {
		return "", fmt.Errorf("upload: no image spec for purpose %q", purpose)
	}
	if err := s.checkLimit(ctx, userID); err != nil {
		return "", err
	}

	data, err := io.ReadAll(io.LimitReader(r, spec.MaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("read upload: %w", err)
	}
	if int64(len(data)) > spec.MaxBytes {
		return "", domain.ErrFileTooLarge
	}
	format, ok := sniff(data)
	if !ok {
		return "", domain.ErrUnsupportedType
	}
	size, err := s.codec.Size(bytes.NewReader(data), format)
	if err != nil {
		return "", err
	}
	if size.Width <= 0 || size.Height <= 0 {
		return "", domain.ErrInvalidImage
	}
	if size.Width > MaxImageSide || size.Height > MaxImageSide {
		return "", domain.ErrImageTooLarge
	}

	out, err := s.render(ctx, data, format, spec)
	if err != nil {
		return "", err
	}
	id, err := newUUID()
	if err != nil {
		return "", err
	}
	key := fmt.Sprintf("users/%s/%s/%s.jpg", userID, purpose, id)
	if err := s.objects.Put(ctx, key, bytes.NewReader(out), int64(len(out)), "image/jpeg"); err != nil {
		return "", fmt.Errorf("store image: %w", err)
	}
	fileID, err := s.files.ReplaceProfileImage(ctx, domain.NewFile{
		OwnerID: userID, Purpose: purpose, ObjectKey: key, ContentType: "image/jpeg",
		Size: int64(len(out)), Width: spec.Width, Height: spec.Height,
	})
	if err != nil {
		// No row points at the object, so the cleanup queue does not know it either.
		// WithoutCancel: a client that went away must not stop the clean-up.
		if derr := s.objects.Delete(context.WithoutCancel(ctx), key); derr != nil {
			s.logger.ErrorContext(ctx, "orphaned object after a failed upload",
				slog.String("object_key", key), slog.Any("error", derr))
		}
		return "", err
	}
	if err := s.limiter.Record(ctx, userID, UploadWindow); err != nil {
		s.logger.WarnContext(ctx, "upload rate limit not recorded", slog.Any("error", err))
	}
	return fileID, nil
}

// checkLimit returns RateLimitedError when the user has no uploads left in the window.
// When the limiter cannot answer, the upload goes ahead (fail open, as the login lockout).
func (s *Images) checkLimit(ctx context.Context, userID string) error {
	used, left, err := s.limiter.Used(ctx, userID)
	if err != nil {
		s.logger.WarnContext(ctx, "upload rate limit unavailable, allowing the upload", slog.Any("error", err))
		return nil
	}
	if used >= UploadsPerWindow {
		return &domain.RateLimitedError{RetryAfter: left}
	}
	return nil
}

// render decodes and re-encodes the image, waiting for a free decoding slot first.
func (s *Images) render(ctx context.Context, data []byte, f domain.Format, spec ImageSpec) ([]byte, error) {
	select {
	case s.renders <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-s.renders }()
	return s.codec.Render(bytes.NewReader(data), f, spec.Width, spec.Height, JPEGQuality)
}

// Remove takes the user's photo or cover off the profile and deletes its file. Nothing
// set is not an error. It is not rate limited.
func (s *Images) Remove(ctx context.Context, userID string, purpose domain.Purpose) error {
	if _, ok := Specs[purpose]; !ok {
		return fmt.Errorf("remove: no image spec for purpose %q", purpose)
	}
	return s.files.ClearProfileImage(ctx, userID, purpose)
}

// FileURL returns a presigned URL to the stored file for callerID, valid for
// PresignExpiry (NFR-13). The owner may see their own file. Anyone else may see a profile
// photo or cover whose owner is verified and not suspended, as with a profile (FR-4.5).
// Other purposes are not served yet, and nothing is served before its scan passed. It
// returns ErrNotFound in every refused case, so the caller cannot tell them apart.
func (s *Images) FileURL(ctx context.Context, callerID, fileID string) (*url.URL, error) {
	f, err := s.files.FindFile(ctx, fileID)
	if err != nil {
		return nil, err
	}
	if _, ok := Specs[f.Purpose]; !ok || f.ScanStatus != "clean" {
		return nil, domain.ErrNotFound
	}
	if f.OwnerID != callerID && (!f.OwnerVerified || f.OwnerSuspended) {
		return nil, domain.ErrNotFound
	}
	u, err := s.objects.PresignGet(ctx, f.ObjectKey, PresignExpiry)
	if err != nil {
		return nil, fmt.Errorf("presign file: %w", err)
	}
	return u, nil
}

// sniff reads the format from the file's first bytes; the name and the content type the
// client sent are never used. An animated WebP is not accepted.
func sniff(b []byte) (domain.Format, bool) {
	switch {
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}):
		return domain.FormatJPEG, true
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return domain.FormatPNG, true
	case len(b) >= 16 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		// The extended format (VP8X) has a flags byte; 0x02 marks an animation.
		if string(b[12:16]) == "VP8X" && (len(b) < 21 || b[20]&0x02 != 0) {
			return "", false
		}
		return domain.FormatWebP, true
	}
	return "", false
}

// newUUID returns a random (version 4) UUID for an object key.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("object key: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
