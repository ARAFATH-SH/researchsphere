//go:build integration

// End-to-end tests of notifications (FR-9.1, FR-9.4 to FR-9.6) through the real router,
// the PostgreSQL adapters and the cross-module transaction. They reuse the environment of
// media_integration_test.go.
package main

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
	socialdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/social/domain"
)

// notifications returns userID's first page of notifications.
func (e mediaEnv) notifications(t *testing.T, userID, query string) ([]map[string]any, any) {
	t.Helper()
	code, body := e.call(t, userID, http.MethodGet, "/api/v1/notifications"+query, "")
	if code != http.StatusOK {
		t.Fatalf("GET /notifications%s: %d %v", query, code, body)
	}
	raw := body["items"].([]any)
	items := make([]map[string]any, len(raw))
	for i, it := range raw {
		items[i] = it.(map[string]any)
	}
	return items, body["nextCursor"]
}

func (e mediaEnv) unreadCount(t *testing.T, userID string) float64 {
	t.Helper()
	code, body := e.call(t, userID, http.MethodGet, "/api/v1/notifications/unread-count", "")
	if code != http.StatusOK {
		t.Fatalf("GET unread-count: %d %v", code, body)
	}
	return body["count"].(float64)
}

func (e mediaEnv) countRows(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(t.Context(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e mediaEnv) mustSend(t *testing.T, from, to string) string {
	t.Helper()
	code, body := e.sendRequest(t, from, to)
	if code != http.StatusCreated {
		t.Fatalf("send: %d %v", code, body)
	}
	return body["requestId"].(string)
}

// Send -> the receiver sees it and the count is 1 -> accept -> the sender is told -> mark
// read -> the count is 0 (FR-9.1, FR-9.4 to FR-9.6).
func TestNotificationsConnectionFlow(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	reqID := e.mustSend(t, ada, bob)

	items, next := e.notifications(t, bob, "")
	if len(items) != 1 || next != nil {
		t.Fatalf("bob has %v (next %v), want one notification", items, next)
	}
	n := items[0]
	actor := n["actor"].(map[string]any)
	if n["type"] != "connection_request_received" || n["read"] != false || n["link"] != "/network" ||
		n["requestId"] != reqID || n["requestPending"] != true || actor["userId"] != ada || actor["fullName"] != "Ada" {
		t.Errorf("bob's notification %v, want an unread pending request from ada", n)
	}
	if c := e.unreadCount(t, bob); c != 1 {
		t.Errorf("bob's unread count %v, want 1", c)
	}
	if c := e.unreadCount(t, ada); c != 0 {
		t.Errorf("the sender's unread count %v, want 0", c)
	}

	if code, body := e.call(t, bob, http.MethodPost, "/api/v1/connections/requests/"+reqID+"/accept", ""); code != http.StatusOK {
		t.Fatalf("accept: %d %v", code, body)
	}
	items, _ = e.notifications(t, ada, "")
	if len(items) != 1 || items[0]["type"] != "connection_request_accepted" || items[0]["link"] != "/profile/"+bob ||
		items[0]["actor"].(map[string]any)["userId"] != bob {
		t.Fatalf("ada has %v, want bob's acceptance", items)
	}
	if _, ok := items[0]["requestPending"]; ok {
		t.Error("an accepted notification carries requestPending")
	}
	if got, _ := e.notifications(t, bob, ""); len(got) != 1 || got[0]["requestPending"] != false {
		t.Errorf("bob's received notification %v, want it kept and no longer pending", got)
	}

	accepted := items[0]["id"].(string)
	for range 2 { // idempotent
		if code, body := e.call(t, ada, http.MethodPost, "/api/v1/notifications/"+accepted+"/read", ""); code != http.StatusNoContent {
			t.Fatalf("mark read: %d %v", code, body)
		}
	}
	if c := e.unreadCount(t, ada); c != 0 {
		t.Errorf("ada's unread count %v after reading, want 0", c)
	}
	if items, _ := e.notifications(t, ada, ""); items[0]["read"] != true {
		t.Error("the notification is not read")
	}
	code, body := e.call(t, bob, http.MethodPost, "/api/v1/notifications/read-all", "")
	if code != http.StatusOK || body["marked"] != float64(1) {
		t.Errorf("read-all: %d %v, want 1 marked", code, body)
	}
	if c := e.unreadCount(t, bob); c != 0 {
		t.Errorf("bob's unread count %v after read-all, want 0", c)
	}
}

// Cancelling or rejecting removes the receiver's notification, nobody is told about a
// rejection (FR-7.6), and re-sending leaves one notification for the pair.
func TestNotificationsCancelAndReject(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)

	reqID := e.mustSend(t, ada, bob)
	if code, body := e.call(t, ada, http.MethodDelete, "/api/v1/connections/requests/"+reqID, ""); code != http.StatusNoContent {
		t.Fatalf("cancel: %d %v", code, body)
	}
	if items, _ := e.notifications(t, bob, ""); len(items) != 0 || e.unreadCount(t, bob) != 0 {
		t.Errorf("after the cancel bob has %v", items)
	}

	reqID = e.mustSend(t, ada, bob)
	if code, body := e.call(t, bob, http.MethodPost, "/api/v1/connections/requests/"+reqID+"/reject", ""); code != http.StatusNoContent {
		t.Fatalf("reject: %d %v", code, body)
	}
	if items, _ := e.notifications(t, bob, ""); len(items) != 0 {
		t.Errorf("after the rejection bob has %v", items)
	}
	if items, _ := e.notifications(t, ada, ""); len(items) != 0 || e.unreadCount(t, ada) != 0 {
		t.Errorf("the sender was told about the rejection: %v", items)
	}

	reqID = e.mustSend(t, ada, bob)
	items, _ := e.notifications(t, bob, "")
	if len(items) != 1 || items[0]["requestId"] != reqID || items[0]["requestPending"] != true {
		t.Errorf("after re-sending bob has %v, want one pending notification", items)
	}
	if n := e.countRows(t, "SELECT count(*) FROM notifications WHERE user_id = $1 AND actor_id = $2", bob, ada); n != 1 {
		t.Errorf("%d notification rows for the pair, want 1", n)
	}
}

// A suspended actor's notifications are hidden from the list and the count and cannot be
// marked read; they come back when the suspension is lifted.
func TestNotificationsHideSuspendedActor(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.mustSend(t, ada, bob)
	items, _ := e.notifications(t, bob, "")
	id := items[0]["id"].(string)

	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET suspended_at = now() WHERE id = $1", ada); err != nil {
		t.Fatal(err)
	}
	if items, _ := e.notifications(t, bob, ""); len(items) != 0 {
		t.Errorf("bob sees %v from a suspended actor", items)
	}
	if c := e.unreadCount(t, bob); c != 0 {
		t.Errorf("unread count %v, want 0", c)
	}
	if code, _ := e.call(t, bob, http.MethodPost, "/api/v1/notifications/"+id+"/read", ""); code != http.StatusNotFound {
		t.Errorf("mark read of a hidden notification: %d, want 404", code)
	}
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET suspended_at = NULL WHERE id = $1", ada); err != nil {
		t.Fatal(err)
	}
	if c := e.unreadCount(t, bob); c != 1 {
		t.Errorf("unread count %v after the suspension is lifted, want 1", c)
	}
}

