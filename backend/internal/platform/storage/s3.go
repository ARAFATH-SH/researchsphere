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
