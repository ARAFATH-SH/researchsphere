package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// fakeService returns err from every operation and records what it was given.
type fakeService struct {
	err      error
	register usecase.RegisterInput
	token    string
	email    string
	login    usecase.LoginInput
	result   usecase.LoginResult // returned by Login
	user     domain.User         // returned by CurrentUser
	userID   string
	refresh  usecase.RefreshResult // returned by Refresh
	sent     []string              // refresh tokens given to Refresh and Logout
}

func (f *fakeService) Refresh(_ context.Context, token string) (usecase.RefreshResult, error) {
	f.sent = append(f.sent, token)
	return f.refresh, f.err
}

func (f *fakeService) Logout(_ context.Context, token string) error {
	f.sent = append(f.sent, token)
	return f.err
}

func (f *fakeService) Register(_ context.Context, in usecase.RegisterInput) (usecase.RegisterResult, error) {
	f.register = in
	return usecase.RegisterResult{Email: in.Email}, f.err
}

func (f *fakeService) VerifyEmail(_ context.Context, token string) error {
	f.token = token
	return f.err
}

func (f *fakeService) ResendVerification(_ context.Context, email string) error {
	f.email = email
	return f.err
}

func (f *fakeService) Login(_ context.Context, in usecase.LoginInput) (usecase.LoginResult, error) {
	f.login = in
	return f.result, f.err
}

func (f *fakeService) CurrentUser(_ context.Context, id string) (domain.User, error) {
	f.userID = id
	return f.user, f.err
}

// server serves only the auth operations: Handler's methods are promoted from a
// shallower depth than the embedded interface's, which stays nil.
type server struct {
	*Handler
	others
}

type others struct{ gen.StrictServerInterface }

func post(t *testing.T, svc *fakeService, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return serve(NewHandler(svc, CookieConfig{}), req)
}

// serve sends req through the generated routes to h.
func serve(h *Handler, req *http.Request) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	gen.HandlerWithOptions(gen.NewStrictHandlerWithOptions(server{Handler: h}, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
			w.WriteHeader(http.StatusBadRequest)
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	}), gen.StdHTTPServerOptions{BaseURL: "/api/v1", BaseRouter: mux})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	if rec.Body.Len() == 0 {
		return nil
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %q is not JSON: %v", rec.Body, err)
	}
	return got
}

func TestRegisterUser(t *testing.T) {
	const body = `{"fullName":"Ada","email":"ada@example.org","password":"password1"}`
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"created", nil, http.StatusCreated, ""},
		{"invalid name", domain.ErrInvalidFullName, http.StatusBadRequest, CodeInvalidFullName},
		{"invalid email", domain.ErrInvalidEmail, http.StatusBadRequest, CodeInvalidEmail},
		{"short password", domain.ErrPasswordTooShort, http.StatusBadRequest, CodePasswordTooShort},
		{"long password", domain.ErrPasswordTooLong, http.StatusBadRequest, CodePasswordTooLong},
		{"email taken", domain.ErrEmailTaken, http.StatusConflict, CodeEmailTaken},
		{"unexpected error", errors.New("database down"), http.StatusInternalServerError, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{err: tt.err}
			rec := post(t, svc, "/auth/register", body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			got := decode(t, rec)
			switch {
			case tt.wantStatus == http.StatusCreated && got["email"] != "ada@example.org":
				t.Errorf("body = %v, want the email", got)
			case tt.wantCode != "" && (got["code"] != tt.wantCode || got["message"] == ""):
				t.Errorf("body = %v, want code %q and a message", got, tt.wantCode)
			}
			if want := (usecase.RegisterInput{FullName: "Ada", Email: "ada@example.org", Password: "password1"}); svc.register != want {
				t.Errorf("usecase got %+v, want %+v", svc.register, want)
			}
		})
	}
}

func TestVerifyEmail(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"verified", nil, http.StatusNoContent, ""},
		{"unknown token", domain.ErrTokenInvalid, http.StatusBadRequest, CodeTokenInvalid},
		{"used token", domain.ErrTokenUsed, http.StatusBadRequest, CodeTokenUsed},
		{"expired token", domain.ErrTokenExpired, http.StatusBadRequest, CodeTokenExpired},
		{"unexpected error", errors.New("database down"), http.StatusInternalServerError, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{err: tt.err}
			rec := post(t, svc, "/auth/verify-email", `{"token":"abc"}`)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			got := decode(t, rec)
			if tt.wantCode != "" && (got["code"] != tt.wantCode || got["message"] == "") {
				t.Errorf("body = %v, want code %q and a message", got, tt.wantCode)
			}
			if tt.wantStatus == http.StatusNoContent && rec.Body.Len() != 0 {
				t.Errorf("204 with body %q", rec.Body)
			}
			if svc.token != "abc" {
				t.Errorf("usecase got token %q", svc.token)
			}
		})
	}
}

