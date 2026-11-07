//go:build integration

// End-to-end tests of blocks (FR-16.9): every effect in both directions, through the real
// router, the PostgreSQL adapters and Redis, on the test database. They reuse the
// environment of media_integration_test.go.
package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
	socialdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/social/domain"
)

func (e mediaEnv) block(t *testing.T, blocker, blocked string) {
	t.Helper()
	code, body := e.call(t, blocker, http.MethodPost, "/api/v1/blocks", fmt.Sprintf(`{"userId":%q}`, blocked))
	if code != http.StatusNoContent {
		t.Fatalf("block: %d %v", code, body)
	}
}

func (e mediaEnv) unblock(t *testing.T, blocker, blocked string) {
	t.Helper()
	if code, body := e.call(t, blocker, http.MethodDelete, "/api/v1/blocks/"+blocked, ""); code != http.StatusNoContent {
		t.Fatalf("unblock: %d %v", code, body)
	}
}

func (e mediaEnv) name(t *testing.T, userID, fullName, institution string) {
	t.Helper()
	if _, err := e.pool.Exec(t.Context(), "UPDATE profiles SET full_name = $1, institution = NULLIF($2, '') WHERE user_id = $3",
		fullName, institution, userID); err != nil {
		t.Fatal(err)
	}
}

// listed returns the caller's conversation with id from the box, nil when it is not listed.
func (e mediaEnv) listed(t *testing.T, userID, box, id string) map[string]any {
	t.Helper()
	for _, c := range e.box(t, userID, box) {
		if c["id"] == id {
			return c
		}
	}
	return nil
}

func (e mediaEnv) linksBetween(t *testing.T, a, b string) int {
	t.Helper()
	return e.countRows(t, `SELECT count(*) FROM connections WHERE status IN ('pending', 'accepted')
		AND ((requester_id = $1 AND receiver_id = $2) OR (requester_id = $2 AND receiver_id = $1))`, a, b)
}

// The blocked person gets 403 cannot_message, which does not say why; the blocker gets 409.
// The conversation stays in both lists.
func TestBlockInAConversation(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	id, _ := e.startChat(t, ada, bob, "hello")
	e.block(t, ada, bob)

	code, body := e.inConversation(t, bob, id, "hi", newClientID())
	wantError(t, "blocked, in the conversation", code, body, http.StatusForbidden, "cannot_message")
	if strings.Contains(strings.ToLower(fmt.Sprint(body)), "block") {
		t.Errorf("the blocked person is told: %v", body)
	}
	code, body = e.direct(t, bob, ada, "hi", newClientID())
	wantError(t, "blocked, direct", code, body, http.StatusForbidden, "cannot_message")
	code, body = e.inConversation(t, ada, id, "hi", newClientID())
	wantError(t, "blocker, in the conversation", code, body, http.StatusConflict, "you_blocked_this_user")
	code, body = e.direct(t, ada, bob, "hi", newClientID())
	wantError(t, "blocker, direct", code, body, http.StatusConflict, "you_blocked_this_user")
	if n := e.countRows(t, "SELECT count(*) FROM messages"); n != 1 {
		t.Errorf("%d messages stored, want 1", n)
	}

	for _, tc := range []struct {
		user        string
		blockedByMe bool
	}{{ada, true}, {bob, false}} {
		c := e.listed(t, tc.user, "inbox", id)
		if c == nil {
			t.Fatal("the conversation left the inbox")
		}
		if c["canMessage"] != false || c["blockedByMe"] != tc.blockedByMe {
			t.Errorf("listed: canMessage %v, blockedByMe %v", c["canMessage"], c["blockedByMe"])
		}
		code, one := e.call(t, tc.user, http.MethodGet, "/api/v1/conversations/"+id, "")
		if code != http.StatusOK || one["canMessage"] != false || one["blockedByMe"] != tc.blockedByMe {
			t.Errorf("get: %d %v", code, one)
		}
		if code, _ := e.call(t, tc.user, http.MethodGet, "/api/v1/conversations/"+id+"/messages", ""); code != http.StatusOK {
			t.Errorf("reading the messages: %d", code)
		}
	}
}

