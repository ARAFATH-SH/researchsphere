//go:build integration

// End-to-end tests of messaging (FR-16.1 to FR-16.3, FR-16.10, NFR-13) through the real
// router, the PostgreSQL adapters and the Redis limiter. They reuse the environment of
// media_integration_test.go, on the test database.
package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	messagingusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
)

// clientSeq numbers the client IDs of a test run.
var clientSeq struct {
	sync.Mutex
	n int
}

// newClientID returns a fresh clientId.
func newClientID() string {
	clientSeq.Lock()
	defer clientSeq.Unlock()
	clientSeq.n++
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", clientSeq.n)
}

func (e mediaEnv) direct(t *testing.T, from, to, body, client string) (int, map[string]any) {
	t.Helper()
	return e.call(t, from, http.MethodPost, "/api/v1/messages/direct",
		fmt.Sprintf(`{"userId":%q,"body":%q,"clientId":%q}`, to, body, client))
}

func (e mediaEnv) inConversation(t *testing.T, from, id, body, client string) (int, map[string]any) {
	t.Helper()
	return e.call(t, from, http.MethodPost, "/api/v1/conversations/"+id+"/messages",
		fmt.Sprintf(`{"body":%q,"clientId":%q}`, body, client))
}

// startChat sends a first message and returns the conversation and message IDs.
func (e mediaEnv) startChat(t *testing.T, from, to, body string) (conversationID, messageID string) {
	t.Helper()
	code, sent := e.direct(t, from, to, body, newClientID())
	if code != http.StatusCreated {
		t.Fatalf("direct message: %d %v", code, sent)
	}
	m := sent["message"].(map[string]any)
	return m["conversationId"].(string), m["id"].(string)
}

// link connects a and b: a sends the request and b accepts it.
func (e mediaEnv) link(t *testing.T, a, b string) {
	t.Helper()
	e.accept(t, b, e.connect(t, a, b))
}

func (e mediaEnv) messageCounts(t *testing.T, userID string) (total, requests float64) {
	t.Helper()
	code, body := e.call(t, userID, http.MethodGet, "/api/v1/messages/unread-count", "")
	if code != http.StatusOK {
		t.Fatalf("unread-count: %d %v", code, body)
	}
	return body["total"].(float64), body["requests"].(float64)
}

func (e mediaEnv) box(t *testing.T, userID, box string) []map[string]any {
	t.Helper()
	code, body := e.call(t, userID, http.MethodGet, "/api/v1/conversations?box="+box, "")
	if code != http.StatusOK {
		t.Fatalf("GET conversations: %d %v", code, body)
	}
	raw := body["items"].([]any)
	items := make([]map[string]any, len(raw))
	for i, it := range raw {
		items[i] = it.(map[string]any)
	}
	return items
}

func wantError(t *testing.T, what string, code int, body map[string]any, status int, errCode string) {
	t.Helper()
	if code != status || body["code"] != errCode {
		t.Errorf("%s: %d %v; want %d %s", what, code, body, status, errCode)
	}
}

