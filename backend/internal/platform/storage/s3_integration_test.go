//go:build integration

// Adapter tests against the MinIO container. Run with `make test-integration` (needs
// `make up`); they read the S3_ settings.
package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func configFromEnv(t *testing.T) Config {
	t.Helper()
	cfg := Config{
		Endpoint:  os.Getenv("S3_ENDPOINT"),
		Region:    os.Getenv("S3_REGION"),
		Bucket:    os.Getenv("S3_BUCKET"),
		AccessKey: os.Getenv("S3_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_SECRET_KEY"),
		UseSSL:    os.Getenv("S3_USE_SSL") == "true",
	}
	if cfg.Endpoint == "" || cfg.Bucket == "" {
		t.Fatal("S3_ENDPOINT and S3_BUCKET are not set")
	}
	return cfg
}

func newTestS3(t *testing.T) *S3 {
	t.Helper()
	s, err := NewS3(configFromEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func httpGet(t *testing.T, url string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, body
}

// Put, Stat, PresignGet and Delete round trip, and the bucket refuses unsigned reads.
func TestS3RoundTrip(t *testing.T) {
	s := newTestS3(t)
	ctx := t.Context()
	key := "it/" + strings.ToLower(rand.Text()) + ".txt"
	content := []byte("hello object storage")
	t.Cleanup(func() { _ = s.Delete(t.Context(), key) })

	if err := s.Put(ctx, key, bytes.NewReader(content), int64(len(content)), "text/plain"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	info, err := s.Stat(ctx, key)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Key != key || info.Size != int64(len(content)) || info.ContentType != "text/plain" || info.ETag == "" {
		t.Errorf("Stat() = %+v, want key %s, size %d, text/plain and an ETag", info, key, len(content))
	}

	u, err := s.PresignGet(ctx, key, time.Minute)
	if err != nil {
		t.Fatalf("PresignGet() error = %v", err)
	}
	if status, body := httpGet(t, u.String()); status != http.StatusOK || !bytes.Equal(body, content) {
		t.Errorf("GET presigned URL = %d %q, want 200 %q", status, body, content)
	}

	// NFR-13: the bucket is private, so the same URL without the signature is refused.
	unsigned := *u
	unsigned.RawQuery = ""
	if status, _ := httpGet(t, unsigned.String()); status != http.StatusForbidden {
		t.Errorf("GET without signature = %d, want 403", status)
	}

	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := s.Stat(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Errorf("Stat() after Delete error = %v, want ErrNotFound", err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Errorf("Delete() of a missing object error = %v, want nil", err)
	}
}

func TestS3Ping(t *testing.T) {
	if err := newTestS3(t).Ping(t.Context()); err != nil {
		t.Errorf("Ping() error = %v", err)
	}
}

func TestS3PingFailsForAMissingBucket(t *testing.T) {
	cfg := configFromEnv(t)
	cfg.Bucket = "no-such-bucket-" + strings.ToLower(rand.Text())
	s, err := NewS3(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Ping(t.Context()); err == nil {
		t.Error("Ping() error = nil, want the bucket reported missing")
	}
}

// Storage that is not running is reported within the health check's timeout.
func TestS3PingFailsWhenStorageIsDown(t *testing.T) {
	cfg := configFromEnv(t)
	cfg.Endpoint = "127.0.0.1:1"
	s, err := NewS3(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	begin := time.Now()
	if err := s.Ping(ctx); err == nil {
		t.Error("Ping() error = nil")
	}
	if elapsed := time.Since(begin); elapsed > 3*time.Second {
		t.Errorf("Ping() took %v, want it to stop at the context's timeout", elapsed)
	}
}
