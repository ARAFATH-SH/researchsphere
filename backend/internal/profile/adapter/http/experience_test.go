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
	experienceID        = "00000000-0000-0000-0000-0000000000f1"
	experiencePath      = "/api/v1/profiles/me/experience"
	experienceEntryPath = experiencePath + "/" + experienceID
	experienceJSON      = `{"title":"RA","organization":"MIT","employmentType":"full_time","startDate":{"year":2020,"month":1},"isCurrent":true}`
)

var remote = domain.LocationRemote

var postdoc = domain.Experience{
	ID: experienceID, UserID: callerID, Title: "Postdoc", Organization: "MIT", EmploymentType: domain.EmploymentPostdoc,
	LocationType: &remote, Start: domain.YearMonth{Year: 2024, Month: 1}, IsCurrent: true,
}

// postdocBody is postdoc as JSON: every field present, null when not set.
var postdocBody = map[string]any{
	"id": experienceID, "title": "Postdoc", "organization": "MIT", "employmentType": "postdoc",
	"location": nil, "locationType": "remote", "startDate": map[string]any{"year": float64(2024), "month": float64(1)},
	"endDate": nil, "isCurrent": true, "description": nil,
}

// The validation errors in the contract's order, and the codes they become.
var experienceInputErrors = []struct {
	err  error
	code string
}{
	{domain.ErrInvalidTitle, CodeInvalidTitle},
	{domain.ErrInvalidOrganization, CodeInvalidOrganization},
	{domain.ErrInvalidEmploymentType, CodeInvalidEmploymentType},
	{domain.ErrInvalidLocation, CodeInvalidLocation},
	{domain.ErrInvalidLocationType, CodeInvalidLocationType},
	{domain.ErrInvalidStartDate, CodeInvalidStartDate},
	{domain.ErrInvalidEndDate, CodeInvalidEndDate},
	{domain.ErrInvalidIsCurrent, CodeInvalidIsCurrent},
	{domain.ErrEndDateRequired, CodeEndDateRequired},
	{domain.ErrEndDateNotAllowed, CodeEndDateNotAllowed},
	{domain.ErrInvalidDateRange, CodeInvalidDateRange},
	{domain.ErrInvalidDescription, CodeInvalidDescription},
}

func TestAddMyExperience(t *testing.T) {
	t.Run("created: 201 with the stored entry", func(t *testing.T) {
		svc := &fakeService{experience: postdoc}
		rec := send(t, svc, http.MethodPost, experiencePath, `{"title":" Postdoc ","organization":"MIT","employmentType":"postdoc",
			"location":"Boston","locationType":"remote","startDate":{"year":2024,"month":1},"endDate":{"year":2025,"month":2},
			"isCurrent":true,"description":"Did research."}`, callerID)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body %s", rec.Code, rec.Body)
		}
		want := usecase.ExperienceInput{
			Title: " Postdoc ", Organization: "MIT", EmploymentType: domain.EmploymentPostdoc, Location: ptr("Boston"),
			LocationType: &remote, Start: domain.YearMonth{Year: 2024, Month: 1}, End: &domain.YearMonth{Year: 2025, Month: 2},
			IsCurrent: true, Description: ptr("Did research."),
		}
		if svc.userID != callerID || !reflect.DeepEqual(svc.experienceIn, want) {
			t.Errorf("service got user %q, %+v; want %q, %+v", svc.userID, svc.experienceIn, callerID, want)
		}
		if got := decode(t, rec); !reflect.DeepEqual(got, postdocBody) {
			t.Errorf("body = %v, want %v", got, postdocBody)
		}
	})

	t.Run("missing optional fields reach the usecase as nil", func(t *testing.T) {
		svc := &fakeService{experience: postdoc}
		send(t, svc, http.MethodPost, experiencePath, experienceJSON, callerID)
		want := usecase.ExperienceInput{
			Title: "RA", Organization: "MIT", EmploymentType: domain.EmploymentFullTime,
			Start: domain.YearMonth{Year: 2020, Month: 1}, IsCurrent: true,
		}
		if !reflect.DeepEqual(svc.experienceIn, want) {
			t.Errorf("input = %+v, want %+v", svc.experienceIn, want)
		}
	})

	tests := []struct {
		name     string
		err      error
		status   int
		code     string
		noCaller bool
	}{
		{"limit reached", domain.ErrExperienceLimitReached, http.StatusConflict, CodeExperienceLimitReached, false},
		{"account no longer exists", domain.ErrNotFound, http.StatusUnauthorized, httpx.CodeUnauthenticated, false},
		{"no user in the context", nil, http.StatusUnauthorized, httpx.CodeUnauthenticated, true},
	}
	for _, e := range experienceInputErrors {
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
			rec := send(t, &fakeService{err: tt.err}, http.MethodPost, experiencePath, experienceJSON, caller)
			got := decode(t, rec)
			if rec.Code != tt.status || got["code"] != tt.code || got["message"] == "" {
				t.Errorf("got %d %v, want %d %s with a message", rec.Code, got, tt.status, tt.code)
			}
		})
	}

	t.Run("limit message names the limit", func(t *testing.T) {
		rec := send(t, &fakeService{err: domain.ErrExperienceLimitReached}, http.MethodPost, experiencePath, experienceJSON, callerID)
		if got := decode(t, rec)["message"]; got != "You can add at most 30 experience entries." {
			t.Errorf("message = %q", got)
		}
	})

	t.Run("unexpected error", func(t *testing.T) {
		rec := send(t, &fakeService{err: errors.New("boom")}, http.MethodPost, experiencePath, experienceJSON, callerID)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rec.Code)
		}
	})

	t.Run("body is not JSON", func(t *testing.T) {
		svc := &fakeService{}
		rec := send(t, svc, http.MethodPost, experiencePath, `{`, callerID)
		if rec.Code != http.StatusBadRequest || svc.userID != "" {
			t.Errorf("got %d, service called for %q; want 400 and no call", rec.Code, svc.userID)
		}
	})
}

