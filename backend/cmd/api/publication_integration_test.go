//go:build integration

// End-to-end test of publications (FR-5.1, FR-5.4, FR-5.5, FR-5.7) through the real router
// and the real PostgreSQL adapter. It reuses the environment of media_integration_test.go.
package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// newDOI returns a DOI no other test run uses and deletes its publication when the test
// ends; deleting the users who added it leaves the record behind.
func (e mediaEnv) newDOI(t *testing.T) string {
	t.Helper()
	doi := "10.5555/e2e-" + strings.ToLower(rand.Text())
	t.Cleanup(func() {
		if _, err := e.pool.Exec(context.Background(), "DELETE FROM publications WHERE doi = $1", doi); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
	return doi
}

func TestPublicationAddListPatchDeleteEndToEnd(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)

	rec := e.send(t, ada, http.MethodPost, "/api/v1/profiles/me/publications", strings.NewReader(
		`{"title":"  Notes on the Engine ","year":2024,"venue":"  ","abstract":"Line one\nline two","url":"https://example.org/p"}`),
		"application/json")
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST: %d %s", rec.Code, rec.Body)
	}
	created := decodeBody(t, rec.Body.Bytes())
	if created["title"] != "Notes on the Engine" || created["venue"] != nil || created["doi"] != nil ||
		created["source"] != "manual" || created["linked"] != false || created["year"] != float64(2024) {
		t.Errorf("created %v", created)
	}
	path := "/api/v1/profiles/me/publications/" + created["id"].(string)

	// Ada's own list, Bob's view of it and both profiles show it (FR-5.5).
	for _, tt := range []struct{ caller, path string }{
		{ada, "/api/v1/profiles/me/publications"},
		{bob, "/api/v1/profiles/" + ada + "/publications"},
	} {
		rec = e.send(t, tt.caller, http.MethodGet, tt.path, nil, "")
		got := decodeBody(t, rec.Body.Bytes())
		items, _ := got["items"].([]any)
		if rec.Code != http.StatusOK || got["total"] != float64(1) || len(items) != 1 || got["pageSize"] != float64(10) {
			t.Errorf("GET %s: %d %v, want one item on a page of 10", tt.path, rec.Code, got)
		}
	}
	rec = e.send(t, bob, http.MethodGet, "/api/v1/profiles/"+ada, nil, "")
	if got := decodeBody(t, rec.Body.Bytes()); got["publicationCount"] != float64(1) {
		t.Errorf("profile publicationCount = %v, want 1", got["publicationCount"])
	}

	// PATCH: null clears, a missing field is kept, the DOI is normalized.
	rec = e.send(t, ada, http.MethodPatch, path, strings.NewReader(`{"url":null,"doi":"DOI:10.1000/ABC"}`), "application/json")
	got := decodeBody(t, rec.Body.Bytes())
	if rec.Code != http.StatusOK || got["url"] != nil || got["doi"] != "10.1000/abc" || got["abstract"] != "Line one\nline two" {
		t.Errorf("PATCH: %d %v", rec.Code, got)
	}
	for _, tt := range []struct{ body, code string }{
		{`{"title":null}`, "invalid_title"},
		{`{"year":1899}`, "invalid_year"},
		{`{"venue":"a\nb"}`, "invalid_venue"},
		{`{"abstract":"` + strings.Repeat("a", 5001) + `"}`, "invalid_abstract"},
		{`{"url":"ftp://example.org"}`, "invalid_url"},
		{`{"doi":"10.12/x"}`, "invalid_doi"},
	} {
		rec = e.send(t, ada, http.MethodPatch, path, strings.NewReader(tt.body), "application/json")
		if got := decodeBody(t, rec.Body.Bytes()); rec.Code != http.StatusBadRequest || got["code"] != tt.code {
			t.Errorf("PATCH %.30s…: %d %v, want 400 %s", tt.body, rec.Code, got, tt.code)
		}
	}

	// Bob is not linked: his PATCH and DELETE find nothing.
	for _, method := range []string{http.MethodPatch, http.MethodDelete} {
		rec = e.send(t, bob, method, path, strings.NewReader(`{"title":"Mine"}`), "application/json")
		if rec.Code != http.StatusNotFound {
			t.Errorf("Bob's %s: %d, want 404", method, rec.Code)
		}
	}

	// DELETE removes it from the list, and the record with it (FR-5.7).
	if rec = e.send(t, ada, http.MethodDelete, path, nil, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE: %d %s", rec.Code, rec.Body)
	}
	if rec = e.send(t, ada, http.MethodDelete, path, nil, ""); rec.Code != http.StatusNotFound {
		t.Errorf("second DELETE: %d, want 404", rec.Code)
	}
	var left bool
	if err := e.pool.QueryRow(t.Context(), "SELECT EXISTS (SELECT 1 FROM publications WHERE id = $1)", created["id"]).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left {
		t.Error("the record outlived its only author")
	}
	rec = e.send(t, ada, http.MethodGet, "/api/v1/profiles/me/publications", nil, "")
	if got := decodeBody(t, rec.Body.Bytes()); got["total"] != float64(0) {
		t.Errorf("after DELETE total = %v, want 0", got["total"])
	}
}

