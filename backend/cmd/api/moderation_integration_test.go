//go:build integration

// End-to-end tests of moderation (FR-3.4, FR-15.3, FR-15.5 to FR-15.10), through the real router,
// the PostgreSQL adapters and Redis, on the test database. They reuse the environment of
// media_integration_test.go, with the real auth usecase and a clock the test moves.
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/argon2"
	moderationdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/moderation/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
)

const (
	modReason   = "Repeated spam in your messages"
	modSecret   = "INTERNAL-NOTE-do-not-send"
	modBody     = `{"reason":"` + modReason + `","note":"` + modSecret + `"}`
	modPassword = "correct horse battery"
	modDay      = 24 * time.Hour
)

// modClock is a clock the test moves.
type modClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *modClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *modClock) move(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// inbox keeps the emails sent.
type inbox struct {
	mu   sync.Mutex
	sent []string
}

func (b *inbox) SendText(_ context.Context, to, name, subject, body string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sent = append(b.sent, strings.Join([]string{to, name, subject, body}, "\n"))
	return nil
}

func (b *inbox) all() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.sent...)
}

type modEnv struct {
	mediaEnv
	clock *modClock
	mail  *inbox
	admin string
}

func newModEnv(t *testing.T) modEnv {
	t.Helper()
	clock := &modClock{t: time.Now()}
	mail := &inbox{}
	e := newMediaEnvOnPool(t, testenv.DB(t), "", "", envOptions{now: clock.now, mail: mail, realAuth: true})
	admin := e.newProfileUser(t)
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET role = 'admin' WHERE id = $1", admin); err != nil {
		t.Fatal(err)
	}
	return modEnv{mediaEnv: e, clock: clock, mail: mail, admin: admin}
}

func (e modEnv) adminDo(t *testing.T, method, path, body string) (int, map[string]any) {
	t.Helper()
	return e.asAdmin(t, e.admin, method, path, body)
}

// userPath is the admin path of an action on a user.
func userPath(id, action string) string { return "/api/v1/admin/users/" + id + "/" + action }

func (e modEnv) restrict(t *testing.T, id string, days int) map[string]any {
	t.Helper()
	code, res := e.adminDo(t, http.MethodPost, userPath(id, "restrictions"),
		fmt.Sprintf(`{"kind":"messaging","days":%d,"reason":%q}`, days, modReason))
	if code != http.StatusCreated {
		t.Fatalf("restrict: %d %v", code, res)
	}
	return res
}

func (e modEnv) suspend(t *testing.T, id, body string) map[string]any {
	t.Helper()
	code, res := e.adminDo(t, http.MethodPost, userPath(id, "suspension"), body)
	if code != http.StatusOK {
		t.Fatalf("suspend: %d %v", code, res)
	}
	return res
}

// withPassword gives the user a password and returns the address to log in with.
func (e modEnv) withPassword(t *testing.T, id string) string {
	t.Helper()
	hash, err := argon2.New(argon2.DefaultParams).Hash(modPassword)
	if err != nil {
		t.Fatal(err)
	}
	var email string
	if err := e.pool.QueryRow(t.Context(), "UPDATE users SET password_hash = $1 WHERE id = $2 RETURNING email::text", hash, id).Scan(&email); err != nil {
		t.Fatal(err)
	}
	return email
}

func (e modEnv) login(t *testing.T, email, password string) (int, map[string]any) {
	t.Helper()
	body := fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec.Code, decodeBody(t, rec.Body.Bytes())
}

