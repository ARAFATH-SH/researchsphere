package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	notificationdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/notification/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/authz"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpserver"
)

const okNotificationID = "3b1f6c2e-9a4d-4e8b-b7c1-2d3e4f5a6b7c"

// okNotifications is a notification service for which every operation succeeds.
type okNotifications struct{}

func (okNotifications) List(context.Context, string, string, *int) (notificationdomain.Page, error) {
	return notificationdomain.Page{}, nil
}
func (okNotifications) UnreadCount(context.Context, string) (int, error) { return 0, nil }
func (okNotifications) MarkRead(context.Context, string, string) error   { return nil }
func (okNotifications) MarkAllRead(context.Context, string) (int, error) { return 0, nil }

// The notification routes go through authz (SRS 2.3): both roles may use them, an unknown
// role gets 403 and no session gets 401.
func TestNotificationRoutesThroughRouter(t *testing.T) {
	router := testRouter()
	routes := []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/api/v1/notifications", http.StatusOK},
		{http.MethodGet, "/api/v1/notifications/unread-count", http.StatusOK},
		{http.MethodPost, "/api/v1/notifications/" + okNotificationID + "/read", http.StatusNoContent},
		{http.MethodPost, "/api/v1/notifications/read-all", http.StatusOK},
	}
	for _, r := range routes {
		send := func(t *testing.T, role string) *httptest.ResponseRecorder {
			t.Helper()
			return sendAs(t, router, r.method, r.path, nil, "", role)
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

func TestNotificationRoutesAreGuarded(t *testing.T) {
	want := map[string]authz.Action{
		"GET /api/v1/notifications":                        authz.ViewContent,
		"GET /api/v1/notifications/unread-count":           authz.ViewContent,
		"POST /api/v1/notifications/{notificationId}/read": authz.ViewContent,
		"POST /api/v1/notifications/read-all":              authz.ViewContent,
	}
	for route, action := range want {
		if got := platformGuards[route]; got != action {
			t.Errorf("platformGuards[%q] = %q, want %q", route, got, action)
		}
	}
	for _, r := range httpserver.Routes() {
		_, path, _ := strings.Cut(r, " ")
		if strings.HasPrefix(path, httpserver.BaseURL+"/notifications") {
			if _, ok := want[r]; !ok {
				t.Errorf("route %q has no expected action in this test; add it", r)
			}
		}
	}
}
