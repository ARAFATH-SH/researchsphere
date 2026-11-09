//go:build integration

// End-to-end tests of editing and deleting one's own messages (FR-16.5, FR-16.10, NFR-13)
// through the real router, the PostgreSQL adapters, Redis and the WebSocket, on the test
// database. They reuse the environment of media_integration_test.go.
package main

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	messagingusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
	realtimedomain "github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/domain"
)

func messagePath(conversationID, messageID string) string {
	return "/api/v1/conversations/" + conversationID + "/messages/" + messageID
}

func (e mediaEnv) edit(t *testing.T, who, conversationID, messageID, body string) (int, map[string]any) {
	t.Helper()
	return e.call(t, who, http.MethodPatch, messagePath(conversationID, messageID), fmt.Sprintf(`{"body":%q}`, body))
}

func (e mediaEnv) deleteMessage(t *testing.T, who, conversationID, messageID string) (int, map[string]any) {
	t.Helper()
	return e.call(t, who, http.MethodDelete, messagePath(conversationID, messageID), "")
}

// thread returns the conversation's newest page as userID sees it.
func (e mediaEnv) thread(t *testing.T, userID, conversationID string) []map[string]any {
	t.Helper()
	code, body := e.call(t, userID, http.MethodGet, "/api/v1/conversations/"+conversationID+"/messages", "")
	if code != http.StatusOK {
		t.Fatalf("GET messages: %d %v", code, body)
	}
	raw := body["items"].([]any)
	out := make([]map[string]any, len(raw))
	for i, m := range raw {
		out[i] = m.(map[string]any)
	}
	return out
}

// FR-16.5: the other member sees the new text and editedAt, in the thread and the list; only the
// sender may edit, and only within the window.
func TestEditReachesTheOtherMember(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, carl := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	id, msg := e.startChat(t, ada, bob, "draft one")

	code, m := e.edit(t, ada, id, msg, " hello ")
	if code != http.StatusOK || m["body"] != "hello" || m["editedAt"] == nil || m["deleted"] != false || m["id"] != msg {
		t.Fatalf("edit: %d %v", code, m)
	}
	got := e.thread(t, bob, id)[0]
	if got["body"] != "hello" || got["editedAt"] != m["editedAt"] || got["deleted"] != false {
		t.Errorf("bob sees %v", got)
	}
	if _, leaked := got["versions"]; leaked {
		t.Errorf("a member got versions: %v", got)
	}
	for _, who := range []string{ada, bob} {
		if strings.Contains(fmt.Sprint(e.thread(t, who, id), e.box(t, who, "inbox")), "draft one") {
			t.Errorf("%s sees the earlier text", who)
		}
	}
	if last := e.box(t, bob, "inbox")[0]["lastMessage"].(map[string]any); last["body"] != "hello" || last["deleted"] != false {
		t.Errorf("preview = %v", last)
	}

	// An unchanged text is a no-op.
	if code, again := e.edit(t, ada, id, msg, "hello"); code != http.StatusOK || again["editedAt"] != m["editedAt"] {
		t.Errorf("unchanged: %d %v", code, again)
	}
	// Anyone else gets the answer for an unknown message (NFR-13).
	for _, who := range []string{bob, carl} {
		code, body := e.edit(t, who, id, msg, "mine now")
		wantError(t, "edit by another user", code, body, http.StatusNotFound, "not_found")
		code, body = e.deleteMessage(t, who, id, msg)
		wantError(t, "delete by another user", code, body, http.StatusNotFound, "not_found")
	}
	code, body := e.edit(t, ada, id, "not-a-uuid", "x")
	wantError(t, "bad message ID", code, body, http.StatusNotFound, "not_found")
	code, body = e.edit(t, ada, id, msg, "   ")
	wantError(t, "empty body", code, body, http.StatusBadRequest, "invalid_body")

	// 15 minutes after sending, the window has closed.
	if _, err := e.pool.Exec(t.Context(), "UPDATE messages SET created_at = created_at - interval '16 minutes', edited_at = NULL WHERE id = $1", msg); err != nil {
		t.Fatal(err)
	}
	code, body = e.edit(t, ada, id, msg, "too late")
	wantError(t, "after the window", code, body, http.StatusConflict, "edit_window_closed")
	if code, _ := e.deleteMessage(t, ada, id, msg); code != http.StatusNoContent {
		t.Errorf("delete after the window: %d", code)
	}
}