// FR-16.1 and FR-16.3: connected users chat at once; unread counts and read markers follow.
func TestConnectedChat(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)

	id, first := e.startChat(t, ada, bob, "line one\nline two")
	if total, requests := e.messageCounts(t, bob); total != 1 || requests != 0 {
		t.Errorf("bob's counts: %v, %v", total, requests)
	}
	if total, _ := e.messageCounts(t, ada); total != 0 {
		t.Errorf("ada's own message is unread: %v", total)
	}

	inbox := e.box(t, bob, "inbox")
	if len(inbox) != 1 || inbox[0]["id"] != id || inbox[0]["state"] != "active" || inbox[0]["unreadCount"] != float64(1) || inbox[0]["requestedBy"] != nil {
		t.Fatalf("bob's inbox: %v", inbox)
	}
	other := inbox[0]["otherMember"].(map[string]any)
	if other["userId"] != ada || other["fullName"] != "Ada" || other["available"] != true {
		t.Errorf("otherMember: %v", other)
	}
	if last := inbox[0]["lastMessage"].(map[string]any); last["senderId"] != ada || last["body"] != "line one\nline two" {
		t.Errorf("lastMessage: %v", last)
	}

	code, reply := e.inConversation(t, bob, id, "  hello back\n", newClientID())
	if code != http.StatusCreated || reply["message"].(map[string]any)["body"] != "hello back" {
		t.Fatalf("reply: %d %v", code, reply)
	}
	second := reply["message"].(map[string]any)["id"].(string)
	// A send moves the sender's own marker, and the reply is what ada has not read.
	if total, _ := e.messageCounts(t, bob); total != 0 {
		t.Errorf("bob's unread after replying: %v", total)
	}
	if total, _ := e.messageCounts(t, ada); total != 1 {
		t.Errorf("ada's unread: %v", total)
	}

	// Seen: ada's list shows where bob's marker is.
	code, page := e.call(t, ada, http.MethodGet, "/api/v1/conversations/"+id+"/messages", "")
	if code != http.StatusOK || page["otherLastReadMessageId"] != second {
		t.Fatalf("messages: %d %v", code, page)
	}
	if items := page["items"].([]any); len(items) != 2 || items[0].(map[string]any)["id"] != second {
		t.Errorf("not newest first: %v", items)
	}

	read := func(user, message string) {
		t.Helper()
		if code, body := e.call(t, user, http.MethodPost, "/api/v1/conversations/"+id+"/read", `{"messageId":"`+message+`"}`); code != http.StatusNoContent {
			t.Fatalf("read: %d %v", code, body)
		}
	}
	read(ada, second)
	if total, _ := e.messageCounts(t, ada); total != 0 {
		t.Errorf("ada's unread after reading: %v", total)
	}
	// Moving back is accepted and changes nothing.
	read(ada, first)
	read(ada, second)
	if code, page = e.call(t, bob, http.MethodGet, "/api/v1/conversations/"+id+"/messages", ""); page["otherLastReadMessageId"] != second {
		t.Errorf("marker after moving back: %d %v", code, page)
	}

	// One conversation for the pair, whoever sends, either way round.
	e.startChatAgain(t, bob, ada, id)
	if n := e.countRows(t, "SELECT count(*) FROM conversations"); n != 1 {
		t.Errorf("%d conversations", n)
	}
}

func (e mediaEnv) startChatAgain(t *testing.T, from, to, want string) {
	t.Helper()
	if got, _ := e.startChat(t, from, to, "again"); got != want {
		t.Errorf("conversation = %s, want %s", got, want)
	}
}

// FR-16.2, accepted.
func TestMessageRequestAccepted(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	id, _ := e.startChat(t, ada, bob, "may I?")

	if got := e.box(t, ada, "inbox"); len(got) != 1 || got[0]["state"] != "request" || got[0]["requestedBy"] != ada {
		t.Errorf("sender's inbox: %v", got)
	}
	if got := e.box(t, ada, "requests"); len(got) != 0 {
		t.Errorf("sender's requests: %v", got)
	}
	if got := e.box(t, bob, "inbox"); len(got) != 0 {
		t.Errorf("receiver's inbox: %v", got)
	}
	if got := e.box(t, bob, "requests"); len(got) != 1 || got[0]["unreadCount"] != float64(1) {
		t.Errorf("receiver's requests: %v", got)
	}
	if total, requests := e.messageCounts(t, bob); total != 0 || requests != 1 {
		t.Errorf("receiver's counts: %v, %v", total, requests)
	}

	// A second message is refused for both, in both forms, until it is accepted.
	code, body := e.direct(t, ada, bob, "please", newClientID())
	wantError(t, "second send", code, body, http.StatusConflict, "request_pending")
	code, body = e.inConversation(t, ada, id, "please", newClientID())
	wantError(t, "second send by id", code, body, http.StatusConflict, "request_pending")
	code, body = e.inConversation(t, bob, id, "sure", newClientID())
	wantError(t, "receiver writing first", code, body, http.StatusConflict, "request_pending")

	// Only the receiver may answer.
	code, body = e.call(t, ada, http.MethodPost, "/api/v1/conversations/"+id+"/accept", "")
	wantError(t, "accept by the sender", code, body, http.StatusNotFound, "not_found")

	code, body = e.call(t, bob, http.MethodPost, "/api/v1/conversations/"+id+"/accept", "")
	if code != http.StatusOK || body["state"] != "active" || body["requestedBy"] != ada {
		t.Fatalf("accept: %d %v", code, body)
	}
	code, body = e.call(t, bob, http.MethodPost, "/api/v1/conversations/"+id+"/accept", "")
	wantError(t, "accept twice", code, body, http.StatusNotFound, "not_found")

	// Now it is a chat: in both inboxes, and the unread message moves to the total.
	if total, requests := e.messageCounts(t, bob); total != 1 || requests != 0 {
		t.Errorf("counts after accept: %v, %v", total, requests)
	}
	if got := e.box(t, bob, "inbox"); len(got) != 1 {
		t.Errorf("receiver's inbox: %v", got)
	}
	if code, body := e.inConversation(t, ada, id, "thanks", newClientID()); code != http.StatusCreated || body["conversationState"] != "active" {
		t.Errorf("after accept: %d %v", code, body)
	}
	if code, body := e.direct(t, bob, ada, "welcome", newClientID()); code != http.StatusCreated {
		t.Errorf("reply: %d %v", code, body)
	}
}

