package main

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	authhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/http"
	authdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
	authusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/usecase"
	embeddinghttp "github.com/ARAFATH-SH/researchsphere/backend/internal/embedding/adapter/http"
	healthdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/health/domain"
	mediahttp "github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/http"
	messaginghttp "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/adapter/http"
	moderationhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/moderation/adapter/http"
	notificationhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/notification/adapter/http"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/aiclient"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/storage"
	profilehttp "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/http"
	profiledomain "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
	profileusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/usecase"
	projecthttp "github.com/ARAFATH-SH/researchsphere/backend/internal/project/adapter/http"
	realtimehttp "github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/adapter/http"
	recommendationhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/http"
	socialhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/social/adapter/http"
)

// freePort returns a TCP port that was free a moment ago.
func freePort(t *testing.T) int {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

// syncBuffer is a bytes.Buffer safe to read while the server writes logs to it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestRunShutsDownWhenContextIsCancelled(t *testing.T) {
	const password = "s3cret-pw"
	port := strconv.Itoa(freePort(t))
	vars := map[string]string{
		// Nothing listens on these ports; the clients connect lazily, so run starts anyway.
		"DATABASE_URL":       "postgres://u:" + password + "@127.0.0.1:1/db?sslmode=disable",
		"REDIS_URL":          "redis://:" + password + "@127.0.0.1:1/0",
		"API_PORT":           port,
		"JWT_SECRET":         testSecret,
		"UNSUBSCRIBE_SECRET": testUnsubscribeSecret,
		"S3_ENDPOINT":        "127.0.0.1:1",
		"S3_BUCKET":          "researchsphere",
		"S3_ACCESS_KEY":      "researchsphere",
		"S3_SECRET_KEY":      password,
	}
	ctx, cancel := context.WithCancel(context.Background())
	var logs syncBuffer
	done := make(chan error, 1)
	go func() { done <- run(ctx, func(k string) string { return vars[k] }, &logs) }()

	// Wait until the server answers, then ask for a shutdown.
	client := &http.Client{Timeout: time.Second}
	url := "http://127.0.0.1:" + port + "/api/v1/nope"
	deadline := time.Now().Add(5 * time.Second)
	for {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("status = %d, want 404", resp.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not start: %v\n%s", err, logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run() = %v, want nil", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("run() did not return after cancel")
	}
	out := logs.String()
	if !strings.Contains(out, "shutdown complete") {
		t.Errorf("logs lack shutdown complete:\n%s", out)
	}
	if strings.Contains(out, password) {
		t.Errorf("logs leak the password:\n%s", out)
	}
	if strings.Contains(out, testSecret) {
		t.Errorf("logs leak the JWT secret:\n%s", out)
	}
}

func TestRunStopsOnMissingSettings(t *testing.T) {
	err := run(context.Background(), func(string) string { return "" }, &bytes.Buffer{})
	for _, name := range []string{"DATABASE_URL", "REDIS_URL", "JWT_SECRET", "UNSUBSCRIBE_SECRET", "S3_ENDPOINT", "S3_BUCKET", "S3_ACCESS_KEY", "S3_SECRET_KEY"} {
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("run() error = %v, want one naming %s", err, name)
		}
	}
}

const testSecret = "test-jwt-secret-0123456789abcdefghij"

// testSyncToken is SYNC_SERVICE_TOKEN in tests (slice 22).
const testSyncToken = "test-sync-service-token-0123456789ab" //nolint:gosec // test value, not a credential

// testUnsubscribeSecret signs unsubscribe tokens in tests (UNSUBSCRIBE_SECRET).
const testUnsubscribeSecret = "test-unsubscribe-secret-0123456789ab" //nolint:gosec // test value, not a credential