func (e modEnv) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(t.Context(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// reported returns a report by reporter about target.
func (e modEnv) reported(t *testing.T, reporter, target string) string {
	t.Helper()
	e.link(t, reporter, target)
	conversation, _ := e.startChat(t, target, reporter, "buy my book")
	return e.report(t, reporter, conversation, `{"reason":"spam"}`)
}

func (e modEnv) reportOutcome(t *testing.T, id string) string {
	t.Helper()
	var outcome *string
	if err := e.pool.QueryRow(t.Context(), "SELECT outcome FROM message_reports WHERE id = $1", id).Scan(&outcome); err != nil {
		t.Fatal(err)
	}
	if outcome == nil {
		return ""
	}
	return *outcome
}

// A warning: notification, email, banner list, acknowledgement, audit row (FR-15.5, FR-15.8).
func TestWarningFlow(t *testing.T) {
	e := newModEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)

	code, action := e.adminDo(t, http.MethodPost, userPath(ada, "warnings"), modBody)
	if code != http.StatusCreated {
		t.Fatalf("warn: %d %v", code, action)
	}
	if action["action"] != "warning" || action["note"] != modSecret || action["adminId"] != e.admin || action["userId"] != ada {
		t.Errorf("audit row = %v", action)
	}
	id := action["warningId"].(string)

	// A system notification: no actor, the warning's link, and counted in the badge.
	if got := e.unreadCount(t, ada); got != 1 {
		t.Errorf("unread = %v, want 1", got)
	}
	code, list := e.call(t, ada, http.MethodGet, "/api/v1/notifications", "")
	if code != http.StatusOK {
		t.Fatalf("list: %d %v", code, list)
	}
	items := list["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("notifications = %v", items)
	}
	n := items[0].(map[string]any)
	if n["type"] != "account_warning" || n["link"] != "/warnings/"+id || n["actor"] != nil {
		t.Errorf("notification = %v", n)
	}
	if strings.Contains(fmt.Sprint(n), e.admin) {
		t.Errorf("the notification names the administrator: %v", n)
	}

	// The email has the reason, not the note.
	sent := e.mail.all()
	if len(sent) != 1 || !strings.Contains(sent[0], modReason) || strings.Contains(sent[0], modSecret) {
		t.Fatalf("emails = %q", sent)
	}

	// The banner's list shows the warning without the note or the administrator.
	code, pending := e.call(t, ada, http.MethodGet, "/api/v1/me/warnings/pending", "")
	if code != http.StatusOK || len(pending["items"].([]any)) != 1 {
		t.Fatalf("pending: %d %v", code, pending)
	}
	shown := fmt.Sprint(pending)
	if !strings.Contains(shown, modReason) || strings.Contains(shown, modSecret) || strings.Contains(shown, e.admin) {
		t.Errorf("pending = %s", shown)
	}

	// Nobody else can read or acknowledge it.
	path := "/api/v1/me/warnings/" + id
	code, body := e.call(t, bob, http.MethodGet, path, "")
	wantError(t, "another user's warning", code, body, http.StatusNotFound, "not_found")
	code, body = e.call(t, bob, http.MethodPost, path+"/acknowledge", "")
	wantError(t, "acknowledging another user's warning", code, body, http.StatusNotFound, "not_found")
	code, body = e.call(t, ada, http.MethodGet, "/api/v1/me/warnings/not-a-uuid", "")
	wantError(t, "not a UUID", code, body, http.StatusNotFound, "not_found")

	// Acknowledged: gone from the banner, still readable through the link, and twice is fine.
	for range 2 {
		if code, body = e.call(t, ada, http.MethodPost, path+"/acknowledge", ""); code != http.StatusNoContent {
			t.Fatalf("acknowledge: %d %v", code, body)
		}
	}
	if _, pending = e.call(t, ada, http.MethodGet, "/api/v1/me/warnings/pending", ""); len(pending["items"].([]any)) != 0 {
		t.Errorf("pending after acknowledging = %v", pending)
	}
	if code, body = e.call(t, ada, http.MethodGet, path, ""); code != http.StatusOK || body["acknowledged"] != true || body["acknowledgedAt"] == nil {
		t.Errorf("after acknowledging: %d %v", code, body)
	}
	code, body = e.call(t, ada, http.MethodGet, path, "")
	if code != http.StatusOK || body["acknowledged"] != true || body["reason"] != modReason {
		t.Errorf("warning after acknowledging: %d %v", code, body)
	}

	// The history shows the time it was acknowledged.
	_, history := e.adminDo(t, http.MethodGet, userPath(ada, "moderation"), "")
	row := history["items"].([]any)[0].(map[string]any)
	if row["acknowledgedAt"] == nil {
		t.Errorf("history row = %v", row)
	}
}

// A system notification is never hidden, and marks read like any other, alongside an actor's,
// which is hidden when its actor is suspended (FR-9.5, FR-15.5).
func TestSystemAndActorNotificationsInTheList(t *testing.T) {
	e := newModEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	if code, body := e.sendRequest(t, bob, ada); code != http.StatusCreated {
		t.Fatalf("request: %d %v", code, body)
	}
	if code, body := e.adminDo(t, http.MethodPost, userPath(ada, "warnings"), modBody); code != http.StatusCreated {
		t.Fatalf("warn: %d %v", code, body)
	}
	if got := e.unreadCount(t, ada); got != 2 {
		t.Fatalf("unread = %v, want 2", got)
	}

	e.suspend(t, bob, `{"reason":"`+modReason+`"}`)
	if got := e.unreadCount(t, ada); got != 1 {
		t.Errorf("unread with the actor suspended = %v, want 1", got)
	}
	_, list := e.call(t, ada, http.MethodGet, "/api/v1/notifications", "")
	items := list["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["type"] != "account_warning" {
		t.Fatalf("items = %v", items)
	}
	id := items[0].(map[string]any)["id"].(string)
	if code, body := e.call(t, ada, http.MethodPost, "/api/v1/notifications/"+id+"/read", ""); code >= 300 {
		t.Fatalf("mark read: %d %v", code, body)
	}
	if got := e.unreadCount(t, ada); got != 0 {
		t.Errorf("unread after marking = %v, want 0", got)
	}
}

// A restriction stops sends with the end time, leaves everything else, ends by itself and can be
// lifted (FR-15.6).
func TestMessagingRestriction(t *testing.T) {
	e := newModEnv(t)
	ada, bob, cy, dee := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	conversation, _ := e.startChat(t, ada, bob, "hello")
	request, _ := e.startChat(t, cy, ada, "may I ask?")

	action := e.restrict(t, ada, 3)
	ends := action["endsAt"].(string)
	if action["action"] != "restriction" || ends == "" {
		t.Fatalf("audit row = %v", action)
	}

	code, body := e.inConversation(t, ada, conversation, "again", newClientID())
	wantError(t, "send", code, body, http.StatusForbidden, "messaging_restricted")
	if body["until"] == nil {
		t.Errorf("no end time: %v", body)
	}
	code, body = e.direct(t, ada, dee, "hi", newClientID())
	wantError(t, "message request", code, body, http.StatusForbidden, "messaging_restricted")

	// Everything else works.
	if code, body = e.call(t, ada, http.MethodGet, "/api/v1/conversations/"+conversation+"/messages", ""); code != http.StatusOK {
		t.Errorf("read: %d %v", code, body)
	}
	if code, body = e.call(t, ada, http.MethodPost, "/api/v1/conversations/"+request+"/accept", ""); code != http.StatusOK {
		t.Errorf("accept: %d %v", code, body)
	}
	if code, body = e.call(t, ada, http.MethodPost, "/api/v1/conversations/"+conversation+"/reports", `{"reason":"spam"}`); code != http.StatusCreated {
		t.Errorf("report: %d %v", code, body)
	}
	if code, body = e.sendRequest(t, ada, dee); code != http.StatusCreated {
		t.Errorf("connection request: %d %v", code, body)
	}
	if code, body = e.inConversation(t, bob, conversation, "others can send", newClientID()); code != http.StatusCreated {
		t.Errorf("the other member's send: %d %v", code, body)
	}

	code, body = e.adminDo(t, http.MethodPost, userPath(ada, "restrictions"), `{"kind":"messaging","days":1,"reason":"`+modReason+`"}`)
	wantError(t, "a second restriction", code, body, http.StatusConflict, "already_restricted")

	// It ends by itself, at its end time.
	e.clock.move(3*modDay - time.Minute)
	code, body = e.inConversation(t, ada, conversation, "still no", newClientID())
	wantError(t, "just before the end", code, body, http.StatusForbidden, "messaging_restricted")
	e.clock.move(time.Minute)
	if code, body = e.inConversation(t, ada, conversation, "free", newClientID()); code != http.StatusCreated {
		t.Errorf("after the end: %d %v", code, body)
	}

	// Too late to lift.
	lift := "/api/v1/admin/restrictions/" + action["restrictionId"].(string) + "/lift"
	code, body = e.adminDo(t, http.MethodPost, lift, `{"reason":"`+modReason+`"}`)
	wantError(t, "lift after the end", code, body, http.StatusConflict, "restriction_not_active")
}

func TestLiftRestriction(t *testing.T) {
	e := newModEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	conversation, _ := e.startChat(t, ada, bob, "hello")
	restrictedUntil := func() any {
		t.Helper()
		code, me := e.call(t, ada, http.MethodGet, "/api/v1/auth/me", "")
		if code != http.StatusOK {
			t.Fatalf("auth/me: %d %v", code, me)
		}
		v, ok := me["messagingRestrictedUntil"]
		if !ok {
			t.Fatalf("auth/me has no messagingRestrictedUntil: %v", me)
		}
		return v
	}
	if v := restrictedUntil(); v != nil {
		t.Errorf("before the restriction: %v", v)
	}
	action := e.restrict(t, ada, 30)
	if v := restrictedUntil(); v == nil {
		t.Error("auth/me does not show the restriction")
	} else if ends, err := time.Parse(time.RFC3339, v.(string)); err != nil || ends.Sub(e.clock.now()) < 29*24*time.Hour {
		t.Errorf("restricted until %v: %v", v, err)
	}
	lift := "/api/v1/admin/restrictions/" + action["restrictionId"].(string) + "/lift"

	code, body := e.adminDo(t, http.MethodPost, lift, `{"reason":"short"}`)
	wantError(t, "a short reason", code, body, http.StatusBadRequest, "bad_request")
	if body["field"] != "reason" {
		t.Errorf("field = %v", body["field"])
	}
	before := len(e.mail.all())
	if code, body = e.adminDo(t, http.MethodPost, lift, modBody); code != http.StatusNoContent {
		t.Fatalf("lift: %d %v", code, body)
	}
	if got := e.mail.all(); len(got) != before+1 || strings.Contains(got[before], modSecret) {
		t.Errorf("emails = %q", got)
	}
	if code, body = e.inConversation(t, ada, conversation, "free", newClientID()); code != http.StatusCreated {
		t.Errorf("after the lift: %d %v", code, body)
	}
	if v := restrictedUntil(); v != nil {
		t.Errorf("after the lift: %v", v)
	}
	code, body = e.adminDo(t, http.MethodPost, lift, modBody)
	wantError(t, "lift twice", code, body, http.StatusConflict, "restriction_not_active")
	code, body = e.adminDo(t, http.MethodPost, "/api/v1/admin/restrictions/not-a-uuid/lift", modBody)
	wantError(t, "unknown", code, body, http.StatusNotFound, "not_found")
	if got := e.count(t, "SELECT count(*) FROM moderation_actions WHERE user_id = $1 AND action = 'restriction_lifted'", ada); got != 1 {
		t.Errorf("audit rows for the lift = %d, want 1", got)
	}
}

// Login says why and until when, only after the right password, and a temporary suspension ends
// by itself (FR-3.4, FR-15.7).
func TestSuspensionAtLogin(t *testing.T) {
	e := newModEnv(t)
	ada := e.newProfileUser(t)
	email := e.withPassword(t, ada)
	if code, body := e.login(t, email, modPassword); code != http.StatusOK {
		t.Fatalf("login: %d %v", code, body)
	}
	if got := e.count(t, "SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL", ada); got != 1 {
		t.Fatalf("live sessions = %d, want 1", got)
	}

	until := e.clock.now().Add(2 * modDay).UTC().Format(time.RFC3339)
	e.suspend(t, ada, fmt.Sprintf(`{"until":%q,"reason":%q,"note":%q}`, until, modReason, modSecret))

	// Sessions are revoked at once, in the action's transaction.
	if got := e.count(t, "SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL", ada); got != 0 {
		t.Errorf("live sessions after suspending = %d, want 0", got)
	}

	code, body := e.login(t, email, modPassword)
	wantError(t, "login while suspended", code, body, http.StatusForbidden, "account_suspended")
	if body["reason"] != modReason || body["until"] == nil {
		t.Errorf("login body = %v", body)
	}
	if strings.Contains(fmt.Sprint(body), modSecret) {
		t.Errorf("the note leaked: %v", body)
	}

	code, body = e.login(t, email, "wrong password")
	wantError(t, "wrong password", code, body, http.StatusUnauthorized, "invalid_credentials")
	if body["reason"] != nil {
		t.Errorf("a wrong password saw the reason: %v", body)
	}

	// Still hidden until the user is back; the admin list judges by time.
	e.clock.move(2*modDay + time.Second)
	code, list := e.adminDo(t, http.MethodGet, "/api/v1/admin/users?pageSize=50", "")
	if code != http.StatusOK {
		t.Fatalf("list users: %d %v", code, list)
	}
	for _, it := range list["items"].([]any) {
		if u := it.(map[string]any); u["id"] == ada && u["suspended"] != false {
			t.Errorf("the admin list still says suspended: %v", u)
		}
	}

	if code, body = e.login(t, email, modPassword); code != http.StatusOK {
		t.Fatalf("login after the end: %d %v", code, body)
	}
	if got := e.count(t, "SELECT count(*) FROM users WHERE id = $1 AND suspended_at IS NULL AND suspended_until IS NULL AND suspension_reason IS NULL", ada); got != 1 {
		t.Error("the login did not clear the suspension")
	}
}

func TestPermanentSuspensionAndRestore(t *testing.T) {
	e := newModEnv(t)
	ada := e.newProfileUser(t)
	email := e.withPassword(t, ada)
	code, body := e.adminDo(t, http.MethodPost, userPath(ada, "restore"), modBody)
	wantError(t, "restore when not suspended", code, body, http.StatusConflict, "not_suspended")

	action := e.suspend(t, ada, modBody)
	if action["action"] != "suspension" || action["endsAt"] != nil {
		t.Errorf("audit row = %v", action)
	}
	code, body = e.adminDo(t, http.MethodPost, userPath(ada, "suspension"), modBody)
	wantError(t, "suspended twice", code, body, http.StatusConflict, "already_suspended")

	// Permanent: no time passes it.
	e.clock.move(4000 * modDay)
	code, body = e.login(t, email, modPassword)
	wantError(t, "years later", code, body, http.StatusForbidden, "account_suspended")
	if body["reason"] != modReason {
		t.Errorf("body = %v", body)
	}
	if _, ok := body["until"]; ok {
		t.Errorf("a permanent suspension has no end: %v", body)
	}

	code, body = e.adminDo(t, http.MethodPost, userPath(ada, "restore"), `{"reason":"short"}`)
	wantError(t, "restore needs a reason", code, body, http.StatusBadRequest, "bad_request")
	code, body = e.adminDo(t, http.MethodPost, userPath(ada, "restore"), `{}`)
	wantError(t, "restore with no reason", code, body, http.StatusBadRequest, "bad_request")
	if code, body = e.adminDo(t, http.MethodPost, userPath(ada, "restore"), modBody); code != http.StatusOK || body["action"] != "restoration" {
		t.Fatalf("restore: %d %v", code, body)
	}
	if code, body = e.login(t, email, modPassword); code != http.StatusOK {
		t.Errorf("login after restoring: %d %v", code, body)
	}
	sent := e.mail.all()
	if len(sent) != 2 || !strings.Contains(sent[1], "restored") {
		t.Errorf("emails = %q", sent)
	}
	for _, m := range sent {
		if strings.Contains(m, modSecret) {
			t.Errorf("the note leaked: %s", m)
		}
	}
}

func TestSuspendUntilRange(t *testing.T) {
	e := newModEnv(t)
	ada := e.newProfileUser(t)
	for _, tc := range []struct {
		name string
		at   time.Time
	}{
		{"an hour", e.clock.now().Add(time.Hour)},
		{"past", e.clock.now().Add(-modDay)},
		{"over a year", e.clock.now().Add(366 * modDay)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, body := e.adminDo(t, http.MethodPost, userPath(ada, "suspension"),
				fmt.Sprintf(`{"until":%q,"reason":%q}`, tc.at.UTC().Format(time.RFC3339), modReason))
			wantError(t, tc.name, code, body, http.StatusBadRequest, "bad_request")
			if body["field"] != "until" {
				t.Errorf("field = %v", body["field"])
			}
		})
	}
	if got := e.count(t, "SELECT count(*) FROM users WHERE id = $1 AND suspended_at IS NOT NULL", ada); got != 0 {
		t.Error("a refused suspension changed the account")
	}
}

