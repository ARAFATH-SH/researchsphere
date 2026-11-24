package main

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	eventdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
	eventusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/event/usecase"
)

// okVenues answers every venue operation with one fixed venue, so the router tests reach the
// handlers.
type okVenues struct{}

const okVenueID = "0192f0a0-0000-7000-8000-0000000e0001"

func okVenue() eventdomain.Venue {
	return eventdomain.Venue{
		ID: okVenueID, Name: "Workshop on Graph Learning", ShortName: "GLW", Type: eventdomain.TypeWorkshop,
		Fields:     []eventdomain.Field{{Key: "explainable_ai", Label: "Artificial intelligence and machine learning"}},
		Attendance: eventdomain.AttendOnline, TimeZone: "AoE",
		SubmissionDeadline: time.Date(2030, 3, 2, 11, 59, 59, 0, time.UTC), OfficialURL: "https://glw.example.org",
		Scope: "Graph learning.", Source: eventdomain.SourceManual,
	}
}

func (okVenues) Fields(context.Context) ([]eventdomain.Field, error) {
	return okVenue().Fields, nil
}

func (okVenues) List(context.Context, eventusecase.ListInput) (eventdomain.Page, int, int, error) {
	return eventdomain.Page{Items: []eventdomain.Venue{okVenue()}, Total: 1}, 1, 20, nil
}

func (okVenues) Get(_ context.Context, id string) (eventdomain.Venue, error) {
	if id != okVenueID {
		return eventdomain.Venue{}, eventdomain.ErrNotFound
	}
	return okVenue(), nil
}

func (okVenues) Create(context.Context, string, eventusecase.Input) (eventdomain.Venue, error) {
	return okVenue(), nil
}

func (okVenues) Update(context.Context, string, string, eventusecase.Patch) (eventdomain.Venue, error) {
	return okVenue(), nil
}

func (okVenues) Remove(context.Context, string, string, string) error { return nil }

func (okVenues) Import(_ context.Context, _, _ string, r io.Reader, dryRun bool) (eventusecase.ImportResult, error) {
	if _, err := io.Copy(io.Discard, r); err != nil {
		return eventusecase.ImportResult{}, err
	}
	return eventusecase.ImportResult{DryRun: dryRun}, nil
}

// okSync answers the automatic import's operations: every start succeeds with one run.
type okSync struct{}

const okSyncRunID = "0192f0a0-0000-7000-8000-0000000e0002"

func (okSync) Start(_ context.Context, _ string, _ []string, dry bool) ([]eventdomain.SyncRun, error) {
	return []eventdomain.SyncRun{{ID: okSyncRunID, Source: eventdomain.SourceCCF, DryRun: dry}}, nil
}

func (okSync) Status(context.Context) ([]eventusecase.SourceStatus, error) {
	return []eventusecase.SourceStatus{{Source: eventdomain.SourceCCF, Info: eventusecase.SourceInfo{Label: "ccf", Licence: "MIT"}}}, nil
}

func (okSync) Daily() bool { return true }

// importBody is a multipart body with one file part.
func importBody(t *testing.T, name string, data []byte) (io.Reader, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	part, err := w.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &b, w.FormDataContentType()
}