// With no conversation, the blocked person gets the 404 of an unknown user, the answer for
// the profile, so a first message does not reveal the block.
func TestBlockWithoutAConversation(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.block(t, ada, bob)
	code, body := e.direct(t, bob, ada, "hi", newClientID())
	wantError(t, "blocked", code, body, http.StatusNotFound, "not_found")
	code, body = e.direct(t, ada, bob, "hi", newClientID())
	wantError(t, "blocker", code, body, http.StatusConflict, "you_blocked_this_user")
	if n := e.countRows(t, "SELECT count(*) FROM conversations"); n != 0 {
		t.Errorf("%d conversations created", n)
	}
}

// A pending message request is declined, whichever of the two sent it, and stays declined.
func TestBlockDeclinesMessageRequest(t *testing.T) {
	for _, requesterBlocks := range []bool{true, false} {
		t.Run(fmt.Sprintf("requester blocks %v", requesterBlocks), func(t *testing.T) {
			e := newMediaEnv(t)
			from, to := e.newProfileUser(t), e.newProfileUser(t)
			id, _ := e.startChat(t, from, to, "may I?")
			if requesterBlocks {
				e.block(t, from, to)
			} else {
				e.block(t, to, from)
			}
			if n := e.countRows(t, "SELECT count(*) FROM conversations WHERE state = 'declined'"); n != 1 {
				t.Fatalf("declined conversations = %d", n)
			}
			if e.listed(t, to, "requests", id) != nil || e.listed(t, from, "inbox", id) != nil {
				t.Error("a declined request is still listed")
			}
			if _, requests := e.messageCounts(t, to); requests != 0 {
				t.Errorf("badge shows %v requests", requests)
			}
			if code, _ := e.call(t, to, http.MethodPost, "/api/v1/conversations/"+id+"/accept", ""); code != http.StatusNotFound {
				t.Errorf("accept: %d", code)
			}
			if requesterBlocks {
				e.unblock(t, from, to)
			} else {
				e.unblock(t, to, from)
			}
			code, body := e.inConversation(t, from, id, "again", newClientID())
			wantError(t, "after unblocking", code, body, http.StatusConflict, "conversation_closed")
		})
	}
}

// An existing connection goes, in both lists, with its notifications.
func TestBlockRemovesConnection(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.accept(t, bob, e.connect(t, ada, bob))
	if n := e.countRows(t, "SELECT count(*) FROM notifications"); n != 2 {
		t.Fatalf("%d notifications before the block, want 2", n)
	}
	e.block(t, bob, ada)
	for _, u := range []string{ada, bob} {
		if total, _ := e.listTotal(t, u, "/api/v1/connections"); total != 0 {
			t.Errorf("connections left: %v", total)
		}
		if _, count := e.call(t, u, http.MethodGet, "/api/v1/notifications/unread-count", ""); count["count"] != float64(0) {
			t.Errorf("unread count: %v", count)
		}
	}
	if n := e.countRows(t, "SELECT count(*) FROM notifications"); n != 0 {
		t.Errorf("%d notifications left", n)
	}
}

// Pending requests both ways are cancelled, with their notifications.
func TestBlockCancelsPendingConnectionRequests(t *testing.T) {
	for _, requesterBlocks := range []bool{true, false} {
		t.Run(fmt.Sprintf("requester blocks %v", requesterBlocks), func(t *testing.T) {
			e := newMediaEnv(t)
			from, to := e.newProfileUser(t), e.newProfileUser(t)
			e.connect(t, from, to)
			if requesterBlocks {
				e.block(t, from, to)
			} else {
				e.block(t, to, from)
			}
			if n := e.linksBetween(t, from, to); n != 0 {
				t.Errorf("%d live links", n)
			}
			if total, _ := e.listTotal(t, to, "/api/v1/connections/requests/received"); total != 0 {
				t.Errorf("received: %v", total)
			}
			if total, _ := e.listTotal(t, from, "/api/v1/connections/requests/sent"); total != 0 {
				t.Errorf("sent: %v", total)
			}
			if items, _ := e.notifications(t, to, ""); len(items) != 0 {
				t.Errorf("notifications: %v", items)
			}
		})
	}
}

// New requests are refused both ways: the same 404 as an unknown user for the blocked person.
func TestBlockRefusesConnectionRequests(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.block(t, ada, bob)
	code, unknown := e.sendRequest(t, bob, "00000000-0000-4000-8000-000000000000")
	wantError(t, "unknown user", code, unknown, http.StatusNotFound, "not_found")
	code, body := e.sendRequest(t, bob, ada)
	wantError(t, "blocked", code, body, http.StatusNotFound, "not_found")
	if fmt.Sprint(body) != fmt.Sprint(unknown) {
		t.Errorf("the answers differ: %v and %v", body, unknown)
	}
	code, body = e.sendRequest(t, ada, bob)
	wantError(t, "blocker", code, body, http.StatusConflict, "you_blocked_this_user")
}

