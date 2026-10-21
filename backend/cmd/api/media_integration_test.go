//go:build integration

// End-to-end test of the profile images (FR-4.6, NFR-13) through the real router with
// the real PostgreSQL, Redis and MinIO adapters. Run with `make test-integration` (needs
// `make up` and `make migrate`); it reads DATABASE_URL, REDIS_URL and the S3_ settings.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"image/jpeg"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	authhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/http"
	mediahttp "github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/http"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/imaging"
	mediapg "github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/postgres"
	mediaredis "github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/redis"
	mediausecase "github.com/ARAFATH-SH/researchsphere/backend/internal/media/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/storage"
	profilehttp "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/http"
	profilepg "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/postgres"
	profileusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/usecase"
)

type mediaEnv struct {
	router http.Handler
	pool   *pgxpool.Pool
	files  *storage.S3
}

func newMediaEnv(t *testing.T) mediaEnv {
	t.Helper()
	for _, k := range []string{"DATABASE_URL", "REDIS_URL", "S3_ENDPOINT", "S3_BUCKET"} {
		if os.Getenv(k) == "" {
			t.Fatalf("%s is not set", k)
		}
	}
	pool, err := pgxpool.New(t.Context(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal("DATABASE_URL is not accepted by the PostgreSQL driver")
	}
	t.Cleanup(pool.Close)
	opts, err := goredis.ParseURL(os.Getenv("REDIS_URL"))
	if err != nil {
		t.Fatal("REDIS_URL is not accepted by the Redis client")
	}
	rdb := goredis.NewClient(opts)
	limiter := mediaredis.New(rdb)
	t.Cleanup(func() { _ = limiter.Close(); _ = rdb.Close() })
	files, err := storage.NewS3(storage.Config{
		Endpoint: os.Getenv("S3_ENDPOINT"), Region: os.Getenv("S3_REGION"), Bucket: os.Getenv("S3_BUCKET"),
		AccessKey: os.Getenv("S3_ACCESS_KEY"), SecretKey: os.Getenv("S3_SECRET_KEY"), UseSSL: os.Getenv("S3_USE_SSL") == "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.DiscardHandler)
	images := mediausecase.NewImages(mediapg.NewStore(pool), files, imaging.Codec{}, limiter, logger)
	up := pingerFunc(func(context.Context) error { return nil })
	router := newRouter(logger, up, up, up, authhttp.NewHandler(okAuth{}, authhttp.CookieConfig{}),
		profilehttp.NewHandler(profileusecase.New(profilepg.NewStore(pool))),
		mediahttp.NewHandler(images),
		httpx.RequireAuth(accesstoken.New([]byte(testSecret), accessTTL), time.Now))
	return mediaEnv{router: router, pool: pool, files: files}
}

// newProfileUser stores a verified user with a profile and removes the user, its queued
// objects, the objects themselves and its upload counter when the test ends.
func (e mediaEnv) newProfileUser(t *testing.T) string {
	t.Helper()
	ctx := t.Context()
	var id string
	email := "it-" + strings.ToLower(rand.Text()) + "@example.org"
	if err := e.pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, email_verified_at)
		VALUES ($1, 'hash', now()) RETURNING id::text`, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, "INSERT INTO profiles (user_id, full_name) VALUES ($1, 'Ada')", id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		prefix := "users/" + id + "/"
		if _, err := e.pool.Exec(bg, "DELETE FROM users WHERE id = $1", id); err != nil {
			t.Errorf("cleanup: %v", err)
		}
		rows, err := e.pool.Query(bg, "DELETE FROM storage_deletions WHERE object_key LIKE $1 RETURNING object_key", prefix+"%")
		if err != nil {
			t.Errorf("cleanup: %v", err)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err == nil {
				_ = e.files.Delete(bg, key)
			}
		}
		opts, _ := goredis.ParseURL(os.Getenv("REDIS_URL"))
		c := goredis.NewClient(opts)
		c.Del(bg, "media:upload:"+id)
		_ = c.Close()
	})
	return id
}

func (e mediaEnv) send(t *testing.T, userID, method, path string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	if body == nil {
		body = http.NoBody
	}
	req := httptest.NewRequestWithContext(t.Context(), method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	token, _, err := accesstoken.New([]byte(testSecret), accessTTL).Sign(userID, "researcher", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", httpx.AccessCookie+"="+token)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func (e mediaEnv) upload(t *testing.T, userID, path, fixture string) *httptest.ResponseRecorder {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "media", "adapter", "imaging", "testdata", fixture)) //nolint:gosec // fixed test file names
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	fw, err := w.CreateFormFile("file", fixture)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return e.send(t, userID, http.MethodPut, path, &b, w.FormDataContentType())
}

func (e mediaEnv) profileURLs(t *testing.T, userID string) (photo, cover *string) {
	t.Helper()
	rec := e.send(t, userID, http.MethodGet, "/api/v1/profiles/me", nil, "")
	var p struct{ PhotoUrl, CoverUrl *string }
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &p) != nil {
		t.Fatalf("GET /profiles/me: %d %s", rec.Code, rec.Body)
	}
	return p.PhotoUrl, p.CoverUrl
}

// fetch follows the media redirect for userID and downloads the object without
// credentials, as the browser does.
func (e mediaEnv) fetch(t *testing.T, userID, mediaURL string) []byte {
	t.Helper()
	rec := e.send(t, userID, http.MethodGet, mediaURL, nil, "")
	if rec.Code != http.StatusFound {
		t.Fatalf("GET %s: status = %d, want 302 (body %s)", mediaURL, rec.Code, rec.Body)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, max-age=300" {
		t.Errorf("Cache-Control = %q", cc)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, rec.Header().Get("Location"), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("presigned GET: %d, %v", resp.StatusCode, err)
	}
	return body
}

func (e mediaEnv) objectKey(t *testing.T, fileID string) string {
	t.Helper()
	var key string
	if err := e.pool.QueryRow(t.Context(), "SELECT object_key FROM stored_files WHERE id = $1", fileID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	return key
}

func (e mediaEnv) queued(t *testing.T, key string) bool {
	t.Helper()
	var ok bool
	if err := e.pool.QueryRow(t.Context(), "SELECT EXISTS (SELECT 1 FROM storage_deletions WHERE object_key = $1)", key).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestProfilePhotoUploadRedirectDelete(t *testing.T) {
	e := newMediaEnv(t)
	ada := e.newProfileUser(t)
	bob := e.newProfileUser(t)

	// Upload a JPEG whose EXIF holds a GPS position and a marker.
	rec := e.upload(t, ada, "/api/v1/profiles/me/photo", "exif.jpg")
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	var stored struct{ Url string }
	if err := json.Unmarshal(rec.Body.Bytes(), &stored); err != nil || !strings.HasPrefix(stored.Url, "/api/v1/media/") {
		t.Fatalf("upload body = %s", rec.Body)
	}
	if photo, cover := e.profileURLs(t, ada); photo == nil || *photo != stored.Url || cover != nil {
		t.Errorf("profile photoUrl = %v, coverUrl = %v; want %s and null", photo, cover, stored.Url)
	}
	firstID := strings.TrimPrefix(stored.Url, "/api/v1/media/")
	firstKey := e.objectKey(t, firstID)
	if !strings.HasPrefix(firstKey, "users/"+ada+"/photo/") || !strings.HasSuffix(firstKey, ".jpg") {
		t.Errorf("object key = %s", firstKey)
	}

	// The owner and another logged-in user get the re-encoded image, without EXIF.
	for _, viewer := range []string{ada, bob} {
		body := e.fetch(t, viewer, stored.Url)
		img, err := jpeg.Decode(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("stored object is not a JPEG: %v", err)
		}
		if b := img.Bounds(); b.Dx() != 400 || b.Dy() != 400 {
			t.Errorf("stored photo is %v, want 400x400", b)
		}
		if bytes.Contains(body, []byte("SECRET-EXIF-MARKER")) || bytes.Contains(body, []byte("Exif\x00\x00")) {
			t.Error("stored photo still has the EXIF segment")
		}
	}

	// A PNG replaces it: the old row goes and its object is queued for deletion.
	rec = e.upload(t, ada, "/api/v1/profiles/me/photo", "alpha.png")
	if rec.Code != http.StatusOK {
		t.Fatalf("second upload: %d %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &stored); err != nil {
		t.Fatal(err)
	}
	if !e.queued(t, firstKey) {
		t.Error("the replaced photo's object is not queued for deletion")
	}
	if rec := e.send(t, ada, http.MethodGet, "/api/v1/media/"+firstID, nil, ""); rec.Code != http.StatusNotFound {
		t.Errorf("replaced photo: status = %d, want 404", rec.Code)
	}
	if body := e.fetch(t, ada, stored.Url); !bytes.HasPrefix(body, []byte{0xFF, 0xD8, 0xFF}) {
		t.Error("the PNG was not stored as a JPEG")
	}

	// Refused files: a text file named .jpg, and an image over 8000 pixels on a side.
	if rec := e.upload(t, ada, "/api/v1/profiles/me/cover", "notimage.jpg"); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text file: %d %s, want 415", rec.Code, rec.Body)
	}
	if rec := e.upload(t, ada, "/api/v1/profiles/me/cover", "huge.png"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "image_too_large") {
		t.Errorf("huge image: %d %s, want 400 image_too_large", rec.Code, rec.Body)
	}

	// Delete: the profile has no photo, the file is gone and its object queued.
	secondID := strings.TrimPrefix(stored.Url, "/api/v1/media/")
	secondKey := e.objectKey(t, secondID)
	if rec := e.send(t, ada, http.MethodDelete, "/api/v1/profiles/me/photo", nil, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if photo, _ := e.profileURLs(t, ada); photo != nil {
		t.Errorf("photoUrl after delete = %s, want null", *photo)
	}
	if rec := e.send(t, bob, http.MethodGet, "/api/v1/media/"+secondID, nil, ""); rec.Code != http.StatusNotFound {
		t.Errorf("deleted photo: status = %d, want 404", rec.Code)
	}
	if !e.queued(t, secondKey) {
		t.Error("the deleted photo's object is not queued for deletion")
	}
	if _, err := e.files.Stat(t.Context(), secondKey); errors.Is(err, storage.ErrNotFound) {
		t.Error("the object was deleted before the cleanup worker ran; the queue owns that")
	}
}

// The 6th upload in an hour is refused with Retry-After; refused files did not count.
func TestUploadRateLimitWithRedis(t *testing.T) {
	e := newMediaEnv(t)
	ada := e.newProfileUser(t)
	if rec := e.upload(t, ada, "/api/v1/profiles/me/photo", "notimage.jpg"); rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("refused upload: %d", rec.Code)
	}
	for i := range mediausecase.UploadsPerWindow {
		path := "/api/v1/profiles/me/photo"
		if i%2 == 1 {
			path = "/api/v1/profiles/me/cover"
		}
		if rec := e.upload(t, ada, path, "alpha.png"); rec.Code != http.StatusOK {
			t.Fatalf("upload %d: %d %s", i+1, rec.Code, rec.Body)
		}
	}
	rec := e.upload(t, ada, "/api/v1/profiles/me/cover", "alpha.png")
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "rate_limited") {
		t.Fatalf("6th upload: %d %s, want 429 rate_limited", rec.Code, rec.Body)
	}
	if ra := rec.Header().Get("Retry-After"); ra < "3500" || len(ra) != 4 {
		t.Errorf("Retry-After = %q, want just under 3600", ra)
	}
	if rec := e.send(t, ada, http.MethodDelete, "/api/v1/profiles/me/cover", nil, ""); rec.Code != http.StatusNoContent {
		t.Errorf("delete while rate limited: %d, want 204", rec.Code)
	}
}