// FR-16.2, declined; and connecting later does not reopen it.
func TestMessageRequestDeclined(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	id, _ := e.startChat(t, ada, bob, "may I?")

	code, body := e.call(t, bob, http.MethodPost, "/api/v1/conversations/"+id+"/decline", "")
	if code != http.StatusOK || body["state"] != "declined" {
		t.Fatalf("decline: %d %v", code, body)
	}
	check := func(when string) {
		t.Helper()
		code, body := e.direct(t, ada, bob, "please", newClientID())
		wantError(t, when+": sender", code, body, http.StatusConflict, "conversation_closed")
		code, body = e.direct(t, bob, ada, "actually", newClientID())
		wantError(t, when+": receiver", code, body, http.StatusConflict, "conversation_closed")
		code, body = e.inConversation(t, bob, id, "actually", newClientID())
		wantError(t, when+": by id", code, body, http.StatusConflict, "conversation_closed")
	}
	check("declined")
	if got := e.box(t, ada, "inbox"); len(got) != 0 {
		t.Errorf("sender's inbox: %v", got)
	}
	if got := e.box(t, bob, "requests"); len(got) != 0 {
		t.Errorf("receiver's requests: %v", got)
	}
	if _, requests := e.messageCounts(t, bob); requests != 0 {
		t.Errorf("requests: %v", requests)
	}
	// A member can still see what it is.
	if code, body := e.call(t, ada, http.MethodGet, "/api/v1/conversations/"+id, ""); code != http.StatusOK || body["state"] != "declined" {
		t.Errorf("get: %d %v", code, body)
	}

	e.link(t, ada, bob)
	check("connected")
}

// A pending request becomes active when the two connect.
func TestConnectingActivatesARequest(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	id, _ := e.startChat(t, ada, bob, "may I?")
	e.link(t, bob, ada)

	if code, body := e.call(t, ada, http.MethodGet, "/api/v1/conversations/"+id, ""); code != http.StatusOK || body["state"] != "active" {
		t.Fatalf("get: %d %v", code, body)
	}
	if code, body := e.inConversation(t, ada, id, "connected now", newClientID()); code != http.StatusCreated {
		t.Errorf("send: %d %v", code, body)
	}
	if got := e.box(t, bob, "requests"); len(got) != 0 {
		t.Errorf("requests: %v", got)
	}
}

// Decided with the developer: removing a connection leaves the conversation active.
func TestRemovingAConnectionKeepsTheChat(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	id, _ := e.startChat(t, ada, bob, "hi")
	if code, body := e.call(t, ada, http.MethodDelete, "/api/v1/connections/"+bob, ""); code != http.StatusNoContent {
		t.Fatalf("remove: %d %v", code, body)
	}
	if code, body := e.inConversation(t, bob, id, "still here", newClientID()); code != http.StatusCreated || body["conversationState"] != "active" {
		t.Errorf("send: %d %v", code, body)
	}
}

