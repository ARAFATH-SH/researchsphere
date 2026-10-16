package usecase

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

func TestListUsers(t *testing.T) {
	e := newEnv()
	for _, email := range []string{"a@example.org", "b@example.org", "c@example.org"} {
		registered(t, e, email)
	}

	tests := []struct {
		name           string
		page, pageSize int
		wantEmails     []string
		wantSize       int
		wantOffset     int
	}{
		{"first page, newest first", 1, 2, []string{"c@example.org", "b@example.org"}, 2, 0},
		{"second page", 2, 2, []string{"a@example.org"}, 2, 2},
		{"past the end", 3, 2, nil, 2, 4},
		{"page size above 50 is capped", 1, 500, []string{"c@example.org", "b@example.org", "a@example.org"}, MaxPageSize, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e.store.listed = nil
			got, err := e.svc.ListUsers(context.Background(), tt.page, tt.pageSize)
			if err != nil {
				t.Fatalf("ListUsers() error = %v", err)
			}
			if got.Page != tt.page || got.PageSize != tt.wantSize || got.Total != 3 {
				t.Errorf("page, size, total = %d, %d, %d; want %d, %d, 3", got.Page, got.PageSize, got.Total, tt.page, tt.wantSize)
			}
			if got.Users == nil {
				t.Error("Users is nil, want an empty list")
			}
			var emails []string
			for _, u := range got.Users {
				emails = append(emails, u.Email)
			}
			if len(emails) != len(tt.wantEmails) {
				t.Fatalf("emails = %v, want %v", emails, tt.wantEmails)
			}
			for i := range emails {
				if emails[i] != tt.wantEmails[i] {
					t.Fatalf("emails = %v, want %v", emails, tt.wantEmails)
				}
			}
			if want := [][2]int{{tt.wantSize, tt.wantOffset}}; len(e.store.listed) != 1 || e.store.listed[0] != want[0] {
				t.Errorf("store asked for (limit, offset) %v, want %v", e.store.listed, want)
			}
		})
	}
}

func TestListUsersRefusesBadPages(t *testing.T) {
	e := newEnv()
	for _, tt := range []struct {
		name           string
		page, pageSize int
	}{
		{"page 0", 0, 20},
		{"negative page", -1, 20},
		{"page size 0", 1, 0},
		{"negative page size", 1, -5},
		{"offset too large", math.MaxInt32, 50},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := e.svc.ListUsers(context.Background(), tt.page, tt.pageSize); !errors.Is(err, domain.ErrInvalidPage) {
				t.Errorf("ListUsers(%d, %d) error = %v, want ErrInvalidPage", tt.page, tt.pageSize, err)
			}
			if len(e.store.listed) != 0 {
				t.Error("the store was asked for users")
			}
		})
	}
}

func TestListUsersStoreFails(t *testing.T) {
	e := newEnv()
	boom := errors.New("database down")
	e.store.failFind = boom
	if _, err := e.svc.ListUsers(context.Background(), 1, 20); !errors.Is(err, boom) {
		t.Errorf("error = %v, want %v", err, boom)
	}
}

// CurrentUser returns the stored platform role, which GET /auth/me reports.
func TestCurrentUserHasRole(t *testing.T) {
	e := newEnv()
	id := verifiedUser(t, e, "ada@example.org")
	setRole(t, e, "ada@example.org", domain.RoleAdmin)
	u, err := e.svc.CurrentUser(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != domain.RoleAdmin {
		t.Errorf("Role = %q, want admin", u.Role)
	}
}