// Only the recipient may mark a notification read; any other ID is 404 (FR-9.6).
func TestNotificationsMarkReadOthers404(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	e.mustSend(t, ada, bob)
	items, _ := e.notifications(t, bob, "")
	id := items[0]["id"].(string)

	for _, tc := range []struct{ user, id string }{
		{ada, id}, {cy, id}, {bob, "00000000-0000-0000-0000-000000000000"}, {bob, "not-a-uuid"},
	} {
		if code, body := e.call(t, tc.user, http.MethodPost, "/api/v1/notifications/"+tc.id+"/read", ""); code != http.StatusNotFound || body["code"] != "not_found" {
			t.Errorf("mark read %s as %s: %d %v, want 404", tc.id, tc.user[:8], code, body)
		}
	}
	if code, body := e.call(t, cy, http.MethodPost, "/api/v1/notifications/read-all", ""); code != http.StatusOK || body["marked"] != float64(0) {
		t.Errorf("read-all by another user: %d %v, want 0 marked", code, body)
	}
	if c := e.unreadCount(t, bob); c != 1 {
		t.Errorf("bob's notification was marked by someone else: count %v", c)
	}
}

// 25 notifications page as 20 and 5 by cursor, newest first; bad limits and cursors are
// refused (FR-9.5).
func TestNotificationsCursorPaging(t *testing.T) {
	e := newMediaEnv(t)
	bob := e.newProfileUser(t)
	for range 25 {
		e.mustSend(t, e.newProfileUser(t), bob)
	}
	page1, next := e.notifications(t, bob, "")
	if len(page1) != 20 || next == nil {
		t.Fatalf("page 1: %d items, next %v", len(page1), next)
	}
	page2, next2 := e.notifications(t, bob, "?cursor="+next.(string))
	if len(page2) != 5 || next2 != nil {
		t.Fatalf("page 2: %d items, next %v", len(page2), next2)
	}
	seen := map[string]bool{}
	all := append(page1, page2...)
	for i, it := range all {
		id := it["id"].(string)
		if seen[id] {
			t.Errorf("%s is on both pages", id)
		}
		seen[id] = true
		if i > 0 && it["createdAt"].(string) > all[i-1]["createdAt"].(string) {
			t.Errorf("item %d is newer than the one before", i)
		}
	}
	if items, _ := e.notifications(t, bob, "?limit=100"); len(items) != 25 {
		t.Errorf("limit 100 gave %d items, want all 25 (capped at 50)", len(items))
	}
	if items, next := e.notifications(t, bob, "?limit=5"); len(items) != 5 || next == nil {
		t.Errorf("limit 5 gave %d items, next %v", len(items), next)
	}
	for _, q := range []string{"?limit=0", "?limit=-1", "?cursor=garbage", "?cursor=" + testenv.Word(t)} {
		if code, body := e.call(t, bob, http.MethodGet, "/api/v1/notifications"+q, ""); code != http.StatusBadRequest || body["code"] != "bad_request" {
			t.Errorf("%s: %d %v, want 400 bad_request", q, code, body)
		}
	}
}

