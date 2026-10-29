package http

import (
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/usecase"
)

const (
	publicationID    = "00000000-0000-0000-0000-0000000000c1"
	publicationsPath = "/api/v1/profiles/me/publications"
	publicationPath  = publicationsPath + "/" + publicationID
	publicationJSON  = `{"title":"Notes","year":2024}`
)

var (
	year2024 = 2024
	stamp    = time.Date(2026, time.October, 28, 9, 30, 0, 0, time.UTC)
)

var notes = domain.Publication{
	ID: publicationID, DOI: ptr("10.1000/xyz"), Title: "Notes", Year: &year2024, Venue: ptr("KDD"),
	Source: domain.SourceManual, CreatedAt: stamp, UpdatedAt: stamp,
}

// notesBody is notes as JSON: every field present, null when not set.
var notesBody = map[string]any{
	"id": publicationID, "doi": "10.1000/xyz", "title": "Notes", "year": float64(2024), "venue": "KDD",
	"abstract": nil, "url": nil, "source": "manual",
	"createdAt": "2026-10-28T09:30:00Z", "updatedAt": "2026-10-28T09:30:00Z",
}

// The validation errors in the contract's order, and the codes they become.
var publicationInputErrors = []struct {
	err  error
	code string
}{
	{domain.ErrInvalidTitle, CodeInvalidTitle},
	{domain.ErrInvalidYear, CodeInvalidYear},
	{domain.ErrInvalidVenue, CodeInvalidVenue},
	{domain.ErrInvalidAbstract, CodeInvalidAbstract},
	{domain.ErrInvalidPublicationURL, CodeInvalidURL},
	{domain.ErrInvalidDOI, CodeInvalidDOI},
}

type errorCase struct {
	name     string
	err      error
	status   int
	code     string
	noCaller bool
}

func withInputErrors(cases []errorCase) []errorCase {
	for _, e := range publicationInputErrors {
		cases = append(cases, errorCase{e.code, e.err, http.StatusBadRequest, e.code, false})
	}
	return cases
}

func checkErrors(t *testing.T, cases []errorCase, method, path, body string) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			caller := callerID
			if tt.noCaller {
				caller = ""
			}
			rec := send(t, &fakeService{err: tt.err}, method, path, body, caller)
			got := decode(t, rec)
			if rec.Code != tt.status || got["code"] != tt.code || got["message"] == "" {
				t.Errorf("got %d %v, want %d %s with a message", rec.Code, got, tt.status, tt.code)
			}
		})
	}
}

