package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/authz"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpserver"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// The profile routes go through authz (FR-3.2, SRS 2.3): both platform roles may view
// profiles and edit their own, and any other role gets exactly the FR-3.3 refusal.
func TestProfileRoutesThroughRouter(t *testing.T) {
	router := testRouter()
	routes := []struct {
		method, path, body string
	}{
		{http.MethodGet, "/api/v1/profiles/me", ""},
		{http.MethodPut, "/api/v1/profiles/me", `{"fullName":"Ada Lovelace","country":"gb"}`},
		{http.MethodGet, "/api/v1/profiles/" + okUserID, ""},
	}
	send := func(t *testing.T, method, path, body, role string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if role != "" {
			token, _, err := accesstoken.New([]byte(testSecret), accessTTL).Sign(okUserID, role, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Cookie", httpx.AccessCookie+"="+token)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	for _, r := range routes {
		name := r.method + " " + r.path
		for _, role := range []authz.Role{authz.RoleResearcher, authz.RoleAdmin} {
			t.Run(name+"/"+string(role), func(t *testing.T) {
				if rec := send(t, r.method, r.path, r.body, string(role)); rec.Code != http.StatusOK {
					t.Errorf("status = %d, want 200; body %s", rec.Code, rec.Body)
				}
			})
		}
		// A signed token with a role outside SRS 2.2 reaches the guard, which must refuse it.
		t.Run(name+"/unknown role", func(t *testing.T) {
			rec := send(t, r.method, r.path, r.body, "visitor")
			if rec.Code != http.StatusForbidden || rec.Body.String() != forbiddenBody {
				t.Errorf("got %d %q, want 403 with exactly %q", rec.Code, rec.Body, forbiddenBody)
			}
		})
		t.Run(name+"/not logged in", func(t *testing.T) {
			checkUnauthenticated(t, "no token", send(t, r.method, r.path, r.body, ""))
		})
	}
}

// Every profile route has a platform guard (FR-3.2), so a new one cannot be added
// without deciding which SRS 2.3 row it belongs to.
func TestProfileRoutesAreGuarded(t *testing.T) {
	want := map[string]authz.Action{
		"GET /api/v1/profiles/me":       authz.ViewContent,
		"PUT /api/v1/profiles/me":       authz.ActAsMember,
		"GET /api/v1/profiles/{userId}": authz.ViewContent,
	}
	for route, action := range want {
		if got := platformGuards[route]; got != action {
			t.Errorf("platformGuards[%q] = %q, want %q", route, got, action)
		}
	}
	for _, r := range httpserver.Routes() {
		_, path, _ := strings.Cut(r, " ")
		if !strings.HasPrefix(path, httpserver.BaseURL+"/profiles") {
			continue
		}
		if _, ok := platformGuards[r]; !ok {
			t.Errorf("profile route %q has no entry in platformGuards", r)
		}
	}
}