func TestUpdateMyExperience(t *testing.T) {
	t.Run("missing, null and set fields reach the usecase apart", func(t *testing.T) {
		svc := &fakeService{experience: postdoc}
		rec := send(t, svc, http.MethodPatch, experienceEntryPath, `{"title":"Lead","location":null,"locationType":"hybrid",
			"endDate":null,"isCurrent":true,"startDate":{"year":2023,"month":4},"description":null}`, callerID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
		}
		want := usecase.ExperiencePatch{
			Title:        usecase.Field[string]{Set: true, Value: "Lead"},
			Location:     usecase.Field[string]{Set: true, Null: true},
			LocationType: usecase.Field[domain.LocationType]{Set: true, Value: domain.LocationHybrid},
			Start:        usecase.Field[domain.YearMonth]{Set: true, Value: domain.YearMonth{Year: 2023, Month: 4}},
			End:          usecase.Field[domain.YearMonth]{Set: true, Null: true},
			IsCurrent:    usecase.Field[bool]{Set: true, Value: true},
			Description:  usecase.Field[string]{Set: true, Null: true},
		}
		if svc.userID != callerID || svc.experienceID != experienceID || !reflect.DeepEqual(svc.experiencePatch, want) {
			t.Errorf("service got user %q, id %q, %+v; want %+v", svc.userID, svc.experienceID, svc.experiencePatch, want)
		}
		if got := decode(t, rec); !reflect.DeepEqual(got, postdocBody) {
			t.Errorf("body = %v, want %v", got, postdocBody)
		}
	})

	t.Run("null required fields are passed on as null", func(t *testing.T) {
		svc := &fakeService{experience: postdoc}
		send(t, svc, http.MethodPatch, experienceEntryPath,
			`{"title":null,"organization":null,"employmentType":null,"startDate":null,"isCurrent":null}`, callerID)
		want := usecase.ExperiencePatch{
			Title:          usecase.Field[string]{Set: true, Null: true},
			Organization:   usecase.Field[string]{Set: true, Null: true},
			EmploymentType: usecase.Field[domain.EmploymentType]{Set: true, Null: true},
			Start:          usecase.Field[domain.YearMonth]{Set: true, Null: true},
			IsCurrent:      usecase.Field[bool]{Set: true, Null: true},
		}
		if !reflect.DeepEqual(svc.experiencePatch, want) {
			t.Errorf("patch = %+v, want %+v", svc.experiencePatch, want)
		}
	})

	t.Run("empty body changes nothing", func(t *testing.T) {
		svc := &fakeService{experience: postdoc}
		rec := send(t, svc, http.MethodPatch, experienceEntryPath, `{}`, callerID)
		if rec.Code != http.StatusOK || !reflect.DeepEqual(svc.experiencePatch, usecase.ExperiencePatch{}) {
			t.Errorf("got %d, patch %+v; want 200 and an empty patch", rec.Code, svc.experiencePatch)
		}
	})

	// Someone else's entry and one that does not exist look the same.
	t.Run("not found: 404", func(t *testing.T) {
		rec := send(t, &fakeService{err: domain.ErrNotFound}, http.MethodPatch, experienceEntryPath, `{"title":"Lead"}`, callerID)
		if got := decode(t, rec); rec.Code != http.StatusNotFound || got["code"] != httpx.CodeNotFound {
			t.Errorf("got %d %v, want 404 not_found", rec.Code, got)
		}
	})

	for _, e := range experienceInputErrors {
		t.Run(e.code, func(t *testing.T) {
			rec := send(t, &fakeService{err: e.err}, http.MethodPatch, experienceEntryPath, `{"title":"Lead"}`, callerID)
			if got := decode(t, rec); rec.Code != http.StatusBadRequest || got["code"] != e.code || got["message"] == "" {
				t.Errorf("got %d %v, want 400 %s with a message", rec.Code, got, e.code)
			}
		})
	}

	t.Run("no user in the context", func(t *testing.T) {
		rec := send(t, &fakeService{}, http.MethodPatch, experienceEntryPath, `{}`, "")
		if got := decode(t, rec); rec.Code != http.StatusUnauthorized || got["code"] != httpx.CodeUnauthenticated {
			t.Errorf("got %d %v, want 401", rec.Code, got)
		}
	})

	t.Run("unexpected error", func(t *testing.T) {
		rec := send(t, &fakeService{err: errors.New("boom")}, http.MethodPatch, experienceEntryPath, `{}`, callerID)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rec.Code)
		}
	})

	t.Run("body is not JSON", func(t *testing.T) {
		svc := &fakeService{}
		rec := send(t, svc, http.MethodPatch, experienceEntryPath, `{`, callerID)
		if rec.Code != http.StatusBadRequest || svc.userID != "" {
			t.Errorf("got %d, service called for %q; want 400 and no call", rec.Code, svc.userID)
		}
	})
}

