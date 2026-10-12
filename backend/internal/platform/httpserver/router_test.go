package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/logging"
)

const panicText = "boom: secret internal detail"

// fakeAPI answers every operation in the contract with a fixed success, so these tests
// exercise the router and middleware without any module.
type fakeAPI struct{}

func (fakeAPI) GetHealth(context.Context, gen.GetHealthRequestObject) (gen.GetHealthResponseObject, error) {
	return gen.GetHealth200JSONResponse{Status: gen.HealthStatusOk, Database: gen.HealthDatabaseOk, Redis: gen.HealthRedisOk}, nil
}

func (fakeAPI) RegisterUser(context.Context, gen.RegisterUserRequestObject) (gen.RegisterUserResponseObject, error) {
	return gen.RegisterUser201JSONResponse{Email: "ada@example.org"}, nil
}

func (fakeAPI) VerifyEmail(context.Context, gen.VerifyEmailRequestObject) (gen.VerifyEmailResponseObject, error) {
	return gen.VerifyEmail204Response{}, nil
}

func (fakeAPI) ResendVerification(context.Context, gen.ResendVerificationRequestObject) (gen.ResendVerificationResponseObject, error) {
	return gen.ResendVerification202Response{}, nil
}

func (fakeAPI) LoginUser(context.Context, gen.LoginUserRequestObject) (gen.LoginUserResponseObject, error) {
	return gen.LoginUser200JSONResponse{Body: gen.CurrentUser{Email: "ada@example.org"}}, nil
}

// GetCurrentUser echoes the user ID that the auth middleware put in the context.
func (fakeAPI) GetCurrentUser(ctx context.Context, _ gen.GetCurrentUserRequestObject) (gen.GetCurrentUserResponseObject, error) {
	id, _ := httpx.UserID(ctx)
	return gen.GetCurrentUser200JSONResponse{FullName: id}, nil
}

var _ gen.StrictServerInterface = fakeAPI{}

var testSigner = accesstoken.New([]byte("test-secret-that-is-at-least-32-bytes-long"), 15*time.Minute)

// newTestRouter returns the full router with a fake API, a /test/panic route, and a
// buffer that receives the JSON logs.
func newTestRouter(t *testing.T) (http.Handler, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	logger := slog.New(logging.NewHandler(slog.NewJSONHandler(&logs, nil)))
	router := newRouter(logger, fakeAPI{}, httpx.RequireAuth(testSigner, time.Now), func(mux *http.ServeMux) {
		mux.HandleFunc("GET /test/panic", func(http.ResponseWriter, *http.Request) { panic(panicText) })
	})
	return router, &logs
}

func serve(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, path, nil))
	return rec
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) (code, message string) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not JSON: %v", rec.Body, err)
	}
	if len(body) != 2 {
		t.Errorf("body has keys %v, want exactly code and message", body)
	}
	code, _ = body["code"].(string)
	message, _ = body["message"].(string)
	if code == "" || message == "" {
		t.Errorf("body %v lacks a code or message", body)
	}
	return code, message
}

func TestErrorShape(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantCode   string
		wantAllow  string
	}{
		{"unknown route", http.MethodGet, "/nope", http.StatusNotFound, "not_found", ""},
		{"unknown route under api", http.MethodGet, "/api/v1/nope", http.StatusNotFound, "not_found", ""},
		{"root", http.MethodGet, "/", http.StatusNotFound, "not_found", ""},
		{"wrong method", http.MethodPost, "/api/v1/health", http.StatusMethodNotAllowed, "method_not_allowed", "GET, HEAD"},
		{"panic", http.MethodGet, "/test/panic", http.StatusInternalServerError, "internal_error", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, _ := newTestRouter(t)
			rec := serve(t, router, tt.method, tt.path)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			code, _ := decodeError(t, rec)
			if code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
			if got := rec.Header().Get("Allow"); got != tt.wantAllow {
				t.Errorf("Allow = %q, want %q", got, tt.wantAllow)
			}
			if strings.Contains(rec.Body.String(), "boom") {
				t.Errorf("body leaks the panic value: %s", rec.Body)
			}
		})
	}
}

