package usecase

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// newExperienceService returns a Service over store whose clock reads 15 June 2026 UTC,
// so June 2026 is the latest month allowed.
func newExperienceService(store *fakeStore) *Service {
	s := New(store)
	s.now = func() time.Time { return time.Date(2026, time.June, 15, 12, 0, 0, 0, time.UTC) }
	return s
}

func ym(year, month int) domain.YearMonth { return domain.YearMonth{Year: year, Month: month} }

func ymptr(year, month int) *domain.YearMonth { v := ym(year, month); return &v }

func ltptr(t domain.LocationType) *domain.LocationType { return &t }

// validExperience is a finished role from March 2020 to August 2023.
func validExperience() ExperienceInput {
	return ExperienceInput{
		Title:          "Research Assistant",
		Organization:   "University of Dhaka",
		EmploymentType: domain.EmploymentResearchAssistant,
		Location:       ptr("Dhaka, Bangladesh"),
		LocationType:   ltptr(domain.LocationOnSite),
		Start:          ym(2020, 3),
		End:            ymptr(2023, 8),
		Description:    ptr("Ran the lab's experiments."),
	}
}

// currentExperience is a current role since January 2024.
func currentExperience() ExperienceInput {
	return ExperienceInput{Title: "Postdoc", Organization: "MIT", EmploymentType: domain.EmploymentPostdoc, Start: ym(2024, 1), IsCurrent: true}
}