func TestRemoveMyExperience(t *testing.T) {
	t.Run("removed: 204", func(t *testing.T) {
		svc := &fakeService{}
		rec := send(t, svc, http.MethodDelete, experienceEntryPath, "", callerID)
		if rec.Code != http.StatusNoContent || svc.userID != callerID || svc.experienceID != experienceID {
			t.Errorf("got %d, service got user %q, id %q", rec.Code, svc.userID, svc.experienceID)
		}
	})

	t.Run("not found: 404", func(t *testing.T) {
		rec := send(t, &fakeService{err: domain.ErrNotFound}, http.MethodDelete, experienceEntryPath, "", callerID)
		if got := decode(t, rec); rec.Code != http.StatusNotFound || got["code"] != httpx.CodeNotFound {
			t.Errorf("got %d %v, want 404 not_found", rec.Code, got)
		}
	})

	t.Run("no user in the context", func(t *testing.T) {
		rec := send(t, &fakeService{}, http.MethodDelete, experienceEntryPath, "", "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("unexpected error", func(t *testing.T) {
		rec := send(t, &fakeService{err: errors.New("boom")}, http.MethodDelete, experienceEntryPath, "", callerID)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rec.Code)
		}
	})
}

// GET /profiles/me returns the entries in the order the usecase gives them (FR-4.8).
func TestProfileBodyHasExperience(t *testing.T) {
	p := ada
	done := domain.Experience{
		ID: otherID, UserID: callerID, Title: "RA", Organization: "BUET", EmploymentType: domain.EmploymentResearchAssistant,
		Location: ptr("Dhaka"), Start: domain.YearMonth{Year: 2019, Month: 9}, End: &domain.YearMonth{Year: 2023, Month: 6},
		Description: ptr("Line one\nline two"),
	}
	p.Experience = []domain.Experience{postdoc, done}
	rec := send(t, &fakeService{profile: p}, http.MethodGet, "/api/v1/profiles/me", "", callerID)
	want := []any{
		postdocBody,
		map[string]any{
			"id": otherID, "title": "RA", "organization": "BUET", "employmentType": "research_assistant",
			"location": "Dhaka", "locationType": nil, "startDate": map[string]any{"year": float64(2019), "month": float64(9)},
			"endDate": map[string]any{"year": float64(2023), "month": float64(6)}, "isCurrent": false,
			"description": "Line one\nline two",
		},
	}
	if got := decode(t, rec)["experience"]; !reflect.DeepEqual(got, want) {
		t.Errorf("experience = %v, want %v", got, want)
	}
}