func TestIdempotentClientID(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	client := newClientID()

	code, first := e.direct(t, ada, bob, "once", client)
	if code != http.StatusCreated {
		t.Fatalf("first: %d %v", code, first)
	}
	m := first["message"].(map[string]any)
	code, again := e.direct(t, ada, bob, "once", client)
	if code != http.StatusOK || again["message"].(map[string]any)["id"] != m["id"] {
		t.Errorf("repeat: %d %v", code, again)
	}
	code, again = e.inConversation(t, ada, m["conversationId"].(string), "changed", client)
	if code != http.StatusOK || again["message"].(map[string]any)["body"] != "once" {
		t.Errorf("repeat by id: %d %v", code, again)
	}
	if n := e.countRows(t, "SELECT count(*) FROM messages"); n != 1 {
		t.Errorf("%d messages", n)
	}
	if total, _ := e.messageCounts(t, bob); total != 1 {
		t.Errorf("a repeat counted as a new message: %v", total)
	}
	// Another sender may use the same value.
	if code, body := e.direct(t, bob, ada, "mine", client); code != http.StatusCreated {
		t.Errorf("other sender: %d %v", code, body)
	}

	// A retry of a request's first message gets it back, not request_pending.
	cy := e.newProfileUser(t)
	req := newClientID()
	if code, body := e.direct(t, ada, cy, "may I?", req); code != http.StatusCreated {
		t.Fatalf("request: %d %v", code, body)
	}
	if code, body := e.direct(t, ada, cy, "may I?", req); code != http.StatusOK || body["conversationState"] != "request" {
		t.Errorf("retry: %d %v", code, body)
	}
}

// Many retries at once store one message: one 201, the rest 200 with the same ID.
func TestConcurrentRetriesStoreOneMessage(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	client := newClientID()
	body := fmt.Sprintf(`{"userId":%q,"body":"once","clientId":%q}`, bob, client)
	type result struct {
		code int
		id   any
	}
	results := make([]result, 8)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, sent := e.call(t, ada, http.MethodPost, "/api/v1/messages/direct", body)
			results[i] = result{code: code}
			if m, ok := sent["message"].(map[string]any); ok {
				results[i].id = m["id"]
			}
		}()
	}
	wg.Wait()
	created := 0
	for _, r := range results {
		if r.code == http.StatusCreated {
			created++
		} else if r.code != http.StatusOK {
			t.Errorf("got %d", r.code)
		}
		if r.id != results[0].id {
			t.Errorf("IDs differ: %v and %v", r.id, results[0].id)
		}
	}
	if created != 1 {
		t.Errorf("%d created", created)
	}
	if n := e.countRows(t, "SELECT count(*) FROM messages"); n != 1 {
		t.Errorf("%d messages", n)
	}
}

// A retry of a first message that races the original gets the original, not a refusal
// of its own request.
func TestConcurrentRetriesOfARequest(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	client := newClientID()
	codes := make([]int, 8)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _ = e.direct(t, ada, bob, "may I?", client)
		}()
	}
	wg.Wait()
	created := 0
	for _, c := range codes {
		if c == http.StatusCreated {
			created++
		} else if c != http.StatusOK {
			t.Errorf("got %d: %v", c, codes)
		}
	}
	if created != 1 {
		t.Errorf("%d created: %v", created, codes)
	}
	if n := e.countRows(t, "SELECT count(*) FROM messages"); n != 1 {
		t.Errorf("%d messages", n)
	}
}

// Two people sending a first message at once get one conversation; a request takes one
// message, whoever gets there first.
func TestConcurrentFirstMessages(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	codes := make([]int, 6)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			from, to := ada, bob
			if i%2 == 1 {
				from, to = bob, ada
			}
			codes[i], _ = e.direct(t, from, to, "hello", newClientID())
		}()
	}
	wg.Wait()
	created := 0
	for _, c := range codes {
		if c == http.StatusCreated {
			created++
		} else if c != http.StatusConflict {
			t.Errorf("got %d", c)
		}
	}
	if created != 1 {
		t.Errorf("%d created: %v", created, codes)
	}
	if n := e.countRows(t, "SELECT count(*) FROM conversations"); n != 1 {
		t.Errorf("%d conversations", n)
	}
	if n := e.countRows(t, "SELECT count(*) FROM messages"); n != 1 {
		t.Errorf("%d messages", n)
	}
}