// The blocked person cannot open the blocker; the blocker can open the blocked person, to
// unblock; anyone else sees both.
func TestBlockHidesProfile(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	e.block(t, ada, bob)
	paths := func(u string) []string {
		return []string{"/api/v1/profiles/" + u, "/api/v1/profiles/" + u + "/publications", "/api/v1/profiles/" + u + "/connections"}
	}
	for _, p := range paths(ada) {
		code, body := e.call(t, bob, http.MethodGet, p, "")
		wantError(t, "blocked person on "+p, code, body, http.StatusNotFound, "not_found")
		if code, _ := e.call(t, cy, http.MethodGet, p, ""); code != http.StatusOK {
			t.Errorf("third user on %s: %d", p, code)
		}
	}
	for _, p := range paths(bob) {
		if code, body := e.call(t, ada, http.MethodGet, p, ""); code != http.StatusOK {
			t.Errorf("blocker on %s: %d %v", p, code, body)
		}
	}
	_, p := e.call(t, ada, http.MethodGet, "/api/v1/profiles/"+bob, "")
	if p["blockedByMe"] != true {
		t.Errorf("blocker's blockedByMe = %v", p["blockedByMe"])
	}
	_, p = e.call(t, cy, http.MethodGet, "/api/v1/profiles/"+bob, "")
	if p["blockedByMe"] != false {
		t.Errorf("third user's blockedByMe = %v", p["blockedByMe"])
	}
}

// Search leaves the blocker out for the blocked person, in the results, totals and facets; the
// blocker still finds the blocked person, and a third user finds both.
func TestBlockHidesFromSearch(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	word, inst := testenv.Word(t), "Institute of "+testenv.Word(t)
	e.name(t, ada, word+" One", inst)
	e.name(t, bob, word+" Two", inst)
	e.block(t, ada, bob)

	search := func(viewer, q string) map[string]any {
		t.Helper()
		code, body := e.call(t, viewer, http.MethodGet, "/api/v1/search/people?q="+url.QueryEscape(q), "")
		if code != http.StatusOK {
			t.Fatalf("search: %d %v", code, body)
		}
		return body
	}
	ids := func(body map[string]any) map[string]bool {
		out := map[string]bool{}
		for _, it := range body["items"].([]any) {
			out[it.(map[string]any)["userId"].(string)] = true
		}
		return out
	}
	byBlocked := search(bob, word)
	if got := ids(byBlocked); got[ada] || !got[bob] || byBlocked["total"] != float64(1) {
		t.Errorf("the blocked person's search: %v, total %v", got, byBlocked["total"])
	}
	insts := byBlocked["facets"].(map[string]any)["institutions"].([]any)
	if len(insts) != 1 || insts[0].(map[string]any)["count"] != float64(1) {
		t.Errorf("the institution facet counts the blocker: %v", insts)
	}
	if got := ids(search(ada, word)); !got[bob] || !got[ada] {
		t.Errorf("the blocker's search: %v", got)
	}
	if got := ids(search(cy, word)); !got[ada] || !got[bob] {
		t.Errorf("a third user's search: %v", got)
	}
}

// A blocker's literal name does not turn fuzzy matching off for the blocked person, which would
// reveal that someone of that name exists.
func TestBlockedNameDoesNotDecideFuzzy(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	e.name(t, ada, "Mahmud Rahman", "")
	e.name(t, cy, "Mahmud Rahmat", "")
	e.block(t, ada, bob)
	code, body := e.call(t, bob, http.MethodGet, "/api/v1/search/people?q="+url.QueryEscape("Mahmud Rahman"), "")
	if code != http.StatusOK {
		t.Fatalf("%d %v", code, body)
	}
	items := body["items"].([]any)
	if body["fuzzy"] != true || len(items) != 1 || items[0].(map[string]any)["userId"] != cy {
		t.Errorf("fuzzy %v, items %v", body["fuzzy"], items)
	}
}

