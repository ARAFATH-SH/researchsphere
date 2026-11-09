//go:build integration

// End-to-end tests of group conversations (FR-16.6, with FR-16.3, FR-16.5, FR-16.9, FR-16.10 and
// NFR-14 for groups) through the real router, the PostgreSQL adapters and triggers, Redis and
// the WebSocket, on the test database. They reuse the environment of media_integration_test.go.
package main

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	realtimedomain "github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/domain"
)

const groupsPath = "/api/v1/conversations/groups"

func groupPath(id string) string { return "/api/v1/conversations/" + id }

func idList(ids ...string) string {
	q := make([]string, len(ids))
	for i, id := range ids {
		q[i] = fmt.Sprintf("%q", id)
	}
	return "[" + strings.Join(q, ",") + "]"
}

func (e mediaEnv) createGroup(t *testing.T, owner, name string, members ...string) (int, map[string]any) {
	t.Helper()
	return e.call(t, owner, http.MethodPost, groupsPath,
		fmt.Sprintf(`{"name":%q,"memberIds":%s,"clientId":%q}`, name, idList(members...), newClientID()))
}

// newGroup links the owner with each member and creates the group.
func (e mediaEnv) newGroup(t *testing.T, owner string, members ...string) string {
	t.Helper()
	for _, m := range members {
		e.link(t, owner, m)
	}
	code, g := e.createGroup(t, owner, "Lab", members...)
	if code != http.StatusCreated {
		t.Fatalf("create group: %d %v", code, g)
	}
	return g["id"].(string)
}

func (e mediaEnv) addMembers(t *testing.T, owner, id string, users ...string) (int, map[string]any) {
	t.Helper()
	return e.call(t, owner, http.MethodPost, groupPath(id)+"/members", `{"userIds":`+idList(users...)+`}`)
}

func (e mediaEnv) say(t *testing.T, from, id, body string) string {
	t.Helper()
	code, sent := e.inConversation(t, from, id, body, newClientID())
	if code != http.StatusCreated {
		t.Fatalf("send: %d %v", code, sent)
	}
	return sent["message"].(map[string]any)["id"].(string)
}

// groupOf returns the conversation's group part as userID sees it.
func (e mediaEnv) groupOf(t *testing.T, userID, id string) map[string]any {
	t.Helper()
	code, c := e.call(t, userID, http.MethodGet, groupPath(id), "")
	if code != http.StatusOK {
		t.Fatalf("GET conversation: %d %v", code, c)
	}
	return c["group"].(map[string]any)
}

func memberRoles(g map[string]any) map[string]string {
	out := map[string]string{}
	for _, m := range g["members"].([]any) {
		mm := m.(map[string]any)
		out[mm["userId"].(string)] = mm["role"].(string)
	}
	return out
}

func eventTypes(thread []map[string]any) []string {
	var out []string
	for _, m := range slices.Backward(thread) {
		if ev, ok := m["event"].(map[string]any); ok {
			out = append(out, ev["type"].(string))
		}
	}
	return out
}

