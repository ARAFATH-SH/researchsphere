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
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
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
		"DATABASE_URL": "postgres://u:" + password + "@127.0.0.1:1/db?sslmode=disable",
		"REDIS_URL":    "redis://:" + password + "@127.0.0.1:1/0",
		"API_PORT":     port,
		"JWT_SECRET":   testSecret,
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
	for _, name := range []string{"DATABASE_URL", "REDIS_URL", "JWT_SECRET"} {
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("run() error = %v, want one naming %s", err, name)
		}
	}
}

const testSecret = "test-jwt-secret-0123456789abcdefghij"

// testRouter wires the real router and auth middleware to fakes.
func testRouter() http.Handler {
	up := pingerFunc(func(context.Context) error { return nil })
	return newRouter(slog.New(slog.DiscardHandler), up, up, authhttp.NewHandler(okAuth{}, authhttp.CookieConfig{}),
		httpx.RequireAuth(accesstoken.New([]byte(testSecret), accessTTL), time.Now))
}

func TestHealthThroughRouter(t *testing.T) {
	router := testRouter()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
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

const okUserID = "6f1c2b9e-1d2a-4c3b-9a8e-0f1e2d3c4b5a"

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
	token, _, err := accesstoken.New([]byte(testSecret), accessTTL).Sign(okUserID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rec := get(httpx.AccessCookie + "=" + token)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), okUserID) {
		t.Errorf("with a valid cookie: %d %s, want 200 with the user", rec.Code, rec.Body)
	}
}

// pingerFunc adapts a function to the health domain's Pinger.
type pingerFunc func(ctx context.Context) error

func (f pingerFunc) Ping(ctx context.Context) error { return f(ctx) }