// A third user's connection list leaves the blocker out for the blocked person, with a total
// and a profile count that agree.
func TestBlockHidesFromConnectionLists(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, cy)
	e.link(t, bob, cy)
	e.block(t, ada, bob)
	path := "/api/v1/profiles/" + cy + "/connections"
	total, items := e.listTotal(t, bob, path)
	if total != 1 || len(items) != 1 || items[0].(map[string]any)["userId"] != bob {
		t.Errorf("the blocked person sees %v", items)
	}
	if _, p := e.call(t, bob, http.MethodGet, "/api/v1/profiles/"+cy, ""); p["connectionCount"] != float64(1) {
		t.Errorf("connectionCount for the blocked person = %v", p["connectionCount"])
	}
	if total, _ := e.listTotal(t, ada, path); total != 2 {
		t.Errorf("the blocker sees %v", total)
	}
	if _, p := e.call(t, ada, http.MethodGet, "/api/v1/profiles/"+cy, ""); p["connectionCount"] != float64(2) {
		t.Errorf("connectionCount for the blocker = %v", p["connectionCount"])
	}
}

// Unblocking restores no connection, request or notification. An active conversation works
// again, because the block never closed it.
func TestUnblockRestoresNothing(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.accept(t, bob, e.connect(t, ada, bob))
	id, _ := e.startChat(t, ada, bob, "hello")
	e.block(t, ada, bob)
	e.unblock(t, ada, bob)

	if n := e.linksBetween(t, ada, bob); n != 0 {
		t.Errorf("%d live links", n)
	}
	if n := e.countRows(t, "SELECT count(*) FROM notifications"); n != 0 {
		t.Errorf("%d notifications", n)
	}
	if c := e.listed(t, ada, "inbox", id); c["canMessage"] != true || c["blockedByMe"] != false {
		t.Errorf("after unblocking: %v", c)
	}
	if code, body := e.inConversation(t, bob, id, "hi again", newClientID()); code != http.StatusCreated {
		t.Errorf("send: %d %v", code, body)
	}
	if code, _ := e.call(t, bob, http.MethodGet, "/api/v1/profiles/"+ada, ""); code != http.StatusOK {
		t.Errorf("profile: %d", code)
	}
	e.connect(t, bob, ada)
}

func TestBlockRules(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, sam := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET suspended_at = now() WHERE id = $1", sam); err != nil {
		t.Fatal(err)
	}
	post := func(from, to string) (int, map[string]any) {
		return e.call(t, from, http.MethodPost, "/api/v1/blocks", fmt.Sprintf(`{"userId":%q}`, to))
	}
	code, body := post(ada, ada)
	wantError(t, "oneself", code, body, http.StatusBadRequest, "cannot_block_self")
	code, body = post(ada, "00000000-0000-4000-8000-000000000000")
	wantError(t, "unknown", code, body, http.StatusNotFound, "not_found")
	code, body = post(ada, sam)
	wantError(t, "suspended", code, body, http.StatusNotFound, "not_found")

	// Idempotent, both ways, and each side may block the other.
	e.block(t, ada, bob)
	e.block(t, ada, bob)
	e.block(t, bob, ada)
	if n := e.countRows(t, "SELECT count(*) FROM blocks"); n != 2 {
		t.Errorf("%d rows", n)
	}
	if code, body := e.direct(t, bob, ada, "hi", newClientID()); code != http.StatusConflict {
		t.Errorf("blocked both ways: %d %v", code, body)
	}
	e.unblock(t, bob, ada)
	e.unblock(t, bob, ada)
	if code, _ := e.call(t, ada, http.MethodDelete, "/api/v1/blocks/not-a-uuid", ""); code != http.StatusNoContent {
		t.Errorf("unblock of a bad ID: %d", code)
	}
	if n := e.countRows(t, "SELECT count(*) FROM blocks"); n != 1 {
		t.Errorf("%d rows after one side's unblock", n)
	}
}