// A suspended user's access token still works until it expires, but the writes that check the
// actor refuse at once, with the reason (decisions.md, 2026-11-05).
func TestSuspendedActorIsRefusedWithAValidToken(t *testing.T) {
	e := newModEnv(t)
	ada, bob, cy, dee := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	conversation, _ := e.startChat(t, ada, bob, "hello")
	request, _ := e.startChat(t, cy, ada, "may I ask?")
	other, _ := e.startChat(t, dee, ada, "and I?")
	e.suspend(t, ada, modBody)

	for _, tc := range []struct {
		name string
		run  func() (int, map[string]any)
	}{
		{"send", func() (int, map[string]any) { return e.inConversation(t, ada, conversation, "x", newClientID()) }},
		{"message request", func() (int, map[string]any) { return e.direct(t, ada, e.newProfileUser(t), "x", newClientID()) }},
		{"accept", func() (int, map[string]any) {
			return e.call(t, ada, http.MethodPost, "/api/v1/conversations/"+request+"/accept", "")
		}},
		{"decline", func() (int, map[string]any) {
			return e.call(t, ada, http.MethodPost, "/api/v1/conversations/"+other+"/decline", "")
		}},
		{"report", func() (int, map[string]any) {
			return e.call(t, ada, http.MethodPost, "/api/v1/conversations/"+conversation+"/reports", `{"reason":"spam"}`)
		}},
		{"connection request", func() (int, map[string]any) { return e.sendRequest(t, ada, dee) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, body := tc.run()
			wantError(t, tc.name, code, body, http.StatusForbidden, "account_suspended")
			if body["reason"] != modReason {
				t.Errorf("body = %v", body)
			}
		})
	}
	if got := e.count(t, "SELECT count(*) FROM conversations WHERE state = 'request'"); got != 2 {
		t.Errorf("requests still pending = %d, want 2", got)
	}
}