func TestTwoUsersAddTheSameDOIEndToEnd(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	doi := e.newDOI(t)

	rec := e.send(t, ada, http.MethodPost, "/api/v1/profiles/me/publications",
		strings.NewReader(fmt.Sprintf(`{"title":"Shared paper","year":2023,"doi":"%s"}`, doi)), "application/json")
	if rec.Code != http.StatusCreated {
		t.Fatalf("Ada's POST: %d %s", rec.Code, rec.Body)
	}
	first := decodeBody(t, rec.Body.Bytes())

	// Bob types the DOI as a link in capitals; he is linked to Ada's record (FR-5.4).
	rec = e.send(t, bob, http.MethodPost, "/api/v1/profiles/me/publications",
		strings.NewReader(fmt.Sprintf(`{"title":"Bob's title","year":2020,"doi":"https://doi.org/%s"}`, strings.ToUpper(doi))),
		"application/json")
	second := decodeBody(t, rec.Body.Bytes())
	if rec.Code != http.StatusCreated || second["linked"] != true || second["id"] != first["id"] || second["title"] != "Shared paper" {
		t.Fatalf("Bob's POST: %d %v, want 201 with Ada's record and linked true", rec.Code, second)
	}

	// Adding it again is refused.
	rec = e.send(t, bob, http.MethodPost, "/api/v1/profiles/me/publications",
		strings.NewReader(fmt.Sprintf(`{"title":"x","year":2020,"doi":"%s"}`, doi)), "application/json")
	if got := decodeBody(t, rec.Body.Bytes()); rec.Code != http.StatusConflict || got["code"] != "publication_already_added" {
		t.Errorf("second add: %d %v, want 409 publication_already_added", rec.Code, got)
	}

	// Shared, so neither can edit it.
	path := "/api/v1/profiles/me/publications/" + first["id"].(string)
	rec = e.send(t, ada, http.MethodPatch, path, strings.NewReader(`{"title":"Renamed"}`), "application/json")
	if got := decodeBody(t, rec.Body.Bytes()); rec.Code != http.StatusConflict || got["code"] != "publication_shared_readonly" {
		t.Errorf("Ada's PATCH: %d %v, want 409 publication_shared_readonly", rec.Code, got)
	}

	// Ada leaves; Bob keeps it.
	if rec = e.send(t, ada, http.MethodDelete, path, nil, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("Ada's DELETE: %d %s", rec.Code, rec.Body)
	}
	rec = e.send(t, ada, http.MethodGet, "/api/v1/profiles/"+bob+"/publications", nil, "")
	if got := decodeBody(t, rec.Body.Bytes()); got["total"] != float64(1) {
		t.Errorf("Bob's list after Ada left: %v, want 1", got)
	}

	// Another user's manual record cannot take the same DOI.
	rec = e.send(t, ada, http.MethodPost, "/api/v1/profiles/me/publications",
		strings.NewReader(`{"title":"Other","year":2022}`), "application/json")
	other := decodeBody(t, rec.Body.Bytes())
	rec = e.send(t, ada, http.MethodPatch, "/api/v1/profiles/me/publications/"+other["id"].(string),
		strings.NewReader(fmt.Sprintf(`{"doi":"%s"}`, doi)), "application/json")
	if got := decodeBody(t, rec.Body.Bytes()); rec.Code != http.StatusConflict || got["code"] != "duplicate_doi" {
		t.Errorf("PATCH to a taken DOI: %d %v, want 409 duplicate_doi", rec.Code, got)
	}
}

func TestPublicationListOfHiddenUserEndToEnd(t *testing.T) {
	e := newMediaEnv(t)
	ada, carl := e.newProfileUser(t), e.newProfileUser(t)
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET suspended_at = now() WHERE id = $1", carl); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{carl, "not-a-uuid", "00000000-0000-4000-8000-000000000000"} {
		rec := e.send(t, ada, http.MethodGet, "/api/v1/profiles/"+id+"/publications", nil, "")
		if got := decodeBody(t, rec.Body.Bytes()); rec.Code != http.StatusNotFound || got["code"] != "not_found" {
			t.Errorf("GET %s: %d %v, want 404 not_found", id, rec.Code, got)
		}
	}
}