// NFR-13: a non-member, an administrator included, gets 404 on every read and write.
func TestConversationIsHiddenFromNonMembers(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, eve := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	id, msg := e.startChat(t, ada, bob, "private")
	base := "/api/v1/conversations/" + id
	routes := []struct{ method, path, body string }{
		{http.MethodGet, base, ""},
		{http.MethodGet, base + "/messages", ""},
		{http.MethodPost, base + "/messages", fmt.Sprintf(`{"body":"hi","clientId":%q}`, newClientID())},
		{http.MethodPost, base + "/read", `{"messageId":"` + msg + `"}`},
		{http.MethodPost, base + "/accept", ""},
		{http.MethodPost, base + "/decline", ""},
	}
	for _, r := range routes {
		code, body := e.call(t, eve, r.method, r.path, r.body)
		wantError(t, r.method+" "+r.path, code, body, http.StatusNotFound, "not_found")
		unknown := strings.Replace(r.path, id, "00000000-0000-4000-8000-000000000000", 1)
		gone, goneBody := e.call(t, eve, r.method, unknown, r.body)
		if gone != code || goneBody["message"] != body["message"] {
			t.Errorf("%s: an unknown ID answers differently: %d %v", r.path, gone, goneBody)
		}
	}
	if got := e.box(t, eve, "inbox"); len(got) != 0 {
		t.Errorf("eve's inbox: %v", got)
	}
	if n := e.countRows(t, "SELECT count(*) FROM messages"); n != 1 {
		t.Errorf("%d messages", n)
	}
	// A value that is no UUID is just another unknown ID.
	code, body := e.call(t, ada, http.MethodGet, "/api/v1/conversations/nonsense", "")
	wantError(t, "bad ID", code, body, http.StatusNotFound, "not_found")
}

func TestOnlyVisibleUsersCanBeMessaged(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)

	code, body := e.direct(t, ada, ada, "me", newClientID())
	wantError(t, "oneself", code, body, http.StatusBadRequest, "cannot_message_self")
	code, body = e.direct(t, ada, "00000000-0000-4000-8000-000000000000", "hi", newClientID())
	wantError(t, "unknown user", code, body, http.StatusNotFound, "not_found")
	code, body = e.direct(t, ada, bob, "   \n  ", newClientID())
	wantError(t, "blank body", code, body, http.StatusBadRequest, "invalid_body")
	code, body = e.direct(t, ada, bob, strings.Repeat("a", 5001), newClientID())
	wantError(t, "long body", code, body, http.StatusBadRequest, "invalid_body")
	code, body = e.call(t, ada, http.MethodPost, "/api/v1/messages/direct",
		fmt.Sprintf(`{"userId":%q,"body":"a\u0000b","clientId":%q}`, bob, newClientID()))
	wantError(t, "NUL", code, body, http.StatusBadRequest, "invalid_body")
	code, body = e.direct(t, ada, bob, "hi", "not-a-uuid")
	wantError(t, "bad clientId", code, body, http.StatusBadRequest, "bad_request")
	if code, _ := e.direct(t, ada, bob, strings.Repeat("é", 5000), newClientID()); code != http.StatusCreated {
		t.Errorf("5,000 characters: %d", code)
	}
	for name, stmt := range map[string]string{
		"suspended":    "UPDATE users SET suspended_at = now() WHERE id = $1",
		"not verified": "UPDATE users SET email_verified_at = NULL WHERE id = $1",
	} {
		cy := e.newProfileUser(t)
		if _, err := e.pool.Exec(t.Context(), stmt, cy); err != nil {
			t.Fatal(err)
		}
		code, body = e.direct(t, ada, cy, "hi", newClientID())
		wantError(t, name, code, body, http.StatusNotFound, "not_found")
	}
}

// Decided with the developer: a suspended member's conversation stays readable, read-only.
func TestSuspendedMemberIsReadOnly(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	id, msg := e.startChat(t, bob, ada, "hi")
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET suspended_at = now() WHERE id = $1", bob); err != nil {
		t.Fatal(err)
	}
	code, body := e.inConversation(t, ada, id, "hello?", newClientID())
	wantError(t, "send", code, body, http.StatusNotFound, "not_found")
	code, body = e.direct(t, ada, bob, "hello?", newClientID())
	wantError(t, "direct", code, body, http.StatusNotFound, "not_found")

	inbox := e.box(t, ada, "inbox")
	if len(inbox) != 1 || inbox[0]["otherMember"].(map[string]any)["available"] != false {
		t.Fatalf("inbox: %v", inbox)
	}
	if code, page := e.call(t, ada, http.MethodGet, "/api/v1/conversations/"+id+"/messages", ""); code != http.StatusOK || len(page["items"].([]any)) != 1 {
		t.Errorf("messages: %d %v", code, page)
	}
	if code, body := e.call(t, ada, http.MethodPost, "/api/v1/conversations/"+id+"/read", `{"messageId":"`+msg+`"}`); code != http.StatusNoContent {
		t.Errorf("read: %d %v", code, body)
	}
}

