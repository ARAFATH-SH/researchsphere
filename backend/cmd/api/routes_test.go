package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpserver"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// publicRoutes are the only operations reachable without a session: FR-3.1's list plus
// the health check (docs/decisions.md). The contract marks them `security: []`; changing
// this set means changing docs/openapi.yaml and this list together.
var publicRoutes = map[string]bool{
	"GET /api/v1/health":                       true,
	"POST /api/v1/auth/register":               true,
	"POST /api/v1/auth/verify-email":           true,
	"POST /api/v1/auth/resend-verification":    true,
	"POST /api/v1/auth/login":                  true,
	"POST /api/v1/auth/refresh":                true,
	"POST /api/v1/auth/logout":                 true,
	"POST /api/v1/auth/password-reset/request": true,
	"POST /api/v1/auth/password-reset/confirm": true,
}

var pathParam = regexp.MustCompile(`\{[^}]+\}`)

// TestEveryRouteRequiresLoginUnlessPublic walks every route the router registers (FR-3.1).
// A route not in publicRoutes must refuse a request with no, an expired, a tampered or a
// garbage access token, even when its path parameters are malformed, so a new operation
// made public by mistake, or one that answers before checking the token, fails here.
func TestEveryRouteRequiresLoginUnlessPublic(t *testing.T) {
	router := testRouter()
	routes := httpserver.Routes()

	registered := map[string]bool{}
	for _, r := range routes {
		registered[r] = true
	}
	for r := range publicRoutes {
		if !registered[r] {
			t.Errorf("public route %q is not registered; update publicRoutes", r)
		}
	}

	signer := accesstoken.New([]byte(testSecret), accessTTL)
	valid, _, err := signer.Sign(okUserID, "researcher", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	expired, _, err := signer.Sign(okUserID, "researcher", time.Now().Add(-accessTTL-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	tampered := tamper(valid)

	send := func(t *testing.T, method, path, token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Cookie", httpx.AccessCookie+"="+token)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	for _, route := range routes {
		method, pattern, ok := strings.Cut(route, " ")
		if !ok {
			t.Fatalf("route %q has no method", route)
		}
		paths := map[string]string{"valid parameters": pathParam.ReplaceAllString(pattern, okUserID)}
		if pathParam.MatchString(pattern) {
			paths["malformed parameters"] = pathParam.ReplaceAllString(pattern, "not-a-uuid")
		}
		for label, path := range paths {
			t.Run(route+"/"+label, func(t *testing.T) {
				if publicRoutes[route] {
					if rec := send(t, method, path, ""); rec.Code == http.StatusUnauthorized {
						t.Errorf("public route without a token: 401 %s", rec.Body)
					}
					return
				}
				for name, token := range map[string]string{
					"no token":       "",
					"expired token":  expired,
					"tampered token": tampered,
					"garbage token":  "garbage",
				} {
					checkUnauthenticated(t, name, send(t, method, path, token))
				}
				// A 401 here means the auth check never ran and the handler refused on its
				// own: the contract most likely marks the operation `security: []`.
				if label == "valid parameters" {
					if rec := send(t, method, path, valid); rec.Code == http.StatusUnauthorized {
						t.Errorf("valid token: 401 %s; is the route public in docs/openapi.yaml but not in publicRoutes?", rec.Body)
					}
				}
			})
		}
	}
}

// checkUnauthenticated requires a 401 whose body is exactly the error shape with the code
// unauthenticated, and nothing else.
func checkUnauthenticated(t *testing.T, name string, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("%s: status = %d, want 401; body %s", name, rec.Code, rec.Body)
		return
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("%s: Content-Type = %q, want application/json", name, ct)
	}
	if sc := rec.Header().Values("Set-Cookie"); len(sc) != 0 {
		t.Errorf("%s: Set-Cookie = %v, want none", name, sc)
	}
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		t.Fatalf("%s: body %q is not the error shape: %v", name, rec.Body, err)
	}
	if dec.More() {
		t.Errorf("%s: body %q has content after the error", name, rec.Body)
	}
	if body.Code != httpx.CodeUnauthenticated || body.Message == "" {
		t.Errorf("%s: body = %+v, want code %q and a message", name, body, httpx.CodeUnauthenticated)
	}
}

// tamper changes one character in the middle of a JWT's signature.
func tamper(token string) string {
	dot := strings.LastIndex(token, ".")
	i := dot + (len(token)-dot)/2
	c := byte('A')
	if token[i] == 'A' {
		c = 'B'
	}
	return token[:i] + string(c) + token[i+1:]
}