// FR-16.6: create, rename, add and remove, each with its system message; members see the group
// in their list with its name, size and avatars, and a system preview as its event key.
func TestGroupLifecycle(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy, dee := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	for _, u := range []string{bob, cy, dee} {
		e.link(t, ada, u)
	}

	code, g := e.createGroup(t, ada, "  Lab  ", bob, cy)
	if code != http.StatusCreated || g["kind"] != "group" || g["otherMember"] != nil || g["canMessage"] != true {
		t.Fatalf("create: %d %v", code, g)
	}
	id := g["id"].(string)
	info := g["group"].(map[string]any)
	if info["name"] != "Lab" || info["memberCount"] != 3.0 || info["myRole"] != "owner" || len(info["members"].([]any)) != 3 {
		t.Errorf("group = %v", info)
	}
	if roles := memberRoles(info); roles[ada] != "owner" || roles[bob] != "member" || roles[cy] != "member" {
		t.Errorf("roles = %v", roles)
	}

	listed := e.listed(t, bob, "inbox", id)
	lg := listed["group"].(map[string]any)
	if listed["kind"] != "group" || lg["name"] != "Lab" || lg["myRole"] != "member" || lg["members"] != nil || len(lg["avatars"].([]any)) != 2 {
		t.Errorf("bob's list item = %v", listed)
	}
	if last := listed["lastMessage"].(map[string]any); last["kind"] != "system" || last["event"] != "group_created" || last["body"] != nil {
		t.Errorf("preview = %v", last)
	}

	// Only the owner renames, adds and removes.
	code, body := e.call(t, bob, http.MethodPatch, groupPath(id), `{"name":"Bob's"}`)
	wantError(t, "member renames", code, body, http.StatusForbidden, "not_group_owner")
	code, body = e.addMembers(t, bob, id, dee)
	wantError(t, "member adds", code, body, http.StatusForbidden, "not_group_owner")
	code, body = e.call(t, bob, http.MethodDelete, groupPath(id)+"/members/"+cy, "")
	wantError(t, "member removes", code, body, http.StatusForbidden, "not_group_owner")
	code, body = e.call(t, ada, http.MethodPatch, groupPath(id), `{"name":"one\ntwo"}`)
	wantError(t, "two-line name", code, body, http.StatusBadRequest, "invalid_group_name")

	if code, r := e.call(t, ada, http.MethodPatch, groupPath(id), `{"name":"Lab 2"}`); code != http.StatusOK || r["group"].(map[string]any)["name"] != "Lab 2" {
		t.Errorf("rename: %d %v", code, r)
	}
	if code, r := e.addMembers(t, ada, id, dee, bob); code != http.StatusOK || r["group"].(map[string]any)["memberCount"] != 4.0 {
		t.Errorf("add: %d %v", code, r)
	}
	if code, _ := e.call(t, ada, http.MethodDelete, groupPath(id)+"/members/"+cy, ""); code != http.StatusNoContent {
		t.Errorf("remove: %d", code)
	}
	code, body = e.call(t, ada, http.MethodDelete, groupPath(id)+"/members/"+ada, "")
	wantError(t, "owner removes themselves", code, body, http.StatusBadRequest, "cannot_remove_self")

	thread := e.thread(t, bob, id)
	if got := eventTypes(thread); !slices.Equal(got, []string{"group_created", "group_renamed", "member_added", "member_removed"}) {
		t.Errorf("events = %v", got)
	}
	renamed := thread[2]["event"].(map[string]any)
	if renamed["oldName"] != "Lab" || renamed["newName"] != "Lab 2" || renamed["actorId"] != ada || thread[2]["senderId"] != ada {
		t.Errorf("renamed = %v", thread[2])
	}
	if added := thread[1]["event"].(map[string]any)["userIds"].([]any); len(added) != 1 || added[0] != dee {
		t.Errorf("added = %v, want only dee (bob was already in)", added)
	}

	// Members, the owner and dee (added) get group_added notifications; the removed member's
	// was taken back.
	for user, want := range map[string]int{bob: 1, dee: 1, cy: 0, ada: 0} {
		if n := e.countRows(t, "SELECT count(*) FROM notifications WHERE user_id = $1 AND type = 'group_added' AND entity_id = $2", user, id); n != want {
			t.Errorf("%s has %d group_added notifications, want %d", user, n, want)
		}
	}
	items, _ := e.notifications(t, dee, "")
	if len(items) == 0 || items[0]["type"] != "group_added" || items[0]["link"] != "/messaging/"+id || items[0]["actor"].(map[string]any)["userId"] != ada {
		t.Errorf("dee's notification = %v", items)
	}
}

// A retried create with the same client ID returns the first group with 200 and creates
// nothing.
func TestCreateGroupIsIdempotent(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	body := fmt.Sprintf(`{"name":"Lab","memberIds":[%q],"clientId":%q}`, bob, newClientID())
	code, first := e.call(t, ada, http.MethodPost, groupsPath, body)
	if code != http.StatusCreated {
		t.Fatalf("first: %d %v", code, first)
	}
	code, again := e.call(t, ada, http.MethodPost, groupsPath, body)
	if code != http.StatusOK || again["id"] != first["id"] {
		t.Errorf("again: %d %v", code, again)
	}
	if n := e.countRows(t, "SELECT count(*) FROM conversations WHERE kind = 'group'"); n != 1 {
		t.Errorf("%d groups", n)
	}
}