// testRouter wires the real router and auth middleware to fakes.
func testRouter() http.Handler {
	up := pingerFunc(func(context.Context) error { return nil })
	return newRouter(slog.New(slog.DiscardHandler), up, up, up, up, authhttp.NewHandler(okAuth{}, authhttp.CookieConfig{}),
		profilehttp.NewHandler(profileusecase.New(okProfiles{}, profileusecase.WithDOILookup(profileusecase.DOILookup{
			Source: okWorks{}, Cache: okWorks{}, Limiter: okWorks{},
		}), profileusecase.WithORCIDImport(profileusecase.ORCIDImport{
			Source: okWorks{}, Cache: okWorks{}, Limiter: okWorks{},
		}), profileusecase.WithPeopleSearch(profileusecase.PeopleSearch{
			Searcher: okWorks{}, Limiter: okWorks{},
		}))),
		mediahttp.NewHandler(okMedia{}),
		socialhttp.NewHandler(okSocial{}),
		notificationhttp.NewHandler(okNotifications{}),
		messaginghttp.NewHandler(okMessaging{}),
		moderationhttp.NewHandler(okModeration{}),
		realtimehttp.NewHandler(okRealtime{}, "http://localhost:3000", slog.New(slog.DiscardHandler)),
		embeddinghttp.NewHandler(okEmbeddings{}),
		recommendationhttp.NewHandler(okRecommendations{}),
		projecthttp.NewHandler(okProjects{}),
		serviceAndSessionAuth([]byte(testSyncToken), httpx.RequireAuth(accesstoken.New([]byte(testSecret), accessTTL), time.Now)))
}

func TestHealthThroughRouter(t *testing.T) {
	router := testRouter()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"status":"ok"`) || !strings.Contains(body, `"ai":"ok"`) {
		t.Errorf("body = %s, want status ok and ai ok", body)
	}
}

// healthRouter is the router with the given storage and AI service pingers, the database
// and Redis up.
func healthRouter(storage, ai healthdomain.Pinger) http.Handler {
	up := pingerFunc(func(context.Context) error { return nil })
	return newRouter(slog.New(slog.DiscardHandler), up, up, storage, ai, authhttp.NewHandler(okAuth{}, authhttp.CookieConfig{}),
		profilehttp.NewHandler(profileusecase.New(okProfiles{})),
		mediahttp.NewHandler(okMedia{}),
		socialhttp.NewHandler(okSocial{}),
		notificationhttp.NewHandler(okNotifications{}),
		messaginghttp.NewHandler(okMessaging{}),
		moderationhttp.NewHandler(okModeration{}),
		realtimehttp.NewHandler(okRealtime{}, "http://localhost:3000", slog.New(slog.DiscardHandler)),
		embeddinghttp.NewHandler(okEmbeddings{}),
		recommendationhttp.NewHandler(okRecommendations{}),
		projecthttp.NewHandler(okProjects{}),
		serviceAndSessionAuth([]byte(testSyncToken), httpx.RequireAuth(accesstoken.New([]byte(testSecret), accessTTL), time.Now)))
}

// The AI service is optional (NFR-10.2): through the real client, GET /health says ok when
// it is ready and degraded, still 200, when it is loading, stopped or not configured.
func TestHealthReportsTheAIService(t *testing.T) {
	const token = "test-ai-service-token-0123456789abcd" //nolint:gosec // test value, not a credential
	service := func(status int) string {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/ready" {
				t.Errorf("health check called %s, want /ready", r.URL.Path)
			}
			w.WriteHeader(status)
		}))
		t.Cleanup(srv.Close)
		return srv.URL
	}
	stopped := httptest.NewServer(http.NotFoundHandler())
	stopped.Close()

	tests := []struct {
		name       string
		client     *aiclient.Client
		wantStatus string
		wantAI     string
	}{
		{"ready", aiclient.New(aiclient.Config{BaseURL: service(http.StatusOK), Token: token}), "ok", "ok"},
		{"loading its model", aiclient.New(aiclient.Config{BaseURL: service(http.StatusServiceUnavailable), Token: token}), "degraded", "unavailable"},
		{"stopped", aiclient.New(aiclient.Config{BaseURL: stopped.URL, Token: token}), "degraded", "unavailable"},
		{"not configured", aiclient.New(aiclient.Config{BaseURL: service(http.StatusOK)}), "degraded", "unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			up := pingerFunc(func(context.Context) error { return nil })
			healthRouter(up, tt.client).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/health", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
			}
			body := rec.Body.String()
			if !strings.Contains(body, `"status":"`+tt.wantStatus+`"`) || !strings.Contains(body, `"ai":"`+tt.wantAI+`"`) {
				t.Errorf("body = %s, want status %s and ai %s", body, tt.wantStatus, tt.wantAI)
			}
		})
	}
}

// Storage that is not running (MinIO stopped) turns the health check into a 503 naming
// storage, through the real S3 client, while the database and Redis answer.
func TestHealthReportsStorageDown(t *testing.T) {
	files, err := storage.NewS3(storage.Config{Endpoint: "127.0.0.1:1", Region: "us-east-1",
		Bucket: "researchsphere", AccessKey: "researchsphere", SecretKey: "secret-for-tests"})
	if err != nil {
		t.Fatal(err)
	}
	up := pingerFunc(func(context.Context) error { return nil })
	router := healthRouter(files, up)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/health", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body %s", rec.Code, rec.Body)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"message":"storage is not responding"`) {
		t.Errorf("body = %s, want the message naming storage only", body)
	}
}