// Reading venues needs a session with a verified email; every write needs the administrator
// (FR-15.1, SRS 2.3: "Add, edit and remove venues"). A researcher gets 403 on each.
func TestVenueRoutes(t *testing.T) {
	router := testRouter()
	const v = "/api/v1/venues"
	const a = "/api/v1/admin/venues"
	body := func(s string) func() (io.Reader, string) {
		return func() (io.Reader, string) { return strings.NewReader(s), "application/json" }
	}
	file := func() (io.Reader, string) { return importBody(t, "venues.csv", []byte("name\nX\n")) }
	tests := []struct {
		name, method, path, role string
		body                     func() (io.Reader, string)
		want                     int
		wantBody                 string
	}{
		{"list", http.MethodGet, v, "researcher", nil, http.StatusOK, `"total":1`},
		{"list, bad page", http.MethodGet, v + "?page=x", "researcher", nil, http.StatusBadRequest, `"field":"page"`},
		{"list without a session", http.MethodGet, v, "", nil, http.StatusUnauthorized, ""},
		{"fields", http.MethodGet, v + "/fields", "researcher", nil, http.StatusOK, `"key":"explainable_ai"`},
		{"one", http.MethodGet, v + "/" + okVenueID, "researcher", nil, http.StatusOK, `"scope":"Graph learning."`},
		{"one, unknown", http.MethodGet, v + "/nope", "researcher", nil, http.StatusNotFound, `"code":"not_found"`},
		{"one, the deadline as AoE local time", http.MethodGet, v + "/" + okVenueID, "researcher", nil, http.StatusOK,
			`"local":"2030-03-01T23:59:59"`},

		{"create as researcher", http.MethodPost, a, "researcher", body(`{}`), http.StatusForbidden, `"code":"forbidden"`},
		{"change as researcher", http.MethodPatch, a + "/" + okVenueID, "researcher", body(`{}`), http.StatusForbidden, `"code":"forbidden"`},
		{"remove as researcher", http.MethodPost, a + "/" + okVenueID + "/remove", "researcher", body(`{"reason":"Duplicate entry"}`), http.StatusForbidden, `"code":"forbidden"`},
		{"import as researcher", http.MethodPost, a + "/import", "researcher", file, http.StatusForbidden, `"code":"forbidden"`},
		{"create without a session", http.MethodPost, a, "", body(`{}`), http.StatusUnauthorized, ""},

		{"create as admin", http.MethodPost, a, "admin", body(`{"name":"X","type":"journal","fields":["x"],"timeZone":"UTC",
			"submissionDeadline":"2030-01-01","officialUrl":"https://x.example.org","scope":"x"}`), http.StatusCreated, `"id":"` + okVenueID},
		{"change as admin", http.MethodPatch, a + "/" + okVenueID, "admin", body(`{"scope":"New"}`), http.StatusOK, `"id"`},
		{"remove as admin", http.MethodPost, a + "/" + okVenueID + "/remove", "admin", body(`{"reason":"Duplicate entry"}`), http.StatusNoContent, ""},
		{"import as admin", http.MethodPost, a + "/import?dryRun=true", "admin", file, http.StatusOK, `"dryRun":true`},

		// The automatic import (FR-14.5): the administrator only.
		{"sync status as researcher", http.MethodGet, a + "/sync", "researcher", nil, http.StatusForbidden, `"code":"forbidden"`},
		{"start sync as researcher", http.MethodPost, a + "/sync?dryRun=true", "researcher", nil, http.StatusForbidden, `"code":"forbidden"`},
		{"sync status without a session", http.MethodGet, a + "/sync", "", nil, http.StatusUnauthorized, ""},
		{"sync status as admin", http.MethodGet, a + "/sync", "admin", nil, http.StatusOK, `"licence":"MIT"`},
		{"start sync as admin", http.MethodPost, a + "/sync?dryRun=true", "admin", nil, http.StatusAccepted, `"id":"` + okSyncRunID},
		{"start sync as admin, with sources", http.MethodPost, a + "/sync", "admin", body(`{"sources":["ccfddl"]}`), http.StatusAccepted, `"dryRun":false`},
		{"list rolling journals", http.MethodGet, v + "?rolling=true", "researcher", nil, http.StatusOK, `"total":1`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				r  io.Reader
				ct string
			)
			if tt.body != nil {
				r, ct = tt.body()
			}
			rec := sendAs(t, router, tt.method, tt.path, r, ct, tt.role)
			if rec.Code != tt.want || !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("status = %d, body %s; want %d with %q", rec.Code, rec.Body, tt.want, tt.wantBody)
			}
		})
	}
}

// A session whose email address is not verified reads no venue and writes none (FR-17.3).
func TestVenueRoutesNeedAVerifiedEmail(t *testing.T) {
	router := testRouter()
	for _, r := range []struct{ method, path, role string }{
		{http.MethodGet, "/api/v1/venues", "researcher"},
		{http.MethodGet, "/api/v1/venues/" + okVenueID, "researcher"},
		{http.MethodPost, "/api/v1/admin/venues", "admin"},
		{http.MethodPost, "/api/v1/admin/venues/" + okVenueID + "/remove", "admin"},
	} {
		rec := sendWithToken(t, router, r.method, r.path, unverifiedToken(t, r.role))
		if !isEmailNotVerified(t, rec) {
			t.Errorf("%s %s: status %d, body %s; want 403 email_not_verified", r.method, r.path, rec.Code, rec.Body)
		}
	}
}

// An import file over the route's body cap gets 413 before the usecase reads it (FR-14.5).
func TestVenueImportBodyCap(t *testing.T) {
	router := testRouter()
	r, ct := importBody(t, "big.csv", bytes.Repeat([]byte("x"), eventusecase.MaxImportBytes+multipartOverhead+1))
	rec := sendAs(t, router, http.MethodPost, "/api/v1/admin/venues/import", r, ct, "admin")
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), `"code":"file_too_large"`) {
		t.Fatalf("status = %d, body %s; want 413 file_too_large", rec.Code, rec.Body)
	}
}
