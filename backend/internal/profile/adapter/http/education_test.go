package http

import (
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/usecase"
)

const (
	educationID   = "00000000-0000-0000-0000-0000000000e1"
	educationPath = "/api/v1/profiles/me/education"
	entryPath     = educationPath + "/" + educationID
)

func iptr(n int) *int { return &n }

var phd = domain.Education{
	ID: educationID, UserID: callerID, Institution: "MIT", Degree: ptr("PhD"), StartYear: 2019,
}

// phdBody is phd as JSON: every field present, null when not set.
var phdBody = map[string]any{
	"id": educationID, "institution": "MIT", "degree": "PhD", "fieldOfStudy": nil,
	"startYear": float64(2019), "endYear": nil,
}

// The validation errors in the contract's order, and the codes they become.
var educationInputErrors = []struct {
	err  error
	code string
}{
	{domain.ErrInvalidEducationInstitution, CodeInvalidInstitution},
	{domain.ErrInvalidDegree, CodeInvalidDegree},
	{domain.ErrInvalidFieldOfStudy, CodeInvalidFieldOfStudy},
	{domain.ErrInvalidStartYear, CodeInvalidStartYear},
	{domain.ErrInvalidEndYear, CodeInvalidEndYear},
}

func TestAddMyEducation(t *testing.T) {
	t.Run("created: 201 with the stored entry", func(t *testing.T) {
		svc := &fakeService{education: phd}
		rec := send(t, svc, http.MethodPost, educationPath,
			`{"institution":" MIT ","degree":"PhD","startYear":2019,"endYear":null}`, callerID)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body %s", rec.Code, rec.Body)
		}
		want := usecase.EducationInput{Institution: " MIT ", Degree: ptr("PhD"), StartYear: 2019}
		if svc.userID != callerID || !reflect.DeepEqual(svc.educationIn, want) {
			t.Errorf("service got user %q, %+v; want %q, %+v", svc.userID, svc.educationIn, callerID, want)
		}
		if got := decode(t, rec); !reflect.DeepEqual(got, phdBody) {
			t.Errorf("body = %v, want %v", got, phdBody)
		}
	})

	tests := []struct {
		name     string
		err      error
		status   int
		code     string
		noCaller bool
	}{
		{"limit reached", domain.ErrEducationLimitReached, http.StatusConflict, CodeEducationLimitReached, false},
		{"account no longer exists", domain.ErrNotFound, http.StatusUnauthorized, httpx.CodeUnauthenticated, false},
		{"no user in the context", nil, http.StatusUnauthorized, httpx.CodeUnauthenticated, true},
	}
	for _, e := range educationInputErrors {
		tests = append(tests, struct {
			name     string
			err      error
			status   int
			code     string
			noCaller bool
		}{e.code, e.err, http.StatusBadRequest, e.code, false})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caller := callerID
			if tt.noCaller {
				caller = ""
			}
			rec := send(t, &fakeService{err: tt.err}, http.MethodPost, educationPath, `{"institution":"MIT","startYear":2019}`, caller)
			got := decode(t, rec)
			if rec.Code != tt.status || got["code"] != tt.code || got["message"] == "" {
				t.Errorf("got %d %v, want %d %s with a message", rec.Code, got, tt.status, tt.code)
			}
		})
	}

	t.Run("limit message names the limit", func(t *testing.T) {
		rec := send(t, &fakeService{err: domain.ErrEducationLimitReached}, http.MethodPost, educationPath, `{"institution":"MIT","startYear":2019}`, callerID)
		if got := decode(t, rec)["message"]; got != "You can add at most 20 education entries." {
			t.Errorf("message = %q", got)
		}
	})

	t.Run("unexpected error", func(t *testing.T) {
		rec := send(t, &fakeService{err: errors.New("boom")}, http.MethodPost, educationPath, `{"institution":"MIT","startYear":2019}`, callerID)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rec.Code)
		}
	})

	t.Run("body is not JSON", func(t *testing.T) {
		svc := &fakeService{}
		rec := send(t, svc, http.MethodPost, educationPath, `{`, callerID)
		if rec.Code != http.StatusBadRequest || svc.userID != "" {
			t.Errorf("got %d, service called for %q; want 400 and no call", rec.Code, svc.userID)
		}
	})
}

