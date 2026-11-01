package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/authz"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpserver"
	socialdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/social/domain"
)

const okRequestID = "7a2d3c4b-5e6f-4a1b-8c9d-0e1f2a3b4c5d"

// okSocial is a connection service for which every operation succeeds with one item.
type okSocial struct{}

var okPerson = socialdomain.Person{UserID: okUserID, FullName: "Ada"}

func (okSocial) Send(context.Context, string, string) (socialdomain.PendingRequest, error) {
	return socialdomain.PendingRequest{ID: okRequestID, Other: okPerson}, nil
}

func (okSocial) Accept(context.Context, string, string) (socialdomain.Connection, error) {
	return socialdomain.Connection{Other: okPerson}, nil
}

func (okSocial) Reject(context.Context, string, string) error { return nil }
func (okSocial) Cancel(context.Context, string, string) error { return nil }
func (okSocial) Remove(context.Context, string, string) error { return nil }

func (okSocial) ListConnections(_ context.Context, _ string, page int) (socialdomain.ConnectionPage, error) {
	return socialdomain.ConnectionPage{Page: page, PageSize: 20}, nil
}

func (okSocial) ListUserConnections(_ context.Context, _ string, page int) (socialdomain.ConnectionPage, error) {
	return socialdomain.ConnectionPage{Page: page, PageSize: 20}, nil
}

func (okSocial) ListReceived(_ context.Context, _ string, page int) (socialdomain.RequestPage, error) {
	return socialdomain.RequestPage{Page: page, PageSize: 20}, nil
}

func (okSocial) ListSent(_ context.Context, _ string, page int) (socialdomain.RequestPage, error) {
	return socialdomain.RequestPage{Page: page, PageSize: 20}, nil
}

// The connection routes go through authz (FR-3.2, SRS 2.3): both roles may use them, an
// unknown role gets 403 and no session gets 401.
func TestConnectionRoutesThroughRouter(t *testing.T) {
	router := testRouter()
	routes := []struct {
		method, path, body string
		status             int
	}{
		{http.MethodPost, "/api/v1/connections/requests", `{"userId":"` + okUserID + `"}`, http.StatusCreated},
		{http.MethodPost, "/api/v1/connections/requests/" + okRequestID + "/accept", "", http.StatusOK},
		{http.MethodPost, "/api/v1/connections/requests/" + okRequestID + "/reject", "", http.StatusNoContent},
		{http.MethodDelete, "/api/v1/connections/requests/" + okRequestID, "", http.StatusNoContent},
		{http.MethodDelete, "/api/v1/connections/" + okUserID, "", http.StatusNoContent},
		{http.MethodGet, "/api/v1/connections", "", http.StatusOK},
		{http.MethodGet, "/api/v1/connections/requests/received", "", http.StatusOK},
		{http.MethodGet, "/api/v1/connections/requests/sent", "", http.StatusOK},
		{http.MethodGet, "/api/v1/profiles/" + okUserID + "/connections", "", http.StatusOK},
	}
	for _, r := range routes {
		send := func(t *testing.T, role string) *httptest.ResponseRecorder {
			t.Helper()
			var body io.Reader
			ct := ""
			if r.body != "" {
				body, ct = strings.NewReader(r.body), "application/json"
			}
			return sendAs(t, router, r.method, r.path, body, ct, role)
		}
		name := r.method + " " + r.path
		for _, role := range []authz.Role{authz.RoleResearcher, authz.RoleAdmin} {
			t.Run(name+"/"+string(role), func(t *testing.T) {
				if rec := send(t, string(role)); rec.Code != r.status {
					t.Errorf("status = %d, want %d; body %s", rec.Code, r.status, rec.Body)
				}
			})
		}
		t.Run(name+"/unknown role", func(t *testing.T) {
			rec := send(t, "visitor")
			if rec.Code != http.StatusForbidden || rec.Body.String() != forbiddenBody {
				t.Errorf("got %d %q, want 403 with exactly %q", rec.Code, rec.Body, forbiddenBody)
			}
		})
		t.Run(name+"/not logged in", func(t *testing.T) {
			checkUnauthenticated(t, "no token", send(t, ""))
		})
	}
}

func TestConnectionRoutesAreGuarded(t *testing.T) {
	want := map[string]authz.Action{
		"POST /api/v1/connections/requests":                    authz.ActAsMember,
		"POST /api/v1/connections/requests/{requestId}/accept": authz.ActAsMember,
		"POST /api/v1/connections/requests/{requestId}/reject": authz.ActAsMember,
		"DELETE /api/v1/connections/requests/{requestId}":      authz.ActAsMember,
		"DELETE /api/v1/connections/{userId}":                  authz.ActAsMember,
		"GET /api/v1/connections":                              authz.ViewContent,
		"GET /api/v1/connections/requests/received":            authz.ViewContent,
		"GET /api/v1/connections/requests/sent":                authz.ViewContent,
		"GET /api/v1/profiles/{userId}/connections":            authz.ViewContent,
	}
	for route, action := range want {
		if got := platformGuards[route]; got != action {
			t.Errorf("platformGuards[%q] = %q, want %q", route, got, action)
		}
	}
	for _, r := range httpserver.Routes() {
		_, path, _ := strings.Cut(r, " ")
		if strings.HasPrefix(path, httpserver.BaseURL+"/connections") || strings.HasSuffix(path, "/connections") {
			if _, ok := want[r]; !ok {
				t.Errorf("route %q has no expected action in this test; add it", r)
			}
		}
	}
}
