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
	password string                // new password given to ResetPassword
	change   usecase.ChangePasswordInput
	deleted  usecase.DeleteAccountInput
	users    []domain.User // returned by ListUsers
	page     int           // given to ListUsers
	pageSize int           // given to ListUsers
}

func (f *fakeService) DeleteAccount(_ context.Context, in usecase.DeleteAccountInput) error {
	f.deleted = in
	return f.err
}

func (f *fakeService) ChangePassword(_ context.Context, in usecase.ChangePasswordInput) error {
	f.change = in
	return f.err
}

func (f *fakeService) RequestPasswordReset(_ context.Context, email string) error {
	f.email = email
	return f.err
}

func (f *fakeService) ResetPassword(_ context.Context, token, newPassword string) error {
	f.token, f.password = token, newPassword
	return f.err
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

func (f *fakeService) ListUsers(_ context.Context, page, pageSize int) (domain.UserPage, error) {
	f.page, f.pageSize = page, pageSize
	if f.err != nil {
		return domain.UserPage{}, f.err
	}
	return domain.UserPage{Users: f.users, Page: page, PageSize: min(pageSize, usecase.MaxPageSize), Total: len(f.users)}, nil
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

func TestRequestPasswordReset(t *testing.T) {
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
			rec := post(t, svc, "/auth/password-reset/request", `{"email":"ada@example.org"}`)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if got := decode(t, rec); tt.wantCode != "" && got["code"] != tt.wantCode {
				t.Errorf("body = %v, want code %q", got, tt.wantCode)
			}
			if tt.wantStatus == http.StatusAccepted && rec.Body.Len() != 0 {
				t.Errorf("202 with body %q", rec.Body)
			}
			if svc.email != "ada@example.org" {
				t.Errorf("usecase got email %q", svc.email)
			}
		})
	}
}

func TestConfirmPasswordReset(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"reset", nil, http.StatusNoContent, ""},
		{"unknown token", domain.ErrTokenInvalid, http.StatusBadRequest, CodeTokenInvalid},
		{"used token", domain.ErrTokenUsed, http.StatusBadRequest, CodeTokenUsed},
		{"expired token", domain.ErrTokenExpired, http.StatusBadRequest, CodeTokenExpired},
		{"short password", domain.ErrPasswordTooShort, http.StatusBadRequest, CodePasswordTooShort},
		{"long password", domain.ErrPasswordTooLong, http.StatusBadRequest, CodePasswordTooLong},
		{"suspended account", domain.ErrAccountSuspended, http.StatusForbidden, CodeAccountSuspended},
		{"unexpected error", errors.New("database down"), http.StatusInternalServerError, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{err: tt.err}
			rec := post(t, svc, "/auth/password-reset/confirm", `{"token":"abc","newPassword":"password1"}`)
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
			if svc.token != "abc" || svc.password != "password1" {
				t.Errorf("usecase got token %q and password %q", svc.token, svc.password)
			}
		})
	}
}

func TestMalformedBody(t *testing.T) {
	for _, path := range []string{"/auth/register", "/auth/verify-email", "/auth/resend-verification", "/auth/password-reset/request", "/auth/password-reset/confirm"} {
		t.Run(path, func(t *testing.T) {
			if rec := post(t, &fakeService{}, path, `{not json`); rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
		})
	}
}