// A request whose sender is suspended cannot be answered, and leaves the receiver's box.
func TestRequestFromSuspendedSender(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	id, _ := e.startChat(t, ada, bob, "may I?")
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET suspended_at = now() WHERE id = $1", ada); err != nil {
		t.Fatal(err)
	}
	if got := e.box(t, bob, "requests"); len(got) != 0 {
		t.Errorf("requests: %v", got)
	}
	if _, requests := e.messageCounts(t, bob); requests != 0 {
		t.Errorf("request count: %v", requests)
	}
	code, body := e.call(t, bob, http.MethodPost, "/api/v1/conversations/"+id+"/accept", "")
	wantError(t, "accept", code, body, http.StatusNotFound, "not_found")
}

// FR-16.10, with low numbers from the configuration.
func TestMessageRateLimits(t *testing.T) {
	env := func(t *testing.T, l messagingusecase.Limits) mediaEnv {
		t.Helper()
		return newMediaEnvOnPool(t, testenv.DB(t), "", "", envOptions{messageLimits: &l})
	}

	wantLimited := func(t *testing.T, rec func() (int, map[string]any, string), low, high int) {
		t.Helper()
		code, body, retry := rec()
		wantError(t, "over the limit", code, body, http.StatusTooManyRequests, "rate_limited")
		if n, err := strconv.Atoi(retry); err != nil || n < low || n > high {
			t.Errorf("Retry-After = %q, want %d to %d", retry, low, high)
		}
	}

	t.Run("messages per minute", func(t *testing.T) {
		e := env(t, messagingusecase.Limits{MessagesPerMinute: 2, MessagesPerDay: 1000, RequestsPerMinute: 5, RequestsPerDay: 20})
		ada, bob := e.newProfileUser(t), e.newProfileUser(t)
		e.link(t, ada, bob)
		e.startChat(t, ada, bob, "one")
		client := newClientID()
		if code, body := e.direct(t, ada, bob, "two", client); code != http.StatusCreated {
			t.Fatalf("second: %d %v", code, body)
		}
		wantLimited(t, func() (int, map[string]any, string) { return e.directRaw(t, ada, bob, "three") }, 1, 60)
		// A retry of a stored message is not refused, and bob's own quota is untouched.
		if code, body := e.direct(t, ada, bob, "two", client); code != http.StatusOK {
			t.Errorf("replay: %d %v", code, body)
		}
		if code, body := e.direct(t, bob, ada, "fine", newClientID()); code != http.StatusCreated {
			t.Errorf("bob: %d %v", code, body)
		}
	})

	t.Run("messages per day", func(t *testing.T) {
		e := env(t, messagingusecase.Limits{MessagesPerMinute: 30, MessagesPerDay: 1, RequestsPerMinute: 5, RequestsPerDay: 20})
		ada, bob := e.newProfileUser(t), e.newProfileUser(t)
		e.link(t, ada, bob)
		e.startChat(t, ada, bob, "one")
		wantLimited(t, func() (int, map[string]any, string) { return e.directRaw(t, ada, bob, "two") }, 60, 86400)
	})

	t.Run("new requests per minute", func(t *testing.T) {
		e := env(t, messagingusecase.Limits{MessagesPerMinute: 30, MessagesPerDay: 1000, RequestsPerMinute: 1, RequestsPerDay: 20})
		ada, bob, cy, dan := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
		e.link(t, ada, dan)
		e.startChat(t, ada, bob, "may I?")
		wantLimited(t, func() (int, map[string]any, string) { return e.directRaw(t, ada, cy, "may I?") }, 1, 60)
		if n := e.countRows(t, "SELECT count(*) FROM conversations"); n != 1 {
			t.Errorf("%d conversations", n)
		}
		// Connections can still be messaged.
		e.startChat(t, ada, dan, "hi")
	})

	t.Run("new requests per day", func(t *testing.T) {
		e := env(t, messagingusecase.Limits{MessagesPerMinute: 30, MessagesPerDay: 1000, RequestsPerMinute: 5, RequestsPerDay: 1})
		ada, bob, cy := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
		e.startChat(t, ada, bob, "may I?")
		wantLimited(t, func() (int, map[string]any, string) { return e.directRaw(t, ada, cy, "may I?") }, 60, 86400)
	})

	t.Run("a refused send uses no quota", func(t *testing.T) {
		e := env(t, messagingusecase.Limits{MessagesPerMinute: 2, MessagesPerDay: 1000, RequestsPerMinute: 5, RequestsPerDay: 20})
		ada, bob, cy := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
		e.link(t, ada, cy)
		e.startChat(t, ada, bob, "may I?")
		for range 3 {
			code, body := e.direct(t, ada, bob, "please", newClientID())
			wantError(t, "second send", code, body, http.StatusConflict, "request_pending")
		}
		e.startChat(t, ada, cy, "still have one")
	})
}