func TestRequestIDIsSetAndUnique(t *testing.T) {
	router, _ := newTestRouter(t)
	seen := map[string]bool{}
	for range 3 {
		id := serve(t, router, http.MethodGet, "/api/v1/health").Header().Get(httpx.RequestIDHeader)
		if len(id) != 32 {
			t.Errorf("request ID %q, want 32 hex characters", id)
		}
		if seen[id] {
			t.Errorf("request ID %q repeated", id)
		}
		seen[id] = true
	}
}

func TestClientRequestIDIsIgnored(t *testing.T) {
	router, _ := newTestRouter(t)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/health", nil)
	req.Header.Set(httpx.RequestIDHeader, "injected\nvalue")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if got := rec.Header().Get(httpx.RequestIDHeader); got == "injected\nvalue" || len(got) != 32 {
		t.Errorf("request ID = %q, want a new server-made ID", got)
	}
}

func TestOneAccessLogLinePerRequest(t *testing.T) {
	tests := []struct {
		method, path string
		wantStatus   float64
	}{
		{http.MethodGet, "/api/v1/health?token=abc", 200},
		{http.MethodGet, "/nope", 404},
		{http.MethodGet, "/test/panic", 500},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			router, logs := newTestRouter(t)
			rec := serve(t, router, tt.method, tt.path)

			var access []map[string]any
			for line := range strings.SplitSeq(strings.TrimSpace(logs.String()), "\n") {
				var entry map[string]any
				if err := json.Unmarshal([]byte(line), &entry); err != nil {
					t.Fatalf("log line %q is not JSON: %v", line, err)
				}
				if entry["msg"] == "request" {
					access = append(access, entry)
				}
			}
			if len(access) != 1 {
				t.Fatalf("got %d access log lines, want 1:\n%s", len(access), logs)
			}
			e := access[0]
			wantPath, _, _ := strings.Cut(tt.path, "?")
			if e["method"] != tt.method || e["path"] != wantPath || e["status"] != tt.wantStatus {
				t.Errorf("access line = %v, want method %s path %s status %v", e, tt.method, wantPath, tt.wantStatus)
			}
			if _, ok := e["duration_ms"].(float64); !ok {
				t.Errorf("access line lacks duration_ms: %v", e)
			}
			if e["request_id"] != rec.Header().Get(httpx.RequestIDHeader) {
				t.Errorf("request_id = %v, want %q", e["request_id"], rec.Header().Get(httpx.RequestIDHeader))
			}
			if strings.Contains(logs.String(), "token=abc") {
				t.Errorf("logs contain the query string:\n%s", logs)
			}
		})
	}
}

func TestPanicIsLoggedWithRequestID(t *testing.T) {
	router, logs := newTestRouter(t)
	rec := serve(t, router, http.MethodGet, "/test/panic")
	out := logs.String()
	if !strings.Contains(out, "panic recovered") || !strings.Contains(out, panicText) {
		t.Errorf("panic not logged:\n%s", out)
	}
	if !strings.Contains(out, `"request_id":"`+rec.Header().Get(httpx.RequestIDHeader)+`"`) {
		t.Errorf("panic log lacks the request ID:\n%s", out)
	}
}

// The contract secures every operation without `security: []`; the generated wrapper
// tells the auth middleware so, and public operations need no cookie.
func TestContractDecidesWhichOperationsNeedASession(t *testing.T) {
	router, _ := newTestRouter(t)

	rec := serve(t, router, http.MethodGet, "/api/v1/auth/me")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /auth/me without a cookie: status = %d, want 401", rec.Code)
	}
	if code, _ := decodeError(t, rec); code != httpx.CodeUnauthenticated {
		t.Errorf("code = %q, want %q", code, httpx.CodeUnauthenticated)
	}

	token, _, err := testSigner.Sign("user-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: httpx.AccessCookie, Value: token})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"user-1"`) {
		t.Errorf("GET /auth/me with a valid cookie: %d %s, want 200 with the user ID", rec.Code, rec.Body)
	}

	if rec := serve(t, router, http.MethodGet, "/api/v1/health"); rec.Code != http.StatusOK {
		t.Errorf("GET /health without a cookie: status = %d, want 200", rec.Code)
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"email":"ada@example.org","password":"pw"}`)))
	if rec.Code != http.StatusOK {
		t.Errorf("POST /auth/login without a cookie: status = %d, want 200", rec.Code)
	}
}
