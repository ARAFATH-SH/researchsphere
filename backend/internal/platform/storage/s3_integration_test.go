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
	"mime/multipart"
	"net/http"
	"os"
	"slices"
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

// postForm sends a presigned POST the way a browser does: the fields, then the file part.
func postForm(t *testing.T, p PresignedPost, content []byte, extra map[string]string) int {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range p.Fields {
		if e, ok := extra[k]; ok {
			v = e
		}
		if err := w.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	fw, err := w.CreateFormFile("file", "figure.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, p.URL.String(), &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// FR-12.2: a presigned POST stores exactly the size it names, under its key only, typed
// application/octet-stream whatever the client claims.
func TestS3PresignPost(t *testing.T) {
	s := newTestS3(t)
	ctx := t.Context()
	key := "it/" + strings.ToLower(rand.Text()) + ".bin"
	t.Cleanup(func() { _ = s.Delete(t.Context(), key) })
	content := []byte("exactly twenty bytes")

	p, err := s.PresignPost(ctx, key, int64(len(content)), time.Minute)
	if err != nil {
		t.Fatalf("PresignPost() error = %v", err)
	}
	if p.ExpiresAt.Before(time.Now()) || p.Fields["key"] != key {
		t.Fatalf("PresignPost() = %+v", p)
	}
	if status := postForm(t, p, append(slices.Clone(content), '!'), nil); status != http.StatusBadRequest {
		t.Errorf("one byte too many: %d, want 400", status)
	}
	if status := postForm(t, p, content[:len(content)-1], nil); status != http.StatusBadRequest {
		t.Errorf("one byte too few: %d, want 400", status)
	}
	other := "it/" + strings.ToLower(rand.Text()) + ".bin"
	t.Cleanup(func() { _ = s.Delete(t.Context(), other) })
	if status := postForm(t, p, content, map[string]string{"key": other}); status != http.StatusForbidden {
		t.Errorf("another key: %d, want 403", status)
	}
	if status := postForm(t, p, content, map[string]string{"Content-Type": "image/png"}); status != http.StatusForbidden {
		t.Errorf("another type: %d, want 403", status)
	}
	if _, err := s.Stat(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("refused posts stored something: %v", err)
	}
	if status := postForm(t, p, content, nil); status != http.StatusNoContent {
		t.Fatalf("exact post: %d, want 204", status)
	}
	info, err := s.Stat(ctx, key)
	if err != nil || info.Size != int64(len(content)) || info.ContentType != "application/octet-stream" {
		t.Errorf("Stat() = %+v, %v", info, err)
	}

	if _, err := s.PresignPost(ctx, key, 0, time.Minute); err == nil {
		t.Error("size 0 accepted")
	}
	if _, err := s.PresignPost(ctx, key, 1, 2*time.Hour); err == nil {
		t.Error("expiry over an hour accepted")
	}
}

// GetIfMatch and CopyIfMatch act on the version they name and refuse one written since.
func TestS3IfMatch(t *testing.T) {
	s := newTestS3(t)
	ctx := t.Context()
	src := "it/" + strings.ToLower(rand.Text()) + ".bin"
	dst := "it/" + strings.ToLower(rand.Text()) + ".bin"
	t.Cleanup(func() { _ = s.Delete(t.Context(), src); _ = s.Delete(t.Context(), dst) })
	put := func(b string) string {
		if err := s.Put(ctx, src, strings.NewReader(b), int64(len(b)), "application/octet-stream"); err != nil {
			t.Fatal(err)
		}
		info, err := s.Stat(ctx, src)
		if err != nil {
			t.Fatal(err)
		}
		return info.ETag
	}
	first := put("first")

	r, err := s.GetIfMatch(ctx, src, first)
	if err != nil {
		t.Fatalf("GetIfMatch() error = %v", err)
	}
	b, _ := io.ReadAll(r)
	_ = r.Close()
	if string(b) != "first" {
		t.Errorf("GetIfMatch() read %q", b)
	}
	if err := s.CopyIfMatch(ctx, src, dst, first, "image/png"); err != nil {
		t.Fatalf("CopyIfMatch() error = %v", err)
	}
	if info, err := s.Stat(ctx, dst); err != nil || info.ContentType != "image/png" || info.Size != 5 {
		t.Errorf("copy: %+v, %v", info, err)
	}

	put("second")
	if _, err := s.GetIfMatch(ctx, src, first); !errors.Is(err, ErrChanged) {
		t.Errorf("GetIfMatch() of an old version: %v, want ErrChanged", err)
	}
	if err := s.CopyIfMatch(ctx, src, dst, first, "image/png"); !errors.Is(err, ErrChanged) {
		t.Errorf("CopyIfMatch() of an old version: %v, want ErrChanged", err)
	}
	if _, err := s.GetIfMatch(ctx, "it/missing-"+strings.ToLower(rand.Text()), first); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetIfMatch() of a missing object: %v, want ErrNotFound", err)
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