func TestResendVerification(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"accepted", nil, http.StatusAccepted, ""},
		{"malformed email", domain.ErrInvalidEmail, http.StatusBadRequest, CodeInvalidEmail},
		{"unexpected error", errors.New("database down"), http.StatusInternalServerError, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{err: tt.err}
			rec := post(t, svc, "/auth/resend-verification", `{"email":"ada@example.org"}`)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if got := decode(t, rec); tt.wantCode != "" && got["code"] != tt.wantCode {
				t.Errorf("body = %v, want code %q", got, tt.wantCode)
			}
			if svc.email != "ada@example.org" {
				t.Errorf("usecase got email %q", svc.email)
			}
		})
	}
}

func TestMalformedBody(t *testing.T) {
	for _, path := range []string{"/auth/register", "/auth/verify-email", "/auth/resend-verification"} {
		t.Run(path, func(t *testing.T) {
			if rec := post(t, &fakeService{}, path, `{not json`); rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
		})
	}
}

const userID = "6f1c2b9e-1d2a-4c3b-9a8e-0f1e2d3c4b5a"

func loginResult() usecase.LoginResult {
	issued := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	return usecase.LoginResult{
		User:             domain.User{ID: userID, Email: "ada@example.org", FullName: "Ada Lovelace"},
		IssuedAt:         issued,
		AccessToken:      "access-token",
		AccessExpiresAt:  issued.Add(15 * time.Minute),
		RefreshToken:     "refresh-token",
		RefreshExpiresAt: issued.Add(30 * 24 * time.Hour),
	}
}