func TestUpdateMyEducation(t *testing.T) {
	t.Run("missing, null and set fields reach the usecase apart", func(t *testing.T) {
		svc := &fakeService{education: phd}
		rec := send(t, svc, http.MethodPatch, entryPath,
			`{"degree":null,"fieldOfStudy":"Physics","endYear":null,"startYear":2018}`, callerID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
		}
		want := usecase.EducationPatch{
			Degree:       usecase.Field[string]{Set: true, Null: true},
			FieldOfStudy: usecase.Field[string]{Set: true, Value: "Physics"},
			StartYear:    usecase.Field[int]{Set: true, Value: 2018},
			EndYear:      usecase.Field[int]{Set: true, Null: true},
		}
		if svc.userID != callerID || svc.educationID != educationID || !reflect.DeepEqual(svc.educationPatch, want) {
			t.Errorf("service got user %q, id %q, %+v; want %+v", svc.userID, svc.educationID, svc.educationPatch, want)
		}
		if got := decode(t, rec); !reflect.DeepEqual(got, phdBody) {
			t.Errorf("body = %v, want %v", got, phdBody)
		}
	})

	t.Run("null institution and start year are passed on as null", func(t *testing.T) {
		svc := &fakeService{education: phd}
		send(t, svc, http.MethodPatch, entryPath, `{"institution":null,"startYear":null}`, callerID)
		want := usecase.EducationPatch{
			Institution: usecase.Field[string]{Set: true, Null: true},
			StartYear:   usecase.Field[int]{Set: true, Null: true},
		}
		if !reflect.DeepEqual(svc.educationPatch, want) {
			t.Errorf("patch = %+v, want %+v", svc.educationPatch, want)
		}
	})

	t.Run("empty body changes nothing", func(t *testing.T) {
		svc := &fakeService{education: phd}
		rec := send(t, svc, http.MethodPatch, entryPath, `{}`, callerID)
		if rec.Code != http.StatusOK || !reflect.DeepEqual(svc.educationPatch, usecase.EducationPatch{}) {
			t.Errorf("got %d, patch %+v; want 200 and an empty patch", rec.Code, svc.educationPatch)
		}
	})

	// Someone else's entry and one that does not exist look the same.
	t.Run("not found: 404", func(t *testing.T) {
		rec := send(t, &fakeService{err: domain.ErrNotFound}, http.MethodPatch, entryPath, `{"degree":"MSc"}`, callerID)
		if got := decode(t, rec); rec.Code != http.StatusNotFound || got["code"] != httpx.CodeNotFound {
			t.Errorf("got %d %v, want 404 not_found", rec.Code, got)
		}
	})

	for _, e := range educationInputErrors {
		t.Run(e.code, func(t *testing.T) {
			rec := send(t, &fakeService{err: e.err}, http.MethodPatch, entryPath, `{"degree":"MSc"}`, callerID)
			if got := decode(t, rec); rec.Code != http.StatusBadRequest || got["code"] != e.code || got["message"] == "" {
				t.Errorf("got %d %v, want 400 %s with a message", rec.Code, got, e.code)
			}
		})
	}

	t.Run("no user in the context", func(t *testing.T) {
		rec := send(t, &fakeService{}, http.MethodPatch, entryPath, `{}`, "")
		if got := decode(t, rec); rec.Code != http.StatusUnauthorized || got["code"] != httpx.CodeUnauthenticated {
			t.Errorf("got %d %v, want 401", rec.Code, got)
		}
	})

	t.Run("unexpected error", func(t *testing.T) {
		rec := send(t, &fakeService{err: errors.New("boom")}, http.MethodPatch, entryPath, `{}`, callerID)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rec.Code)
		}
	})

	t.Run("body is not JSON", func(t *testing.T) {
		svc := &fakeService{}
		rec := send(t, svc, http.MethodPatch, entryPath, `{`, callerID)
		if rec.Code != http.StatusBadRequest || svc.userID != "" {
			t.Errorf("got %d, service called for %q; want 400 and no call", rec.Code, svc.userID)
		}
	})
}

func TestRemoveMyEducation(t *testing.T) {
	t.Run("removed: 204", func(t *testing.T) {
		svc := &fakeService{}
		rec := send(t, svc, http.MethodDelete, entryPath, "", callerID)
		if rec.Code != http.StatusNoContent || svc.userID != callerID || svc.educationID != educationID {
			t.Errorf("got %d, service got user %q, id %q", rec.Code, svc.userID, svc.educationID)
		}
	})

	t.Run("not found: 404", func(t *testing.T) {
		rec := send(t, &fakeService{err: domain.ErrNotFound}, http.MethodDelete, entryPath, "", callerID)
		if got := decode(t, rec); rec.Code != http.StatusNotFound || got["code"] != httpx.CodeNotFound {
			t.Errorf("got %d %v, want 404 not_found", rec.Code, got)
		}
	})

	t.Run("no user in the context", func(t *testing.T) {
		rec := send(t, &fakeService{}, http.MethodDelete, entryPath, "", "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("unexpected error", func(t *testing.T) {
		rec := send(t, &fakeService{err: errors.New("boom")}, http.MethodDelete, entryPath, "", callerID)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rec.Code)
		}
	})
}

// GET /profiles/me returns the entries in the order the usecase gives them (FR-4.4).
func TestProfileBodyHasEducation(t *testing.T) {
	p := ada
	current := domain.Education{ID: otherID, UserID: callerID, Institution: "ETH", FieldOfStudy: ptr("CS"), StartYear: 2023}
	done := phd
	done.EndYear = iptr(2023)
	p.Education = []domain.Education{current, done}
	rec := send(t, &fakeService{profile: p}, http.MethodGet, "/api/v1/profiles/me", "", callerID)
	want := []any{
		map[string]any{"id": otherID, "institution": "ETH", "degree": nil, "fieldOfStudy": "CS", "startYear": float64(2023), "endYear": nil},
		map[string]any{"id": educationID, "institution": "MIT", "degree": "PhD", "fieldOfStudy": nil, "startYear": float64(2019), "endYear": float64(2023)},
	}
	if got := decode(t, rec)["education"]; !reflect.DeepEqual(got, want) {
		t.Errorf("education = %v, want %v", got, want)
	}
}