// Only the creator's connections who are verified, not suspended and in no block with the
// creator; the refusal lists them without saying why. A blocked pair cannot be put together by
// either of them.
func TestCreateGroupRefusals(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, stranger, blocked, blocker, suspended := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t),
		e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	for _, u := range []string{bob, blocked, blocker, suspended} {
		e.link(t, ada, u)
	}
	e.block(t, ada, blocked)
	e.block(t, blocker, ada)
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET suspended_at = now() WHERE id = $1", suspended); err != nil {
		t.Fatal(err)
	}
	code, body := e.createGroup(t, ada, "Lab", bob, stranger, blocked, blocker, suspended)
	wantError(t, "ineligible", code, body, http.StatusBadRequest, "ineligible_members")
	var got []string
	for _, id := range body["userIds"].([]any) {
		got = append(got, id.(string))
	}
	want := []string{stranger, blocked, blocker, suspended}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("userIds = %v, want %v", got, want)
	}
	if !strings.Contains(fmt.Sprint(body), "cannot be added") || strings.Contains(fmt.Sprint(body), "block") {
		t.Errorf("the refusal says why: %v", body)
	}
	if n := e.countRows(t, "SELECT count(*) FROM conversations WHERE kind = 'group'"); n != 0 {
		t.Errorf("%d groups stored", n)
	}
	// The blocker cannot put the pair together either.
	e.link(t, blocker, bob)
	if code, body := e.createGroup(t, blocker, "Lab", bob, ada); code != http.StatusBadRequest || body["code"] != "ineligible_members" {
		t.Errorf("blocker: %d %v", code, body)
	}
	code, body = e.createGroup(t, ada, "Lab", ada)
	wantError(t, "oneself", code, body, http.StatusBadRequest, "bad_request")
}

// The total stays at most 50: the 51st member is 409 group_full and changes nothing.
func TestGroupFull(t *testing.T) {
	e := newMediaEnv(t)
	ada := e.newProfileUser(t)
	var members []string
	for range 49 {
		u := e.newProfileUser(t)
		if _, err := e.pool.Exec(t.Context(),
			"INSERT INTO connections (requester_id, receiver_id, status, responded_at) VALUES ($1, $2, 'accepted', now())", ada, u); err != nil {
			t.Fatal(err)
		}
		members = append(members, u)
	}
	code, g := e.createGroup(t, ada, "Big", members...)
	if code != http.StatusCreated || g["group"].(map[string]any)["memberCount"] != 50.0 {
		t.Fatalf("create 50: %d %v", code, g)
	}
	extra := e.newProfileUser(t)
	e.link(t, ada, extra)
	code, body := e.addMembers(t, ada, g["id"].(string), extra)
	wantError(t, "51st", code, body, http.StatusConflict, "group_full")
}