func TestAddMyPublication(t *testing.T) {
	for _, linked := range []bool{false, true} {
		t.Run("201 with the record and linked", func(t *testing.T) {
			svc := &fakeService{publication: notes, linked: linked}
			rec := send(t, svc, http.MethodPost, publicationsPath, `{"title":" Notes ","year":2024,"venue":"KDD",
				"abstract":"Two\nlines","url":"https://example.org/p","doi":"https://doi.org/10.1000/XYZ"}`, callerID)
			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201; body %s", rec.Code, rec.Body)
			}
			year := 2024
			want := usecase.PublicationInput{
				Title: " Notes ", Year: &year, Venue: ptr("KDD"), Abstract: ptr("Two\nlines"),
				URL: ptr("https://example.org/p"), DOI: ptr("https://doi.org/10.1000/XYZ"),
			}
			if svc.userID != callerID || !reflect.DeepEqual(svc.publicationIn, want) {
				t.Errorf("service got user %q, %+v; want %q, %+v", svc.userID, svc.publicationIn, callerID, want)
			}
			wantBody := map[string]any{"linked": linked}
			for k, v := range notesBody {
				wantBody[k] = v
			}
			if got := decode(t, rec); !reflect.DeepEqual(got, wantBody) {
				t.Errorf("body = %v, want %v", got, wantBody)
			}
		})
	}

	t.Run("a missing year reaches the usecase as 0, which it refuses", func(t *testing.T) {
		svc := &fakeService{publication: notes}
		send(t, svc, http.MethodPost, publicationsPath, `{"title":"Notes"}`, callerID)
		if svc.publicationIn.Year == nil || *svc.publicationIn.Year != 0 || svc.publicationIn.Venue != nil || svc.publicationIn.DOI != nil {
			t.Errorf("input = %+v, want year 0 and nil optional fields", svc.publicationIn)
		}
	})

	t.Run("a body that is not JSON", func(t *testing.T) {
		if rec := send(t, &fakeService{}, http.MethodPost, publicationsPath, `{`, callerID); rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	checkErrors(t, withInputErrors([]errorCase{
		{"limit reached", domain.ErrPublicationLimitReached, http.StatusConflict, CodePublicationLimitReached, false},
		{"already added", domain.ErrPublicationAlreadyAdded, http.StatusConflict, CodePublicationAlreadyAdded, false},
		{"account no longer exists", domain.ErrNotFound, http.StatusUnauthorized, httpx.CodeUnauthenticated, false},
		{"no user in the context", nil, http.StatusUnauthorized, httpx.CodeUnauthenticated, true},
	}), http.MethodPost, publicationsPath, publicationJSON)
}

func TestUpdateMyPublication(t *testing.T) {
	t.Run("200 with the record; missing, null and set fields reach the usecase", func(t *testing.T) {
		svc := &fakeService{publication: notes}
		rec := send(t, svc, http.MethodPatch, publicationPath, `{"title":"New","year":2020,"venue":null}`, callerID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
		}
		want := usecase.PublicationPatch{
			Title: usecase.Field[string]{Set: true, Value: "New"},
			Year:  usecase.Field[int]{Set: true, Value: 2020},
			Venue: usecase.Field[string]{Set: true, Null: true},
		}
		if svc.publicationID != publicationID || !reflect.DeepEqual(svc.publicationPatch, want) {
			t.Errorf("service got %q, %+v; want %q, %+v", svc.publicationID, svc.publicationPatch, publicationID, want)
		}
		if got := decode(t, rec); !reflect.DeepEqual(got, notesBody) {
			t.Errorf("body = %v, want %v", got, notesBody)
		}
	})

	checkErrors(t, withInputErrors([]errorCase{
		{"not linked", domain.ErrNotFound, http.StatusNotFound, httpx.CodeNotFound, false},
		{"shared or imported", domain.ErrPublicationSharedReadonly, http.StatusConflict, CodePublicationSharedReadonly, false},
		{"duplicate DOI", domain.ErrDuplicateDOI, http.StatusConflict, CodeDuplicateDOI, false},
		{"no user in the context", nil, http.StatusUnauthorized, httpx.CodeUnauthenticated, true},
	}), http.MethodPatch, publicationPath, `{"title":"x"}`)
}

func TestRemoveMyPublication(t *testing.T) {
	svc := &fakeService{}
	rec := send(t, svc, http.MethodDelete, publicationPath, "", callerID)
	if rec.Code != http.StatusNoContent || svc.userID != callerID || svc.publicationID != publicationID {
		t.Errorf("got %d, service got %q %q; want 204 for the caller's publication", rec.Code, svc.userID, svc.publicationID)
	}
	checkErrors(t, []errorCase{
		{"not linked", domain.ErrNotFound, http.StatusNotFound, httpx.CodeNotFound, false},
		{"no user in the context", nil, http.StatusUnauthorized, httpx.CodeUnauthenticated, true},
	}, http.MethodDelete, publicationPath, "")
}

func TestListPublications(t *testing.T) {
	page := domain.PublicationPage{Items: []domain.Publication{notes}, Page: 2, PageSize: 5, Total: 6}
	wantBody := map[string]any{
		"items": []any{notesBody}, "page": float64(2), "pageSize": float64(5), "total": float64(6),
	}
	tests := []struct {
		name               string
		path               string
		wantUser           string
		wantOwn            bool
		wantPage, wantSize int
	}{
		{"own, defaults", publicationsPath, callerID, true, 1, usecase.DefaultPublicationPageSize},
		{"own, page and size", publicationsPath + "?page=2&pageSize=5", callerID, true, 2, 5},
		{"someone else's", "/api/v1/profiles/" + otherID + "/publications?pageSize=500", otherID, false, 1, 500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{publicationPage: page}
			rec := send(t, svc, http.MethodGet, tt.path, "", callerID)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
			}
			if svc.userID != tt.wantUser || svc.listedOwn != tt.wantOwn || svc.page != tt.wantPage || svc.pageSize != tt.wantSize {
				t.Errorf("service got user %q own %v page %d size %d; want %q %v %d %d",
					svc.userID, svc.listedOwn, svc.page, svc.pageSize, tt.wantUser, tt.wantOwn, tt.wantPage, tt.wantSize)
			}
			if got := decode(t, rec); !reflect.DeepEqual(got, wantBody) {
				t.Errorf("body = %v, want %v", got, wantBody)
			}
		})
	}

	t.Run("an empty list is [], never null", func(t *testing.T) {
		svc := &fakeService{publicationPage: domain.PublicationPage{Page: 1, PageSize: 10}}
		got := decode(t, send(t, svc, http.MethodGet, publicationsPath, "", callerID))
		if items, ok := got["items"].([]any); !ok || len(items) != 0 {
			t.Errorf("items = %v, want []", got["items"])
		}
	})

	t.Run("a page that is not a number", func(t *testing.T) {
		if rec := send(t, &fakeService{}, http.MethodGet, publicationsPath+"?page=abc", "", callerID); rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	checkErrors(t, []errorCase{
		{"page below 1", domain.ErrInvalidPage, http.StatusBadRequest, httpx.CodeBadRequest, false},
		{"no user in the context", nil, http.StatusUnauthorized, httpx.CodeUnauthenticated, true},
	}, http.MethodGet, publicationsPath+"?page=0", "")
	checkErrors(t, []errorCase{
		{"hidden or unknown user", domain.ErrNotFound, http.StatusNotFound, httpx.CodeNotFound, false},
		{"page below 1", domain.ErrInvalidPage, http.StatusBadRequest, httpx.CodeBadRequest, false},
		{"no user in the context", nil, http.StatusUnauthorized, httpx.CodeUnauthenticated, true},
	}, http.MethodGet, "/api/v1/profiles/"+otherID+"/publications", "")
}
