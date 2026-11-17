package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Config holds the connection settings. SecretKey is a password: never log it.
type Config struct {
	Endpoint  string // host:port, no scheme
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	UseSSL    bool
}

// S3 is a Storage on any S3-compatible service (MinIO locally).
type S3 struct {
	client *minio.Client
	bucket string
}

var _ Storage = (*S3)(nil)

// NewS3 returns an S3 for cfg. It does not connect until first used, so the API starts
// while storage is down; the health check reports it. With the region set, presigning
// needs no request to the server.
func NewS3(cfg Config) (*S3, error) {
	var missing []string
	for name, v := range map[string]string{"endpoint": cfg.Endpoint, "region": cfg.Region, "bucket": cfg.Bucket,
		"access key": cfg.AccessKey, "secret key": cfg.SecretKey} {
		if v == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("storage: missing %v", missing)
	}
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, errors.New("storage: the S3 endpoint is not accepted by the client")
	}
	return &S3{client: client, bucket: cfg.Bucket}, nil
}

// Put stores the object. size must be known: the API checks it before uploading.
func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if key == "" || size < 0 || contentType == "" {
		return errors.New("storage: put needs a key, a size of 0 or more and a content type")
	}
	if _, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType}); err != nil {
		return fmt.Errorf("storage: put %s: %w", key, err)
	}
	return nil
}

// Delete removes the object; S3 answers success for a key with no object.
func (s *S3) Delete(ctx context.Context, key string) error {
	if key == "" {
		return errors.New("storage: delete needs a key")
	}
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("storage: delete %s: %w", key, err)
	}
	return nil
}

// PresignGet signs a GET URL for the object. It does not check that the object exists.
func (s *S3) PresignGet(ctx context.Context, key string, expiry time.Duration) (*url.URL, error) {
	if key == "" {
		return nil, errors.New("storage: presign needs a key")
	}
	if expiry <= 0 || expiry > MaxPresignExpiry {
		return nil, fmt.Errorf("storage: presign expiry must be more than 0 and at most %v, got %v", MaxPresignExpiry, expiry)
	}
	u, err := s.client.PresignedGetObject(ctx, s.bucket, key, expiry, nil)
	if err != nil {
		return nil, fmt.Errorf("storage: presign %s: %w", key, err)
	}
	return u, nil
}

// PresignDownload signs a GET URL whose answer is a download: S3's response-content-disposition
// and response-content-type parameters override what was stored with the object.
func (s *S3) PresignDownload(ctx context.Context, key string, expiry time.Duration, filename, contentType string) (*url.URL, error) {
	if key == "" || contentType == "" {
		return nil, errors.New("storage: presign download needs a key and a content type")
	}
	if expiry <= 0 || expiry > MaxPresignExpiry {
		return nil, fmt.Errorf("storage: presign expiry must be more than 0 and at most %v, got %v", MaxPresignExpiry, expiry)
	}
	params := url.Values{}
	params.Set("response-content-disposition", ContentDisposition(filename))
	params.Set("response-content-type", contentType)
	u, err := s.client.PresignedGetObject(ctx, s.bucket, key, expiry, params)
	if err != nil {
		return nil, fmt.Errorf("storage: presign %s: %w", key, err)
	}
	return u, nil
}

// PresignPost signs a POST policy for exactly one key and exactly size bytes. The stored
// type is fixed to application/octet-stream, so whatever the browser claims is never kept;
// the API reads the content to find the type.
func (s *S3) PresignPost(ctx context.Context, key string, size int64, expiry time.Duration) (PresignedPost, error) {
	if key == "" || size <= 0 {
		return PresignedPost{}, errors.New("storage: presign post needs a key and a size over 0")
	}
	if expiry <= 0 || expiry > MaxPresignExpiry {
		return PresignedPost{}, fmt.Errorf("storage: presign expiry must be more than 0 and at most %v, got %v", MaxPresignExpiry, expiry)
	}
	expires := time.Now().Add(expiry).UTC()
	policy := minio.NewPostPolicy()
	for _, err := range []error{
		policy.SetBucket(s.bucket),
		policy.SetKey(key),
		policy.SetExpires(expires),
		policy.SetContentLengthRange(size, size),
		policy.SetContentType(octetStream),
	} {
		if err != nil {
			return PresignedPost{}, fmt.Errorf("storage: presign post %s: %w", key, err)
		}
	}
	u, fields, err := s.client.PresignedPostPolicy(ctx, policy)
	if err != nil {
		return PresignedPost{}, fmt.Errorf("storage: presign post %s: %w", key, err)
	}
	return PresignedPost{URL: u, Fields: fields, ExpiresAt: expires}, nil
}

const octetStream = "application/octet-stream"

// Get opens the object. minio-go sends the request on the first read, so the object is
// stat-ed first to tell a missing object apart.
func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if key == "" {
		return nil, errors.New("storage: get needs a key")
	}
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("storage: get %s: %w", key, err)
	}
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		if minio.ToErrorResponse(err).Code == minio.NoSuchKey {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: get %s: %w", key, err)
	}
	return obj, nil
}

// GetIfMatch is Get with an If-Match on the ETag.
func (s *S3) GetIfMatch(ctx context.Context, key, etag string) (io.ReadCloser, error) {
	if key == "" || etag == "" {
		return nil, errors.New("storage: get needs a key and an ETag")
	}
	opts := minio.GetObjectOptions{}
	if err := opts.SetMatchETag(etag); err != nil {
		return nil, fmt.Errorf("storage: get %s: %w", key, err)
	}
	obj, err := s.client.GetObject(ctx, s.bucket, key, opts)
	if err != nil {
		return nil, fmt.Errorf("storage: get %s: %w", key, err)
	}
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		return nil, objectError("get", key, err)
	}
	return obj, nil
}

// CopyIfMatch copies inside storage with an If-Match on the source's ETag, replacing the
// metadata so the copy has the given content type.
func (s *S3) CopyIfMatch(ctx context.Context, src, dst, etag, contentType string) error {
	if src == "" || dst == "" || etag == "" || contentType == "" {
		return errors.New("storage: copy needs a source, a destination, an ETag and a content type")
	}
	_, err := s.client.CopyObject(ctx,
		minio.CopyDestOptions{Bucket: s.bucket, Object: dst, ReplaceMetadata: true, ContentType: contentType},
		minio.CopySrcOptions{Bucket: s.bucket, Object: src, MatchETag: etag})
	if err != nil {
		return objectError("copy", src, err)
	}
	return nil
}

// objectError maps a missing object to ErrNotFound and a failed If-Match to ErrChanged.
func objectError(op, key string, err error) error {
	switch minio.ToErrorResponse(err).Code {
	case minio.NoSuchKey:
		return ErrNotFound
	case minio.PreconditionFailed:
		return ErrChanged
	}
	return fmt.Errorf("storage: %s %s: %w", op, key, err)
}

// Stat returns ErrNotFound when no object has the key.
func (s *S3) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).Code == minio.NoSuchKey {
			return ObjectInfo{}, ErrNotFound
		}
		return ObjectInfo{}, fmt.Errorf("storage: stat %s: %w", key, err)
	}
	return ObjectInfo{
		Key:          info.Key,
		Size:         info.Size,
		ContentType:  info.ContentType,
		ETag:         info.ETag,
		LastModified: info.LastModified,
	}, nil
}

// Ping checks that the bucket exists and the credentials are accepted.
func (s *S3) Ping(ctx context.Context) error {
	ok, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	if !ok {
		return fmt.Errorf("storage: bucket %s does not exist", s.bucket)
	}
	return nil
}