func TestAuthRoutesThroughRouter(t *testing.T) {
	router := testRouter()

	tests := []struct {
		path, body string
		want       int
	}{
		{"/api/v1/auth/register", `{"fullName":"Ada","email":"ada@example.org","password":"password1"}`, http.StatusCreated},
		{"/api/v1/auth/verify-email", `{"token":"x"}`, http.StatusNoContent},
		{"/api/v1/auth/resend-verification", `{"email":"ada@example.org"}`, http.StatusAccepted},
		{"/api/v1/auth/login", `{"email":"ada@example.org","password":"password1"}`, http.StatusOK},
		{"/api/v1/auth/password-reset/request", `{"email":"ada@example.org"}`, http.StatusAccepted},
		{"/api/v1/auth/password-reset/confirm", `{"token":"x","newPassword":"password1"}`, http.StatusNoContent},
		// Public: reachable with no access cookie, which may have expired.
		{"/api/v1/auth/refresh", "", http.StatusNoContent},
		{"/api/v1/auth/logout", "", http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.want, rec.Body)
			}
		})
	}
}

// okAuth is an auth usecase that accepts everything.
type okAuth struct{}

func (okAuth) Register(_ context.Context, in authusecase.RegisterInput) (authusecase.RegisterResult, error) {
	return authusecase.RegisterResult{Email: in.Email}, nil
}

func (okAuth) VerifyEmail(context.Context, string) error { return nil }

func (okAuth) ResendVerification(context.Context, string) error { return nil }

func (okAuth) Login(context.Context, authusecase.LoginInput) (authusecase.LoginResult, error) {
	now := time.Now()
	return authusecase.LoginResult{
		User:     authdomain.User{ID: okUserID, Email: "ada@example.org", FullName: "Ada"},
		IssuedAt: now, AccessToken: "a", AccessExpiresAt: now.Add(accessTTL),
		RefreshToken: "r", RefreshExpiresAt: now.Add(authusecase.RefreshTTL),
	}, nil
}

func (okAuth) CurrentUser(_ context.Context, id string) (authdomain.User, error) {
	return authdomain.User{ID: id, Email: "ada@example.org", FullName: "Ada"}, nil
}

func (okAuth) MessagingRestrictedUntil(context.Context, string) (*time.Time, error) {
	return nil, nil
}

func (okAuth) Refresh(context.Context, string) (authusecase.RefreshResult, error) {
	now := time.Now()
	return authusecase.RefreshResult{
		IssuedAt: now, AccessToken: "a", AccessExpiresAt: now.Add(accessTTL),
		RefreshToken: "r", RefreshExpiresAt: now.Add(authusecase.RefreshTTL),
	}, nil
}

func (okAuth) Logout(context.Context, string) error { return nil }

func (okAuth) RequestPasswordReset(context.Context, string) error { return nil }

func (okAuth) ResetPassword(context.Context, string, string) error { return nil }

func (okAuth) ChangePassword(context.Context, authusecase.ChangePasswordInput) error { return nil }

func (okAuth) DeleteAccount(context.Context, authusecase.DeleteAccountInput) error { return nil }

