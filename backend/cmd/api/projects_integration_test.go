//go:build integration

// End-to-end tests of research projects (FR-10.1 to FR-10.3, FR-10.6, FR-15.11, AI-1.2)
// through the real router, usecase, PostgreSQL store and Redis limits. They reuse the
// environment of media_integration_test.go.
package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
)

// sendRole is send with the given platform role and email state in the token.
func (e mediaEnv) sendRole(t *testing.T, userID, role string, verified bool, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader = http.NoBody
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequestWithContext(t.Context(), method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	token, _, err := accesstoken.New([]byte(testSecret), accessTTL).Sign(userID, role, verified, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", httpx.AccessCookie+"="+token)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func (e mediaEnv) json(t *testing.T, userID, method, path, body string) (int, map[string]any) {
	t.Helper()
	rec := e.sendRole(t, userID, "researcher", true, method, path, body)
	if rec.Body.Len() == 0 {
		return rec.Code, nil
	}
	return rec.Code, decodeBody(t, rec.Body.Bytes())
}

// projectUser is newProfileUser whose owned projects are deleted before the account: the
// database refuses to delete an owner while the project is there (FR-2.9), and the
// environment's cleanup deletes the user row directly.
func (e mediaEnv) projectUser(t *testing.T) string {
	t.Helper()
	id := e.newProfileUser(t)
	t.Cleanup(func() {
		if _, err := e.pool.Exec(context.Background(), `DELETE FROM projects WHERE id IN
			(SELECT project_id FROM project_members WHERE user_id = $1 AND role = 'owner')`, id); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
	return id
}

func (e mediaEnv) projectJobs(t *testing.T, id string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM embedding_jobs WHERE owner_type = 'project' AND owner_id = $1", id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestProjectsEndToEnd(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.projectUser(t), e.projectUser(t)
	w := testenv.Word(t)

	// Create (FR-10.1): the caller owns it; the embedding job is queued (AI-1.2).
	code, p := e.json(t, ada, http.MethodPost, "/api/v1/projects",
		`{"title":" `+w+` graphs ","description":"A study","requiredSkills":["Graph theory","graph  theory","Go"],"abstract":"Our abstract"}`)
	if code != http.StatusCreated || p["myRole"] != "owner" || p["status"] != "open" || p["title"] != w+" graphs" || p["abstract"] != "Our abstract" {
		t.Fatalf("create: %d %v", code, p)
	}
	if skills := p["requiredSkills"].([]any); len(skills) != 2 {
		t.Errorf("skills = %v", skills)
	}
	id := p["id"].(string)
	path := "/api/v1/projects/" + id
	if e.projectJobs(t, id) != 1 {
		t.Error("no embedding job after create")
	}
	code, p = e.json(t, ada, http.MethodPost, "/api/v1/projects", `{"title":"ab","description":"x"}`)
	if code != http.StatusBadRequest || p["field"] != "title" {
		t.Errorf("short title: %d %v", code, p)
	}

	// Read: another user sees it without the abstract.
	code, p = e.json(t, bob, http.MethodGet, path, "")
	if code != http.StatusOK || p["abstract"] != nil || p["myRole"] != nil || p["memberCount"] != float64(1) {
		t.Errorf("bob reads: %d %v", code, p)
	}

	// Edit (FR-10.2): owner only; nothing changes on a refusal.
	if code, _ := e.json(t, bob, http.MethodPatch, path, `{"title":"Taken over"}`); code != http.StatusForbidden {
		t.Errorf("bob's edit: %d, want 403", code)
	}
	if code, p := e.json(t, ada, http.MethodPatch, path, `{"status":"paused"}`); code != http.StatusBadRequest || p["field"] != "status" {
		t.Errorf("bad status: %d %v", code, p)
	}
	if _, err := e.pool.Exec(t.Context(), "DELETE FROM embedding_jobs"); err != nil {
		t.Fatal(err)
	}
	code, p = e.json(t, ada, http.MethodPatch, path, `{"status":"archived","abstract":null}`)
	if code != http.StatusOK || p["status"] != "archived" || p["abstract"] != nil {
		t.Fatalf("archive: %d %v", code, p)
	}
	if e.projectJobs(t, id) != 1 {
		t.Error("clearing the abstract did not queue the project")
	}

	// Archived: members only, also for edits and deletes.
	for _, m := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		body := ""
		if m == http.MethodPatch {
			body = `{"status":"open"}`
		}
		if code, _ := e.json(t, bob, m, path+"?confirm=x", body); code != http.StatusNotFound {
			t.Errorf("bob %s an archived project: %d, want 404", m, code)
		}
	}
	code, p = e.json(t, ada, http.MethodGet, "/api/v1/me/projects", "")
	if items := p["items"].([]any); code != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["status"] != "archived" {
		t.Errorf("my projects: %d %v", code, p)
	}
	if code, p := e.json(t, ada, http.MethodPatch, path, `{"status":"open"}`); code != http.StatusOK || p["status"] != "open" {
		t.Errorf("reopen: %d %v", code, p)
	}

	// Search (FR-10.3): open only.
	code, p = e.json(t, bob, http.MethodGet, "/api/v1/search/projects?q="+url.QueryEscape(w), "")
	if code != http.StatusOK || p["total"] != float64(1) || p["pageSize"] != float64(20) {
		t.Errorf("search: %d %v", code, p)
	}
	if code, _ := e.json(t, ada, http.MethodPatch, path, `{"status":"closed"}`); code != http.StatusOK {
		t.Fatal(code)
	}
	if _, p := e.json(t, bob, http.MethodGet, "/api/v1/search/projects?q="+url.QueryEscape(w), ""); p["total"] != float64(0) {
		t.Errorf("closed project found: %v", p)
	}
	if code, p := e.json(t, bob, http.MethodGet, "/api/v1/search/projects?q=a", ""); code != http.StatusBadRequest || p["code"] != "invalid_query" {
		t.Errorf("short q: %d %v", code, p)
	}

	// Delete (FR-10.6): owner only, with the exact title.
	if code, _ := e.json(t, bob, http.MethodDelete, path+"?confirm="+url.QueryEscape(w+" graphs"), ""); code != http.StatusForbidden {
		t.Errorf("bob deletes: %d, want 403", code)
	}
	if code, p := e.json(t, ada, http.MethodDelete, path+"?confirm="+url.QueryEscape(strings.ToLower(w)+" graphs"), ""); code != http.StatusBadRequest || p["code"] != "confirmation_mismatch" {
		t.Errorf("wrong case: %d %v", code, p)
	}
	if code, _ := e.json(t, ada, http.MethodDelete, path+"?confirm="+url.QueryEscape(w+" graphs"), ""); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := e.json(t, ada, http.MethodGet, path, ""); code != http.StatusNotFound {
		t.Errorf("after delete: %d", code)
	}
	if e.projectJobs(t, id) != 0 {
		t.Error("the embedding job outlived the project")
	}

	// An unverified session is refused before anything else (FR-17.3).
	if rec := e.sendRole(t, ada, "researcher", false, http.MethodGet, "/api/v1/me/projects", ""); rec.Code != http.StatusForbidden ||
		!strings.Contains(rec.Body.String(), "email_not_verified") {
		t.Errorf("unverified: %d %s", rec.Code, rec.Body)
	}
}

// Ten projects a day per user (decisions.md 2026-11-16), counted in Redis.
func TestProjectCreateRateLimitEndToEnd(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.projectUser(t), e.projectUser(t)
	for i := range 10 {
		if code, p := e.json(t, ada, http.MethodPost, "/api/v1/projects", `{"title":"Project title","description":"d"}`); code != http.StatusCreated {
			t.Fatalf("create %d: %d %v", i+1, code, p)
		}
	}
	rec := e.sendRole(t, ada, "researcher", true, http.MethodPost, "/api/v1/projects", `{"title":"Project title","description":"d"}`)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" || !strings.Contains(rec.Body.String(), "rate_limited") {
		t.Errorf("11th create: %d %s, Retry-After %q", rec.Code, rec.Body, rec.Header().Get("Retry-After"))
	}
	if code, _ := e.json(t, bob, http.MethodPost, "/api/v1/projects", `{"title":"Project title","description":"d"}`); code != http.StatusCreated {
		t.Errorf("another user's create: %d", code)
	}
}

// FR-15.11: a user reports a project; an administrator lists, reads and resolves it, and has
// no right inside the project.
func TestProjectReportsEndToEnd(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, admin := e.projectUser(t), e.projectUser(t), e.projectUser(t)
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET role = 'admin' WHERE id = $1", admin); err != nil {
		t.Fatal(err)
	}
	_, p := e.json(t, ada, http.MethodPost, "/api/v1/projects", `{"title":"Spammy project","description":"Buy now"}`)
	id := p["id"].(string)
	path := "/api/v1/projects/" + id

	if code, p := e.json(t, ada, http.MethodPost, path+"/reports", `{"reason":"spam"}`); code != http.StatusBadRequest || p["code"] != "cannot_report_own_project" {
		t.Errorf("own report: %d %v", code, p)
	}
	if code, p := e.json(t, bob, http.MethodPost, path+"/reports", `{"reason":"spam","details":"Ads"}`); code != http.StatusCreated || p["id"] == nil {
		t.Fatalf("report: %d %v", code, p)
	}
	if code, p := e.json(t, bob, http.MethodPost, path+"/reports", `{"reason":"other"}`); code != http.StatusConflict || p["code"] != "already_reported" {
		t.Errorf("second report: %d %v", code, p)
	}
	if code, _ := e.json(t, bob, http.MethodGet, "/api/v1/admin/project-reports", ""); code != http.StatusForbidden {
		t.Errorf("researcher lists reports: %d", code)
	}

	// The owner archives it; the administrator still reads it through the report.
	if code, _ := e.json(t, ada, http.MethodPatch, path, `{"status":"archived"}`); code != http.StatusOK {
		t.Fatal(code)
	}
	admin1 := func(method, p, body string) (int, map[string]any) {
		rec := e.sendRole(t, admin, "admin", true, method, p, body)
		if rec.Body.Len() == 0 {
			return rec.Code, nil
		}
		return rec.Code, decodeBody(t, rec.Body.Bytes())
	}
	code, list := admin1(http.MethodGet, "/api/v1/admin/project-reports", "")
	items, _ := list["items"].([]any)
	if code != http.StatusOK || len(items) != 1 {
		t.Fatalf("admin list: %d %v", code, list)
	}
	rid := items[0].(map[string]any)["id"].(string)
	code, detail := admin1(http.MethodGet, "/api/v1/admin/project-reports/"+rid, "")
	if code != http.StatusOK || detail["project"].(map[string]any)["status"] != "archived" {
		t.Errorf("admin detail: %d %v", code, detail)
	}
	// No admin right inside the project (SRS 2.3).
	for _, m := range []string{http.MethodGet, http.MethodDelete} {
		if code, _ := admin1(m, path+"?confirm=Spammy+project", ""); code != http.StatusNotFound {
			t.Errorf("admin %s of an archived project: %d, want 404", m, code)
		}
	}
	code, res := admin1(http.MethodPost, "/api/v1/admin/project-reports/"+rid+"/resolve", `{"outcome":"dismissed","note":"Fine"}`)
	if code != http.StatusOK || res["status"] != "resolved" || res["outcome"] != "dismissed" {
		t.Errorf("resolve: %d %v", code, res)
	}
	if code, _ := admin1(http.MethodPost, "/api/v1/admin/project-reports/"+rid+"/resolve", `{"outcome":"dismissed"}`); code != http.StatusConflict {
		t.Errorf("resolve twice: %d", code)
	}

	// A suspended reporter is refused at once.
	if _, err := e.pool.Exec(t.Context(), "UPDATE projects SET status = 'open' WHERE id = $1", id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET suspended_at = now(), suspension_reason = 'Sent spam to many users' WHERE id = $1", bob); err != nil {
		t.Fatal(err)
	}
	if code, p := e.json(t, bob, http.MethodPost, path+"/reports", `{"reason":"other"}`); code != http.StatusForbidden || p["code"] != "account_suspended" {
		t.Errorf("suspended reporter: %d %v", code, p)
	}
}