// The admin's transaction and a send's cannot overlap: FOR UPDATE on one side, FOR SHARE on the
// other, so a send cannot pass the check once a suspension has committed (decisions.md).
func TestStandingWaitsForAnActionAndTheReverse(t *testing.T) {
	e := newModEnv(t)
	ada := e.newProfileUser(t)
	tx := postgres.NewTransactor(e.pool)
	accounts := moderationAccounts{auth: e.auth, events: e.events}
	ctx := t.Context()

	t.Run("a check waits for an action in progress, and sees it", func(t *testing.T) {
		locked, release := make(chan struct{}), make(chan struct{})
		free := sync.OnceFunc(func() { close(release) })
		defer free() // a failure below must not leave the transaction open
		done := make(chan error, 1)
		go func() {
			done <- tx.InTx(ctx, func(ctx context.Context) error {
				if _, err := accounts.Lock(ctx, ada); err != nil {
					return err
				}
				if err := accounts.Suspend(ctx, ada, time.Now(), nil, modReason); err != nil {
					return err
				}
				close(locked)
				<-release
				return nil
			})
		}()
		<-locked
		seen := make(chan moderationdomain.Standing, 1)
		go func() {
			st, err := e.standings.Standing(ctx, ada)
			if err != nil {
				t.Error(err)
			}
			seen <- st
		}()
		select {
		case st := <-seen:
			t.Fatalf("the check did not wait: %+v", st)
		case <-time.After(150 * time.Millisecond):
		}
		free()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if st := <-seen; !st.Suspended {
			t.Errorf("the check missed the suspension: %+v", st)
		}
	})

	if err := accounts.Restore(ctx, ada); err != nil {
		t.Fatal(err)
	}

	t.Run("an action waits for a write that has checked", func(t *testing.T) {
		checked, release := make(chan struct{}), make(chan struct{})
		free := sync.OnceFunc(func() { close(release) })
		defer free() // a failure below must not leave the transaction open
		done := make(chan error, 1)
		go func() {
			done <- tx.InTx(ctx, func(ctx context.Context) error {
				if _, err := e.standings.Standing(ctx, ada); err != nil {
					return err
				}
				close(checked)
				<-release
				return nil
			})
		}()
		<-checked
		acted := make(chan error, 1)
		go func() {
			_, err := e.moderation.Suspend(ctx, e.admin, ada, moderationdomain.SuspendInput{Input: moderationdomain.Input{Reason: modReason}})
			acted <- err
		}()
		select {
		case err := <-acted:
			t.Fatalf("the action did not wait: %v", err)
		case <-time.After(150 * time.Millisecond):
		}
		free()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if err := <-acted; err != nil {
			t.Fatal(err)
		}
	})
}

