//go:build integration

// End-to-end test of the profile images (FR-4.6, NFR-13) through the real router with
// the real PostgreSQL, Redis and MinIO adapters. Run with `make test-integration` (needs
// `make up`); it reads TEST_DATABASE_URL, TEST_REDIS_URL and the S3_ settings, and each
// test starts with the test database and Redis DB empty.
package main

import (
	"bytes"
	"context"
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

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/argon2"
	authhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/http"
	authpg "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/postgres"
	authredis "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/redis"
	authdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
	authusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/usecase"
	mediahttp "github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/http"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/imaging"
	mediapg "github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/postgres"
	mediaredis "github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/redis"
	mediausecase "github.com/ARAFATH-SH/researchsphere/backend/internal/media/usecase"
	messaginghttp "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/adapter/http"
	messagingpg "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/adapter/postgres"
	messagingredis "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/adapter/redis"
	messagingusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/usecase"
	moderationhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/moderation/adapter/http"
	moderationpg "github.com/ARAFATH-SH/researchsphere/backend/internal/moderation/adapter/postgres"
	moderationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/moderation/usecase"
	notificationhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/notification/adapter/http"
	notificationpg "github.com/ARAFATH-SH/researchsphere/backend/internal/notification/adapter/postgres"
	notificationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/notification/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/storage"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
	profilehttp "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/http"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/openalex"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/orcid"
	profilepg "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/postgres"
	profileredis "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/redis"
	profileusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/usecase"
	realtimehttp "github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/adapter/http"
	realtimeusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/usecase"
	socialhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/social/adapter/http"
	socialpg "github.com/ARAFATH-SH/researchsphere/backend/internal/social/adapter/postgres"
	socialredis "github.com/ARAFATH-SH/researchsphere/backend/internal/social/adapter/redis"
	socialdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/social/domain"
	socialusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/social/usecase"
)

type mediaEnv struct {
	router     http.Handler
	pool       *pgxpool.Pool
	files      *storage.S3
	rdb        *goredis.Client
	auth       *authusecase.Service
	moderation *moderationusecase.Service
	standings  *moderationusecase.Standings
	hub        *realtimeusecase.Hub
	events     *realtimeEvents
	opt        envOptions
	// messaging and notifications serve the attachment tests (FR-16.7): the scan worker is
	// built per test with a fake scanner.
	messaging *messagingusecase.Service
	notifier  *notificationusecase.Service
}

func newMediaEnv(t *testing.T) mediaEnv {
	t.Helper()
	return newMediaEnvWithOpenAlex(t, "")
}

// newMediaEnvWithOpenAlex is newMediaEnv with DOI lookups (FR-5.2) sent to the OpenAlex
// at openAlexURL, the OPENALEX_BASE_URL setting; tests pass a fake server, never the real
// one. Empty leaves lookups unconfigured.
func newMediaEnvWithOpenAlex(t *testing.T, openAlexURL string) mediaEnv {
	t.Helper()
	return newMediaEnvWithSources(t, openAlexURL, "")
}

// newMediaEnvWithSources is newMediaEnvWithOpenAlex with ORCID imports (FR-5.3) sent to
// the ORCID API at orcidURL, the ORCID_BASE_URL setting; tests pass a fake server, never
// the real one. Empty leaves imports unconfigured.
func newMediaEnvWithSources(t *testing.T, openAlexURL, orcidURL string) mediaEnv {
	t.Helper()
	return newMediaEnvOnPool(t, testenv.DB(t), openAlexURL, orcidURL)
}

// envOptions change how newMediaEnvOnPool wires the modules.
type envOptions struct {
	// wrapNotifier, when set, wraps the connection notifier the social usecase calls.
	wrapNotifier func(socialdomain.Notifier) socialdomain.Notifier
	// messageLimits, when set, replaces the production send limits (FR-16.10).
	messageLimits *messagingusecase.Limits
	// now, when set, is the clock of the auth and moderation modules. Access tokens are still
	// checked by the real clock, so a test can move past an end time and stay logged in.
	now func() time.Time
	// mail, when set, receives moderation's emails.
	mail textMailer
	// realAuth serves the auth operations from the real usecase instead of okAuth.
	realAuth bool
}

