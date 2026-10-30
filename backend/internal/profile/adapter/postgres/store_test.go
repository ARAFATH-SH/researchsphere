//go:build integration

// Repository tests against the real database. Run with `make test-integration`
// (needs `make up` and `make migrate`); they read DATABASE_URL.
package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

func newTestStore(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Fatal("DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(t.Context(), url)
	if err != nil {
		t.Fatal("DATABASE_URL is not accepted by the PostgreSQL driver")
	}
	t.Cleanup(pool.Close)
	return NewStore(pool), pool
}

// newUser stores a verified user with no profile row and deletes it (and, through the
// cascade, its profile) when the test ends.
func newUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	email := "it-" + strings.ToLower(rand.Text()) + "@example.org"
	var id string
	err := pool.QueryRow(t.Context(),
		"INSERT INTO users (email, password_hash, email_verified_at) VALUES ($1, 'hash', now()) RETURNING id", email).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", id); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
	return id
}

func ptr(s string) *string { return &s }

func TestFindAccountWithoutProfileRow(t *testing.T) {
	s, pool := newTestStore(t)
	id := newUser(t, pool)

	a, err := s.FindAccount(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Account{UserID: id, Verified: true}
	if !reflect.DeepEqual(a, want) {
		t.Errorf("got %+v, want %+v", a, want)
	}
}

func TestFindAccountUnknownOrMalformed(t *testing.T) {
	s, _ := newTestStore(t)
	for _, id := range []string{"00000000-0000-0000-0000-0000000000ff", "not-a-uuid"} {
		if _, err := s.FindAccount(t.Context(), id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("FindAccount(%q) err = %v, want ErrNotFound", id, err)
		}
	}
}

func TestFindAccountSuspendedAndUnverified(t *testing.T) {
	s, pool := newTestStore(t)
	id := newUser(t, pool)
	if _, err := pool.Exec(t.Context(), "UPDATE users SET suspended_at = now(), email_verified_at = NULL WHERE id = $1", id); err != nil {
		t.Fatal(err)
	}
	a, err := s.FindAccount(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Suspended || a.Verified {
		t.Errorf("got %+v, want suspended and not verified", a)
	}
}

// The upsert inserts the row the first time and replaces the edited fields the second
// time; a nil field is stored as NULL, and columns it does not edit stay.
func TestUpsertProfile(t *testing.T) {
	s, pool := newTestStore(t)
	id := newUser(t, pool)

	first := domain.Profile{
		UserID: id, FullName: "Ada Lovelace", Headline: ptr("Analyst"), Bio: ptr("Line one\nline two"),
		Institution: ptr("University of London"), Department: ptr("Mathematics"), JobTitle: ptr("Researcher"), Country: ptr("GB"),
		City: ptr("London"),
	}
	got, err := s.UpsertProfile(t.Context(), first)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, first) {
		t.Errorf("insert returned %+v, want %+v", got, first)
	}
	a, err := s.FindAccount(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if a.Profile == nil || !reflect.DeepEqual(*a.Profile, first) {
		t.Errorf("after insert, stored %+v, want %+v", a.Profile, first)
	}

	// A column outside this operation must survive the update.
	if _, err := pool.Exec(t.Context(), "UPDATE profiles SET orcid_id = '0000-0002-1825-0097' WHERE user_id = $1", id); err != nil {
		t.Fatal(err)
	}
	var createdBefore string
	if err := pool.QueryRow(t.Context(), "SELECT created_at::text FROM profiles WHERE user_id = $1", id).Scan(&createdBefore); err != nil {
		t.Fatal(err)
	}

	second := domain.Profile{UserID: id, FullName: "Augusta Ada King", Country: ptr("DE")}
	got, err = s.UpsertProfile(t.Context(), second)
	if err != nil {
		t.Fatal(err)
	}
	// The ORCID iD is not edited here, so it is returned as stored (FR-5.3).
	wantSecond := second
	wantSecond.OrcidID = ptr("0000-0002-1825-0097")
	if !reflect.DeepEqual(got, wantSecond) {
		t.Errorf("update returned %+v, want %+v", got, wantSecond)
	}

	var (
		rows                                                            int
		headline, bio, institution, department, jobTitle, city, orcidID *string
		createdAfter                                                    string
		updatedAfterCreated                                             bool
	)
	err = pool.QueryRow(t.Context(), `
		SELECT (SELECT count(*) FROM profiles WHERE user_id = $1),
		       headline, bio, institution, department, job_title, city, orcid_id,
		       created_at::text, updated_at > created_at
		FROM profiles WHERE user_id = $1`, id).
		Scan(&rows, &headline, &bio, &institution, &department, &jobTitle, &city, &orcidID, &createdAfter, &updatedAfterCreated)
	if err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("%d profile rows, want 1", rows)
	}
	for name, v := range map[string]*string{"headline": headline, "bio": bio, "institution": institution, "department": department, "job_title": jobTitle, "city": city} {
		if v != nil {
			t.Errorf("%s = %q, want NULL", name, *v)
		}
	}
	if orcidID == nil || *orcidID != "0000-0002-1825-0097" {
		t.Errorf("orcid_id = %v, want it kept", orcidID)
	}
	if createdAfter != createdBefore {
		t.Errorf("created_at changed from %s to %s", createdBefore, createdAfter)
	}
	if !updatedAfterCreated {
		t.Error("updated_at was not moved forward by the update")
	}
}

func TestUpsertProfileUnknownUser(t *testing.T) {
	s, _ := newTestStore(t)
	_, err := s.UpsertProfile(t.Context(), domain.Profile{UserID: "00000000-0000-0000-0000-0000000000ff", FullName: "Ghost"})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// The database refuses what the usecase would refuse, should a value ever get past it.
func TestUpsertProfileConstraints(t *testing.T) {
	s, pool := newTestStore(t)
	id := newUser(t, pool)
	for name, p := range map[string]domain.Profile{
		"name too long":          {UserID: id, FullName: strings.Repeat("a", 201)},
		"blank headline":         {UserID: id, FullName: "Ada", Headline: ptr(" ")},
		"bio too long":           {UserID: id, FullName: "Ada", Bio: ptr(strings.Repeat("a", 2001))},
		"lower-case country":     {UserID: id, FullName: "Ada", Country: ptr("de")},
		"city too long":          {UserID: id, FullName: "Ada", City: ptr(strings.Repeat("a", 101))},
		"city with a line break": {UserID: id, FullName: "Ada", City: ptr("a\nb")},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := s.UpsertProfile(t.Context(), p)
			pgErr, ok := errors.AsType[*pgconn.PgError](err)
			if !ok || pgErr.Code != "23514" {
				t.Errorf("err = %v, want a check violation", err)
			}
		})
	}
}
