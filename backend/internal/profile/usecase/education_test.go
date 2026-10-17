package usecase

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// newEducationService returns a Service over store whose clock reads 2026, so the latest
// year allowed is 2036.
func newEducationService(store *fakeStore) *Service {
	s := New(store)
	s.now = func() time.Time { return time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC) }
	return s
}

func iptr(n int) *int { return &n }

func validEducation() EducationInput {
	return EducationInput{Institution: "MIT", Degree: ptr("PhD"), FieldOfStudy: ptr("Physics"), StartYear: 2019, EndYear: iptr(2023)}
}

func TestAddEducation(t *testing.T) {
	t.Run("stored trimmed, blank optional text as nil, under the user lock", func(t *testing.T) {
		store := newFakeStore(testAccounts()...)
		got, err := newEducationService(store).AddEducation(t.Context(), adaID, EducationInput{
			Institution: "  University of London ", Degree: ptr("  "), FieldOfStudy: ptr(" Mathematics "), StartYear: 1950,
		})
		if err != nil {
			t.Fatal(err)
		}
		want := domain.Education{ID: got.ID, UserID: adaID, Institution: "University of London", FieldOfStudy: ptr("Mathematics"), StartYear: 1950}
		if got.ID == "" || !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
		if store.lockedUser != adaID {
			t.Errorf("locked %q, want the caller locked before counting", store.lockedUser)
		}
		if stored := store.education[got.ID]; !reflect.DeepEqual(stored, want) {
			t.Errorf("stored %+v, want %+v", stored, want)
		}
	})

	tests := []struct {
		name    string
		change  func(*EducationInput)
		wantErr error
	}{
		{"valid entry", func(*EducationInput) {}, nil},
		{"current entry: no end year", func(in *EducationInput) { in.EndYear = nil }, nil},
		{"end year equals start year", func(in *EducationInput) { in.EndYear = iptr(2019) }, nil},
		{"earliest start year", func(in *EducationInput) { in.StartYear = 1950 }, nil},
		{"latest start year: ten years ahead", func(in *EducationInput) { in.StartYear, in.EndYear = 2036, nil }, nil},
		{"latest end year: ten years ahead", func(in *EducationInput) { in.EndYear = iptr(2036) }, nil},
		{"degree and field missing", func(in *EducationInput) { in.Degree, in.FieldOfStudy = nil, nil }, nil},
		{"institution of 200 characters", func(in *EducationInput) { in.Institution = strings.Repeat("é", 200) }, nil},
		{"degree and field of 100 characters", func(in *EducationInput) {
			in.Degree, in.FieldOfStudy = ptr(strings.Repeat("é", 100)), ptr(strings.Repeat("é", 100))
		}, nil},

		{"institution missing", func(in *EducationInput) { in.Institution = "" }, domain.ErrInvalidEducationInstitution},
		{"institution blank", func(in *EducationInput) { in.Institution = " \t " }, domain.ErrInvalidEducationInstitution},
		{"institution of 201 characters", func(in *EducationInput) { in.Institution = strings.Repeat("a", 201) }, domain.ErrInvalidEducationInstitution},
		{"institution with a line break", func(in *EducationInput) { in.Institution = "MIT\nCambridge" }, domain.ErrInvalidEducationInstitution},
		{"degree of 101 characters", func(in *EducationInput) { in.Degree = ptr(strings.Repeat("a", 101)) }, domain.ErrInvalidDegree},
		{"degree with a line break", func(in *EducationInput) { in.Degree = ptr("Ph\nD") }, domain.ErrInvalidDegree},
		{"field of 101 characters", func(in *EducationInput) { in.FieldOfStudy = ptr(strings.Repeat("a", 101)) }, domain.ErrInvalidFieldOfStudy},
		{"start year missing", func(in *EducationInput) { in.StartYear = 0 }, domain.ErrInvalidStartYear},
		{"start year before 1950", func(in *EducationInput) { in.StartYear = 1949 }, domain.ErrInvalidStartYear},
		{"start year more than ten years ahead", func(in *EducationInput) { in.StartYear, in.EndYear = 2037, nil }, domain.ErrInvalidStartYear},
		{"end year before start year", func(in *EducationInput) { in.EndYear = iptr(2018) }, domain.ErrInvalidEndYear},
		{"end year more than ten years ahead", func(in *EducationInput) { in.EndYear = iptr(2037) }, domain.ErrInvalidEndYear},
		// The first invalid field in the contract's order is reported.
		{"institution before the other fields", func(in *EducationInput) {
			in.Institution, in.Degree, in.StartYear = "", ptr(strings.Repeat("a", 101)), 1900
		}, domain.ErrInvalidEducationInstitution},
		{"start year before end year", func(in *EducationInput) { in.StartYear, in.EndYear = 1900, iptr(1800) }, domain.ErrInvalidStartYear},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore(testAccounts()...)
			in := validEducation()
			tt.change(&in)
			_, err := newEducationService(store).AddEducation(t.Context(), adaID, in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if n, _ := store.CountEducation(t.Context(), adaID); tt.wantErr != nil && n != 0 {
				t.Errorf("%d entries stored after a refused add", n)
			}
		})
	}

	t.Run("at most 20 entries", func(t *testing.T) {
		store := newFakeStore(testAccounts()...)
		svc := newEducationService(store)
		for i := range MaxEducationEntries {
			if _, err := svc.AddEducation(t.Context(), adaID, validEducation()); err != nil {
				t.Fatalf("entry %d: %v", i+1, err)
			}
		}
		if _, err := svc.AddEducation(t.Context(), adaID, validEducation()); !errors.Is(err, domain.ErrEducationLimitReached) {
			t.Fatalf("21st entry: err = %v, want ErrEducationLimitReached", err)
		}
		if n, _ := store.CountEducation(t.Context(), adaID); n != MaxEducationEntries {
			t.Errorf("%d entries stored, want %d", n, MaxEducationEntries)
		}
		// The limit is per user.
		if _, err := svc.AddEducation(t.Context(), bobID, validEducation()); err != nil {
			t.Errorf("another user: %v", err)
		}
	})

	t.Run("account no longer exists", func(t *testing.T) {
		_, err := newEducationService(newFakeStore()).AddEducation(t.Context(), nobody, validEducation())
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("store error", func(t *testing.T) {
		store := newFakeStore(testAccounts()...)
		store.err = errDatabase
		if _, err := newEducationService(store).AddEducation(t.Context(), adaID, validEducation()); !errors.Is(err, errDatabase) {
			t.Errorf("err = %v, want the store error", err)
		}
	})
}

func set[T any](v T) Field[T] { return Field[T]{Set: true, Value: v} }

func null[T any]() Field[T] { return Field[T]{Set: true, Null: true} }

func TestUpdateEducation(t *testing.T) {
	// stored is Ada's entry: MIT, PhD in Physics, 2019 to 2023.
	setup := func(t *testing.T) (*fakeStore, *Service, domain.Education) {
		t.Helper()
		store := newFakeStore(testAccounts()...)
		svc := newEducationService(store)
		stored, err := svc.AddEducation(t.Context(), adaID, validEducation())
		if err != nil {
			t.Fatal(err)
		}
		return store, svc, stored
	}

	tests := []struct {
		name  string
		patch EducationPatch
		want  func(e *domain.Education) // the change to the stored entry
	}{
		{"empty patch changes nothing", EducationPatch{}, func(*domain.Education) {}},
		{"only the degree, trimmed", EducationPatch{Degree: set(" MSc ")}, func(e *domain.Education) { e.Degree = ptr("MSc") }},
		{"null degree clears it", EducationPatch{Degree: null[string]()}, func(e *domain.Education) { e.Degree = nil }},
		{"blank field clears it", EducationPatch{FieldOfStudy: set("  ")}, func(e *domain.Education) { e.FieldOfStudy = nil }},
		{"null end year makes the entry current", EducationPatch{EndYear: null[int]()}, func(e *domain.Education) { e.EndYear = nil }},
		{"institution and years", EducationPatch{Institution: set("ETH Zürich"), StartYear: set(2020), EndYear: set(2024)}, func(e *domain.Education) {
			e.Institution, e.StartYear, e.EndYear = "ETH Zürich", 2020, iptr(2024)
		}},
		{"start year up to the stored end year", EducationPatch{StartYear: set(2023)}, func(e *domain.Education) { e.StartYear = 2023 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, svc, stored := setup(t)
			got, err := svc.UpdateEducation(t.Context(), adaID, stored.ID, tt.patch)
			if err != nil {
				t.Fatal(err)
			}
			want := stored
			tt.want(&want)
			if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(store.education[stored.ID], want) {
				t.Errorf("got %+v, stored %+v; want %+v", got, store.education[stored.ID], want)
			}
		})
	}

	refused := []struct {
		name    string
		patch   EducationPatch
		wantErr error
	}{
		{"null institution", EducationPatch{Institution: null[string]()}, domain.ErrInvalidEducationInstitution},
		{"blank institution", EducationPatch{Institution: set(" ")}, domain.ErrInvalidEducationInstitution},
		{"degree of 101 characters", EducationPatch{Degree: set(strings.Repeat("a", 101))}, domain.ErrInvalidDegree},
		{"field with a line break", EducationPatch{FieldOfStudy: set("a\nb")}, domain.ErrInvalidFieldOfStudy},
		{"null start year", EducationPatch{StartYear: null[int]()}, domain.ErrInvalidStartYear},
		{"start year before 1950", EducationPatch{StartYear: set(1949)}, domain.ErrInvalidStartYear},
		// endYear >= startYear holds for the entry after the change, whichever side changes.
		{"end year before the stored start year", EducationPatch{EndYear: set(2018)}, domain.ErrInvalidEndYear},
		{"start year after the stored end year", EducationPatch{StartYear: set(2024)}, domain.ErrInvalidEndYear},
		{"end year more than ten years ahead", EducationPatch{EndYear: set(2037)}, domain.ErrInvalidEndYear},
	}
	for _, tt := range refused {
		t.Run(tt.name, func(t *testing.T) {
			store, svc, stored := setup(t)
			if _, err := svc.UpdateEducation(t.Context(), adaID, stored.ID, tt.patch); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got := store.education[stored.ID]; !reflect.DeepEqual(got, stored) {
				t.Errorf("stored %+v, want it unchanged %+v", got, stored)
			}
		})
	}

	// Someone else's entry gives the same ErrNotFound as one that does not exist, even
	// for a patch that would be refused, so the answer reveals nothing.
	for name, patch := range map[string]EducationPatch{
		"valid patch":   {Degree: set("MSc")},
		"invalid patch": {Institution: null[string]()},
	} {
		t.Run("someone else's entry, "+name, func(t *testing.T) {
			store, svc, stored := setup(t)
			if _, err := svc.UpdateEducation(t.Context(), bobID, stored.ID, patch); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
			if got := store.education[stored.ID]; !reflect.DeepEqual(got, stored) {
				t.Errorf("stored %+v, want it unchanged %+v", got, stored)
			}
		})
		t.Run("unknown entry, "+name, func(t *testing.T) {
			_, svc, _ := setup(t)
			if _, err := svc.UpdateEducation(t.Context(), adaID, "not-a-uuid", patch); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestRemoveEducation(t *testing.T) {
	store := newFakeStore(testAccounts()...)
	svc := newEducationService(store)
	stored, err := svc.AddEducation(t.Context(), adaID, validEducation())
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.RemoveEducation(t.Context(), bobID, stored.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("someone else's entry: err = %v, want ErrNotFound", err)
	}
	if _, ok := store.education[stored.ID]; !ok {
		t.Fatal("someone else removed Ada's entry")
	}
	if err := svc.RemoveEducation(t.Context(), adaID, stored.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.education[stored.ID]; ok {
		t.Error("entry still stored after removal")
	}
	if err := svc.RemoveEducation(t.Context(), adaID, stored.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("removed twice: err = %v, want ErrNotFound", err)
	}
}

// GetOwn and View include the entries in profile order (FR-4.4, FR-4.5).
func TestProfileHasEducation(t *testing.T) {
	store := newFakeStore(testAccounts()...)
	svc := newEducationService(store)
	add := func(start int, end *int) domain.Education {
		t.Helper()
		e, err := svc.AddEducation(t.Context(), adaID, EducationInput{Institution: "MIT", StartYear: start, EndYear: end})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	old := add(2010, iptr(2014))
	current := add(2022, nil)
	recent := add(2015, iptr(2019))
	want := []domain.Education{current, recent, old}

	own, err := svc.GetOwn(t.Context(), adaID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(own.Education, want) {
		t.Errorf("GetOwn education = %+v, want %+v", own.Education, want)
	}
	viewed, err := svc.View(t.Context(), adaID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(viewed.Education, want) {
		t.Errorf("View education = %+v, want %+v", viewed.Education, want)
	}
}