func (okAuth) ListUsers(_ context.Context, page, pageSize int) (authdomain.UserPage, error) {
	return authdomain.UserPage{
		Users:    []authdomain.User{{ID: okUserID, Email: "ada@example.org", FullName: "Ada", Role: authdomain.RoleAdmin, CreatedAt: time.Now()}},
		Page:     page,
		PageSize: min(pageSize, authusecase.MaxPageSize),
		Total:    1,
	}, nil
}

const okUserID = "6f1c2b9e-1d2a-4c3b-9a8e-0f1e2d3c4b5a"

// okProfiles is a profile store in which every ID belongs to a verified user named Ada,
// every write succeeds and every tag is new to the user.
type okProfiles struct{}

func (okProfiles) FindAccount(_ context.Context, id string) (profiledomain.Account, error) {
	orcidID := "0000-0002-1825-0097"
	return profiledomain.Account{UserID: id, Verified: true, Profile: &profiledomain.Profile{UserID: id, FullName: "Ada", OrcidID: &orcidID}}, nil
}

func (okProfiles) UpsertProfile(_ context.Context, p profiledomain.Profile) (profiledomain.Profile, error) {
	return p, nil
}

func (okProfiles) RunInTx(ctx context.Context, fn func(profiledomain.Tx) error) error {
	return fn(okProfiles{})
}

func (okProfiles) ListTags(context.Context, string, profiledomain.TagKind) ([]profiledomain.Tag, error) {
	return nil, nil
}

func (okProfiles) RemoveTag(context.Context, string, profiledomain.TagKind, string) error { return nil }

func (okProfiles) SuggestTags(context.Context, profiledomain.TagKind, string, int) ([]profiledomain.Tag, error) {
	return []profiledomain.Tag{{ID: okTagID, Name: "Machine Learning"}}, nil
}

func (okProfiles) LockUser(context.Context, string) error { return nil }

func (okProfiles) FindOrCreateTag(_ context.Context, _ profiledomain.TagKind, name string) (profiledomain.Tag, error) {
	return profiledomain.Tag{ID: okTagID, Name: name}, nil
}

func (okProfiles) HasTag(context.Context, string, profiledomain.TagKind, string) (bool, error) {
	return false, nil
}

func (okProfiles) CountTags(context.Context, string, profiledomain.TagKind) (int, error) {
	return 0, nil
}

func (okProfiles) LinkTag(context.Context, string, profiledomain.TagKind, string) error { return nil }

func (okProfiles) ListEducation(context.Context, string) ([]profiledomain.Education, error) {
	return nil, nil
}

func (okProfiles) DeleteEducation(context.Context, string, string) error { return nil }

func (okProfiles) CountEducation(context.Context, string) (int, error) { return 0, nil }

func (okProfiles) InsertEducation(_ context.Context, e profiledomain.Education) (profiledomain.Education, error) {
	e.ID = okEducationID
	return e, nil
}

func (okProfiles) FindEducationForUpdate(_ context.Context, userID, id string) (profiledomain.Education, error) {
	return profiledomain.Education{ID: id, UserID: userID, Institution: "MIT", StartYear: 2020}, nil
}

func (okProfiles) UpdateEducation(_ context.Context, e profiledomain.Education) (profiledomain.Education, error) {
	return e, nil
}

func (okProfiles) ListExperience(context.Context, string) ([]profiledomain.Experience, error) {
	return nil, nil
}

func (okProfiles) DeleteExperience(context.Context, string, string) error { return nil }

func (okProfiles) CountExperience(context.Context, string) (int, error) { return 0, nil }

func (okProfiles) InsertExperience(_ context.Context, e profiledomain.Experience) (profiledomain.Experience, error) {
	e.ID = okExperienceID
	return e, nil
}

func (okProfiles) FindExperienceForUpdate(_ context.Context, userID, id string) (profiledomain.Experience, error) {
	return profiledomain.Experience{
		ID: id, UserID: userID, Title: "Postdoc", Organization: "MIT", EmploymentType: profiledomain.EmploymentPostdoc,
		Start: profiledomain.YearMonth{Year: 2020, Month: 1}, IsCurrent: true,
	}, nil
}