func login(t *testing.T, svc *fakeService, cookies CookieConfig) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"email":"ada@example.org","password":"password1"}`))
	req.Header.Set("Content-Type", "application/json")
	return serve(NewHandler(svc, cookies), req)
}

func TestLoginUserSetsCookies(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(map[bool]string{false: "local http", true: "https"}[secure], func(t *testing.T) {
			svc := &fakeService{result: loginResult()}
			rec := login(t, svc, CookieConfig{Secure: secure})

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
			}
			if svc.login != (usecase.LoginInput{Email: "ada@example.org", Password: "password1"}) {
				t.Errorf("service got %+v", svc.login)
			}
			if got := decode(t, rec); got["id"] != userID || got["email"] != "ada@example.org" || got["fullName"] != "Ada Lovelace" || len(got) != 3 {
				t.Errorf("body = %v", got)
			}
			if strings.Contains(rec.Body.String(), "token") {
				t.Errorf("the body carries a token: %s", rec.Body)
			}
			if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", cc)
			}

			cookies := map[string]*http.Cookie{}
			for _, c := range rec.Result().Cookies() {
				cookies[c.Name] = c
			}
			want := []struct {
				name, value, path string
				maxAge            int
			}{
				{httpx.AccessCookie, "access-token", "/", 15 * 60},
				{httpx.RefreshCookie, "refresh-token", "/api/v1/auth", 30 * 24 * 60 * 60},
			}
			if len(cookies) != len(want) {
				t.Errorf("cookies = %v, want exactly %d", rec.Header().Values("Set-Cookie"), len(want))
			}
			for _, w := range want {
				c, ok := cookies[w.name]
				if !ok {
					t.Errorf("cookie %s not set", w.name)
					continue
				}
				if c.Value != w.value || c.Path != w.path || c.MaxAge != w.maxAge {
					t.Errorf("%s = value %q, path %q, max-age %d; want %q, %q, %d", w.name, c.Value, c.Path, c.MaxAge, w.value, w.path, w.maxAge)
				}
				if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Secure != secure {
					t.Errorf("%s: HttpOnly=%v SameSite=%v Secure=%v, want true, Lax, %v", w.name, c.HttpOnly, c.SameSite, c.Secure, secure)
				}
			}
		})
	}
}

func TestLoginUserRefused(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"wrong email or password", domain.ErrInvalidCredentials, http.StatusUnauthorized, CodeInvalidCredentials},
		{"unverified email address", domain.ErrEmailNotVerified, http.StatusForbidden, CodeEmailNotVerified},
		{"unexpected error", errors.New("database down"), http.StatusInternalServerError, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := login(t, &fakeService{err: tt.err}, CookieConfig{})
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := decode(t, rec); tt.wantCode != "" && (got["code"] != tt.wantCode || got["message"] == "") {
				t.Errorf("body = %v, want code %q and a message", got, tt.wantCode)
			}
			if c := rec.Header().Values("Set-Cookie"); len(c) != 0 {
				t.Errorf("a refused login set cookies: %v", c)
			}
		})
	}
}

func TestLoginUserLocked(t *testing.T) {
	tests := []struct {
		name           string
		retryAfter     time.Duration
		wantRetryAfter string
		wantMinutes    string
	}{
		{"just locked", 15 * time.Minute, "900", "Try again in 15 minutes."},
		{"part of a minute rounds up", 14*time.Minute + 1*time.Second, "841", "Try again in 15 minutes."},
		{"under a minute left", 1500 * time.Millisecond, "2", "Try again in 1 minute."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fmt.Errorf("login: %w", &domain.LockedError{Until: time.Now().Add(tt.retryAfter), RetryAfter: tt.retryAfter})
			rec := login(t, &fakeService{err: err}, CookieConfig{})
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("status = %d, want 429", rec.Code)
			}
			if got := rec.Header().Get("Retry-After"); got != tt.wantRetryAfter {
				t.Errorf("Retry-After = %q, want %q", got, tt.wantRetryAfter)
			}
			got := decode(t, rec)
			if got["code"] != CodeAccountLocked {
				t.Errorf("code = %v, want %q", got["code"], CodeAccountLocked)
			}
			if msg := got["message"]; !strings.Contains(msg, tt.wantMinutes) {
				t.Errorf("message = %q, want it to say %q", msg, tt.wantMinutes)
			}
			if c := rec.Header().Values("Set-Cookie"); len(c) != 0 {
				t.Errorf("a refused login set cookies: %v", c)
			}
		})
	}
}

func TestGetCurrentUser(t *testing.T) {
	me := func(t *testing.T, svc *fakeService, ctxUser string) *httptest.ResponseRecorder {
		t.Helper()
		ctx := t.Context()
		if ctxUser != "" {
			ctx = httpx.WithUserID(ctx, ctxUser) // what the auth middleware does
		}
		return serve(NewHandler(svc, CookieConfig{}), httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/auth/me", nil))
	}

	t.Run("logged in", func(t *testing.T) {
		svc := &fakeService{user: domain.User{ID: userID, Email: "ada@example.org", FullName: "Ada Lovelace"}}
		rec := me(t, svc, userID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if svc.userID != userID {
			t.Errorf("service asked for %q, want %q", svc.userID, userID)
		}
		if got := decode(t, rec); got["id"] != userID || got["email"] != "ada@example.org" || got["fullName"] != "Ada Lovelace" {
			t.Errorf("body = %v", got)
		}
	})

	t.Run("account no longer exists", func(t *testing.T) {
		rec := me(t, &fakeService{err: domain.ErrNotFound}, userID)
		if rec.Code != http.StatusUnauthorized || decode(t, rec)["code"] != httpx.CodeUnauthenticated {
			t.Errorf("got %d %s, want 401 unauthenticated", rec.Code, rec.Body)
		}
	})

	t.Run("no user in the context", func(t *testing.T) {
		rec := me(t, &fakeService{}, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})
}

// postWithCookie posts to path with the refresh cookie set to token, or no cookie when
// token is empty.
func postWithCookie(t *testing.T, svc *fakeService, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1"+path, nil)
	if token != "" {
		req.Header.Set("Cookie", httpx.RefreshCookie+"="+token)
	}
	return serve(NewHandler(svc, CookieConfig{Secure: true}), req)
}

// wantCookie is a Set-Cookie the test expects.
type wantCookie struct {
	name, value, path string
	maxAge            int // -1 for a cleared cookie (Max-Age=0)
}

func checkCookies(t *testing.T, rec *httptest.ResponseRecorder, want []wantCookie) {
	t.Helper()
	cookies := map[string]*http.Cookie{}
	for _, c := range rec.Result().Cookies() {
		cookies[c.Name] = c
	}
	if len(cookies) != len(want) {
		t.Errorf("cookies = %v, want exactly %d", rec.Header().Values("Set-Cookie"), len(want))
	}
	for _, w := range want {
		c, ok := cookies[w.name]
		if !ok {
			t.Errorf("cookie %s not set", w.name)
			continue
		}
		if c.Value != w.value || c.Path != w.path || c.MaxAge != w.maxAge {
			t.Errorf("%s = value %q, path %q, max-age %d; want %q, %q, %d", w.name, c.Value, c.Path, c.MaxAge, w.value, w.path, w.maxAge)
		}
		if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || !c.Secure {
			t.Errorf("%s: HttpOnly=%v SameSite=%v Secure=%v, want true, Lax, true", w.name, c.HttpOnly, c.SameSite, c.Secure)
		}
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

var clearedCookies = []wantCookie{
	{httpx.AccessCookie, "", "/", -1},
	{httpx.RefreshCookie, "", "/api/v1/auth", -1},
}

func TestRefreshSessionSetsCookies(t *testing.T) {
	issued := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		refreshLeft   time.Duration
		wantRefreshMA int
	}{
		{"fresh session", 29 * 24 * time.Hour, 29 * 24 * 60 * 60},
		{"less than a second left", 300 * time.Millisecond, 1}, // never Max-Age 0
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{refresh: usecase.RefreshResult{
				IssuedAt:         issued,
				AccessToken:      "new-access",
				AccessExpiresAt:  issued.Add(15 * time.Minute),
				RefreshToken:     "new-refresh",
				RefreshExpiresAt: issued.Add(tt.refreshLeft),
			}}
			rec := postWithCookie(t, svc, "/auth/refresh", "old-refresh")

			if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
				t.Fatalf("status = %d, body %q; want 204 and no body", rec.Code, rec.Body)
			}
			if len(svc.sent) != 1 || svc.sent[0] != "old-refresh" {
				t.Errorf("service got %v, want the cookie's token", svc.sent)
			}
			checkCookies(t, rec, []wantCookie{
				{httpx.AccessCookie, "new-access", "/", 15 * 60},
				{httpx.RefreshCookie, "new-refresh", "/api/v1/auth", tt.wantRefreshMA},
			})
		})
	}
}

func TestRefreshSessionRefused(t *testing.T) {
	tests := []struct {
		name   string
		cookie string
	}{
		{"no cookie", ""},
		{"invalid session", "old-refresh"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{err: domain.ErrSessionInvalid}
			rec := postWithCookie(t, svc, "/auth/refresh", tt.cookie)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if got := decode(t, rec); got["code"] != CodeSessionInvalid || got["message"] == "" {
				t.Errorf("body = %v, want code %q and a message", got, CodeSessionInvalid)
			}
			if len(svc.sent) != 1 || svc.sent[0] != tt.cookie {
				t.Errorf("service got %v, want [%q]", svc.sent, tt.cookie)
			}
			checkCookies(t, rec, clearedCookies)
		})
	}
}

func TestRefreshSessionFails(t *testing.T) {
	rec := postWithCookie(t, &fakeService{err: errors.New("database down")}, "/auth/refresh", "old-refresh")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if c := rec.Header().Values("Set-Cookie"); len(c) != 0 {
		t.Errorf("a failed refresh changed cookies: %v", c)
	}
}

func TestLogoutUser(t *testing.T) {
	for _, cookie := range []string{"refresh", ""} {
		t.Run(map[string]string{"refresh": "with cookie", "": "without cookie"}[cookie], func(t *testing.T) {
			svc := &fakeService{}
			rec := postWithCookie(t, svc, "/auth/logout", cookie)

			if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
				t.Fatalf("status = %d, body %q; want 204 and no body", rec.Code, rec.Body)
			}
			if len(svc.sent) != 1 || svc.sent[0] != cookie {
				t.Errorf("service got %v, want [%q]", svc.sent, cookie)
			}
			checkCookies(t, rec, clearedCookies)
		})
	}
}

func TestLogoutUserFails(t *testing.T) {
	rec := postWithCookie(t, &fakeService{err: errors.New("database down")}, "/auth/logout", "refresh")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if c := rec.Header().Values("Set-Cookie"); len(c) != 0 {
		t.Errorf("a failed logout cleared cookies: %v", c)
	}
}