// TestUnknownBodyField checks that a body with a field the contract does not define is
// refused, not silently ignored.
func TestUnknownBodyField(t *testing.T) {
	tests := []struct{ path, body string }{
		{"/auth/register", `{"fullName":"Ada","email":"ada@example.org","password":"password1","role":"admin"}`},
		{"/auth/login", `{"email":"ada@example.org","password":"password1","remember":true}`},
		{"/auth/password-reset/request", `{"email":"ada@example.org","extra":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if rec := post(t, &fakeService{}, tt.path, tt.body); rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
		})
	}
}

const userID = "6f1c2b9e-1d2a-4c3b-9a8e-0f1e2d3c4b5a"

func loginResult() usecase.LoginResult {
	issued := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	return usecase.LoginResult{
		User:             domain.User{ID: userID, Email: "ada@example.org", FullName: "Ada Lovelace", Role: domain.RoleResearcher},
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
			if got := decode(t, rec); got["id"] != userID || got["email"] != "ada@example.org" || got["fullName"] != "Ada Lovelace" || got["role"] != "researcher" || len(got) != 4 {
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
		{"suspended account", domain.ErrAccountSuspended, http.StatusForbidden, CodeAccountSuspended},
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
		svc := &fakeService{user: domain.User{ID: userID, Email: "ada@example.org", FullName: "Ada Lovelace", Role: domain.RoleAdmin}}
		rec := me(t, svc, userID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if svc.userID != userID {
			t.Errorf("service asked for %q, want %q", svc.userID, userID)
		}
		if got := decode(t, rec); got["id"] != userID || got["email"] != "ada@example.org" || got["fullName"] != "Ada Lovelace" || got["role"] != "admin" {
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

// changePassword posts a password change as the logged-in user ctxUser, with the refresh
// cookie set to refresh unless it is empty.
func changePassword(t *testing.T, svc *fakeService, ctxUser, refresh string) *httptest.ResponseRecorder {
	t.Helper()
	ctx := t.Context()
	if ctxUser != "" {
		ctx = httpx.WithUserID(ctx, ctxUser) // what the auth middleware does
	}
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/auth/password",
		strings.NewReader(`{"currentPassword":"password1","newPassword":"password2"}`))
	req.Header.Set("Content-Type", "application/json")
	if refresh != "" {
		req.Header.Set("Cookie", httpx.RefreshCookie+"="+refresh)
	}
	return serve(NewHandler(svc, CookieConfig{}), req)
}

func TestChangePassword(t *testing.T) {
	svc := &fakeService{}
	rec := changePassword(t, svc, userID, "refresh-token")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d %s, want 204", rec.Code, rec.Body)
	}
	want := usecase.ChangePasswordInput{UserID: userID, RefreshToken: "refresh-token", CurrentPassword: "password1", NewPassword: "password2"}
	if svc.change != want {
		t.Errorf("service got %+v, want %+v", svc.change, want)
	}
	if c := rec.Header().Values("Set-Cookie"); len(c) != 0 {
		t.Errorf("a password change set cookies: %v: the current session must stay as it is", c)
	}

	svc = &fakeService{}
	if rec := changePassword(t, svc, userID, ""); rec.Code != http.StatusNoContent || svc.change.RefreshToken != "" {
		t.Errorf("without a refresh cookie: %d, token %q, want 204 and an empty token", rec.Code, svc.change.RefreshToken)
	}
}

func TestChangePasswordRefused(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"wrong current password", domain.ErrWrongCurrentPassword, http.StatusBadRequest, CodeWrongCurrentPassword},
		{"too short", domain.ErrPasswordTooShort, http.StatusBadRequest, CodePasswordTooShort},
		{"too long", domain.ErrPasswordTooLong, http.StatusBadRequest, CodePasswordTooLong},
		{"same as current", domain.ErrSamePassword, http.StatusBadRequest, CodeSamePassword},
		{"account no longer exists", fmt.Errorf("find user: %w", domain.ErrNotFound), http.StatusUnauthorized, httpx.CodeUnauthenticated},
		{"locked", &domain.LockedError{RetryAfter: 90 * time.Second}, http.StatusTooManyRequests, CodeAccountLocked},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := changePassword(t, &fakeService{err: tt.err}, userID, "refresh-token")
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			got := decode(t, rec)
			if got["code"] != tt.wantCode || got["message"] == "" {
				t.Errorf("body = %v, want code %q and a message", got, tt.wantCode)
			}
			if tt.wantStatus == http.StatusTooManyRequests {
				if ra := rec.Header().Get("Retry-After"); ra != "90" {
					t.Errorf("Retry-After = %q, want 90", ra)
				}
				if !strings.Contains(got["message"], "2 minutes") {
					t.Errorf("message = %q, want the minutes left", got["message"])
				}
			}
		})
	}
}

func TestChangePasswordNotLoggedIn(t *testing.T) {
	svc := &fakeService{}
	rec := changePassword(t, svc, "", "refresh-token")
	if rec.Code != http.StatusUnauthorized || decode(t, rec)["code"] != httpx.CodeUnauthenticated {
		t.Errorf("got %d %s, want 401 unauthenticated", rec.Code, rec.Body)
	}
	if svc.change != (usecase.ChangePasswordInput{}) {
		t.Error("the service was called without a logged-in user")
	}
}

func TestChangePasswordFails(t *testing.T) {
	rec := changePassword(t, &fakeService{err: errors.New("database down")}, userID, "")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func deleteAccount(t *testing.T, svc *fakeService, ctxUser string) *httptest.ResponseRecorder {
	t.Helper()
	ctx := t.Context()
	if ctxUser != "" {
		ctx = httpx.WithUserID(ctx, ctxUser) // what the auth middleware does
	}
	req := httptest.NewRequestWithContext(ctx, http.MethodDelete, "/api/v1/account", strings.NewReader(`{"password":"password1"}`))
	req.Header.Set("Content-Type", "application/json")
	return serve(NewHandler(svc, CookieConfig{}), req)
}

func TestDeleteAccount(t *testing.T) {
	svc := &fakeService{}
	rec := deleteAccount(t, svc, userID)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d %s, want 204", rec.Code, rec.Body)
	}
	if want := (usecase.DeleteAccountInput{UserID: userID, Password: "password1"}); svc.deleted != want {
		t.Errorf("service got %+v, want %+v", svc.deleted, want)
	}
	cleared := map[string]string{}
	for _, c := range rec.Result().Cookies() {
		if c.Value != "" || c.MaxAge >= 0 {
			t.Errorf("cookie %s = %q, max-age %d: want it cleared", c.Name, c.Value, c.MaxAge)
		}
		cleared[c.Name] = c.Path
	}
	if cleared[httpx.AccessCookie] != "/" || cleared[httpx.RefreshCookie] != "/api/v1/auth" {
		t.Errorf("cleared cookies = %v, want both session cookies on their paths", cleared)
	}
}

func TestDeleteAccountRefused(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"wrong password", domain.ErrWrongPassword, http.StatusBadRequest, CodeWrongPassword},
		{"account no longer exists", fmt.Errorf("find user: %w", domain.ErrNotFound), http.StatusUnauthorized, httpx.CodeUnauthenticated},
		{"locked", &domain.LockedError{RetryAfter: 90 * time.Second}, http.StatusTooManyRequests, CodeAccountLocked},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := deleteAccount(t, &fakeService{err: tt.err}, userID)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			got := decode(t, rec)
			if got["code"] != tt.wantCode || got["message"] == "" {
				t.Errorf("body = %v, want code %q and a message", got, tt.wantCode)
			}
			if c := rec.Header().Values("Set-Cookie"); len(c) != 0 {
				t.Errorf("a refused deletion set cookies: %v", c)
			}
			if tt.wantStatus == http.StatusTooManyRequests && rec.Header().Get("Retry-After") != "90" {
				t.Errorf("Retry-After = %q, want 90", rec.Header().Get("Retry-After"))
			}
		})
	}
}

func TestDeleteAccountOwnsSharedProject(t *testing.T) {
	const projectID = "7d0b5c8e-2f43-4a51-9a3e-0c6f1f3b2a10"
	err := fmt.Errorf("delete: %w", &domain.SharedProjectsError{Projects: []domain.ProjectRef{{ID: projectID, Title: "Graph survey"}}})
	rec := deleteAccount(t, &fakeService{err: err}, userID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d %s, want 409", rec.Code, rec.Body)
	}
	var body gen.OwnsSharedProjectError
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Code != CodeOwnsSharedProject || body.Message == "" {
		t.Errorf("body = %+v, want code %q and a message", body, CodeOwnsSharedProject)
	}
	if len(body.Projects) != 1 || body.Projects[0].Id.String() != projectID || body.Projects[0].Title != "Graph survey" {
		t.Errorf("projects = %+v, want the one blocking project", body.Projects)
	}
	if c := rec.Header().Values("Set-Cookie"); len(c) != 0 {
		t.Errorf("a refused deletion set cookies: %v", c)
	}
}

func TestDeleteAccountNotLoggedIn(t *testing.T) {
	svc := &fakeService{}
	rec := deleteAccount(t, svc, "")
	if rec.Code != http.StatusUnauthorized || decode(t, rec)["code"] != httpx.CodeUnauthenticated {
		t.Errorf("got %d %s, want 401 unauthenticated", rec.Code, rec.Body)
	}
	if svc.deleted != (usecase.DeleteAccountInput{}) {
		t.Error("the service was called without a logged-in user")
	}
}

func TestDeleteAccountFails(t *testing.T) {
	if rec := deleteAccount(t, &fakeService{err: errors.New("database down")}, userID); rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}