func (okProfiles) UpdateExperience(_ context.Context, e profiledomain.Experience) (profiledomain.Experience, error) {
	return e, nil
}

func (okProfiles) ListLanguages(context.Context, string) ([]profiledomain.Language, error) {
	return nil, nil
}

func (okProfiles) DeleteLanguage(context.Context, string, string) error { return nil }

func (okProfiles) CountLanguages(context.Context, string) (int, error) { return 0, nil }

func (okProfiles) LanguageNameTaken(context.Context, string, string, string) (bool, error) {
	return false, nil
}

func (okProfiles) InsertLanguage(_ context.Context, l profiledomain.Language) (profiledomain.Language, error) {
	l.ID = okLanguageID
	return l, nil
}

func (okProfiles) FindLanguageForUpdate(_ context.Context, userID, id string) (profiledomain.Language, error) {
	return profiledomain.Language{
		ID: id, UserID: userID, Name: "Bangla", Proficiency: profiledomain.ProficiencyNativeOrBilingual,
	}, nil
}

func (okProfiles) UpdateLanguage(_ context.Context, l profiledomain.Language) (profiledomain.Language, error) {
	return l, nil
}

func (okProfiles) FindContact(_ context.Context, userID string) (profiledomain.Contact, error) {
	return profiledomain.Contact{
		UserID: userID, Email: "ada@example.org", EmailVisibility: profiledomain.OnlyMe,
		PhoneVisibility: profiledomain.OnlyMe, BirthdayVisibility: profiledomain.OnlyMe,
	}, nil
}

func (okProfiles) UpsertContact(context.Context, profiledomain.Contact) error { return nil }

func (okProfiles) ListLinks(context.Context, string) ([]profiledomain.Link, error) { return nil, nil }

func (okProfiles) DeleteLink(context.Context, string, string) error { return nil }

func (okProfiles) CountLinks(context.Context, string) (int, error) { return 0, nil }

func (okProfiles) InsertLink(_ context.Context, l profiledomain.Link) (profiledomain.Link, error) {
	l.ID = okLinkID
	return l, nil
}

func (okProfiles) ListConfirmedPublications(context.Context, string) ([]profiledomain.Publication, error) {
	return nil, nil
}

func (okProfiles) CountConfirmedPublications(context.Context, string) (int, error) { return 0, nil }

func (okProfiles) CountPublicationLinks(context.Context, string) (int, error) { return 0, nil }

func (okProfiles) FindPublicationByDOI(context.Context, string) (profiledomain.Publication, error) {
	return profiledomain.Publication{}, profiledomain.ErrNotFound
}

func (okProfiles) InsertPublication(_ context.Context, p profiledomain.Publication) (profiledomain.Publication, error) {
	p.ID = okPublicationID
	return p, nil
}

func (okProfiles) PublicationLink(context.Context, string, string) (profiledomain.LinkState, error) {
	return profiledomain.NotLinked, nil
}

func (okProfiles) LinkPublication(context.Context, string, string) error { return nil }

func (okProfiles) ConfirmPublication(context.Context, string, string) error { return nil }

func (okProfiles) FindPublicationForUpdate(_ context.Context, _, id string) (profiledomain.Publication, error) {
	year := 2024
	return profiledomain.Publication{ID: id, Title: "Notes", Year: &year, Source: profiledomain.SourceManual}, nil
}

func (okProfiles) CountPublicationAuthors(context.Context, string) (int, error) { return 1, nil }

func (okProfiles) UpdatePublication(_ context.Context, p profiledomain.Publication) (profiledomain.Publication, error) {
	return p, nil
}

func (okProfiles) UnlinkPublication(context.Context, string, string) error { return nil }

func (okProfiles) DeletePublicationIfOrphaned(context.Context, string) error { return nil }

func (okProfiles) SetOrcidID(context.Context, string, *string) error { return nil }

func (okProfiles) MatchPublicationsByDOI(context.Context, string, []string) (map[string]profiledomain.DOIMatch, error) {
	return map[string]profiledomain.DOIMatch{}, nil
}

