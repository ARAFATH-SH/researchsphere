//go:build integration

package postgres

import (
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// ListUsers returns real rows from users and profiles, newest first, a page at a time
// (FR-15.3). The database may hold other users, so the test dates its own users in the
// future to make them the newest.
func TestListUsers(t *testing.T) {
	ctx := t.Context()
	s, pool := newTestStore(t)
	future := time.Now().Add(100 * 365 * 24 * time.Hour).UTC().Truncate(time.Microsecond)

	var emails, ids []string
	for i, name := range []string{"Oldest", "Middle", "Newest"} {
		email := uniqueEmail(t, pool)
		id, err := register(ctx, s, email, name, rand.Text(), time.Now().Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, "UPDATE users SET created_at = $1 WHERE id = $2", future.Add(time.Duration(i)*time.Second), id); err != nil {
			t.Fatal(err)
		}
		emails, ids = append(emails, email), append(ids, id)
	}
	// The middle user is a verified, suspended admin; the others keep the defaults.
	if _, err := pool.Exec(ctx, "UPDATE users SET role = 'admin', email_verified_at = now(), suspended_at = now() WHERE id = $1", ids[1]); err != nil {
		t.Fatal(err)
	}

	first, err := s.ListUsers(ctx, 2, 0)
	if err != nil {
		t.Fatalf("ListUsers(2, 0): %v", err)
	}
	if len(first) != 2 || first[0].ID != ids[2] || first[1].ID != ids[1] {
		t.Fatalf("first page = %+v, want the newest two users, newest first", first)
	}
	newest, middle := first[0], first[1]
	if newest.Email != emails[2] || newest.FullName != "Newest" || newest.Role != domain.RoleResearcher ||
		newest.Verified() || newest.Suspended() || !newest.CreatedAt.Equal(future.Add(2*time.Second)) {
		t.Errorf("newest = %+v", newest)
	}
	if middle.Role != domain.RoleAdmin || !middle.Verified() || !middle.Suspended() {
		t.Errorf("middle = %+v, want a verified, suspended admin", middle)
	}

	second, err := s.ListUsers(ctx, 2, 2)
	if err != nil {
		t.Fatalf("ListUsers(2, 2): %v", err)
	}
	if len(second) == 0 || second[0].ID != ids[0] {
		t.Errorf("second page starts with %+v, want the oldest of the three", second)
	}

	n, err := s.CountUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var want int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&want); err != nil {
		t.Fatal(err)
	}
	if n != want || n < 3 {
		t.Errorf("CountUsers() = %d, want %d (at least 3)", n, want)
	}
}

// SetRoleByEmail is what `make admin` and `make unadmin` run.
func TestSetRoleByEmail(t *testing.T) {
	ctx := t.Context()
	s, pool := newTestStore(t)
	email := uniqueEmail(t, pool)
	id, err := register(ctx, s, email, "Ada", rand.Text(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	for _, role := range []string{domain.RoleAdmin, domain.RoleResearcher} {
		if err := s.SetRoleByEmail(ctx, strings.ToUpper(email), role); err != nil {
			t.Fatalf("SetRoleByEmail(%s): %v", role, err)
		}
		u, err := s.FindUserByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if u.Role != role {
			t.Errorf("role = %q, want %q", u.Role, role)
		}
	}

	if err := s.SetRoleByEmail(ctx, "nobody-"+strings.ToLower(rand.Text())+"@example.org", domain.RoleAdmin); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown email: error = %v, want ErrNotFound", err)
	}
	if err := s.SetRoleByEmail(ctx, email, "superuser"); err == nil || errors.Is(err, domain.ErrNotFound) {
		t.Errorf("role outside the enum: error = %v, want a database error", err)
	}
}
