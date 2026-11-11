//go:build integration

// End-to-end tests of reports and their review by an administrator (FR-16.9, NFR-13),
// through the real router, the PostgreSQL adapters and Redis, on the test database. They
// reuse the environment of media_integration_test.go.
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

const adminReports = "/api/v1/admin/message-reports"

// asAdmin is call with an administrator's token.
func (e mediaEnv) asAdmin(t *testing.T, adminID, method, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	token, _, err := accesstoken.New([]byte(testSecret), accessTTL).Sign(adminID, "admin", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", httpx.AccessCookie+"="+token)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	if rec.Body.Len() == 0 {
		return rec.Code, nil
	}
	return rec.Code, decodeBody(t, rec.Body.Bytes())
}

func (e mediaEnv) report(t *testing.T, from, conversationID, body string) string {
	t.Helper()
	code, res := e.call(t, from, http.MethodPost, "/api/v1/conversations/"+conversationID+"/reports", body)
	if code != http.StatusCreated {
		t.Fatalf("report: %d %v", code, res)
	}
	return res["id"].(string)
}

func bodies(t *testing.T, detail map[string]any) []string {
	t.Helper()
	raw := detail["messages"].([]any)
	out := make([]string, len(raw))
	for i, m := range raw {
		out[i] = m.(map[string]any)["body"].(string)
	}
	return out
}

func TestReportRules(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	id, mine := e.startChat(t, ada, bob, "hello")
	_, theirs := e.startChat(t, bob, ada, "unwanted")
	_, elsewhere := e.startChat(t, cy, ada, "other chat")
	path := "/api/v1/conversations/" + id + "/reports"

	code, body := e.call(t, cy, http.MethodPost, path, `{"reason":"spam"}`)
	wantError(t, "non-member", code, body, http.StatusNotFound, "not_found")
	code, body = e.call(t, ada, http.MethodPost, "/api/v1/conversations/not-a-uuid/reports", `{"reason":"spam"}`)
	wantError(t, "bad ID", code, body, http.StatusNotFound, "not_found")
	code, body = e.call(t, ada, http.MethodPost, path, `{"reason":"rude"}`)
	wantError(t, "bad reason", code, body, http.StatusBadRequest, "bad_request")
	if body["field"] != "reason" {
		t.Errorf("field = %v", body["field"])
	}
	code, body = e.call(t, ada, http.MethodPost, path, fmt.Sprintf(`{"reason":"other","details":%q}`, strings.Repeat("x", 1001)))
	wantError(t, "long details", code, body, http.StatusBadRequest, "bad_request")
	if body["field"] != "details" {
		t.Errorf("field = %v", body["field"])
	}
	code, body = e.call(t, ada, http.MethodPost, path, fmt.Sprintf(`{"reason":"spam","messageId":%q}`, elsewhere))
	wantError(t, "message elsewhere", code, body, http.StatusNotFound, "not_found")
	code, body = e.call(t, ada, http.MethodPost, path, fmt.Sprintf(`{"reason":"spam","messageId":%q}`, mine))
	wantError(t, "own message", code, body, http.StatusBadRequest, "cannot_report_own_message")
	if n := e.countRows(t, "SELECT count(*) FROM message_reports"); n != 0 {
		t.Fatalf("%d reports stored by refused requests", n)
	}

	code, body = e.call(t, ada, http.MethodPost, path,
		fmt.Sprintf(`{"reason":"harassment","details":" repeated ","messageId":%q}`, theirs))
	if code != http.StatusCreated || body["reportedUserId"] != bob || body["blockedByMe"] != false || body["createdAt"] == nil {
		t.Fatalf("report: %d %v", code, body)
	}
	if n := e.countRows(t, "SELECT count(*) FROM blocks"); n != 0 {
		t.Error("the report blocked the user")
	}
	if n := e.countRows(t, `SELECT count(*) FROM message_reports WHERE reporter_id = $1 AND reported_id = $2
		AND reason = 'harassment' AND details = 'repeated' AND message_id = $3`, ada, bob, theirs); n != 1 {
		t.Errorf("stored rows = %d", n)
	}

	// A member can still report after blocking, and the receipt says so.
	e.block(t, ada, bob)
	code, body = e.call(t, ada, http.MethodPost, path, `{"reason":"spam"}`)
	if code != http.StatusCreated || body["blockedByMe"] != true {
		t.Errorf("report after blocking: %d %v", code, body)
	}
}

