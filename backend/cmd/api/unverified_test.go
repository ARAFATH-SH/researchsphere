package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpserver"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// unverifiedToken hand-signs an access token for a session whose email address is not
// verified (FR-17.3). Login refuses such accounts today, so only a test can make one.
func unverifiedToken(t *testing.T, role string) string {
	t.Helper()
	token, _, err := accesstoken.New([]byte(testSecret), accessTTL).Sign(okUserID, role, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func sendWithToken(t *testing.T, router http.Handler, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", httpx.AccessCookie+"="+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// isEmailNotVerified reports whether rec is exactly the 403 of the FR-17.3 gate.
func isEmailNotVerified(t *testing.T, rec *httptest.ResponseRecorder) bool {
	t.Helper()
	if rec.Code != http.StatusForbidden {
		return false
	}
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		return false
	}
	if body.Code != httpx.CodeEmailNotVerified {
		return false
	}
	if body.Message != "Verify your email address to do this." {
		t.Errorf("message = %q", body.Message)
	}
	return true
}

// TestUnverifiedAllowedListsRealRoutes keeps the FR-17.3 table honest: every route it lists
// is registered and needs a session, and no administrator route is in it.
func TestUnverifiedAllowedListsRealRoutes(t *testing.T) {
	registered := map[string]bool{}
	for _, r := range httpserver.Routes() {
		registered[r] = true
	}
	for r := range unverifiedAllowed {
		if !registered[r] {
			t.Errorf("unverifiedAllowed lists %q, which is not a registered route", r)
		}
		if publicRoutes[r] {
			t.Errorf("unverifiedAllowed lists the public route %q; public routes need no session", r)
		}
		if strings.Contains(r, " /api/v1/admin/") {
			t.Errorf("unverifiedAllowed lists the administrator route %q", r)
		}
	}
}

// TestUnverifiedSessionGate sends an unverified token to every route that needs a session
// (FR-17.3). A route in unverifiedAllowed must get past the gate; any other must answer
// exactly 403 email_not_verified, whatever its path parameters, before its handler runs.
func TestUnverifiedSessionGate(t *testing.T) {
	router := testRouter()
	token := unverifiedToken(t, "researcher")
	for _, route := range httpserver.Routes() {
		method, pattern, _ := strings.Cut(route, " ")
		if publicRoutes[route] || strings.HasPrefix(pattern, internalPrefix) {
			continue // internal routes take no session (routes_test.go)
		}
		path := pathParam.ReplaceAllString(pattern, okUserID)
		t.Run(route, func(t *testing.T) {
			rec := sendWithToken(t, router, method, path, token)
			refused := isEmailNotVerified(t, rec)
			if unverifiedAllowed[route] && refused {
				t.Errorf("allowed route refused with email_not_verified")
			}
			if !unverifiedAllowed[route] && !refused {
				t.Errorf("status = %d, body %s; want 403 email_not_verified", rec.Code, rec.Body)
			}
		})
	}
}

// TestUnverifiedSessionClasses checks a sample of each class of FR-17.3 against the
// expected answer written out here, so a wrong edit of unverifiedAllowed fails a test that
// does not read the table.
func TestUnverifiedSessionClasses(t *testing.T) {
	router := testRouter()
	researcher := unverifiedToken(t, "researcher")
	admin := unverifiedToken(t, "admin")
	verified, _, err := accesstoken.New([]byte(testSecret), accessTTL).Sign(okUserID, "researcher", true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	id := okUserID
	tests := []struct {
		class, method, path string
		allowed             bool
	}{
		// Account settings.
		{"account", "GET", "/api/v1/auth/me", true},
		{"account", "POST", "/api/v1/auth/password", true},
		{"account", "POST", "/api/v1/account/delete", true},
		// Own profile, photo and cover included.
		{"own profile", "GET", "/api/v1/profiles/me", true},
		{"own profile", "PUT", "/api/v1/profiles/me", true},
		{"own profile", "POST", "/api/v1/profiles/me/education", true},
		{"own profile", "PUT", "/api/v1/profiles/me/photo", true},
		{"own profile", "DELETE", "/api/v1/profiles/me/cover", true},
		{"own profile", "GET", "/api/v1/tags/skills", true},
		{"own profile", "POST", "/api/v1/profiles/me/publications", true},
		// Own notifications and warnings.
		{"notifications", "GET", "/api/v1/notifications", true},
		{"notifications", "POST", "/api/v1/notifications/read-all", true},
		{"warnings", "GET", "/api/v1/me/warnings/pending", true},
		{"warnings", "POST", "/api/v1/me/warnings/" + id + "/acknowledge", true},
		// Outside lookups.
		{"lookups", "GET", "/api/v1/publications/lookup?doi=10.1000/x", false},
		{"lookups", "POST", "/api/v1/profiles/me/publications/from-doi", false},
		{"lookups", "GET", "/api/v1/profiles/me/publications/orcid-works", false},
		{"lookups", "POST", "/api/v1/profiles/me/publications/from-orcid", false},
		// Other people, search and media.
		{"others", "GET", "/api/v1/profiles/" + id, false},
		{"others", "GET", "/api/v1/profiles/" + id + "/publications", false},
		{"others", "GET", "/api/v1/search/people?q=ada", false},
		{"others", "GET", "/api/v1/media/" + id, false},
		// Connections and blocks.
		{"connections", "POST", "/api/v1/connections/requests", false},
		{"connections", "GET", "/api/v1/connections", false},
		{"blocks", "POST", "/api/v1/blocks", false},
		// Messaging, uploads, reports and real time.
		{"messaging", "POST", "/api/v1/messages/direct", false},
		{"messaging", "GET", "/api/v1/conversations", false},
		{"uploads", "POST", "/api/v1/conversations/" + id + "/attachments", false},
		{"reports", "POST", "/api/v1/conversations/" + id + "/reports", false},
		{"real time", "GET", "/api/v1/ws", false},
		{"real time", "GET", "/api/v1/presence", false},
	}
	for _, tt := range tests {
		t.Run(tt.class+"/"+tt.method+" "+tt.path, func(t *testing.T) {
			rec := sendWithToken(t, router, tt.method, tt.path, researcher)
			if refused := isEmailNotVerified(t, rec); refused == tt.allowed {
				t.Errorf("unverified: status = %d, body %s; allowed = %v", rec.Code, rec.Body, tt.allowed)
			}
			// A verified session never meets the gate.
			if rec := sendWithToken(t, router, tt.method, tt.path, verified); isEmailNotVerified(t, rec) {
				t.Errorf("verified: refused with email_not_verified")
			}
		})
	}

	// The gate runs before the role: an unverified administrator gets email_not_verified on
	// the administrator routes, not forbidden, and not the page.
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/v1/admin/users"},
		{"GET", "/api/v1/admin/message-reports"},
		{"POST", "/api/v1/admin/users/" + id + "/suspension"},
	} {
		t.Run("admin/"+route.method+" "+route.path, func(t *testing.T) {
			if rec := sendWithToken(t, router, route.method, route.path, admin); !isEmailNotVerified(t, rec) {
				t.Errorf("unverified admin: status = %d, body %s; want 403 email_not_verified", rec.Code, rec.Body)
			}
		})
	}
}
