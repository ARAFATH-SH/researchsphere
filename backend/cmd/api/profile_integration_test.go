//go:build integration

// End-to-end test of the education grade and description (FR-4.4) and the profile city
// (FR-4.1) through the real router and the real PostgreSQL adapter. It reuses the
// environment of media_integration_test.go.
package main

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func decodeBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("body %s: %v", body, err)
	}
	return m
}

func TestEducationGradeAndDescriptionEndToEnd(t *testing.T) {
	e := newMediaEnv(t)
	ada := e.newProfileUser(t)

	rec := e.send(t, ada, http.MethodPost, "/api/v1/profiles/me/education", strings.NewReader(
		`{"institution":"BUET","startYear":2016,"endYear":2020,"grade":"  CGPA 3.57 / 4.00 ","description":"Thesis on routing.\nDean's list."}`),
		"application/json")
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST: %d %s", rec.Code, rec.Body)
	}
	created := decodeBody(t, rec.Body.Bytes())
	if created["grade"] != "CGPA 3.57 / 4.00" || created["description"] != "Thesis on routing.\nDean's list." {
		t.Errorf("created %v, want the trimmed grade and the description with its line break", created)
	}
	path := "/api/v1/profiles/me/education/" + created["id"].(string)

	// null clears the grade; the description, left out, is kept.
	rec = e.send(t, ada, http.MethodPatch, path, strings.NewReader(`{"grade":null}`), "application/json")
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH: %d %s", rec.Code, rec.Body)
	}
	if got := decodeBody(t, rec.Body.Bytes()); got["grade"] != nil || got["description"] != "Thesis on routing.\nDean's list." {
		t.Errorf("after PATCH %v, want grade null and the description kept", got)
	}

	// A blank description is stored as null.
	rec = e.send(t, ada, http.MethodPatch, path, strings.NewReader(`{"description":"  "}`), "application/json")
	if got := decodeBody(t, rec.Body.Bytes()); rec.Code != http.StatusOK || got["description"] != nil {
		t.Errorf("blank description: %d %v, want 200 and null", rec.Code, got)
	}

	for _, tt := range []struct{ body, code string }{
		{`{"grade":"` + strings.Repeat("a", 51) + `"}`, "invalid_grade"},
		{`{"grade":"a\nb"}`, "invalid_grade"},
		{`{"description":"` + strings.Repeat("a", 2001) + `"}`, "invalid_description"},
	} {
		rec = e.send(t, ada, http.MethodPatch, path, strings.NewReader(tt.body), "application/json")
		if got := decodeBody(t, rec.Body.Bytes()); rec.Code != http.StatusBadRequest || got["code"] != tt.code {
			t.Errorf("PATCH %.30s…: %d %v, want 400 %s", tt.body, rec.Code, got, tt.code)
		}
	}
}

func TestProfileCityEndToEnd(t *testing.T) {
	e := newMediaEnv(t)
	ada := e.newProfileUser(t)
	bob := e.newProfileUser(t)

	rec := e.send(t, ada, http.MethodPut, "/api/v1/profiles/me",
		strings.NewReader(`{"fullName":"Ada","country":"bd","city":"  Dhaka "}`), "application/json")
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body)
	}
	want := map[string]any{"country": "BD", "city": "Dhaka"}
	for _, path := range []string{"/api/v1/profiles/me", "/api/v1/profiles/" + ada} {
		viewer := ada
		if path != "/api/v1/profiles/me" {
			viewer = bob
		}
		got := decodeBody(t, e.send(t, viewer, http.MethodGet, path, nil, "").Body.Bytes())
		if g := map[string]any{"country": got["country"], "city": got["city"]}; !reflect.DeepEqual(g, want) {
			t.Errorf("GET %s: %v, want %v", path, g, want)
		}
	}

	// PUT replaces every field: a blank city is stored as null.
	rec = e.send(t, ada, http.MethodPut, "/api/v1/profiles/me",
		strings.NewReader(`{"fullName":"Ada","city":" "}`), "application/json")
	if got := decodeBody(t, rec.Body.Bytes()); rec.Code != http.StatusOK || got["city"] != nil {
		t.Errorf("blank city: %d %v, want 200 and null", rec.Code, got)
	}

	rec = e.send(t, ada, http.MethodPut, "/api/v1/profiles/me",
		strings.NewReader(`{"fullName":"Ada","city":"`+strings.Repeat("a", 101)+`"}`), "application/json")
	if got := decodeBody(t, rec.Body.Bytes()); rec.Code != http.StatusBadRequest || got["code"] != "invalid_city" {
		t.Errorf("long city: %d %v, want 400 invalid_city", rec.Code, got)
	}
}