// A report given to an action is resolved with it and linked to it, or the action is undone
// (FR-15.8).
func TestReportLinking(t *testing.T) {
	e := newModEnv(t)
	ada, bob, cy := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	report := e.reported(t, bob, ada)

	withReport := func(id string) string {
		return fmt.Sprintf(`{"reason":%q,"note":%q,"reportId":%q}`, modReason, modSecret, id)
	}
	warnings := func() int { return e.count(t, "SELECT count(*) FROM account_warnings WHERE user_id = $1", ada) }

	code, body := e.adminDo(t, http.MethodPost, userPath(cy, "warnings"), withReport(report))
	wantError(t, "a report about someone else", code, body, http.StatusBadRequest, "bad_request")
	if body["field"] != "reportId" {
		t.Errorf("field = %v", body["field"])
	}
	code, body = e.adminDo(t, http.MethodPost, userPath(ada, "warnings"), withReport("00000000-0000-4000-8000-000000000000"))
	wantError(t, "an unknown report", code, body, http.StatusNotFound, "not_found")
	if got := e.reportOutcome(t, report); got != "" {
		t.Errorf("outcome = %q, want none", got)
	}
	if warnings() != 0 || e.count(t, "SELECT count(*) FROM moderation_actions") != 0 || len(e.mail.all()) != 0 {
		t.Fatal("a refused action left something behind")
	}

	code, action := e.adminDo(t, http.MethodPost, userPath(ada, "warnings"), withReport(report))
	if code != http.StatusCreated {
		t.Fatalf("warn: %d %v", code, action)
	}
	if action["reportId"] != report {
		t.Errorf("audit row = %v", action)
	}
	linked := action["report"].(map[string]any)
	if linked["id"] != report || linked["outcome"] != "action_taken" || linked["reason"] != "spam" {
		t.Errorf("linked report = %v", linked)
	}
	if got := e.reportOutcome(t, report); got != "action_taken" {
		t.Errorf("outcome = %q", got)
	}

	// A resolved report undoes a second action entirely.
	code, body = e.adminDo(t, http.MethodPost, userPath(ada, "warnings"), withReport(report))
	wantError(t, "a resolved report", code, body, http.StatusConflict, "report_already_resolved")
	if warnings() != 1 || e.count(t, "SELECT count(*) FROM moderation_actions") != 1 {
		t.Error("the refused action was not undone")
	}
	code, body = e.adminDo(t, http.MethodPost, userPath(ada, "suspension"), withReport(report))
	wantError(t, "a resolved report on a suspension", code, body, http.StatusConflict, "report_already_resolved")
	if got := e.count(t, "SELECT count(*) FROM users WHERE id = $1 AND suspended_at IS NOT NULL", ada); got != 0 {
		t.Error("the suspension was not undone")
	}
}

