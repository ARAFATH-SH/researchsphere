package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/authz"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpserver"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// forbiddenBody is the one answer to a refused action (FR-3.3).
const forbiddenBody = `{"code":"forbidden","message":"You do not have permission to do this."}` + "\n"

// getAs sends GET path through the wired router with an access token for role, or no
// cookie when role is empty.
func getAs(t *testing.T, router http.Handler, path string, role authz.Role) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	if role != "" {
		token, _, err := accesstoken.New([]byte(testSecret), accessTTL).Sign(okUserID, string(role), true, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Cookie", httpx.AccessCookie+"="+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// GET /admin/users is for administrators only (FR-3.2, SRS 2.3 "Suspend a user account").
func TestListUsersThroughRouter(t *testing.T) {
	router := testRouter()

	t.Run("admin", func(t *testing.T) {
		rec := getAs(t, router, "/api/v1/admin/users", authz.RoleAdmin)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
		}
		var page gen.UserPage
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || page.Items[0].Email != "ada@example.org" {
			t.Errorf("body = %s", rec.Body)
		}
	})

	t.Run("admin, page size 500 is capped to 50", func(t *testing.T) {
		rec := getAs(t, router, "/api/v1/admin/users?pageSize=500", authz.RoleAdmin)
		var page gen.UserPage
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusOK || page.PageSize != 50 {
			t.Errorf("got %d, pageSize %d; want 200 and 50", rec.Code, page.PageSize)
		}
	})

	t.Run("researcher gets exactly the FR-3.3 body", func(t *testing.T) {
		rec := getAs(t, router, "/api/v1/admin/users", authz.RoleResearcher)
		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", rec.Code)
		}
		if got := rec.Body.String(); got != forbiddenBody {
			t.Errorf("body = %q, want exactly %q", got, forbiddenBody)
		}
		if strings.Contains(rec.Body.String(), "ada@example.org") {
			t.Error("the refusal leaks user data")
		}
	})

	t.Run("not logged in", func(t *testing.T) {
		checkUnauthenticated(t, "no token", getAs(t, router, "/api/v1/admin/users", ""))
	})
}

// Every administration route needs a platform action, and every guard names a route
// that exists, so a renamed route cannot silently lose its guard.
func TestAdminRoutesAreGuarded(t *testing.T) {
	registered := map[string]bool{}
	for _, r := range httpserver.Routes() {
		registered[r] = true
		_, path, _ := strings.Cut(r, " ")
		if strings.HasPrefix(path, httpserver.BaseURL+"/admin/") {
			if _, ok := platformGuards[r]; !ok {
				t.Errorf("admin route %q has no entry in platformGuards", r)
			}
		}
	}
	for r := range platformGuards {
		if !registered[r] {
			t.Errorf("platformGuards names %q, which is not a registered route", r)
		}
	}
}

// A request that reached the guard without a matched route pattern is refused, not let
// through.
func TestGuardPlatformRefusesUnknownRoute(t *testing.T) {
	var reached bool
	h := guardPlatform(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	req := httptest.NewRequestWithContext(httpx.WithRole(t.Context(), authz.RoleAdmin), http.MethodGet, "/api/v1/admin/users", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if reached || rec.Code != http.StatusForbidden || rec.Body.String() != forbiddenBody {
		t.Errorf("reached = %v, got %d %q; want 403 with the FR-3.3 body", reached, rec.Code, rec.Body)
	}
}
