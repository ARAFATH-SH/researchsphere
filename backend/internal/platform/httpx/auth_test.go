package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/authz"
)

func TestRequireAuth(t *testing.T) {
	start := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	signer := accesstoken.New([]byte("test-secret-that-is-at-least-32-bytes-long"), 15*time.Minute)
	other := accesstoken.New([]byte("another-secret-of-at-least-32-bytes!!"), 15*time.Minute)
	valid, _, err := signer.Sign("user-1", "admin", true, start)
	if err != nil {
		t.Fatal(err)
	}
	foreign, _, err := other.Sign("user-1", "admin", true, start)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		secured   bool
		cookie    string // the Cookie request header
		at        time.Time
		wantCode  int
		wantUser  string
		wantRole  authz.Role
		wantError string
	}{
		{name: "valid token", secured: true, cookie: AccessCookie + "=" + valid, at: start.Add(time.Minute), wantCode: 200, wantUser: "user-1", wantRole: authz.RoleAdmin},
		{name: "missing cookie", secured: true, at: start, wantCode: 401, wantError: CodeUnauthenticated},
		{name: "expired token", secured: true, cookie: AccessCookie + "=" + valid, at: start.Add(15 * time.Minute), wantCode: 401, wantError: CodeUnauthenticated},
		{name: "token from another secret", secured: true, cookie: AccessCookie + "=" + foreign, at: start, wantCode: 401, wantError: CodeUnauthenticated},
		{name: "garbage token", secured: true, cookie: AccessCookie + "=garbage", at: start, wantCode: 401, wantError: CodeUnauthenticated},
		{name: "token in another cookie", secured: true, cookie: RefreshCookie + "=" + valid, at: start, wantCode: 401, wantError: CodeUnauthenticated},
		{name: "public operation without cookie", secured: false, at: start, wantCode: 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotUser string
			var gotRole authz.Role
			var reached bool
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reached = true
				gotUser, _ = UserID(r.Context())
				gotRole, _ = Role(r.Context())
			})
			h := RequireAuth(signer, func() time.Time { return tt.at })(next)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/me", nil)
			if tt.secured {
				// What the generated wrapper does for an operation with security.
				req = req.WithContext(context.WithValue(req.Context(), gen.CookieAuthScopes, []string{}))
			}
			if tt.cookie != "" {
				req.Header.Set("Cookie", tt.cookie)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if tt.wantCode != 200 {
				if reached {
					t.Error("the handler ran for a refused request")
				}
				var body gen.Error
				if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body.Code != tt.wantError || body.Message == "" {
					t.Errorf("body = %+v, want code %q and a message", body, tt.wantError)
				}
				return
			}
			if !reached {
				t.Fatal("the handler did not run")
			}
			if gotUser != tt.wantUser {
				t.Errorf("user ID in context = %q, want %q", gotUser, tt.wantUser)
			}
			if gotRole != tt.wantRole {
				t.Errorf("role in context = %q, want %q", gotRole, tt.wantRole)
			}
		})
	}
}

// FR-17.3: RequireAuth puts the token's email verification state in the context, and a
// context without it counts as unverified.
func TestRequireAuthEmailVerified(t *testing.T) {
	signer := accesstoken.New([]byte("test-secret-that-is-at-least-32-bytes-long"), 15*time.Minute)
	if EmailVerified(context.Background()) {
		t.Error("EmailVerified of an empty context = true, want false")
	}
	for _, verified := range []bool{true, false} {
		token, _, err := signer.Sign("user-1", "researcher", verified, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		var got *bool
		h := RequireAuth(signer, time.Now)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			v := EmailVerified(r.Context())
			got = &v
		}))
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/me", nil)
		req = req.WithContext(context.WithValue(req.Context(), gen.CookieAuthScopes, []string{}))
		req.Header.Set("Cookie", AccessCookie+"="+token)
		h.ServeHTTP(httptest.NewRecorder(), req)
		if got == nil || *got != verified {
			t.Errorf("signed verified=%v: EmailVerified = %v", verified, got)
		}
	}
}