// Audit rows hold the admin, user, action, reason, note, report and time (FR-15.8), and the
// history is newest first with the linked reports (FR-15.9).
func TestHistoryAndAudit(t *testing.T) {
	e := newModEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	report := e.reported(t, bob, ada)

	if code, body := e.adminDo(t, http.MethodPost, userPath(ada, "warnings"), fmt.Sprintf(`{"reason":%q,"note":%q,"reportId":%q}`, modReason, modSecret, report)); code != http.StatusCreated {
		t.Fatalf("warn: %d %v", code, body)
	}
	action := e.restrict(t, ada, 2)
	if code, body := e.adminDo(t, http.MethodPost, "/api/v1/admin/restrictions/"+action["restrictionId"].(string)+"/lift", modBody); code != http.StatusNoContent {
		t.Fatalf("lift: %d %v", code, body)
	}
	e.suspend(t, ada, modBody)
	// The history still names a suspended user, whom no profile shows.
	if code, h := e.adminDo(t, http.MethodGet, userPath(ada, "moderation"), ""); code != http.StatusOK {
		t.Fatalf("history while suspended: %d %v", code, h)
	} else if u := h["user"].(map[string]any); u["fullName"] == "" || u["role"] != "researcher" || u["suspended"] != true ||
		u["suspensionReason"] != modReason || u["suspendedUntil"] != nil {
		t.Errorf("user while suspended = %v", u)
	}
	if code, body := e.adminDo(t, http.MethodPost, userPath(ada, "restore"), modBody); code != http.StatusOK {
		t.Fatalf("restore: %d %v", code, body)
	}

	code, history := e.adminDo(t, http.MethodGet, userPath(ada, "moderation"), "")
	if code != http.StatusOK {
		t.Fatalf("history: %d %v", code, history)
	}
	if u := history["user"].(map[string]any); u["id"] != ada || u["suspended"] != false || u["suspensionReason"] != nil {
		t.Errorf("user after restoring = %v", u)
	}
	if code, h := e.adminDo(t, http.MethodGet, userPath(e.admin, "moderation"), ""); code != http.StatusOK || h["user"].(map[string]any)["role"] != "admin" {
		t.Errorf("history of an administrator: %d %v", code, h)
	}
	var kinds []string
	var rows []map[string]any
	for _, it := range history["items"].([]any) {
		r := it.(map[string]any)
		rows = append(rows, r)
		kinds = append(kinds, r["action"].(string))
	}
	want := "restoration suspension restriction_lifted restriction warning"
	if got := strings.Join(kinds, " "); got != want {
		t.Fatalf("history = %s, want %s", got, want)
	}
	if history["total"] != float64(5) || history["page"] != float64(1) || history["pageSize"] != float64(20) {
		t.Errorf("history page = %v", history)
	}
	oldest := rows[4]
	if oldest["reason"] != modReason || oldest["note"] != modSecret || oldest["adminId"] != e.admin || oldest["createdAt"] == nil {
		t.Errorf("audit row = %v", oldest)
	}
	if oldest["report"].(map[string]any)["id"] != report {
		t.Errorf("no linked report: %v", oldest)
	}
	if rows[3]["liftedAt"] == nil {
		t.Errorf("the restriction does not show it was lifted: %v", rows[3])
	}
	if rows[0]["reportId"] != nil {
		t.Errorf("a restoration has no report: %v", rows[0])
	}

	code, body := e.adminDo(t, http.MethodGet, userPath("00000000-0000-4000-8000-000000000000", "moderation"), "")
	wantError(t, "unknown user", code, body, http.StatusNotFound, "not_found")
	code, body = e.adminDo(t, http.MethodGet, userPath(ada, "moderation")+"?page=0", "")
	wantError(t, "page 0", code, body, http.StatusBadRequest, "bad_request")
}