func TestReportDailyLimit(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	id, _ := e.startChat(t, ada, bob, "hello")
	for range 10 {
		e.report(t, bob, id, `{"reason":"spam"}`)
	}
	rec := e.send(t, bob, http.MethodPost, "/api/v1/conversations/"+id+"/reports", strings.NewReader(`{"reason":"spam"}`), "application/json")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("11th report: %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	if n := e.countRows(t, "SELECT count(*) FROM message_reports"); n != 10 {
		t.Errorf("%d reports stored", n)
	}
	e.report(t, ada, id, `{"reason":"spam"}`)
}

// NFR-13: an administrator sees the messages up to the report, never later ones, and every
// view leaves an audit row.
func TestAdminReadsUpToTheReport(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, admin := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	id, _ := e.startChat(t, ada, bob, "before one")
	if code, body := e.inConversation(t, bob, id, "before two", newClientID()); code != http.StatusCreated {
		t.Fatalf("%d %v", code, body)
	}
	rid := e.report(t, ada, id, `{"reason":"harassment","details":"rude"}`)
	if code, body := e.inConversation(t, bob, id, "after the report", newClientID()); code != http.StatusCreated {
		t.Fatalf("%d %v", code, body)
	}

	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET role = 'admin' WHERE id = $1", bob); err != nil {
		t.Fatal(err)
	}
	code, d := e.asAdmin(t, admin, http.MethodGet, adminReports+"/"+rid, "")
	if code != http.StatusOK {
		t.Fatalf("detail: %d %v", code, d)
	}
	if got := fmt.Sprint(bodies(t, d)); got != "[before two before one]" {
		t.Errorf("messages = %s", got)
	}
	r := d["report"].(map[string]any)
	if r["reporter"].(map[string]any)["role"] != "researcher" || r["reportedUser"].(map[string]any)["role"] != "admin" {
		t.Errorf("roles = %v, %v", r["reporter"], r["reportedUser"])
	}
	if r["status"] != "open" || r["reason"] != "harassment" || r["details"] != "rude" || r["conversationId"] != id ||
		r["reporter"].(map[string]any)["userId"] != ada || r["reportedUser"].(map[string]any)["userId"] != bob {
		t.Errorf("report = %v", r)
	}
	if n := e.countRows(t, "SELECT count(*) FROM message_report_views WHERE report_id = $1 AND admin_id = $2 AND viewed_at IS NOT NULL", rid, admin); n != 1 {
		t.Errorf("%d audit rows after one view", n)
	}
	e.asAdmin(t, admin, http.MethodGet, adminReports+"/"+rid, "")
	if n := e.countRows(t, "SELECT count(*) FROM message_report_views"); n != 2 {
		t.Errorf("%d audit rows after two views", n)
	}
	// The list holds no messages, so it writes no audit row.
	code, list := e.asAdmin(t, admin, http.MethodGet, adminReports, "")
	if code != http.StatusOK || list["total"] != float64(1) || strings.Contains(fmt.Sprint(list), "before one") {
		t.Errorf("list: %d %v", code, list)
	}
	if got := list["items"].([]any)[0].(map[string]any)["reportedUser"].(map[string]any)["role"]; got != "admin" {
		t.Errorf("role in the list = %v", got)
	}
	if n := e.countRows(t, "SELECT count(*) FROM message_report_views"); n != 2 {
		t.Errorf("%d audit rows after the list", n)
	}
}

