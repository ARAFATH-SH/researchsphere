//go:build integration

package postgres

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

func iptr(n int) *int { return &n }

// insertEducation stores e in its own transaction.
func insertEducation(t *testing.T, s *Store, e domain.Education) domain.Education {
	t.Helper()
	var saved domain.Education
	err := s.RunInTx(t.Context(), func(tx domain.Tx) error {
		var err error
		saved, err = tx.InsertEducation(t.Context(), e)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

// updateEducation runs the steps the usecase runs to change an entry: the owner-scoped
// lookup, then the owner-scoped update.
func updateEducation(ctx context.Context, s *Store, userID, id string, change func(*domain.Education)) (domain.Education, error) {
	var saved domain.Education
	err := s.RunInTx(ctx, func(tx domain.Tx) error {
		e, err := tx.FindEducationForUpdate(ctx, userID, id)
		if err != nil {
			return err
		}
		change(&e)
		saved, err = tx.UpdateEducation(ctx, e)
		return err
	})
	return saved, err
}

func TestInsertAndUpdateEducation(t *testing.T) {
	s, pool := newTestStore(t)
	ada := newUser(t, pool)

	saved := insertEducation(t, s, domain.Education{UserID: ada, Institution: "MIT", FieldOfStudy: ptr("Physics"), StartYear: 2019})
	want := domain.Education{ID: saved.ID, UserID: ada, Institution: "MIT", FieldOfStudy: ptr("Physics"), StartYear: 2019}
	if saved.ID == "" || !reflect.DeepEqual(saved, want) {
		t.Fatalf("inserted %+v, want %+v", saved, want)
	}

	got, err := updateEducation(t.Context(), s, ada, saved.ID, func(e *domain.Education) {
		e.Degree, e.FieldOfStudy, e.EndYear = ptr("PhD"), nil, iptr(2023)
	})
	if err != nil {
		t.Fatal(err)
	}
	want = domain.Education{ID: saved.ID, UserID: ada, Institution: "MIT", Degree: ptr("PhD"), StartYear: 2019, EndYear: iptr(2023)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("updated %+v, want %+v", got, want)
	}
	list, err := s.ListEducation(t.Context(), ada)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(list, []domain.Education{want}) {
		t.Errorf("listed %+v, want %+v", list, want)
	}
}

func TestInsertEducationUnknownUser(t *testing.T) {
	s, _ := newTestStore(t)
	err := s.RunInTx(t.Context(), func(tx domain.Tx) error {
		_, err := tx.InsertEducation(t.Context(), domain.Education{
			UserID: "00000000-0000-0000-0000-0000000000ff", Institution: "MIT", StartYear: 2019,
		})
		return err
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// The queries name the owner, so another user's entry is never found, changed or
// deleted, and the answer is the same as for an entry that does not exist (FR-4.4).
func TestEducationIsOwnerScoped(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool), newUser(t, pool)
	entry := insertEducation(t, s, domain.Education{UserID: ada, Institution: "MIT", StartYear: 2019})

	err := s.RunInTx(t.Context(), func(tx domain.Tx) error {
		_, err := tx.FindEducationForUpdate(t.Context(), bob, entry.ID)
		return err
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Bob finds Ada's entry: err = %v, want ErrNotFound", err)
	}

	// The update query checks the owner itself, not only the lookup before it.
	err = s.RunInTx(t.Context(), func(tx domain.Tx) error {
		hijack := entry
		hijack.UserID, hijack.Institution = bob, "Hijacked"
		_, err := tx.UpdateEducation(t.Context(), hijack)
		return err
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Bob updates Ada's entry: err = %v, want ErrNotFound", err)
	}

	if err := s.DeleteEducation(t.Context(), bob, entry.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Bob deletes Ada's entry: err = %v, want ErrNotFound", err)
	}

	list, err := s.ListEducation(t.Context(), ada)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(list, []domain.Education{entry}) {
		t.Fatalf("Ada's entries = %+v, want %+v unchanged", list, entry)
	}

	for _, id := range []string{"not-a-uuid", "00000000-0000-0000-0000-0000000000ff"} {
		if _, err := updateEducation(t.Context(), s, ada, id, func(*domain.Education) {}); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("update %q: err = %v, want ErrNotFound", id, err)
		}
		if err := s.DeleteEducation(t.Context(), ada, id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("delete %q: err = %v, want ErrNotFound", id, err)
		}
	}

	if err := s.DeleteEducation(t.Context(), ada, entry.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteEducation(t.Context(), ada, entry.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("deleted twice: err = %v, want ErrNotFound", err)
	}
}

// Current entries first, then the latest end year, then the latest start year.
func TestListEducationOrder(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool), newUser(t, pool)
	add := func(user string, start int, end *int) domain.Education {
		return insertEducation(t, s, domain.Education{UserID: user, Institution: "MIT", StartYear: start, EndYear: end})
	}
	// Inserted out of order on purpose.
	bachelor := add(ada, 2008, iptr(2012))
	masterOld := add(ada, 2012, iptr(2014))
	currentOld := add(ada, 2018, nil)
	master := add(ada, 2013, iptr(2014))
	currentNew := add(ada, 2021, nil)
	add(bob, 2020, nil) // someone else's, never listed

	got, err := s.ListEducation(t.Context(), ada)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Education{currentNew, currentOld, master, masterOld, bachelor}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order:\n got %+v\nwant %+v", got, want)
	}

	if got, err := s.ListEducation(t.Context(), newUser(t, pool)); err != nil || got == nil || len(got) != 0 {
		t.Errorf("user without entries: %v, %v; want an empty, non-nil list", got, err)
	}
}

// The user-row lock makes concurrent adds by one user respect the limit, as the usecase
// runs them: lock, count, insert.
func TestAddEducationLimitUnderConcurrency(t *testing.T) {
	s, pool := newTestStore(t)
	user := newUser(t, pool)
	const limit = 3
	add := func(pause time.Duration) error {
		return s.RunInTx(context.Background(), func(tx domain.Tx) error {
			if err := tx.LockUser(context.Background(), user); err != nil {
				return err
			}
			n, err := tx.CountEducation(context.Background(), user)
			if err != nil {
				return err
			}
			if n >= limit {
				return domain.ErrEducationLimitReached
			}
			time.Sleep(pause)
			_, err = tx.InsertEducation(context.Background(), domain.Education{UserID: user, Institution: "MIT", StartYear: 2019})
			return err
		})
	}
	for range limit - 1 {
		if err := add(0); err != nil {
			t.Fatal(err)
		}
	}

	const n = 6
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { errs[i] = add(50 * time.Millisecond) })
	}
	wg.Wait()
	var ok, refused int
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, domain.ErrEducationLimitReached):
			refused++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != 1 || refused != n-1 {
		t.Errorf("%d added and %d refused, want 1 and %d", ok, refused, n-1)
	}
	if list, _ := s.ListEducation(t.Context(), user); len(list) != limit {
		t.Errorf("%d entries, want %d", len(list), limit)
	}
}