// FR-16.5: a deleted message stays as a tombstone in the thread and the list preview, stops
// counting as unread, and can be deleted again; it cannot be edited.
func TestDeleteLeavesATombstone(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	id, first := e.startChat(t, ada, bob, "first")
	code, sent := e.inConversation(t, ada, id, "regret", newClientID())
	if code != http.StatusCreated {
		t.Fatalf("send: %d %v", code, sent)
	}
	last := sent["message"].(map[string]any)["id"].(string)
	if total, _ := e.messageCounts(t, bob); total != 2 {
		t.Fatalf("unread before = %v", total)
	}

	if code, body := e.deleteMessage(t, ada, id, last); code != http.StatusNoContent {
		t.Fatalf("delete: %d %v", code, body)
	}
	for _, who := range []string{ada, bob} {
		th := e.thread(t, who, id)
		if len(th) != 2 || th[0]["id"] != last || th[0]["deleted"] != true || th[0]["body"] != nil || th[1]["body"] != "first" {
			t.Errorf("%s sees %v", who, th)
		}
		if strings.Contains(fmt.Sprint(th), "regret") {
			t.Errorf("%s sees the deleted text", who)
		}
		preview := e.box(t, who, "inbox")[0]["lastMessage"].(map[string]any)
		if preview["deleted"] != true || preview["body"] != nil || preview["senderId"] != ada {
			t.Errorf("%s's preview = %v", who, preview)
		}
	}
	if total, _ := e.messageCounts(t, bob); total != 1 {
		t.Errorf("unread after = %v, want 1", total)
	}
	if code, body := e.deleteMessage(t, ada, id, last); code != http.StatusNoContent {
		t.Errorf("second delete: %d %v", code, body)
	}
	code, body := e.edit(t, ada, id, last, "back again")
	wantError(t, "edit of a tombstone", code, body, http.StatusNotFound, "not_found")
	if n := e.countRows(t, "SELECT count(*) FROM message_versions WHERE message_id = $1 AND ended_by = 'deleted' AND body = 'regret'", last); n != 1 {
		t.Errorf("%d kept versions of the deleted text", n)
	}

	// After a block the sender can no longer edit, but can still delete.
	e.block(t, bob, ada)
	code, body = e.edit(t, ada, id, first, "first, edited")
	wantError(t, "edit after being blocked", code, body, http.StatusForbidden, "cannot_message")
	if code, body := e.deleteMessage(t, ada, id, first); code != http.StatusNoContent {
		t.Errorf("delete after being blocked: %d %v", code, body)
	}
}

// FR-16.10: an edit counts toward the message limits.
func TestEditCountsTowardTheMessageLimit(t *testing.T) {
	e := newMediaEnvOnPool(t, testenv.DB(t), "", "", envOptions{messageLimits: &messagingusecase.Limits{
		MessagesPerMinute: 2, MessagesPerDay: 1000, RequestsPerMinute: 5, RequestsPerDay: 20,
	}})
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	id, msg := e.startChat(t, ada, bob, "one")
	if code, body := e.edit(t, ada, id, msg, "one, edited"); code != http.StatusOK {
		t.Fatalf("first edit: %d %v", code, body)
	}
	code, body := e.edit(t, ada, id, msg, "one, edited twice")
	wantError(t, "edit over the limit", code, body, http.StatusTooManyRequests, "rate_limited")
	code, body = e.inConversation(t, ada, id, "two", newClientID())
	wantError(t, "send over the limit", code, body, http.StatusTooManyRequests, "rate_limited")
	if code, _ := e.deleteMessage(t, ada, id, msg); code != http.StatusNoContent {
		t.Errorf("a deletion is not limited: %d", code)
	}
}