// A later report moves only its own window.
func TestEachReportHasItsOwnMoment(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, admin := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	id, _ := e.startChat(t, ada, bob, "one")
	first := e.report(t, ada, id, `{"reason":"spam"}`)
	if code, body := e.inConversation(t, bob, id, "two", newClientID()); code != http.StatusCreated {
		t.Fatalf("%d %v", code, body)
	}
	second := e.report(t, ada, id, `{"reason":"spam"}`)
	_, d := e.asAdmin(t, admin, http.MethodGet, adminReports+"/"+first, "")
	if got := fmt.Sprint(bodies(t, d)); got != "[one]" {
		t.Errorf("first report: %s", got)
	}
	_, d = e.asAdmin(t, admin, http.MethodGet, adminReports+"/"+second, "")
	if got := fmt.Sprint(bodies(t, d)); got != "[two one]" {
		t.Errorf("second report: %s", got)
	}
}

// NFR-13: only a report opens a conversation, and the member routes stay closed to an
// administrator, also for a reported conversation.
func TestAdminReachesOnlyReportedConversations(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy, admin := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	reported, _ := e.startChat(t, ada, bob, "reported")
	private, _ := e.startChat(t, ada, cy, "private")
	e.report(t, bob, reported, `{"reason":"spam"}`)

	for _, id := range []string{reported, private} {
		for _, p := range []string{"/api/v1/conversations/" + id, "/api/v1/conversations/" + id + "/messages"} {
			code, body := e.asAdmin(t, admin, http.MethodGet, p, "")
			wantError(t, "admin on "+p, code, body, http.StatusNotFound, "not_found")
		}
		// A conversation's ID is not a report's.
		code, body := e.asAdmin(t, admin, http.MethodGet, adminReports+"/"+id, "")
		wantError(t, "conversation ID as a report", code, body, http.StatusNotFound, "not_found")
	}
	code, body := e.asAdmin(t, admin, http.MethodGet, adminReports+"/not-a-uuid", "")
	wantError(t, "bad ID", code, body, http.StatusNotFound, "not_found")
	if n := e.countRows(t, "SELECT count(*) FROM message_report_views"); n != 0 {
		t.Errorf("%d audit rows for reads that returned nothing", n)
	}
}

func TestNonAdminIsRefused(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	id, _ := e.startChat(t, ada, bob, "hello")
	rid := e.report(t, bob, id, `{"reason":"spam"}`)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, adminReports, ""},
		{http.MethodGet, adminReports + "/" + rid, ""},
		{http.MethodPost, adminReports + "/" + rid + "/resolve", `{"outcome":"dismissed"}`},
	} {
		code, body := e.call(t, bob, tc.method, tc.path, tc.body)
		wantError(t, tc.method+" "+tc.path, code, body, http.StatusForbidden, "forbidden")
	}
	if n := e.countRows(t, "SELECT count(*) FROM message_report_views"); n != 0 {
		t.Errorf("%d audit rows", n)
	}
	if n := e.countRows(t, "SELECT count(*) FROM message_reports WHERE resolved_at IS NOT NULL"); n != 0 {
		t.Error("a researcher resolved a report")
	}
}

// 100 messages a page, newest first; each page is a view.
func TestReportedConversationPaging(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, admin := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	id, _ := e.startChat(t, ada, bob, "first")
	if _, err := e.pool.Exec(t.Context(), `INSERT INTO messages (conversation_id, sender_id, body, client_id)
		SELECT $1, $2, 'm' || g, gen_random_uuid() FROM generate_series(1, 104) g`, id, bob); err != nil {
		t.Fatal(err)
	}
	rid := e.report(t, ada, id, `{"reason":"spam"}`)
	_, d := e.asAdmin(t, admin, http.MethodGet, adminReports+"/"+rid, "")
	if len(bodies(t, d)) != 100 || d["nextBefore"] == nil {
		t.Fatalf("first page: %d messages, next %v", len(bodies(t, d)), d["nextBefore"])
	}
	code, d := e.asAdmin(t, admin, http.MethodGet, adminReports+"/"+rid+"?before="+d["nextBefore"].(string), "")
	if code != http.StatusOK || len(bodies(t, d)) != 5 || d["nextBefore"] != nil {
		t.Fatalf("second page: %d, %d messages, next %v", code, len(bodies(t, d)), d["nextBefore"])
	}
	if got := bodies(t, d); got[4] != "first" {
		t.Errorf("oldest = %s", got[4])
	}
	if n := e.countRows(t, "SELECT count(*) FROM message_report_views"); n != 2 {
		t.Errorf("%d audit rows", n)
	}
	code, body := e.asAdmin(t, admin, http.MethodGet, adminReports+"/"+rid+"?before=x", "")
	wantError(t, "bad before", code, body, http.StatusBadRequest, "bad_request")
}