// A new member sees only what was sent after they joined; a removed member loses all access at
// once and the group leaves their list (NFR-14).
func TestGroupJoiningAndRemoval(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	id := e.newGroup(t, ada, bob)
	earlier := e.say(t, bob, id, "before cy")
	e.link(t, ada, cy)
	if code, body := e.addMembers(t, ada, id, cy); code != http.StatusOK {
		t.Fatalf("add: %d %v", code, body)
	}
	e.say(t, bob, id, "after cy")

	thread := e.thread(t, cy, id)
	if len(thread) != 2 || thread[0]["body"] != "after cy" || thread[1]["event"].(map[string]any)["type"] != "member_added" {
		t.Errorf("cy sees %v", thread)
	}
	if strings.Contains(fmt.Sprint(thread, e.box(t, cy, "inbox")), "before cy") {
		t.Error("cy sees an earlier message")
	}
	code, body := e.call(t, cy, http.MethodPost, groupPath(id)+"/read", `{"messageId":"`+earlier+`"}`)
	wantError(t, "mark an earlier message read", code, body, http.StatusNotFound, "not_found")
	if total, _ := e.messageCounts(t, cy); total != 1 {
		t.Errorf("cy's unread = %v, want 1", total)
	}

	if code, _ := e.call(t, ada, http.MethodDelete, groupPath(id)+"/members/"+cy, ""); code != http.StatusNoContent {
		t.Fatalf("remove: %d", code)
	}
	for _, req := range []struct{ method, path, body string }{
		{http.MethodGet, groupPath(id), ""},
		{http.MethodGet, groupPath(id) + "/messages", ""},
		{http.MethodPost, groupPath(id) + "/messages", fmt.Sprintf(`{"body":"hi","clientId":%q}`, newClientID())},
		{http.MethodPost, groupPath(id) + "/leave", ""},
		{http.MethodPost, groupPath(id) + "/reports", `{"reason":"spam"}`},
	} {
		code, body := e.call(t, cy, req.method, req.path, req.body)
		wantError(t, "removed member: "+req.method+" "+req.path, code, body, http.StatusNotFound, "not_found")
	}
	if len(e.box(t, cy, "inbox")) != 0 {
		t.Error("the group is still in the removed member's list")
	}
	if total, _ := e.messageCounts(t, cy); total != 0 {
		t.Errorf("removed member's unread = %v", total)
	}
}

// Leaving: the owner's leave hands ownership to the member who joined earliest, with a
// member_left then an owner_changed message; the last member's leave closes the group.
func TestGroupLeaveAndHandOver(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	id := e.newGroup(t, ada, cy)
	e.link(t, ada, bob)
	if code, _ := e.addMembers(t, ada, id, bob); code != http.StatusOK {
		t.Fatal(code)
	}

	if code, _ := e.call(t, ada, http.MethodPost, groupPath(id)+"/leave", ""); code != http.StatusNoContent {
		t.Fatalf("owner leaves: %d", code)
	}
	if roles := memberRoles(e.groupOf(t, bob, id)); roles[cy] != "owner" || roles[bob] != "member" || len(roles) != 2 {
		t.Errorf("roles = %v, want cy (joined first) as owner", roles)
	}
	thread := e.thread(t, bob, id)
	if got := eventTypes(thread[:2]); !slices.Equal(got, []string{"member_left", "owner_changed"}) {
		t.Errorf("latest events = %v", got)
	}
	if ev := thread[0]["event"].(map[string]any); thread[0]["senderId"] != nil || ev["actorId"] != nil || ev["userIds"].([]any)[0] != cy {
		t.Errorf("owner_changed = %v", thread[0])
	}
	code, body := e.call(t, ada, http.MethodGet, groupPath(id), "")
	wantError(t, "former owner", code, body, http.StatusNotFound, "not_found")
	if code, _ := e.call(t, cy, http.MethodPatch, groupPath(id), `{"name":"Cy's"}`); code != http.StatusOK {
		t.Errorf("new owner renames: %d", code)
	}

	for _, who := range []string{bob, cy} {
		if code, _ := e.call(t, who, http.MethodPost, groupPath(id)+"/leave", ""); code != http.StatusNoContent {
			t.Fatalf("%s leaves: %d", who, code)
		}
	}
	if n := e.countRows(t, "SELECT count(*) FROM conversations WHERE id = $1 AND closed_at IS NOT NULL", id); n != 1 {
		t.Error("the last member left and the group is not closed")
	}
	if n := e.countRows(t, "SELECT count(*) FROM conversation_memberships WHERE conversation_id = $1 AND left_at IS NULL", id); n != 0 {
		t.Errorf("%d stays still open", n)
	}
	// A direct conversation has no leave.
	e.link(t, bob, cy)
	direct, _ := e.startChat(t, bob, cy, "hi")
	code, body = e.call(t, bob, http.MethodPost, groupPath(direct)+"/leave", "")
	wantError(t, "leave a direct conversation", code, body, http.StatusNotFound, "not_found")
}

