//go:build integration

package main

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// importAs posts a file to the venue import as the user with role.
func (e mediaEnv) importAs(t *testing.T, userID, role, query, name, data string) (int, map[string]any) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	part, err := w.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/admin/venues/import"+query, &b)
	req.Header.Set("Content-Type", w.FormDataContentType())
	token, _, err := accesstoken.New([]byte(testSecret), accessTTL).Sign(userID, role, true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", httpx.AccessCookie+"="+token)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec.Code, decodeBody(t, rec.Body.Bytes())
}

// FR-15.1, FR-14.1, FR-14.4 end to end: an administrator adds, changes and removes a venue,
// each audited; a researcher reads it and may write nothing; a removed venue is a 404.
func TestVenuesEndToEnd(t *testing.T) {
	e := newMediaEnv(t)
	admin, researcher := e.newProfileUser(t), e.newProfileUser(t)
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET role = 'admin' WHERE id = $1", admin); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().UTC().AddDate(0, 2, 0).Format("2006-01-02")
	create := `{"name":"Workshop on Graph Learning","shortName":"GLW","type":"workshop","fields":["robotics","explainable_ai"],
		"attendance":"online","timeZone":"AoE","submissionDeadline":"` + deadline + `","officialUrl":"https://glw.example.org",
		"scope":"<script>alert(1)</script> **Graphs**"}`

	code, body := e.asAdmin(t, admin, http.MethodPost, "/api/v1/admin/venues", strings.Replace(create, "https://", "http://", 1))
	if code != http.StatusBadRequest || body["field"] != "officialUrl" {
		t.Fatalf("http link: %d %v", code, body)
	}
	code, body = e.asAdmin(t, admin, http.MethodPost, "/api/v1/admin/venues", create)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, body)
	}
	id := body["id"].(string)
	sd := body["submissionDeadline"].(map[string]any)
	if body["timeZone"] != "AoE" || sd["local"] != deadline+"T23:59:59" || body["past"] != false || body["source"] != "manual" ||
		body["scope"] != "<script>alert(1)</script> **Graphs**" {
		t.Errorf("created = %v", body)
	}
	if fields := body["fields"].([]any); len(fields) != 2 || fields[0].(map[string]any)["key"] != "explainable_ai" {
		t.Errorf("fields = %v", fields)
	}

	// A researcher reads it, with the facets; every write is refused.
	rec := e.send(t, researcher, http.MethodGet, "/api/v1/venues?field=robotics&type=workshop", nil, "")
	list := decodeBody(t, rec.Body.Bytes())
	if rec.Code != http.StatusOK || list["total"] != float64(1) || len(list["facets"].(map[string]any)["fields"].([]any)) != 21 {
		t.Fatalf("list: %d %v", rec.Code, list)
	}
	if rec := e.send(t, researcher, http.MethodGet, "/api/v1/venues/"+id, nil, ""); rec.Code != http.StatusOK {
		t.Errorf("get: %d %s", rec.Code, rec.Body)
	}
	for _, r := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/admin/venues", create},
		{http.MethodPatch, "/api/v1/admin/venues/" + id, `{"scope":"Taken over"}`},
		{http.MethodPost, "/api/v1/admin/venues/" + id + "/remove", `{"reason":"Not a real venue"}`},
	} {
		if rec := e.send(t, researcher, r.method, r.path, strings.NewReader(r.body), "application/json"); rec.Code != http.StatusForbidden {
			t.Errorf("researcher %s %s: %d", r.method, r.path, rec.Code)
		}
	}
	if code, _ := e.importAs(t, researcher, "researcher", "", "v.csv", "name\nX\n"); code != http.StatusForbidden {
		t.Errorf("researcher import: %d", code)
	}

	code, body = e.asAdmin(t, admin, http.MethodPatch, "/api/v1/admin/venues/"+id, `{"shortName":null,"scope":"Graphs only"}`)
	if code != http.StatusOK || body["shortName"] != nil || body["scope"] != "Graphs only" {
		t.Errorf("patch: %d %v", code, body)
	}
	code, body = e.asAdmin(t, admin, http.MethodPost, "/api/v1/admin/venues/"+id+"/remove", `{"reason":"tiny"}`)
	if code != http.StatusBadRequest || body["field"] != "reason" {
		t.Errorf("short reason: %d %v", code, body)
	}
	if code, body = e.asAdmin(t, admin, http.MethodPost, "/api/v1/admin/venues/"+id+"/remove", `{"reason":"Duplicate entry of GLW"}`); code != http.StatusNoContent {
		t.Fatalf("remove: %d %v", code, body)
	}
	if rec := e.send(t, researcher, http.MethodGet, "/api/v1/venues/"+id, nil, ""); rec.Code != http.StatusNotFound {
		t.Errorf("get after removal: %d", rec.Code)
	}
	rec = e.send(t, researcher, http.MethodGet, "/api/v1/venues?includePast=true", nil, "")
	if list := decodeBody(t, rec.Body.Bytes()); list["total"] != float64(0) {
		t.Errorf("list after removal: %v", list)
	}
	var actions []string
	rows, err := e.pool.Query(t.Context(), "SELECT action FROM venue_actions WHERE venue_id = $1 ORDER BY created_at", id)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, a)
	}
	if strings.Join(actions, ",") != "create,update,remove" {
		t.Errorf("audit = %v", actions)
	}
}

// FR-14.5 end to end: a dry run, then the import, then the same file again.
func TestVenueImportEndToEnd(t *testing.T) {
	e := newMediaEnv(t)
	admin := e.newProfileUser(t)
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET role = 'admin' WHERE id = $1", admin); err != nil {
		t.Fatal(err)
	}
	data := `[{"sourceKey":"cfp-1","name":"Journal of Floods","type":"journal","fields":["water_resources"],"timeZone":"UTC",
		"submissionDeadline":"2030-01-01","officialUrl":"https://jof.example.org","scope":"Floods."},
		{"name":"Bad","type":"journal","fields":["water_resources"],"timeZone":"UTC","submissionDeadline":"2030-01-01",
		"officialUrl":"ftp://bad.example.org","scope":"x"}]`

	code, body := e.importAs(t, admin, "admin", "?dryRun=true", "venues.json", data)
	if code != http.StatusOK || body["dryRun"] != true || body["created"] != float64(1) || body["rejected"] != float64(1) {
		t.Fatalf("dry run: %d %v", code, body)
	}
	reasons := body["rows"].([]any)[1].(map[string]any)["reasons"].([]any)
	if r := reasons[0].(map[string]any); r["field"] != "officialUrl" || r["code"] != "not_https" {
		t.Errorf("reasons = %v", reasons)
	}
	var n int
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM venues").Scan(&n); err != nil || n != 0 {
		t.Fatalf("a dry run wrote %d venues, %v", n, err)
	}
	if code, body = e.importAs(t, admin, "admin", "", "venues.json", data); code != http.StatusOK || body["created"] != float64(1) {
		t.Fatalf("import: %d %v", code, body)
	}
	if code, body = e.importAs(t, admin, "admin", "", "venues.json", data); code != http.StatusOK || body["unchanged"] != float64(1) {
		t.Fatalf("again: %d %v", code, body)
	}
	if code, body = e.importAs(t, admin, "admin", "", "venues.csv", "name,colour\nA,red\n"); code != http.StatusBadRequest || body["code"] != "invalid_import_file" {
		t.Errorf("unknown column: %d %v", code, body)
	}
}
