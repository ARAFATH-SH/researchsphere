package usecase

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

func TestGetOwn(t *testing.T) {
	tests := []struct {
		name    string
		userID  string
		want    domain.Profile
		wantErr error
	}{
		{"profile row exists", adaID, *testAccounts()[0].Profile, nil},
		{"no profile row gives an empty profile, not an error", bobID, domain.Profile{UserID: bobID}, nil},
		// The caller's own token is still valid for up to 15 minutes after suspension.
		{"suspended caller still reads their own profile", carlID, domain.Profile{UserID: carlID, FullName: "Carl Suspended"}, nil},
		{"account no longer exists", nobody, domain.Profile{}, domain.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := New(newFakeStore(testAccounts()...))
			got, err := svc.GetOwn(t.Context(), tt.userID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// FR-4.5: any logged-in user sees any profile, except that unknown, suspended and
// unverified accounts all look the same: not found.
func TestView(t *testing.T) {
	tests := []struct {
		name    string
		userID  string
		want    domain.Profile
		wantErr error
	}{
		{"another user's profile", adaID, *testAccounts()[0].Profile, nil},
		{"verified user without a profile row", bobID, domain.Profile{UserID: bobID}, nil},
		{"unknown user", nobody, domain.Profile{}, domain.ErrNotFound},
		{"malformed ID", "not-a-uuid", domain.Profile{}, domain.ErrNotFound},
		{"suspended user", carlID, domain.Profile{}, domain.ErrNotFound},
		{"unverified user", dianaID, domain.Profile{}, domain.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := New(newFakeStore(testAccounts()...))
			got, err := svc.View(t.Context(), tt.userID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestUpdateOwnCreatesAndUpdates(t *testing.T) {
	t.Run("create when there is no profile row", func(t *testing.T) {
		store := newFakeStore(testAccounts()...)
		svc := New(store)
		got, err := svc.UpdateOwn(t.Context(), bobID, UpdateInput{
			FullName: "Bob Builder", Headline: ptr("Structures"), Country: ptr("NL"),
		})
		if err != nil {
			t.Fatal(err)
		}
		want := domain.Profile{UserID: bobID, FullName: "Bob Builder", Headline: ptr("Structures"), Country: ptr("NL")}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
		if again, _ := svc.GetOwn(t.Context(), bobID); !reflect.DeepEqual(again, want) {
			t.Errorf("stored %+v, want %+v", again, want)
		}
	})

	t.Run("update replaces every field; missing optional fields become nil", func(t *testing.T) {
		store := newFakeStore(testAccounts()...)
		svc := New(store)
		got, err := svc.UpdateOwn(t.Context(), adaID, UpdateInput{FullName: "Augusta Ada King", Bio: ptr("Countess.")})
		if err != nil {
			t.Fatal(err)
		}
		want := domain.Profile{UserID: adaID, FullName: "Augusta Ada King", Bio: ptr("Countess.")}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("account no longer exists", func(t *testing.T) {
		svc := New(newFakeStore(testAccounts()...))
		if _, err := svc.UpdateOwn(t.Context(), nobody, UpdateInput{FullName: "Ghost"}); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("store failure is passed on", func(t *testing.T) {
		store := newFakeStore(testAccounts()...)
		store.err = errDatabase
		if _, err := New(store).UpdateOwn(t.Context(), adaID, UpdateInput{FullName: "Ada"}); !errors.Is(err, errDatabase) {
			t.Errorf("err = %v, want the store's error", err)
		}
	})
}

// Values are trimmed; blank optional values become nil; the country is upper-cased.
func TestUpdateOwnNormalizes(t *testing.T) {
	svc := New(newFakeStore(testAccounts()...))
	got, err := svc.UpdateOwn(t.Context(), eveID, UpdateInput{
		FullName:    "  Eve Example \t",
		Headline:    ptr("   "),
		Bio:         ptr("\n Line one\nline two \n"),
		Institution: ptr(""),
		Department:  nil,
		JobTitle:    ptr(" Lecturer "),
		Country:     ptr(" de "),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Profile{
		UserID: eveID, FullName: "Eve Example", Bio: ptr("Line one\nline two"),
		JobTitle: ptr("Lecturer"), Country: ptr("DE"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestUpdateOwnValidation(t *testing.T) {
	valid := func() UpdateInput { return UpdateInput{FullName: "Ada"} }
	tests := []struct {
		name    string
		edit    func(*UpdateInput)
		wantErr error
	}{
		// Full name: required, 1 to 200 characters after trimming, one line.
		{"full name empty", func(in *UpdateInput) { in.FullName = "" }, domain.ErrInvalidFullName},
		{"full name blank", func(in *UpdateInput) { in.FullName = " \t " }, domain.ErrInvalidFullName},
		{"full name 201 characters", func(in *UpdateInput) { in.FullName = strings.Repeat("é", 201) }, domain.ErrInvalidFullName},
		{"full name with a line break", func(in *UpdateInput) { in.FullName = "Ada\nLovelace" }, domain.ErrInvalidFullName},
		{"full name 200 characters", func(in *UpdateInput) { in.FullName = strings.Repeat("é", 200) }, nil},
		{"full name 200 characters plus spaces", func(in *UpdateInput) { in.FullName = "  " + strings.Repeat("a", 200) + "  " }, nil},
		// Headline: at most 220, one line.
		{"headline 221 characters", func(in *UpdateInput) { in.Headline = ptr(strings.Repeat("a", 221)) }, domain.ErrInvalidHeadline},
		{"headline with a line break", func(in *UpdateInput) { in.Headline = ptr("a\r\nb") }, domain.ErrInvalidHeadline},
		{"headline 220 characters", func(in *UpdateInput) { in.Headline = ptr(strings.Repeat("é", 220)) }, nil},
		// Bio: at most 2000, line breaks allowed.
		{"bio 2001 characters", func(in *UpdateInput) { in.Bio = ptr(strings.Repeat("a", 2001)) }, domain.ErrInvalidBio},
		{"bio 2000 characters with line breaks", func(in *UpdateInput) { in.Bio = ptr(strings.Repeat("é\n", 1000)) }, nil},
		// Institution, department, job title: at most 200, one line.
		{"institution 201 characters", func(in *UpdateInput) { in.Institution = ptr(strings.Repeat("a", 201)) }, domain.ErrInvalidInstitution},
		{"institution with a line break", func(in *UpdateInput) { in.Institution = ptr("a\nb") }, domain.ErrInvalidInstitution},
		{"department 201 characters", func(in *UpdateInput) { in.Department = ptr(strings.Repeat("a", 201)) }, domain.ErrInvalidDepartment},
		{"department with a line break", func(in *UpdateInput) { in.Department = ptr("a\nb") }, domain.ErrInvalidDepartment},
		{"job title 201 characters", func(in *UpdateInput) { in.JobTitle = ptr(strings.Repeat("a", 201)) }, domain.ErrInvalidJobTitle},
		{"job title with a line break", func(in *UpdateInput) { in.JobTitle = ptr("a\nb") }, domain.ErrInvalidJobTitle},
		{"institution, department, job title 200 characters", func(in *UpdateInput) {
			in.Institution, in.Department, in.JobTitle = ptr(strings.Repeat("é", 200)), ptr(strings.Repeat("é", 200)), ptr(strings.Repeat("é", 200))
		}, nil},
		// Country: an assigned ISO 3166-1 alpha-2 code.
		{"country unassigned code", func(in *UpdateInput) { in.Country = ptr("XX") }, domain.ErrInvalidCountry},
		{"country alpha-3 code", func(in *UpdateInput) { in.Country = ptr("DEU") }, domain.ErrInvalidCountry},
		{"country name", func(in *UpdateInput) { in.Country = ptr("Germany") }, domain.ErrInvalidCountry},
		{"country one letter", func(in *UpdateInput) { in.Country = ptr("D") }, domain.ErrInvalidCountry},
		{"country digits", func(in *UpdateInput) { in.Country = ptr("12") }, domain.ErrInvalidCountry},
		{"country lower case is accepted", func(in *UpdateInput) { in.Country = ptr("de") }, nil},
		// The first invalid field, in contract order, is reported.
		{"several invalid fields report the full name", func(in *UpdateInput) {
			in.FullName, in.Country = "", ptr("XX")
		}, domain.ErrInvalidFullName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore(testAccounts()...)
			in := valid()
			tt.edit(&in)
			_, err := New(store).UpdateOwn(t.Context(), adaID, in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil && store.upserts != 0 {
				t.Error("an invalid profile was stored")
			}
		})
	}
}

func TestCountryList(t *testing.T) {
	if n := len(countryCodes); n != 249 {
		t.Errorf("%d country codes, want the 249 assigned ISO 3166-1 alpha-2 codes", n)
	}
	for code := range countryCodes {
		if len(code) != 2 || strings.ToUpper(code) != code {
			t.Errorf("code %q is not two capital letters", code)
		}
	}
}