// The owner deleting their account hands ownership over through the database trigger, with
// its owner_changed message (SRS 7, Retention).
func TestGroupOwnerAccountDeleted(t *testing.T) {
	e := newModEnv(t)
	ada, bob, cy := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	id := e.newGroup(t, ada, bob, cy)
	e.say(t, bob, id, "still here")
	e.withPassword(t, ada)
	if code, body := e.call(t, ada, http.MethodPost, "/api/v1/account/delete", fmt.Sprintf(`{"password":%q}`, modPassword)); code != http.StatusNoContent {
		t.Fatalf("delete account: %d %v", code, body)
	}
	heir := min(bob, cy)
	roles := memberRoles(e.groupOf(t, bob, id))
	if roles[heir] != "owner" || len(roles) != 2 {
		t.Errorf("roles = %v, want %s as owner", roles, heir)
	}
	thread := e.thread(t, bob, id)
	if ev, ok := thread[0]["event"].(map[string]any); !ok || ev["type"] != "owner_changed" || ev["actorId"] != nil || ev["userIds"].([]any)[0] != heir {
		t.Errorf("latest = %v", thread[0])
	}
}

// FR-16.3: each member's read marker, "seen by" from readers; unread counts include groups and
// system messages never count.
func TestGroupReadMarkersAndUnread(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	id := e.newGroup(t, ada, bob, cy)
	if total, _ := e.messageCounts(t, bob); total != 0 {
		t.Errorf("unread after create = %v (system message counted)", total)
	}
	m := e.say(t, ada, id, "hello all")
	e.call(t, ada, http.MethodPatch, groupPath(id), `{"name":"Lab 2"}`)
	e.startChat(t, ada, bob, "direct too")
	if total, _ := e.messageCounts(t, bob); total != 2 {
		t.Errorf("bob's unread = %v, want 2 (group and direct)", total)
	}
	if it := e.listed(t, bob, "inbox", id); it["unreadCount"] != 1.0 {
		t.Errorf("group unreadCount = %v", it["unreadCount"])
	}

	if code, _ := e.call(t, bob, http.MethodPost, groupPath(id)+"/read", `{"messageId":"`+m+`"}`); code != http.StatusNoContent {
		t.Fatalf("read: %d", code)
	}
	code, page := e.call(t, ada, http.MethodGet, groupPath(id)+"/messages", "")
	if code != http.StatusOK || page["otherLastReadMessageId"] != nil {
		t.Fatalf("page: %d %v", code, page)
	}
	seenBy := 0
	for _, r := range page["readers"].([]any) {
		rr := r.(map[string]any)
		if rr["userId"] == ada {
			t.Error("readers lists the caller")
		}
		if id, ok := rr["lastReadMessageId"].(string); ok && id >= m {
			seenBy++
		}
	}
	if len(page["readers"].([]any)) != 2 || seenBy != 1 {
		t.Errorf("readers = %v, seen by %d", page["readers"], seenBy)
	}
	if total, _ := e.messageCounts(t, bob); total != 1 {
		t.Errorf("bob's unread after reading the group = %v", total)
	}

	// System messages cannot be edited, deleted or reported.
	sys := e.thread(t, bob, id)[0]["id"].(string)
	code, body := e.edit(t, ada, id, sys, "x")
	wantError(t, "edit a system message", code, body, http.StatusNotFound, "not_found")
	code, body = e.deleteMessage(t, ada, id, sys)
	wantError(t, "delete a system message", code, body, http.StatusNotFound, "not_found")
	code, body = e.call(t, bob, http.MethodPost, groupPath(id)+"/reports", `{"reason":"spam","messageId":"`+sys+`"}`)
	wantError(t, "report a system message", code, body, http.StatusNotFound, "not_found")
}

