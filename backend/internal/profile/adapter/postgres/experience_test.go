//go:build integration

package postgres

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

func ym(year, month int) domain.YearMonth { return domain.YearMonth{Year: year, Month: month} }

func ymptr(year, month int) *domain.YearMonth { v := ym(year, month); return &v }

// role returns a current full-time role at MIT starting in start.
func role(userID string, start domain.YearMonth) domain.Experience {
	return domain.Experience{
		UserID: userID, Title: "Researcher", Organization: "MIT", EmploymentType: domain.EmploymentFullTime,
		Start: start, IsCurrent: true,
	}
}

// insertExperience stores e in its own transaction.
func insertExperience(t *testing.T, s *Store, e domain.Experience) domain.Experience {
	t.Helper()
	var saved domain.Experience
	err := s.RunInTx(t.Context(), func(tx domain.Tx) error {
		var err error
		saved, err = tx.InsertExperience(t.Context(), e)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

// updateExperience runs the steps the usecase runs to change an entry: the owner-scoped
// lookup, then the owner-scoped update.
func updateExperience(ctx context.Context, s *Store, userID, id string, change func(*domain.Experience)) (domain.Experience, error) {
	var saved domain.Experience
	err := s.RunInTx(ctx, func(tx domain.Tx) error {
		e, err := tx.FindExperienceForUpdate(ctx, userID, id)
		if err != nil {
			return err
		}
		change(&e)
		saved, err = tx.UpdateExperience(ctx, e)
		return err
	})
	return saved, err
}

// An entry is created, changed and deleted, and reads back as stored each time (FR-4.8).
func TestCreatePatchDeleteExperience(t *testing.T) {
	s, pool := newTestStore(t)
	ada := newUser(t, pool)

	onSite := domain.LocationOnSite
	saved := insertExperience(t, s, domain.Experience{
		UserID: ada, Title: "Research Assistant", Organization: "University of Dhaka",
		EmploymentType: domain.EmploymentResearchAssistant, Location: ptr("Dhaka"), LocationType: &onSite,
		Start: ym(2020, 3), End: ymptr(2023, 8), Description: ptr("Line one\nline two"),
	})
	want := domain.Experience{
		ID: saved.ID, UserID: ada, Title: "Research Assistant", Organization: "University of Dhaka",
		EmploymentType: domain.EmploymentResearchAssistant, Location: ptr("Dhaka"), LocationType: &onSite,
		Start: ym(2020, 3), End: ymptr(2023, 8), Description: ptr("Line one\nline two"),
	}
	if saved.ID == "" || !reflect.DeepEqual(saved, want) {
		t.Fatalf("inserted %+v, want %+v", saved, want)
	}

	// Make it current and clear the optional fields.
	got, err := updateExperience(t.Context(), s, ada, saved.ID, func(e *domain.Experience) {
		e.Title, e.EmploymentType = "Postdoc", domain.EmploymentPostdoc
		e.Location, e.LocationType, e.Description = nil, nil, nil
		e.End, e.IsCurrent = nil, true
	})
	if err != nil {
		t.Fatal(err)
	}
	want = domain.Experience{
		ID: saved.ID, UserID: ada, Title: "Postdoc", Organization: "University of Dhaka",
		EmploymentType: domain.EmploymentPostdoc, Start: ym(2020, 3), IsCurrent: true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("updated %+v, want %+v", got, want)
	}
	list, err := s.ListExperience(t.Context(), ada)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(list, []domain.Experience{want}) {
		t.Errorf("listed %+v, want %+v", list, want)
	}

	if err := s.DeleteExperience(t.Context(), ada, saved.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.ListExperience(t.Context(), ada); len(list) != 0 {
		t.Errorf("%d entries after delete, want none", len(list))
	}
	if err := s.DeleteExperience(t.Context(), ada, saved.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("deleted twice: err = %v, want ErrNotFound", err)
	}
}

func TestInsertExperienceUnknownUser(t *testing.T) {
	s, _ := newTestStore(t)
	err := s.RunInTx(t.Context(), func(tx domain.Tx) error {
		_, err := tx.InsertExperience(t.Context(), role("00000000-0000-0000-0000-0000000000ff", ym(2020, 1)))
		return err
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// The constraints of migration 000009 catch an entry the usecase would have refused.
func TestInsertExperienceConstraintBackstop(t *testing.T) {
	s, pool := newTestStore(t)
	ada := newUser(t, pool)
	bad := role(ada, ym(2020, 1))
	bad.End = ymptr(2021, 1) // current with an end date
	err := s.RunInTx(t.Context(), func(tx domain.Tx) error {
		_, err := tx.InsertExperience(t.Context(), bad)
		return err
	})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.Code != "23514" {
		t.Errorf("err = %v, want a check violation", err)
	}
}

// The queries name the owner, so another user's entry is never found, changed or
// deleted, and the answer is the same as for an entry that does not exist (FR-4.8).
func TestExperienceIsOwnerScoped(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool), newUser(t, pool)
	entry := insertExperience(t, s, role(ada, ym(2020, 1)))

	err := s.RunInTx(t.Context(), func(tx domain.Tx) error {
		_, err := tx.FindExperienceForUpdate(t.Context(), bob, entry.ID)
		return err
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Bob finds Ada's entry: err = %v, want ErrNotFound", err)
	}

	// The update query checks the owner itself, not only the lookup before it.
	err = s.RunInTx(t.Context(), func(tx domain.Tx) error {
		hijack := entry
		hijack.UserID, hijack.Title = bob, "Hijacked"
		_, err := tx.UpdateExperience(t.Context(), hijack)
		return err
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Bob updates Ada's entry: err = %v, want ErrNotFound", err)
	}

	if err := s.DeleteExperience(t.Context(), bob, entry.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Bob deletes Ada's entry: err = %v, want ErrNotFound", err)
	}
	list, err := s.ListExperience(t.Context(), ada)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(list, []domain.Experience{entry}) {
		t.Fatalf("Ada's entries = %+v, want %+v unchanged", list, entry)
	}

	for _, id := range []string{"not-a-uuid", "00000000-0000-0000-0000-0000000000ff"} {
		if _, err := updateExperience(t.Context(), s, ada, id, func(*domain.Experience) {}); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("update %q: err = %v, want ErrNotFound", id, err)
		}
		if err := s.DeleteExperience(t.Context(), ada, id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("delete %q: err = %v, want ErrNotFound", id, err)
		}
	}
}

// Current roles first, then the latest start, then the oldest entry.
func TestListExperienceOrder(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool), newUser(t, pool)
	past := func(start domain.YearMonth, end *domain.YearMonth) domain.Experience {
		e := role(ada, start)
		e.End, e.IsCurrent = end, false
		return e
	}
	// Inserted out of order on purpose, each in its own transaction so created_at differs.
	oldPast := insertExperience(t, s, past(ym(2010, 1), ymptr(2026, 1)))
	pastA := insertExperience(t, s, past(ym(2018, 5), ymptr(2019, 1)))
	currentOld := insertExperience(t, s, role(ada, ym(2015, 1)))
	pastB := insertExperience(t, s, past(ym(2018, 5), ymptr(2020, 1))) // same start as pastA, added later
	pastLate := insertExperience(t, s, past(ym(2018, 9), ymptr(2018, 12)))
	currentNew := insertExperience(t, s, role(ada, ym(2025, 2)))
	insertExperience(t, s, role(bob, ym(2024, 1))) // someone else's, never listed

	got, err := s.ListExperience(t.Context(), ada)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Experience{currentNew, currentOld, pastLate, pastA, pastB, oldPast}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order:\n got %+v\nwant %+v", got, want)
	}

	if got, err := s.ListExperience(t.Context(), newUser(t, pool)); err != nil || got == nil || len(got) != 0 {
		t.Errorf("user without entries: %v, %v; want an empty, non-nil list", got, err)
	}
}

// Deleting the user deletes their entries (FR-2.8).
func TestExperienceDeletedWithUser(t *testing.T) {
	s, pool := newTestStore(t)
	ada := newUser(t, pool)
	insertExperience(t, s, role(ada, ym(2020, 1)))
	if _, err := pool.Exec(t.Context(), "DELETE FROM users WHERE id = $1", ada); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM profile_experience WHERE user_id = $1", ada).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d entries left after the user was deleted", n)
	}
}