// newMediaEnvOnPool is newMediaEnvWithSources on a given pool to the test database, such
// as one that counts its queries.
func newMediaEnvOnPool(t *testing.T, pool *pgxpool.Pool, openAlexURL, orcidURL string, opts ...envOptions) mediaEnv {
	t.Helper()
	var opt envOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	for _, k := range []string{"S3_ENDPOINT", "S3_BUCKET"} {
		if os.Getenv(k) == "" {
			t.Fatalf("%s is not set", k)
		}
	}
	rdb := testenv.Redis(t)
	files, err := storage.NewS3(storage.Config{
		Endpoint: os.Getenv("S3_ENDPOINT"), Region: os.Getenv("S3_REGION"), Bucket: os.Getenv("S3_BUCKET"),
		AccessKey: os.Getenv("S3_ACCESS_KEY"), SecretKey: os.Getenv("S3_SECRET_KEY"), UseSSL: os.Getenv("S3_USE_SSL") == "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	return wireEnv(t, pool, rdb, files, openAlexURL, orcidURL, opt)
}

// wireEnv builds one API instance on the given stores. A second call gives a second instance
// of the same deployment, with sockets of its own.
func wireEnv(t *testing.T, pool *pgxpool.Pool, rdb *goredis.Client, files *storage.S3, openAlexURL, orcidURL string, opt envOptions) mediaEnv {
	t.Helper()
	limiter := mediaredis.New(rdb)
	t.Cleanup(func() { _ = limiter.Close() })
	logger := slog.New(slog.DiscardHandler)
	images := mediausecase.NewImages(mediapg.NewStore(pool), files, imaging.Codec{}, limiter, logger)
	up := pingerFunc(func(context.Context) error { return nil })
	lookups := profileredis.New(rdb)
	t.Cleanup(func() { _ = lookups.Close() })
	connectLimiter := socialredis.New(rdb)
	t.Cleanup(func() { _ = connectLimiter.Close() })
	messageLimiter := messagingredis.New(rdb)
	t.Cleanup(func() { _ = messageLimiter.Close() })
	limits := messagingusecase.Limits{MessagesPerMinute: 30, MessagesPerDay: 1000, RequestsPerMinute: 5, RequestsPerDay: 20}
	if opt.messageLimits != nil {
		limits = *opt.messageLimits
	}
	if limits.EditWindow == 0 {
		limits.EditWindow = 15 * time.Minute
	}
	now := time.Now
	if opt.now != nil {
		now = opt.now
	}
	moderationStore := moderationpg.NewStore(pool)
	standings := moderationusecase.NewStandings(moderationStore, now)
	realtimeConfig := realtimeusecase.DefaultConfig()
	realtimeConfig.OfflineGrace = 50 * time.Millisecond
	realtime := newRealtime(rdb, standings, realtimeConfig, logger)
	hubCtx, stopHub := context.WithCancel(context.Background())
	hubDone := make(chan struct{})
	go func() {
		defer close(hubDone)
		realtime.hub.Run(hubCtx)
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		realtime.hub.Shutdown(ctx)
		stopHub()
		<-hubDone
		realtime.close()
	})
	notifications := notificationusecase.New(notificationpg.NewStore(pool), notificationusecase.WithPublisher(realtime.events))
	connected := &connectionChecker{}
	messaging := messagingusecase.New(messagingpg.NewStore(pool), postgres.NewTransactor(pool), connected, connected, messagingActors{standings: standings}, messageLimiter,
		realtime.events, groupNotifier{notifications: notifications}, limits, now, logger)
	realtime.bind(messaging, notifications)
	messaging.UseFiles(messagingusecase.FileDeps{Intake: fileIntake{media: mediausecase.NewAttachments(images, imaging.Codec{})}, Objects: files})
	var notifier socialdomain.Notifier = connectionNotifier{notifications: notifications, messaging: messaging, events: realtime.events}
	if opt.wrapNotifier != nil {
		notifier = opt.wrapNotifier(notifier)
	}
	social := socialusecase.New(socialpg.NewStore(pool), postgres.NewTransactor(pool), connectLimiter, notifier, socialActors{standings: standings}, logger)
	connected.social = social
	profileStore := profilepg.NewStore(pool)
	profileOpts := []profileusecase.Option{profileusecase.WithPeopleSearch(profileusecase.PeopleSearch{
		Searcher: profileStore, Limiter: lookups.SearchLimiter(), Logger: logger,
	}), profileusecase.WithConnections(connectionLookup{social: social}),
		profileusecase.WithConversations(conversationLookup{messaging: messaging})}
	if orcidURL != "" {
		profileOpts = append(profileOpts, profileusecase.WithORCIDImport(profileusecase.ORCIDImport{
			Source:  orcid.New(orcid.Config{BaseURL: orcidURL, Timeout: time.Second, RetryDelay: time.Millisecond}),
			Cache:   lookups,
			Limiter: lookups.ORCIDLimiter(),
			Logger:  logger,
		}))
	}
	if openAlexURL != "" {
		profileOpts = append(profileOpts, profileusecase.WithDOILookup(profileusecase.DOILookup{
			Source:  openalex.New(openalex.Config{BaseURL: openAlexURL, Mailto: "it@example.org", Timeout: time.Second, RetryDelay: time.Millisecond}),
			Cache:   lookups,
			Limiter: lookups,
			Logger:  logger,
		}))
	}
	attempts := authredis.New(rdb)
	t.Cleanup(func() { _ = attempts.Close() })
	auth := authusecase.New(authpg.NewStore(pool), argon2.New(argon2.DefaultParams), noAuthMail{},
		accesstoken.New([]byte(testSecret), accessTTL), attempts, authusecase.Config{AppBaseURL: "http://localhost:3000", Sessions: realtime.events, Restrictions: standings}, now, logger)
	var mail textMailer = noText{}
	if opt.mail != nil {
		mail = opt.mail
	}
	moderation := moderationusecase.New(moderationStore, postgres.NewTransactor(pool), moderationAccounts{auth: auth, events: realtime.events},
		moderationReports{messaging: messaging}, moderationNotifier{notifications: notifications},
		moderationMailer{mailer: mail}, now, logger)
	authAPI := authhttp.NewHandler(okAuth{}, authhttp.CookieConfig{})
	if opt.realAuth {
		authAPI = authhttp.NewHandler(auth, authhttp.CookieConfig{})
	}
	router := newRouter(logger, up, up, up, authAPI,
		profilehttp.NewHandler(profileusecase.New(profileStore, profileOpts...)),
		mediahttp.NewHandler(images),
		socialhttp.NewHandler(social),
		notificationhttp.NewHandler(notifications),
		messaginghttp.NewHandler(messaging),
		moderationhttp.NewHandler(moderation),
		realtimehttp.NewHandler(realtime.hub, testOrigin, logger),
		httpx.RequireAuth(accesstoken.New([]byte(testSecret), accessTTL), time.Now))
	return mediaEnv{router: router, pool: pool, files: files, rdb: rdb, auth: auth, moderation: moderation, standings: standings, hub: realtime.hub, events: realtime.events, opt: opt,
		messaging: messaging, notifier: notifications}
}

// noAuthMail and noText send nothing.
type noAuthMail struct{}

func (noAuthMail) SendVerification(context.Context, authdomain.VerificationEmail) error { return nil }

func (noAuthMail) SendPasswordReset(context.Context, authdomain.PasswordResetEmail) error {
	return nil
}

type noText struct{}

func (noText) SendText(context.Context, string, string, string, string) error { return nil }

// newProfileUser stores a verified user with a profile. When the test ends it deletes the
// user and then the stored objects its files left in the deletion queue: emptying the
// database before the next test does not remove objects from the bucket.
func (e mediaEnv) newProfileUser(t *testing.T) string {
	t.Helper()
	ctx := t.Context()
	var id string
	if err := e.pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, email_verified_at)
		VALUES ($1, 'hash', now()) RETURNING id::text`, testenv.Email(t)).Scan(&id); err != nil {
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