// FR-16.9: any member reports the group (no reported user) or a message (its sender); the
// administrator reads every message up to the report, system messages included, and who was in
// the group then; an action may name any of them, nobody else.
func TestGroupReport(t *testing.T) {
	e := newModEnv(t)
	ada, bob, cy, dee := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	id := e.newGroup(t, ada, bob, cy)
	rude := e.say(t, bob, id, "rude words")
	e.say(t, cy, id, "from cy")

	code, receipt := e.call(t, cy, http.MethodPost, groupPath(id)+"/reports", `{"reason":"harassment"}`)
	if code != http.StatusCreated || receipt["reportedUserId"] != nil || receipt["blockedByMe"] != false {
		t.Fatalf("whole group: %d %v", code, receipt)
	}
	whole := receipt["id"].(string)
	code, receipt = e.call(t, cy, http.MethodPost, groupPath(id)+"/reports", `{"reason":"spam","messageId":"`+rude+`"}`)
	if code != http.StatusCreated || receipt["reportedUserId"] != bob {
		t.Fatalf("message: %d %v", code, receipt)
	}

	// Written after the report: not shown. Dee joins after it: not a member at its moment.
	e.say(t, ada, id, "after the report")
	e.link(t, ada, dee)
	e.addMembers(t, ada, id, dee)

	code, detail := e.adminDo(t, http.MethodGet, adminReports+"/"+whole, "")
	if code != http.StatusOK {
		t.Fatalf("detail: %d %v", code, detail)
	}
	r := detail["report"].(map[string]any)
	if r["reportedUser"] != nil || r["conversationKind"] != "group" || r["groupName"] != "Lab" {
		t.Errorf("report = %v", r)
	}
	var shown []string
	for _, m := range detail["messages"].([]any) {
		mm := m.(map[string]any)
		if mm["kind"] == "system" {
			shown = append(shown, mm["event"].(map[string]any)["type"].(string))
		} else {
			shown = append(shown, mm["body"].(string))
		}
	}
	if !slices.Equal(shown, []string{"from cy", "rude words", "group_created"}) {
		t.Errorf("messages = %v", shown)
	}
	var atReport []string
	for _, p := range detail["membersAtReport"].([]any) {
		atReport = append(atReport, p.(map[string]any)["userId"].(string))
	}
	slices.Sort(atReport)
	want := []string{ada, bob, cy}
	slices.Sort(want)
	if !slices.Equal(atReport, want) {
		t.Errorf("membersAtReport = %v, want %v", atReport, want)
	}

	withReport := fmt.Sprintf(`{"reason":%q,"reportId":%q}`, modReason, whole)
	code, body := e.adminDo(t, http.MethodPost, userPath(dee, "warnings"), withReport)
	if code != http.StatusBadRequest || body["field"] != "reportId" {
		t.Errorf("action on someone not in the group then: %d %v", code, body)
	}
	if code, body := e.adminDo(t, http.MethodPost, userPath(bob, "warnings"), withReport); code != http.StatusCreated || body["reportId"] != whole {
		t.Errorf("action on a member: %d %v", code, body)
	}
	if e.reportOutcome(t, whole) != "action_taken" {
		t.Error("the report was not resolved by the action")
	}
}