// failingNotifier calls the real notifier, so its write is made, and then fails when fail
// is set, as if something after the write went wrong.
type failingNotifier struct {
	next socialdomain.Notifier
	fail *atomic.Bool
}

var errNotifyFailed = errors.New("notifier failed after writing")

func (n failingNotifier) after(err error) error {
	if err != nil {
		return err
	}
	if n.fail.Load() {
		return errNotifyFailed
	}
	return nil
}

func (n failingNotifier) RequestSent(ctx context.Context, r socialdomain.Request) error {
	return n.after(n.next.RequestSent(ctx, r))
}

func (n failingNotifier) RequestAccepted(ctx context.Context, r socialdomain.Request) error {
	return n.after(n.next.RequestAccepted(ctx, r))
}

func (n failingNotifier) RequestWithdrawn(ctx context.Context, id string) error {
	return n.after(n.next.RequestWithdrawn(ctx, id))
}

// The notification and the connection change commit together: when the notifier fails
// after writing, neither the change nor the notification is stored (FR-9.1).
func TestNotificationsCommitWithConnectionChange(t *testing.T) {
	fail := &atomic.Bool{}
	e := newMediaEnvOnPool(t, testenv.DB(t), "", "", envOptions{wrapNotifier: func(n socialdomain.Notifier) socialdomain.Notifier {
		return failingNotifier{next: n, fail: fail}
	}})
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	rows := func() (connections, pending, notifications int) {
		return e.countRows(t, "SELECT count(*) FROM connections"),
			e.countRows(t, "SELECT count(*) FROM connections WHERE status = 'pending'"),
			e.countRows(t, "SELECT count(*) FROM notifications")
	}

	fail.Store(true)
	if code, body := e.sendRequest(t, ada, bob); code != http.StatusInternalServerError {
		t.Fatalf("send with a failing notifier: %d %v, want 500", code, body)
	}
	if c, _, n := rows(); c != 0 || n != 0 {
		t.Fatalf("after a failed send: %d connections, %d notifications; want none", c, n)
	}

	fail.Store(false)
	reqID := e.mustSend(t, ada, bob)
	fail.Store(true)
	for _, op := range []struct{ user, method, path string }{
		{bob, http.MethodPost, "/api/v1/connections/requests/" + reqID + "/accept"},
		{bob, http.MethodPost, "/api/v1/connections/requests/" + reqID + "/reject"},
		{ada, http.MethodDelete, "/api/v1/connections/requests/" + reqID},
	} {
		if code, body := e.call(t, op.user, op.method, op.path, ""); code != http.StatusInternalServerError {
			t.Errorf("%s %s: %d %v, want 500", op.method, op.path, code, body)
		}
		if c, p, n := rows(); c != 1 || p != 1 || n != 1 {
			t.Errorf("after a failed %s %s: %d connections (%d pending), %d notifications; want the pending request and its one notification",
				op.method, op.path, c, p, n)
		}
	}
	if n := e.countRows(t, "SELECT count(*) FROM notifications WHERE type = 'connection_request_received' AND entity_id = $1", reqID); n != 1 {
		t.Errorf("the received notification is gone (%d)", n)
	}
}
