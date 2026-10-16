package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// The role check (403 for a researcher, 401 without a session) is the route guard's job;
// cmd/api tests it through the real router.

func listUsers(t *testing.T, svc *fakeService, query string) *httptest.ResponseRecorder {
	t.Helper()
	return serve(NewHandler(svc, CookieConfig{}), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/admin/users"+query, nil))
}

func TestListUsers(t *testing.T) {
	joined := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)
	verified := joined.Add(time.Hour)
	svc := &fakeService{users: []domain.User{
		{ID: userID, Email: "ada@example.org", FullName: "Ada Lovelace", Role: domain.RoleAdmin, EmailVerifiedAt: &verified, CreatedAt: joined},
		{ID: "6f1c1f5e-3c3e-4d55-9a4c-2f0f6a8f3b11", Email: "bob@example.org", FullName: "Bob", Role: domain.RoleResearcher, SuspendedAt: &verified, CreatedAt: joined},
	}}
	rec := listUsers(t, svc, "?page=2&pageSize=10")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if svc.page != 2 || svc.pageSize != 10 {
		t.Errorf("service got page %d, size %d; want 2, 10", svc.page, svc.pageSize)
	}
	var got gen.UserPage
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Page != 2 || got.PageSize != 10 || got.Total != 2 || len(got.Items) != 2 {
		t.Fatalf("body = %+v", got)
	}
	ada, bob := got.Items[0], got.Items[1]
	if ada.Id.String() != userID || ada.FullName != "Ada Lovelace" || ada.Email != "ada@example.org" ||
		ada.Role != "admin" || !ada.Verified || ada.Suspended || !ada.CreatedAt.Equal(joined) {
		t.Errorf("first item = %+v", ada)
	}
	if bob.Role != "researcher" || bob.Verified || !bob.Suspended {
		t.Errorf("second item = %+v", bob)
	}
}

func TestListUsersPageSize(t *testing.T) {
	tests := []struct {
		name               string
		query              string
		wantPage, wantSize int // what the service is given
		wantBodySize       int
	}{
		{"defaults", "", 1, usecase.DefaultPageSize, usecase.DefaultPageSize},
		{"page size 500 is capped to 50", "?pageSize=500", 1, 500, 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{}
			rec := listUsers(t, svc, tt.query)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if svc.page != tt.wantPage || svc.pageSize != tt.wantSize {
				t.Errorf("service got page %d, size %d; want %d, %d", svc.page, svc.pageSize, tt.wantPage, tt.wantSize)
			}
			var got gen.UserPage
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.PageSize != tt.wantBodySize || got.Items == nil {
				t.Errorf("pageSize = %d, items = %v; want %d and an empty list", got.PageSize, got.Items, tt.wantBodySize)
			}
		})
	}
}

func TestListUsersRefused(t *testing.T) {
	rec := listUsers(t, &fakeService{err: domain.ErrInvalidPage}, "?page=0")
	if rec.Code != http.StatusBadRequest || decode(t, rec)["code"] != httpx.CodeBadRequest {
		t.Errorf("got %d %s, want 400 bad_request", rec.Code, rec.Body)
	}
}

func TestListUsersFails(t *testing.T) {
	rec := listUsers(t, &fakeService{err: errors.New("database down")}, "")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}