const okEducationID = "8b3e4d5c-6f7a-4b2c-9d0e-1f2a3b4c5d6e"

const okLinkID = "9c4f5e6d-7a8b-4c3d-8e1f-2a3b4c5d6e7f"

const okExperienceID = "ad5a6f7e-8b9c-4d4e-9f2a-3b4c5d6e7f8a"

const okLanguageID = "be6b7a8f-9c0d-4e5f-8a3b-4c5d6e7f8a9b"

const okPublicationID = "cf7c8b9a-0d1e-4f6a-9b4c-5d6e7f8a9b0c"

const okTagID = "7a2d3c4b-5e6f-4a1b-8c9d-0e1f2a3b4c5d"

// The wired router needs the access cookie for /auth/me and accepts a token it signed.
func TestCurrentUserThroughRouter(t *testing.T) {
	router := testRouter()
	get := func(cookie string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/me", nil)
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	if rec := get(""); rec.Code != http.StatusUnauthorized {
		t.Errorf("without a cookie: status = %d, want 401", rec.Code)
	}
	token, _, err := accesstoken.New([]byte(testSecret), accessTTL).Sign(okUserID, "researcher", true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rec := get(httpx.AccessCookie + "=" + token)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), okUserID) {
		t.Errorf("with a valid cookie: %d %s, want 200 with the user", rec.Code, rec.Body)
	}
}

// The wired router needs the access cookie to delete an account (FR-2.8).
func TestDeleteAccountThroughRouter(t *testing.T) {
	router := testRouter()
	del := func(cookie string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/account/delete", strings.NewReader(`{"password":"password1"}`))
		req.Header.Set("Content-Type", "application/json")
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	if rec := del(""); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), httpx.CodeUnauthenticated) {
		t.Errorf("without a cookie: %d %s, want 401 unauthenticated", rec.Code, rec.Body)
	}
	token, _, err := accesstoken.New([]byte(testSecret), accessTTL).Sign(okUserID, "researcher", true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rec := del(httpx.AccessCookie + "=" + token); rec.Code != http.StatusNoContent {
		t.Errorf("with a valid cookie: %d %s, want 204", rec.Code, rec.Body)
	}
}

// pingerFunc adapts a function to the health domain's Pinger.
type pingerFunc func(ctx context.Context) error

func (f pingerFunc) Ping(ctx context.Context) error { return f(ctx) }

// okWorks finds every DOI, lists one ORCID work with put-code 1, caches nothing and never
// limits.
type okWorks struct{}

func (okWorks) FetchWork(_ context.Context, doi string) (profiledomain.WorkMetadata, error) {
	return profiledomain.WorkMetadata{DOI: doi, Title: "Notes"}, nil
}

func (okWorks) GetWork(context.Context, string) (profiledomain.WorkMetadata, bool, error) {
	return profiledomain.WorkMetadata{}, false, nil
}

func (okWorks) PutWork(context.Context, profiledomain.WorkMetadata, time.Duration) error { return nil }

func (okWorks) Used(context.Context, string) (int, time.Duration, error) { return 0, 0, nil }

func (okWorks) Record(context.Context, string, time.Duration) error { return nil }

func (okWorks) SearchPeople(context.Context, profiledomain.PeopleQuery, int) (profiledomain.PeopleResults, error) {
	return profiledomain.PeopleResults{}, nil
}

func (okWorks) SearchPeopleHybrid(context.Context, profiledomain.PeopleQuery, profiledomain.QueryVector, int) (profiledomain.PeopleResults, error) {
	return profiledomain.PeopleResults{}, nil
}

func (okWorks) ListWorks(context.Context, string) ([]profiledomain.ORCIDWork, error) {
	return []profiledomain.ORCIDWork{{PutCode: "1", Title: "Notes"}}, nil
}

func (okWorks) GetORCIDWorks(context.Context, string) ([]profiledomain.ORCIDWork, bool, error) {
	return nil, false, nil
}

func (okWorks) PutORCIDWorks(context.Context, string, []profiledomain.ORCIDWork, time.Duration) error {
	return nil
}
