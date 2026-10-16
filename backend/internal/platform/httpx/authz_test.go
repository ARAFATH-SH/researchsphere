package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/authz"
)

func TestRequirePlatform(t *testing.T) {
	// The exact body FR-3.3 allows: no resource data, no reason.
	const forbiddenBody = `{"code":"forbidden","message":"You do not have permission to do this."}` + "\n"

	tests := []struct {
		name    string
		ctx     func(context.Context) context.Context
		action  authz.Action
		header  string // a role the client tries to claim; it must be ignored
		body    string
		allowed bool
	}{
		{name: "researcher may view content", ctx: withRole(authz.RoleResearcher), action: authz.ViewContent, allowed: true},
		{name: "admin may suspend", ctx: withRole(authz.RoleAdmin), action: authz.SuspendUser, allowed: true},
		{name: "researcher may not suspend", ctx: withRole(authz.RoleResearcher), action: authz.SuspendUser},
		{name: "researcher may not manage venues", ctx: withRole(authz.RoleResearcher), action: authz.ManageVenues},
		{name: "no role in context", ctx: func(c context.Context) context.Context { return c }, action: authz.ViewContent},
		{name: "unknown role", ctx: withRole("superuser"), action: authz.ViewContent},
		{name: "unknown action", ctx: withRole(authz.RoleAdmin), action: "delete_everything"},
		{
			name: "role claimed by the client is ignored", ctx: withRole(authz.RoleResearcher), action: authz.SuspendUser,
			header: "admin", body: `{"role":"admin"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reached bool
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reached = true
				w.WriteHeader(http.StatusNoContent)
			})
			h := RequirePlatform(tt.action)(next)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/anything", strings.NewReader(tt.body))
			req = req.WithContext(tt.ctx(req.Context()))
			if tt.header != "" {
				req.Header.Set("X-Role", tt.header)
				req.Header.Set("Role", tt.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if tt.allowed {
				if !reached || rec.Code != http.StatusNoContent {
					t.Fatalf("reached = %v, status = %d; want the next handler to run", reached, rec.Code)
				}
				return
			}
			if reached {
				t.Error("the next handler ran for a refused request")
			}
			if rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403", rec.Code)
			}
			if got := rec.Body.String(); got != forbiddenBody {
				t.Errorf("body = %q, want exactly %q", got, forbiddenBody)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
		})
	}
}

func withRole(role authz.Role) func(context.Context) context.Context {
	return func(ctx context.Context) context.Context { return WithRole(WithUserID(ctx, "user-1"), role) }
}