// FR-16.6 in real time: a message reaches every other member with userId-free fan-out; a
// change reaches every member as conversation.members; the removed member's tabs hear the
// access is lost; typing goes to every other member except a blocked pair.
func TestGroupRealtime(t *testing.T) {
	e := newMediaEnv(t)
	srv := serve(t, e.router)
	ada, bob, cy, dee := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	id := e.newGroup(t, ada, bob, cy, dee)
	e.block(t, dee, bob)
	adaTab, bobTab, cyTab, deeTab := dial(t, srv, ada), dial(t, srv, bob), dial(t, srv, cy), dial(t, srv, dee)

	m := e.say(t, bob, id, "hello group")
	isMsg := func(d map[string]any) bool { return d["message"].(map[string]any)["id"] == m }
	for _, tab := range []*wsClient{adaTab, cyTab, deeTab} {
		ev := tab.expect(t, realtimedomain.EventMessageCreated, isMsg)
		if _, leaked := ev.Data["clientId"]; leaked {
			t.Errorf("another member got the clientId: %v", ev.Data)
		}
	}
	if ev := bobTab.expect(t, realtimedomain.EventMessageCreated, isMsg); ev.Data["clientId"] == nil {
		t.Errorf("the sender's tab has no clientId: %v", ev.Data)
	}

	// Typing goes to all others but not across the block.
	bobTab.typing(t, realtimedomain.ClientTypingStart, id)
	for _, tab := range []*wsClient{adaTab, cyTab} {
		if ev := tab.expect(t, realtimedomain.EventTyping); ev.Data["userId"] != bob || ev.Data["conversationId"] != id {
			t.Errorf("typing = %v", ev.Data)
		}
	}
	deeTab.silent(t, realtimedomain.EventTyping, 200*time.Millisecond)

	// Rename: every member hears the change and the system message.
	e.call(t, ada, http.MethodPatch, groupPath(id), `{"name":"Lab 2"}`)
	for _, tab := range []*wsClient{adaTab, bobTab, cyTab, deeTab} {
		// The system message goes out first, then the change.
		tab.expect(t, realtimedomain.EventMessageCreated, func(d map[string]any) bool {
			return d["message"].(map[string]any)["kind"] == "system"
		})
		ev := tab.expect(t, realtimedomain.EventConversationMembers)
		if ev.Data["change"] != "renamed" || ev.Data["name"] != "Lab 2" || ev.Data["actorId"] != ada || ev.Data["conversationId"] != id {
			t.Errorf("renamed = %v", ev.Data)
		}
	}

	// Removal: the removed member's tab is told access is lost; the others hear removed.
	if code, _ := e.call(t, ada, http.MethodDelete, groupPath(id)+"/members/"+cy, ""); code != http.StatusNoContent {
		t.Fatal(code)
	}
	lost := cyTab.expect(t, realtimedomain.EventConversationUpdated, func(d map[string]any) bool { return d["conversationId"] == id })
	if lost.Data["accessLost"] != true || lost.Data["canMessage"] != false {
		t.Errorf("access lost = %v", lost.Data)
	}
	cyTab.expect(t, realtimedomain.EventUnreadChanged)
	for _, tab := range []*wsClient{adaTab, bobTab, deeTab} {
		ev := tab.expect(t, realtimedomain.EventConversationMembers, func(d map[string]any) bool { return d["change"] == "removed" })
		if users := ev.Data["userIds"].([]any); len(users) != 1 || users[0] != cy {
			t.Errorf("removed = %v", ev.Data)
		}
	}
	cyTab.silent(t, realtimedomain.EventConversationMembers, 100*time.Millisecond)

	// The owner leaves: left, then owner_changed with no actor, to everyone still in.
	e.call(t, ada, http.MethodPost, groupPath(id)+"/leave", "")
	adaTab.expect(t, realtimedomain.EventConversationUpdated, func(d map[string]any) bool { return d["accessLost"] == true })
	for _, tab := range []*wsClient{bobTab, deeTab} {
		tab.expect(t, realtimedomain.EventConversationMembers, func(d map[string]any) bool { return d["change"] == "left" && d["actorId"] == ada })
		ev := tab.expect(t, realtimedomain.EventConversationMembers, func(d map[string]any) bool { return d["change"] == "owner_changed" })
		if ev.Data["actorId"] != nil || ev.Data["userIds"].([]any)[0] != min(bob, dee) {
			t.Errorf("owner_changed = %v", ev.Data)
		}
	}
	// Group members do not see each other's presence.
	if online := e.presence(t, bob, dee); online[dee] {
		t.Error("bob sees dee's presence through the group")
	}
}

// Group creation counts toward 10 a day (FR-16.10).
func TestGroupCreationLimit(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	for i := range 10 {
		if code, body := e.createGroup(t, ada, fmt.Sprintf("Lab %d", i), bob); code != http.StatusCreated {
			t.Fatalf("group %d: %d %v", i, code, body)
		}
	}
	code, body := e.createGroup(t, ada, "Lab 11", bob)
	wantError(t, "11th group", code, body, http.StatusTooManyRequests, "rate_limited")
}