func TestAddExperience(t *testing.T) {
	t.Run("stored trimmed, blank optional text as nil, under the user lock", func(t *testing.T) {
		store := newFakeStore(testAccounts()...)
		got, err := newExperienceService(store).AddExperience(t.Context(), adaID, ExperienceInput{
			Title: "  Lecturer ", Organization: " BUET  ", EmploymentType: domain.EmploymentFullTime,
			Location: ptr("   "), Start: ym(1950, 1), End: ymptr(1960, 12), Description: ptr(" \n Taught\nphysics. \n"),
		})
		if err != nil {
			t.Fatal(err)
		}
		want := domain.Experience{
			ID: got.ID, UserID: adaID, Title: "Lecturer", Organization: "BUET", EmploymentType: domain.EmploymentFullTime,
			Start: ym(1950, 1), End: ymptr(1960, 12), Description: ptr("Taught\nphysics."),
		}
		if got.ID == "" || !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
		if store.lockedUser != adaID {
			t.Errorf("locked %q, want the caller locked before counting", store.lockedUser)
		}
		if stored := store.experience[got.ID]; !reflect.DeepEqual(stored, want) {
			t.Errorf("stored %+v, want %+v", stored, want)
		}
	})

	tests := []struct {
		name    string
		change  func(*ExperienceInput)
		wantErr error
	}{
		{"valid finished role", func(*ExperienceInput) {}, nil},
		{"valid current role", func(in *ExperienceInput) { *in = currentExperience() }, nil},
		{"optional fields missing", func(in *ExperienceInput) { in.Location, in.LocationType, in.Description = nil, nil, nil }, nil},
		{"title of 100 characters", func(in *ExperienceInput) { in.Title = strings.Repeat("é", 100) }, nil},
		{"organization of 150 characters", func(in *ExperienceInput) { in.Organization = strings.Repeat("é", 150) }, nil},
		{"location of 100 characters", func(in *ExperienceInput) { in.Location = ptr(strings.Repeat("é", 100)) }, nil},
		{"description of 2000 characters with line breaks", func(in *ExperienceInput) {
			in.Description = ptr(strings.Repeat("é\n", 999) + "é")
		}, nil},
		{"end month equals start month", func(in *ExperienceInput) { in.End = ymptr(2020, 3) }, nil},
		{"start in the current month", func(in *ExperienceInput) { in.Start, in.End = ym(2026, 6), ymptr(2026, 6) }, nil},
		{"earliest start: January 1950", func(in *ExperienceInput) { in.Start = ym(1950, 1) }, nil},

		{"title missing", func(in *ExperienceInput) { in.Title = "" }, domain.ErrInvalidTitle},
		{"title blank", func(in *ExperienceInput) { in.Title = " \t " }, domain.ErrInvalidTitle},
		{"title of 101 characters", func(in *ExperienceInput) { in.Title = strings.Repeat("a", 101) }, domain.ErrInvalidTitle},
		{"title with a line break", func(in *ExperienceInput) { in.Title = "Research\nAssistant" }, domain.ErrInvalidTitle},
		{"organization missing", func(in *ExperienceInput) { in.Organization = "  " }, domain.ErrInvalidOrganization},
		{"organization of 151 characters", func(in *ExperienceInput) { in.Organization = strings.Repeat("a", 151) }, domain.ErrInvalidOrganization},
		{"organization with a line break", func(in *ExperienceInput) { in.Organization = "MIT\r\nCSAIL" }, domain.ErrInvalidOrganization},
		{"employment type missing", func(in *ExperienceInput) { in.EmploymentType = "" }, domain.ErrInvalidEmploymentType},
		{"unknown employment type", func(in *ExperienceInput) { in.EmploymentType = "gig" }, domain.ErrInvalidEmploymentType},
		{"employment type in capitals", func(in *ExperienceInput) { in.EmploymentType = "FULL_TIME" }, domain.ErrInvalidEmploymentType},
		{"location of 101 characters", func(in *ExperienceInput) { in.Location = ptr(strings.Repeat("a", 101)) }, domain.ErrInvalidLocation},
		{"location with a line break", func(in *ExperienceInput) { in.Location = ptr("Dhaka\nBangladesh") }, domain.ErrInvalidLocation},
		{"unknown location type", func(in *ExperienceInput) { in.LocationType = ltptr("office") }, domain.ErrInvalidLocationType},
		{"empty location type", func(in *ExperienceInput) { in.LocationType = ltptr("") }, domain.ErrInvalidLocationType},
		{"start date missing", func(in *ExperienceInput) { in.Start = domain.YearMonth{} }, domain.ErrInvalidStartDate},
		{"start month 0", func(in *ExperienceInput) { in.Start = ym(2020, 0) }, domain.ErrInvalidStartDate},
		{"start month 13", func(in *ExperienceInput) { in.Start = ym(2020, 13) }, domain.ErrInvalidStartDate},
		{"start before 1950", func(in *ExperienceInput) { in.Start = ym(1949, 12) }, domain.ErrInvalidStartDate},
		{"start next month", func(in *ExperienceInput) { in.Start, in.End = ym(2026, 7), nil; in.IsCurrent = true }, domain.ErrInvalidStartDate},
		{"start next year", func(in *ExperienceInput) { in.Start, in.End = ym(2027, 1), nil; in.IsCurrent = true }, domain.ErrInvalidStartDate},
		{"end month 13", func(in *ExperienceInput) { in.End = ymptr(2023, 13) }, domain.ErrInvalidEndDate},
		{"end month 0", func(in *ExperienceInput) { in.End = ymptr(2023, 0) }, domain.ErrInvalidEndDate},
		{"end next month", func(in *ExperienceInput) { in.End = ymptr(2026, 7) }, domain.ErrInvalidEndDate},
		{"end before 1950", func(in *ExperienceInput) { in.Start, in.End = ym(1950, 1), ymptr(1949, 12) }, domain.ErrInvalidEndDate},
		{"end before start, earlier year", func(in *ExperienceInput) { in.End = ymptr(2019, 12) }, domain.ErrInvalidDateRange},
		{"end before start, same year", func(in *ExperienceInput) { in.End = ymptr(2020, 2) }, domain.ErrInvalidDateRange},
		{"description of 2001 characters", func(in *ExperienceInput) { in.Description = ptr(strings.Repeat("a", 2001)) }, domain.ErrInvalidDescription},
		// The first invalid field in the contract's order is reported.
		{"title before the other fields", func(in *ExperienceInput) {
			in.Title, in.Organization, in.EmploymentType, in.Start = "", "", "gig", domain.YearMonth{}
		}, domain.ErrInvalidTitle},
		{"start date before the end date", func(in *ExperienceInput) { in.Start, in.End = ym(2020, 13), ymptr(2020, 0) }, domain.ErrInvalidStartDate},
		{"end date before the range", func(in *ExperienceInput) { in.End = ymptr(2019, 13) }, domain.ErrInvalidEndDate},
		{"date range before the description", func(in *ExperienceInput) {
			in.End, in.Description = ymptr(2019, 1), ptr(strings.Repeat("a", 2001))
		}, domain.ErrInvalidDateRange},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore(testAccounts()...)
			in := validExperience()
			tt.change(&in)
			_, err := newExperienceService(store).AddExperience(t.Context(), adaID, in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if n, _ := store.CountExperience(t.Context(), adaID); tt.wantErr != nil && n != 0 {
				t.Errorf("%d entries stored after a refused add", n)
			}
		})
	}

	// isCurrent and endDate must agree: a current role has no end, a finished one has one.
	combinations := []struct {
		name      string
		isCurrent bool
		end       *domain.YearMonth
		wantErr   error
	}{
		{"current without an end date", true, nil, nil},
		{"current with an end date", true, ymptr(2023, 8), domain.ErrEndDateNotAllowed},
		{"finished with an end date", false, ymptr(2023, 8), nil},
		{"finished without an end date", false, nil, domain.ErrEndDateRequired},
		// The end date's own checks come first.
		{"current with an invalid end date", true, ymptr(2023, 13), domain.ErrInvalidEndDate},
		// The pairing comes before the range.
		{"current with an end date before the start", true, ymptr(2019, 1), domain.ErrEndDateNotAllowed},
	}
	for _, tt := range combinations {
		t.Run(tt.name, func(t *testing.T) {
			in := validExperience()
			in.IsCurrent, in.End = tt.isCurrent, tt.end
			got, err := newExperienceService(newFakeStore(testAccounts()...)).AddExperience(t.Context(), adaID, in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && (got.IsCurrent != tt.isCurrent || !reflect.DeepEqual(got.End, tt.end)) {
				t.Errorf("stored isCurrent %v, end %+v", got.IsCurrent, got.End)
			}
		})
	}

	t.Run("every employment and location type is accepted", func(t *testing.T) {
		store := newFakeStore(testAccounts()...)
		svc := newExperienceService(store)
		types := []domain.EmploymentType{
			domain.EmploymentFullTime, domain.EmploymentPartTime, domain.EmploymentInternship, domain.EmploymentContract,
			domain.EmploymentFreelance, domain.EmploymentSelfEmployed, domain.EmploymentResearchAssistant,
			domain.EmploymentTeachingAssistant, domain.EmploymentPostdoc, domain.EmploymentVolunteer,
		}
		locations := []domain.LocationType{domain.LocationOnSite, domain.LocationHybrid, domain.LocationRemote}
		for i, et := range types {
			in := validExperience()
			in.EmploymentType, in.LocationType = et, ltptr(locations[i%len(locations)])
			if _, err := svc.AddExperience(t.Context(), adaID, in); err != nil {
				t.Errorf("%s: %v", et, err)
			}
		}
	})

	t.Run("future is judged by the month in UTC", func(t *testing.T) {
		svc := newExperienceService(newFakeStore(testAccounts()...))
		// 1 July 02:00 in Dhaka (UTC+6) is still 30 June in UTC.
		svc.now = func() time.Time { return time.Date(2026, time.July, 1, 2, 0, 0, 0, time.FixedZone("BDT", 6*3600)) }
		in := currentExperience()
		in.Start = ym(2026, 7)
		if _, err := svc.AddExperience(t.Context(), adaID, in); !errors.Is(err, domain.ErrInvalidStartDate) {
			t.Fatalf("July: err = %v, want ErrInvalidStartDate", err)
		}
		in.Start = ym(2026, 6)
		if _, err := svc.AddExperience(t.Context(), adaID, in); err != nil {
			t.Fatalf("June: %v", err)
		}
	})

	t.Run("at most 30 entries", func(t *testing.T) {
		store := newFakeStore(testAccounts()...)
		svc := newExperienceService(store)
		for i := range MaxExperienceEntries {
			if _, err := svc.AddExperience(t.Context(), adaID, validExperience()); err != nil {
				t.Fatalf("entry %d: %v", i+1, err)
			}
		}
		if _, err := svc.AddExperience(t.Context(), adaID, validExperience()); !errors.Is(err, domain.ErrExperienceLimitReached) {
			t.Fatalf("31st entry: err = %v, want ErrExperienceLimitReached", err)
		}
		if n, _ := store.CountExperience(t.Context(), adaID); n != MaxExperienceEntries {
			t.Errorf("%d entries stored, want %d", n, MaxExperienceEntries)
		}
		// The limit is per user.
		if _, err := svc.AddExperience(t.Context(), bobID, validExperience()); err != nil {
			t.Errorf("another user: %v", err)
		}
	})

	t.Run("account no longer exists", func(t *testing.T) {
		_, err := newExperienceService(newFakeStore()).AddExperience(t.Context(), nobody, validExperience())
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("store error", func(t *testing.T) {
		store := newFakeStore(testAccounts()...)
		store.err = errDatabase
		if _, err := newExperienceService(store).AddExperience(t.Context(), adaID, validExperience()); !errors.Is(err, errDatabase) {
			t.Errorf("err = %v, want the store error", err)
		}
	})
}

func TestUpdateExperience(t *testing.T) {
	// stored is Ada's finished role from validExperience: March 2020 to August 2023.
	setup := func(t *testing.T) (*fakeStore, *Service, domain.Experience) {
		t.Helper()
		store := newFakeStore(testAccounts()...)
		svc := newExperienceService(store)
		stored, err := svc.AddExperience(t.Context(), adaID, validExperience())
		if err != nil {
			t.Fatal(err)
		}
		return store, svc, stored
	}

	tests := []struct {
		name  string
		patch ExperiencePatch
		want  func(e *domain.Experience) // the change to the stored entry
	}{
		{"empty patch changes nothing", ExperiencePatch{}, func(*domain.Experience) {}},
		{"only the title, trimmed", ExperiencePatch{Title: set(" Lab Manager ")}, func(e *domain.Experience) { e.Title = "Lab Manager" }},
		{"organization and employment type", ExperiencePatch{Organization: set("BRAC"), EmploymentType: set(domain.EmploymentContract)}, func(e *domain.Experience) {
			e.Organization, e.EmploymentType = "BRAC", domain.EmploymentContract
		}},
		{"null location clears it", ExperiencePatch{Location: null[string]()}, func(e *domain.Experience) { e.Location = nil }},
		{"blank location clears it", ExperiencePatch{Location: set("  ")}, func(e *domain.Experience) { e.Location = nil }},
		{"null location type clears it", ExperiencePatch{LocationType: null[domain.LocationType]()}, func(e *domain.Experience) { e.LocationType = nil }},
		{"location type changes", ExperiencePatch{LocationType: set(domain.LocationRemote)}, func(e *domain.Experience) { e.LocationType = ltptr(domain.LocationRemote) }},
		{"null description clears it", ExperiencePatch{Description: null[string]()}, func(e *domain.Experience) { e.Description = nil }},
		{"current with a null end date", ExperiencePatch{IsCurrent: set(true), End: null[domain.YearMonth]()}, func(e *domain.Experience) {
			e.IsCurrent, e.End = true, nil
		}},
		{"start up to the stored end", ExperiencePatch{Start: set(ym(2023, 8))}, func(e *domain.Experience) { e.Start = ym(2023, 8) }},
		{"end down to the stored start", ExperiencePatch{End: set(ym(2020, 3))}, func(e *domain.Experience) { e.End = ymptr(2020, 3) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, svc, stored := setup(t)
			got, err := svc.UpdateExperience(t.Context(), adaID, stored.ID, tt.patch)
			if err != nil {
				t.Fatal(err)
			}
			want := stored
			tt.want(&want)
			if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(store.experience[stored.ID], want) {
				t.Errorf("got %+v, stored %+v; want %+v", got, store.experience[stored.ID], want)
			}
		})
	}

	t.Run("a current role is finished by sending isCurrent false and an end date", func(t *testing.T) {
		store := newFakeStore(testAccounts()...)
		svc := newExperienceService(store)
		stored, err := svc.AddExperience(t.Context(), adaID, currentExperience())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.UpdateExperience(t.Context(), adaID, stored.ID, ExperiencePatch{IsCurrent: set(false)}); !errors.Is(err, domain.ErrEndDateRequired) {
			t.Fatalf("isCurrent false alone: err = %v, want ErrEndDateRequired", err)
		}
		if _, err := svc.UpdateExperience(t.Context(), adaID, stored.ID, ExperiencePatch{End: set(ym(2025, 5))}); !errors.Is(err, domain.ErrEndDateNotAllowed) {
			t.Fatalf("end date alone: err = %v, want ErrEndDateNotAllowed", err)
		}
		got, err := svc.UpdateExperience(t.Context(), adaID, stored.ID, ExperiencePatch{IsCurrent: set(false), End: set(ym(2025, 5))})
		if err != nil {
			t.Fatal(err)
		}
		if got.IsCurrent || !reflect.DeepEqual(got.End, ymptr(2025, 5)) {
			t.Errorf("got isCurrent %v, end %+v", got.IsCurrent, got.End)
		}
	})

	refused := []struct {
		name    string
		patch   ExperiencePatch
		wantErr error
	}{
		{"null title", ExperiencePatch{Title: null[string]()}, domain.ErrInvalidTitle},
		{"blank title", ExperiencePatch{Title: set(" ")}, domain.ErrInvalidTitle},
		{"null organization", ExperiencePatch{Organization: null[string]()}, domain.ErrInvalidOrganization},
		{"organization of 151 characters", ExperiencePatch{Organization: set(strings.Repeat("a", 151))}, domain.ErrInvalidOrganization},
		{"null employment type", ExperiencePatch{EmploymentType: null[domain.EmploymentType]()}, domain.ErrInvalidEmploymentType},
		{"unknown employment type", ExperiencePatch{EmploymentType: set[domain.EmploymentType]("gig")}, domain.ErrInvalidEmploymentType},
		{"location with a line break", ExperiencePatch{Location: set("a\nb")}, domain.ErrInvalidLocation},
		{"unknown location type", ExperiencePatch{LocationType: set[domain.LocationType]("office")}, domain.ErrInvalidLocationType},
		{"null start date", ExperiencePatch{Start: null[domain.YearMonth]()}, domain.ErrInvalidStartDate},
		{"start in the future", ExperiencePatch{Start: set(ym(2026, 7))}, domain.ErrInvalidStartDate},
		{"end in the future", ExperiencePatch{End: set(ym(2026, 7))}, domain.ErrInvalidEndDate},
		{"null isCurrent", ExperiencePatch{IsCurrent: null[bool]()}, domain.ErrInvalidIsCurrent},
		// The server never clears the end date itself.
		{"isCurrent true keeps the stored end date", ExperiencePatch{IsCurrent: set(true)}, domain.ErrEndDateNotAllowed},
		{"null end date on a finished role", ExperiencePatch{End: null[domain.YearMonth]()}, domain.ErrEndDateRequired},
		// endDate >= startDate holds for the entry after the change, whichever side changes.
		{"end before the stored start", ExperiencePatch{End: set(ym(2020, 2))}, domain.ErrInvalidDateRange},
		{"start after the stored end", ExperiencePatch{Start: set(ym(2023, 9))}, domain.ErrInvalidDateRange},
		{"description of 2001 characters", ExperiencePatch{Description: set(strings.Repeat("a", 2001))}, domain.ErrInvalidDescription},
	}
	for _, tt := range refused {
		t.Run(tt.name, func(t *testing.T) {
			store, svc, stored := setup(t)
			if _, err := svc.UpdateExperience(t.Context(), adaID, stored.ID, tt.patch); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got := store.experience[stored.ID]; !reflect.DeepEqual(got, stored) {
				t.Errorf("stored %+v, want it unchanged %+v", got, stored)
			}
		})
	}

	// Someone else's entry gives the same ErrNotFound as one that does not exist, even
	// for a patch that would be refused, so the answer reveals nothing.
	for name, patch := range map[string]ExperiencePatch{
		"valid patch":   {Title: set("Lab Manager")},
		"invalid patch": {Title: null[string]()},
	} {
		t.Run("someone else's entry, "+name, func(t *testing.T) {
			store, svc, stored := setup(t)
			if _, err := svc.UpdateExperience(t.Context(), bobID, stored.ID, patch); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
			if got := store.experience[stored.ID]; !reflect.DeepEqual(got, stored) {
				t.Errorf("stored %+v, want it unchanged %+v", got, stored)
			}
		})
		t.Run("unknown entry, "+name, func(t *testing.T) {
			_, svc, _ := setup(t)
			if _, err := svc.UpdateExperience(t.Context(), adaID, "not-a-uuid", patch); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestRemoveExperience(t *testing.T) {
	store := newFakeStore(testAccounts()...)
	svc := newExperienceService(store)
	stored, err := svc.AddExperience(t.Context(), adaID, validExperience())
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.RemoveExperience(t.Context(), bobID, stored.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("someone else's entry: err = %v, want ErrNotFound", err)
	}
	if _, ok := store.experience[stored.ID]; !ok {
		t.Fatal("someone else removed Ada's entry")
	}
	if err := svc.RemoveExperience(t.Context(), adaID, "not-a-uuid"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown entry: err = %v, want ErrNotFound", err)
	}
	if err := svc.RemoveExperience(t.Context(), adaID, stored.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.experience[stored.ID]; ok {
		t.Error("entry still stored after removal")
	}
	if err := svc.RemoveExperience(t.Context(), adaID, stored.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("removed twice: err = %v, want ErrNotFound", err)
	}
}

// GetOwn and View include the entries in profile order (FR-4.8, FR-4.5): current roles
// first, then the latest start first, then the oldest entry first.
func TestProfileHasExperience(t *testing.T) {
	store := newFakeStore(testAccounts()...)
	svc := newExperienceService(store)
	add := func(start domain.YearMonth, end *domain.YearMonth) domain.Experience {
		t.Helper()
		e, err := svc.AddExperience(t.Context(), adaID, ExperienceInput{
			Title: "Role", Organization: "Org", EmploymentType: domain.EmploymentFullTime, Start: start, End: end, IsCurrent: end == nil,
		})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	oldPast := add(ym(2010, 1), ymptr(2026, 1)) // the latest end, but the earliest start
	pastA := add(ym(2018, 5), ymptr(2019, 1))
	currentOld := add(ym(2015, 1), nil)
	pastB := add(ym(2018, 5), ymptr(2020, 1)) // same start as pastA, added after it
	pastLate := add(ym(2018, 9), ymptr(2018, 12))
	currentNew := add(ym(2025, 2), nil)
	want := []domain.Experience{currentNew, currentOld, pastLate, pastA, pastB, oldPast}

	own, err := svc.GetOwn(t.Context(), adaID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(own.Experience, want) {
		t.Errorf("GetOwn experience = %+v, want %+v", own.Experience, want)
	}
	viewed, err := svc.View(t.Context(), eveID, adaID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(viewed.Experience, want) {
		t.Errorf("View experience = %+v, want %+v", viewed.Experience, want)
	}
	// Another user's profile does not show Ada's entries.
	eve, err := svc.View(t.Context(), adaID, eveID)
	if err != nil {
		t.Fatal(err)
	}
	if len(eve.Experience) != 0 {
		t.Errorf("Eve's profile has %d experience entries, want none", len(eve.Experience))
	}
}