// An administrator cannot act on themselves or on another administrator, whatever the action
// (FR-15.10), and only an administrator can reach these routes (FR-3.2).
func TestAdminOnAdminAndRoles(t *testing.T) {
	e := newModEnv(t)
	other, ada := e.newProfileUser(t), e.newProfileUser(t)
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET role = 'admin' WHERE id = $1", other); err != nil {
		t.Fatal(err)
	}
	restriction := e.restrict(t, ada, 5)["restrictionId"].(string)
	before := e.count(t, "SELECT count(*) FROM moderation_actions")

	for _, target := range []string{other, e.admin} {
		for _, tc := range []struct{ path, body string }{
			{"warnings", modBody},
			{"restrictions", `{"kind":"messaging","days":1,"reason":"` + modReason + `"}`},
			{"suspension", modBody},
			{"restore", modBody},
		} {
			code, body := e.adminDo(t, http.MethodPost, userPath(target, tc.path), tc.body)
			wantError(t, tc.path, code, body, http.StatusForbidden, "cannot_moderate_admin")
		}
	}
	if got := e.count(t, "SELECT count(*) FROM moderation_actions"); got != before {
		t.Errorf("audit rows = %d, want %d", got, before)
	}

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, userPath(ada, "warnings"), modBody},
		{http.MethodPost, userPath(ada, "restrictions"), `{"kind":"messaging","days":1,"reason":"` + modReason + `"}`},
		{http.MethodPost, "/api/v1/admin/restrictions/" + restriction + "/lift", modBody},
		{http.MethodPost, userPath(ada, "suspension"), modBody},
		{http.MethodPost, userPath(ada, "restore"), modBody},
		{http.MethodGet, userPath(ada, "moderation"), ""},
	} {
		code, body := e.call(t, ada, tc.method, tc.path, tc.body)
		wantError(t, "a researcher on "+tc.path, code, body, http.StatusForbidden, "forbidden")
	}
}