// directRaw is direct, also returning the Retry-After header.
func (e mediaEnv) directRaw(t *testing.T, from, to, body string) (int, map[string]any, string) {
	t.Helper()
	rec := e.send(t, from, http.MethodPost, "/api/v1/messages/direct",
		strings.NewReader(fmt.Sprintf(`{"userId":%q,"body":%q,"clientId":%q}`, to, body, newClientID())), "application/json")
	return rec.Code, decodeBody(t, rec.Body.Bytes()), rec.Header().Get("Retry-After")
}

func TestConversationCursorPaging(t *testing.T) {
	e := newMediaEnv(t)
	ada := e.newProfileUser(t)
	seen := map[string]bool{}
	var want []string
	for range 5 {
		other := e.newProfileUser(t)
		e.link(t, ada, other)
		id, _ := e.startChat(t, ada, other, "hi")
		want = append([]string{id}, want...) // newest first
		time.Sleep(2 * time.Millisecond)
	}
	var got []string
	query := "?box=inbox&limit=2"
	for pages := 0; ; pages++ {
		if pages > 3 {
			t.Fatal("too many pages")
		}
		code, body := e.call(t, ada, http.MethodGet, "/api/v1/conversations"+query, "")
		if code != http.StatusOK {
			t.Fatalf("page: %d %v", code, body)
		}
		for _, it := range body["items"].([]any) {
			id := it.(map[string]any)["id"].(string)
			if seen[id] {
				t.Errorf("%s came twice", id)
			}
			seen[id] = true
			got = append(got, id)
		}
		next, ok := body["nextCursor"].(string)
		if !ok {
			break
		}
		query = "?box=inbox&limit=2&cursor=" + next
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", got, want)
	}

	// New activity moves a conversation to the top.
	if code, body := e.inConversation(t, ada, want[4], "again", newClientID()); code != http.StatusCreated {
		t.Fatalf("send: %d %v", code, body)
	}
	if inbox := e.box(t, ada, "inbox"); inbox[0]["id"] != want[4] {
		t.Errorf("top = %v, want %s", inbox[0]["id"], want[4])
	}

	for _, q := range []string{"?box=archive", "?cursor=nonsense", "?limit=0"} {
		code, body := e.call(t, ada, http.MethodGet, "/api/v1/conversations"+q, "")
		wantError(t, q, code, body, http.StatusBadRequest, "bad_request")
	}
}

func TestMessageCursorPaging(t *testing.T) {
	limits := messagingusecase.Limits{MessagesPerMinute: 1000, MessagesPerDay: 1000, RequestsPerMinute: 5, RequestsPerDay: 20}
	e := newMediaEnvOnPool(t, testenv.DB(t), "", "", envOptions{messageLimits: &limits})
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	id, _ := e.startChat(t, ada, bob, "m0")
	for i := 1; i < 35; i++ {
		if code, body := e.inConversation(t, ada, id, fmt.Sprintf("m%d", i), newClientID()); code != http.StatusCreated {
			t.Fatalf("send %d: %d %v", i, code, body)
		}
	}
	path := "/api/v1/conversations/" + id + "/messages"

	code, page := e.call(t, ada, http.MethodGet, path, "")
	items := page["items"].([]any)
	if code != http.StatusOK || len(items) != 30 || items[0].(map[string]any)["body"] != "m34" {
		t.Fatalf("default page: %d, %d items", code, len(items))
	}
	code, page = e.call(t, ada, http.MethodGet, path+"?before="+page["nextBefore"].(string), "")
	items = page["items"].([]any)
	if code != http.StatusOK || len(items) != 5 || items[0].(map[string]any)["body"] != "m4" || items[4].(map[string]any)["body"] != "m0" || page["nextBefore"] != nil {
		t.Errorf("last page: %d %v", code, page)
	}
	if code, page = e.call(t, ada, http.MethodGet, path+"?limit=1000", ""); len(page["items"].([]any)) != 35 {
		t.Errorf("limit 1000: %d, %d items", code, len(page["items"].([]any)))
	}
	code, body := e.call(t, ada, http.MethodGet, path+"?before=nonsense", "")
	wantError(t, "bad before", code, body, http.StatusBadRequest, "bad_request")
	code, body = e.call(t, ada, http.MethodGet, path+"?limit=0", "")
	wantError(t, "limit 0", code, body, http.StatusBadRequest, "bad_request")
}