// NFR-13: the administrator's report detail shows the text at the report's moment, the versions
// shown up to it and the deletion, and nothing written after the report. Members never see
// versions.
func TestAdminSeesHistoryAtTheReportsMoment(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, admin := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	id, _ := e.startChat(t, ada, bob, "hi")
	code, sent := e.inConversation(t, bob, id, "first draft", newClientID())
	if code != http.StatusCreated {
		t.Fatalf("send: %d %v", code, sent)
	}
	msg := sent["message"].(map[string]any)["id"].(string)
	if code, body := e.edit(t, bob, id, msg, "second draft"); code != http.StatusOK {
		t.Fatalf("edit: %d %v", code, body)
	}
	rid := e.report(t, ada, id, fmt.Sprintf(`{"reason":"harassment","messageId":%q}`, msg))
	if code, body := e.edit(t, bob, id, msg, "written after the report"); code != http.StatusOK {
		t.Fatalf("edit after the report: %d %v", code, body)
	}
	if code, body := e.deleteMessage(t, bob, id, msg); code != http.StatusNoContent {
		t.Fatalf("delete: %d %v", code, body)
	}

	code, d := e.asAdmin(t, admin, http.MethodGet, adminReports+"/"+rid, "")
	if code != http.StatusOK {
		t.Fatalf("detail: %d %v", code, d)
	}
	if strings.Contains(fmt.Sprint(d), "written after the report") {
		t.Errorf("the detail shows text written after the report: %v", d)
	}
	msgs := d["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %v", msgs)
	}
	m := msgs[0].(map[string]any)
	if m["id"] != msg || m["body"] != "second draft" || m["deletedAt"] == nil || m["editedAt"] == nil || m["changedAfterReport"] != true {
		t.Errorf("reported message = %v", m)
	}
	versions := m["versions"].([]any)
	want := [][2]string{{"first draft", "edited"}, {"second draft", "edited"}}
	if len(versions) != len(want) {
		t.Fatalf("versions = %v", versions)
	}
	for i, w := range want {
		v := versions[i].(map[string]any)
		if v["body"] != w[0] || v["endedBy"] != w[1] || v["shownFrom"] == nil || v["shownUntil"] == nil {
			t.Errorf("version %d = %v, want %v", i, v, w)
		}
	}
	if plain := msgs[1].(map[string]any); plain["body"] != "hi" || plain["changedAfterReport"] != false || len(plain["versions"].([]any)) != 0 {
		t.Errorf("unchanged message = %v", plain)
	}

	// The members see only the tombstone.
	for _, who := range []string{ada, bob} {
		th := e.thread(t, who, id)
		if s := fmt.Sprint(th); strings.Contains(s, "draft") || strings.Contains(s, "written after") || strings.Contains(s, "versions") {
			t.Errorf("%s sees %s", who, s)
		}
		if th[0]["deleted"] != true {
			t.Errorf("%s sees %v", who, th[0])
		}
	}
}

// The edit and deletion events reach the other member and the sender's other tabs, after commit.
func TestEditAndDeleteEventsReachEveryTab(t *testing.T) {
	e := newMediaEnv(t)
	srv := serve(t, e.router)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	id, msg := e.startChat(t, ada, bob, "draft one")
	bobTab := dial(t, srv, bob)
	adaOtherTab := dial(t, srv, ada)

	code, edited := e.edit(t, ada, id, msg, "hello")
	if code != http.StatusOK {
		t.Fatalf("edit: %d %v", code, edited)
	}
	isEdit := func(d map[string]any) bool {
		m := d["message"].(map[string]any)
		return m["id"] == msg && m["body"] == "hello" && m["editedAt"] == edited["editedAt"] && m["conversationId"] == id
	}
	bobTab.expect(t, realtimedomain.EventMessageUpdated, isEdit)
	adaOtherTab.expect(t, realtimedomain.EventMessageUpdated, isEdit)

	// An unchanged text publishes nothing.
	if code, _ := e.edit(t, ada, id, msg, "hello"); code != http.StatusOK {
		t.Fatalf("unchanged edit: %d", code)
	}
	bobTab.silent(t, realtimedomain.EventMessageUpdated, 200*time.Millisecond)

	if code, body := e.deleteMessage(t, ada, id, msg); code != http.StatusNoContent {
		t.Fatalf("delete: %d %v", code, body)
	}
	isDelete := func(d map[string]any) bool { return d["conversationId"] == id && d["messageId"] == msg }
	bobTab.expect(t, realtimedomain.EventMessageDeleted, isDelete)
	adaOtherTab.expect(t, realtimedomain.EventMessageDeleted, isDelete)
	if u := bobTab.expect(t, realtimedomain.EventUnreadChanged, func(d map[string]any) bool { return d["total"] == 0.0 }); u.Data["requests"] != 0.0 {
		t.Errorf("unread = %+v", u)
	}

	// Deleting again publishes nothing.
	if code, _ := e.deleteMessage(t, ada, id, msg); code != http.StatusNoContent {
		t.Fatalf("second delete: %d", code)
	}
	bobTab.silent(t, realtimedomain.EventMessageDeleted, 200*time.Millisecond)
}