// Deleting an account takes its moderation rows with it (FR-2.8); deleting an administrator's
// keeps the audit trail without the name.
func TestModerationRowsAndDeletedAccounts(t *testing.T) {
	e := newModEnv(t)
	ada, gone := e.newProfileUser(t), e.newProfileUser(t)
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET role = 'admin' WHERE id = $1", gone); err != nil {
		t.Fatal(err)
	}
	if code, body := e.asAdmin(t, gone, http.MethodPost, userPath(ada, "warnings"), modBody); code != http.StatusCreated {
		t.Fatalf("warn: %d %v", code, body)
	}
	if _, err := e.pool.Exec(t.Context(), "DELETE FROM users WHERE id = $1", gone); err != nil {
		t.Fatal(err)
	}
	_, history := e.adminDo(t, http.MethodGet, userPath(ada, "moderation"), "")
	row := history["items"].([]any)[0].(map[string]any)
	if row["adminId"] != nil || row["adminName"] != nil || row["reason"] != modReason {
		t.Errorf("row after the administrator left = %v", row)
	}

	if _, err := e.pool.Exec(t.Context(), "DELETE FROM users WHERE id = $1", ada); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"moderation_actions", "account_warnings"} {
		if got := e.count(t, "SELECT count(*) FROM "+table); got != 0 {
			t.Errorf("%s has %d rows after the account was deleted", table, got)
		}
	}
}