func TestResolveReport(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, admin := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	id, _ := e.startChat(t, ada, bob, "hello")
	rid := e.report(t, bob, id, `{"reason":"spam"}`)
	other := e.report(t, bob, id, `{"reason":"other"}`)
	path := adminReports + "/" + rid + "/resolve"

	code, body := e.asAdmin(t, admin, http.MethodPost, path, `{"outcome":"maybe"}`)
	wantError(t, "bad outcome", code, body, http.StatusBadRequest, "bad_request")
	code, body = e.asAdmin(t, admin, http.MethodPost, path, fmt.Sprintf(`{"outcome":"dismissed","note":%q}`, strings.Repeat("x", 1001)))
	wantError(t, "long note", code, body, http.StatusBadRequest, "bad_request")
	code, body = e.asAdmin(t, admin, http.MethodPost, adminReports+"/00000000-0000-4000-8000-000000000000/resolve", `{"outcome":"dismissed"}`)
	wantError(t, "no report", code, body, http.StatusNotFound, "not_found")

	code, body = e.asAdmin(t, admin, http.MethodPost, path, `{"outcome":"action_taken","note":" warned "}`)
	if code != http.StatusOK || body["status"] != "resolved" || body["outcome"] != "action_taken" || body["note"] != "warned" || body["resolvedAt"] == nil {
		t.Fatalf("resolve: %d %v", code, body)
	}
	if n := e.countRows(t, "SELECT count(*) FROM message_reports WHERE id = $1 AND resolved_by = $2", rid, admin); n != 1 {
		t.Error("resolved_by not stored")
	}
	code, body = e.asAdmin(t, admin, http.MethodPost, path, `{"outcome":"dismissed"}`)
	wantError(t, "twice", code, body, http.StatusConflict, "report_already_resolved")

	for status, want := range map[string]string{"open": other, "resolved": rid, "": other} {
		_, list := e.asAdmin(t, admin, http.MethodGet, adminReports+"?status="+status, "")
		items := list["items"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["id"] != want {
			t.Errorf("status %q: %v", status, items)
		}
	}
	code, body = e.asAdmin(t, admin, http.MethodGet, adminReports+"?status=closed", "")
	wantError(t, "bad status", code, body, http.StatusBadRequest, "bad_request")

	// A resolved report stays readable, and reading it is recorded.
	if code, _ := e.asAdmin(t, admin, http.MethodGet, adminReports+"/"+rid, ""); code != http.StatusOK {
		t.Errorf("resolved report: %d", code)
	}
	if n := e.countRows(t, "SELECT count(*) FROM message_report_views WHERE report_id = $1", rid); n != 1 {
		t.Errorf("%d audit rows", n)
	}
}

// A deleted account takes its conversation and the report with it (SRS 7, Retention), and the
// audit trail stays.
func TestAuditOutlivesTheReport(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, admin := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	id, _ := e.startChat(t, ada, bob, "hello")
	rid := e.report(t, bob, id, `{"reason":"spam"}`)
	e.asAdmin(t, admin, http.MethodGet, adminReports+"/"+rid, "")
	if _, err := e.pool.Exec(t.Context(), "DELETE FROM users WHERE id = $1", ada); err != nil {
		t.Fatal(err)
	}
	if n := e.countRows(t, "SELECT count(*) FROM message_reports"); n != 0 {
		t.Errorf("%d reports left", n)
	}
	if n := e.countRows(t, "SELECT count(*) FROM message_report_views WHERE report_id = $1 AND admin_id = $2", rid, admin); n != 1 {
		t.Errorf("%d audit rows left", n)
	}
}