func TestListBlockedPeople(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	e.name(t, bob, "Bob Gone", "")
	e.block(t, ada, bob)
	e.block(t, ada, cy)
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET suspended_at = now() WHERE id = $1", cy); err != nil {
		t.Fatal(err)
	}
	total, items := e.listTotal(t, ada, "/api/v1/blocks")
	if total != 1 || len(items) != 1 {
		t.Fatalf("total %v, items %v", total, items)
	}
	it := items[0].(map[string]any)
	if it["userId"] != bob || it["fullName"] != "Bob Gone" || it["blockedAt"] == nil {
		t.Errorf("item = %v", it)
	}
	if _, ok := it["photoUrl"]; !ok {
		t.Error("no photoUrl")
	}
	if _, ok := it["headline"]; !ok {
		t.Error("no headline")
	}
	if total, _ := e.listTotal(t, bob, "/api/v1/blocks"); total != 0 {
		t.Errorf("the blocked person's list has %v", total)
	}
	if code, _ := e.call(t, ada, http.MethodGet, "/api/v1/blocks?page=0", ""); code != http.StatusBadRequest {
		t.Errorf("page 0: %d", code)
	}
}

// A failure anywhere in the block rolls all of it back.
func TestBlockIsOneTransaction(t *testing.T) {
	fail := &atomic.Bool{}
	e := newMediaEnvOnPool(t, testenv.DB(t), "", "", envOptions{wrapNotifier: func(n socialdomain.Notifier) socialdomain.Notifier {
		return failingNotifier{next: n, fail: fail}
	}})
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.accept(t, bob, e.connect(t, ada, bob))
	fail.Store(true)
	code, _ := e.call(t, ada, http.MethodPost, "/api/v1/blocks", fmt.Sprintf(`{"userId":%q}`, bob))
	if code != http.StatusInternalServerError {
		t.Fatalf("block: %d", code)
	}
	if b, l, n := e.countRows(t, "SELECT count(*) FROM blocks"), e.linksBetween(t, ada, bob), e.countRows(t, "SELECT count(*) FROM notifications"); b != 0 || l != 1 || n != 2 {
		t.Errorf("blocks %d, links %d, notifications %d; want 0, 1, 2", b, l, n)
	}
}

// A block racing a connection request and a first message leaves neither behind: whichever
// runs first, the other sees it.
func TestBlockRacingNewContact(t *testing.T) {
	e := newMediaEnv(t)
	for range 15 {
		ada, bob := e.newProfileUser(t), e.newProfileUser(t)
		var wg sync.WaitGroup
		wg.Go(func() {
			if code, body := e.call(t, ada, http.MethodPost, "/api/v1/blocks", fmt.Sprintf(`{"userId":%q}`, bob)); code != http.StatusNoContent {
				t.Errorf("block: %d %v", code, body)
			}
		})
		wg.Go(func() {
			if code, body := e.sendRequest(t, bob, ada); code != http.StatusCreated && code != http.StatusNotFound {
				t.Errorf("request: %d %v", code, body)
			}
		})
		wg.Go(func() {
			if code, body := e.direct(t, bob, ada, "hi", newClientID()); code != http.StatusCreated && code != http.StatusNotFound {
				t.Errorf("message: %d %v", code, body)
			}
		})
		wg.Wait()
		if n := e.linksBetween(t, ada, bob); n != 0 {
			t.Fatalf("%d live links after the block", n)
		}
		if n := e.countRows(t, `SELECT count(*) FROM conversations
			WHERE state = 'request' AND LEAST($1::uuid, $2::uuid) = user_low AND GREATEST($1::uuid, $2::uuid) = user_high`, ada, bob); n != 0 {
			t.Fatalf("%d pending requests after the block", n)
		}
	}
}

// A block that lands while a connection is being accepted leaves no connection.
func TestBlockRacingAccept(t *testing.T) {
	e := newMediaEnv(t)
	for range 15 {
		ada, bob := e.newProfileUser(t), e.newProfileUser(t)
		id := e.connect(t, ada, bob)
		var wg sync.WaitGroup
		wg.Go(func() { e.call(t, bob, http.MethodPost, "/api/v1/connections/requests/"+id+"/accept", "") })
		wg.Go(func() {
			if code, body := e.call(t, ada, http.MethodPost, "/api/v1/blocks", fmt.Sprintf(`{"userId":%q}`, bob)); code != http.StatusNoContent {
				t.Errorf("block: %d %v", code, body)
			}
		})
		wg.Wait()
		if n := e.linksBetween(t, ada, bob); n != 0 {
			t.Fatalf("%d live links after the block", n)
		}
		if n := e.countRows(t, `SELECT count(*) FROM notifications WHERE (user_id = $1 AND actor_id = $2) OR (user_id = $2 AND actor_id = $1)`, ada, bob); n != 0 {
			t.Fatalf("%d notifications after the block", n)
		}
	}
}