// Messages have their own badge: they create no notification.
func TestMessagesCreateNoNotification(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	id, _ := e.startChat(t, ada, bob, "may I?")
	if code, body := e.call(t, bob, http.MethodPost, "/api/v1/conversations/"+id+"/accept", ""); code != http.StatusOK {
		t.Fatalf("accept: %d %v", code, body)
	}
	e.direct(t, bob, ada, "hi", newClientID())
	if n := e.countRows(t, "SELECT count(*) FROM notifications"); n != 0 {
		t.Errorf("%d notifications", n)
	}
}

// profileChat is the viewer's conversation on the owner's profile, nil when it is null.
func (e mediaEnv) profileChat(t *testing.T, viewer, owner string) map[string]any {
	t.Helper()
	code, p := e.call(t, viewer, http.MethodGet, "/api/v1/profiles/"+owner, "")
	if code != http.StatusOK {
		t.Fatalf("profile: %d %v", code, p)
	}
	value, present := p["conversation"]
	if !present {
		t.Fatal("profile has no conversation field")
	}
	chat, _ := value.(map[string]any)
	return chat
}

func wantProfileChat(t *testing.T, what string, chat map[string]any, id, state string, requestedBy any, canMessage bool) {
	t.Helper()
	if chat == nil {
		t.Fatalf("%s: conversation is null", what)
	}
	if chat["id"] != id || chat["state"] != state || chat["requestedBy"] != requestedBy || chat["canMessage"] != canMessage {
		t.Errorf("%s: %v; want %s %s by %v canMessage %v", what, chat, id, state, requestedBy, canMessage)
	}
}

// A profile shows the viewer's own conversation with its owner, in every state, for each
// side; a third user sees null, and so does the owner on their own profile.
func TestProfileShowsConversation(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy, dee := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)

	if chat := e.profileChat(t, ada, bob); chat != nil {
		t.Errorf("before any message: %v", chat)
	}

	// Pending: each side sees who asked, and neither can send.
	id, _ := e.startChat(t, ada, bob, "hello")
	wantProfileChat(t, "sender of a request", e.profileChat(t, ada, bob), id, "request", ada, false)
	wantProfileChat(t, "receiver of a request", e.profileChat(t, bob, ada), id, "request", ada, false)
	if chat := e.profileChat(t, cy, bob); chat != nil {
		t.Errorf("third user: %v", chat)
	}

	// Active.
	if code, body := e.call(t, bob, http.MethodPost, "/api/v1/conversations/"+id+"/accept", ""); code != http.StatusOK {
		t.Fatalf("accept: %d %v", code, body)
	}
	wantProfileChat(t, "active", e.profileChat(t, ada, bob), id, "active", ada, true)

	// Active but blocked: the blocker still opens the profile, and cannot send.
	e.block(t, ada, bob)
	wantProfileChat(t, "blocker", e.profileChat(t, ada, bob), id, "active", ada, false)
	e.unblock(t, ada, bob)

	// Declined: the sender is told by the profile, which the list never does.
	declined, _ := e.startChat(t, dee, bob, "hello")
	if code, body := e.call(t, bob, http.MethodPost, "/api/v1/conversations/"+declined+"/decline", ""); code != http.StatusOK {
		t.Fatalf("decline: %d %v", code, body)
	}
	wantProfileChat(t, "declined", e.profileChat(t, dee, bob), declined, "declined", dee, false)

	// Connected from the start: never a request.
	e.link(t, cy, dee)
	direct, _ := e.startChat(t, cy, dee, "hello")
	wantProfileChat(t, "connected", e.profileChat(t, cy, dee), direct, "active", nil, true)

	// One's own profile, by either route.
	if chat := e.profileChat(t, ada, ada); chat != nil {
		t.Errorf("own profile by ID: %v", chat)
	}
	code, own := e.call(t, ada, http.MethodGet, "/api/v1/profiles/me", "")
	if value, present := own["conversation"]; code != http.StatusOK || !present || value != nil {
		t.Errorf("profiles/me: %d conversation %v present %v", code, value, present)
	}
}
