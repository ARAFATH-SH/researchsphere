//go:build integration

// End-to-end tests of what the group and search screens read (slice 14B, chunk 14B.5b): a page
// of messages centred on a search result, the people a page names, and the group a group_added
// notification names.
package main

import (
	"net/http"
	"testing"

	realtimedomain "github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/domain"
)

func (e mediaEnv) page(t *testing.T, userID, conversationID, query string) (int, map[string]any) {
	t.Helper()
	return e.call(t, userID, http.MethodGet, "/api/v1/conversations/"+conversationID+"/messages"+query, "")
}

func itemIDs(page map[string]any) []string {
	var out []string
	for _, m := range page["items"].([]any) {
		out = append(out, m.(map[string]any)["id"].(string))
	}
	return out
}

// FR-16.8, NFR-13: a page centred on a message, with cursors both ways; a message the caller
// cannot see is 404; two cursors are 400 with the field.
func TestMessagesAroundEndToEnd(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	id := e.newGroup(t, ada, bob)
	var sent []string
	for range 9 {
		sent = append(sent, e.say(t, bob, id, "hello"))
	}
	code, page := e.page(t, ada, id, "?around="+sent[4]+"&limit=3")
	if code != http.StatusOK {
		t.Fatalf("around: %d %v", code, page)
	}
	if got := itemIDs(page); len(got) != 3 || got[0] != sent[5] || got[1] != sent[4] || got[2] != sent[3] {
		t.Errorf("around page = %v", got)
	}
	if page["nextBefore"] != sent[3] || page["nextAfter"] != sent[5] {
		t.Errorf("cursors = %v, %v", page["nextBefore"], page["nextAfter"])
	}
	code, newer := e.page(t, ada, id, "?after="+sent[5]+"&limit=2")
	if got := itemIDs(newer); code != http.StatusOK || len(got) != 2 || got[0] != sent[7] || got[1] != sent[6] || newer["nextAfter"] != sent[7] {
		t.Errorf("after page = %d %v, next %v", code, got, newer["nextAfter"])
	}

	// cy is not in the group: 404 for the conversation. A message of another conversation, for
	// a member: 404 for the message.
	if code, body := e.page(t, cy, id, "?around="+sent[4]); code != http.StatusNotFound || body["message"] != "This conversation does not exist." {
		t.Errorf("outsider: %d %v", code, body)
	}
	other := e.newGroup(t, ada, cy)
	foreign := e.say(t, cy, other, "elsewhere")
	if code, body := e.page(t, ada, id, "?around="+foreign); code != http.StatusNotFound || body["message"] != "This message does not exist." {
		t.Errorf("foreign message: %d %v", code, body)
	}
	if code, body := e.page(t, ada, id, "?before="+sent[1]+"&around="+sent[4]); code != http.StatusBadRequest || body["field"] != "around" {
		t.Errorf("two cursors: %d %v", code, body)
	}
}

// FR-16.6: a page names the senders and the users its system messages name, also a member who
// was removed, so "Ada removed Bob" keeps its name.
func TestMessagePeopleEndToEnd(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	id := e.newGroup(t, ada, bob, cy)
	e.say(t, bob, id, "hello")
	if code, body := e.call(t, ada, http.MethodDelete, groupPath(id)+"/members/"+bob, ""); code != http.StatusNoContent {
		t.Fatalf("remove: %d %v", code, body)
	}
	code, page := e.page(t, cy, id, "")
	if code != http.StatusOK {
		t.Fatalf("page: %d %v", code, page)
	}
	// The list's preview names the actor of the latest message, the removal.
	code, list := e.call(t, cy, http.MethodGet, "/api/v1/conversations", "")
	if code != http.StatusOK {
		t.Fatalf("list: %d %v", code, list)
	}
	row := list["items"].([]any)[0].(map[string]any)["lastMessage"].(map[string]any)
	if row["senderId"] != ada || row["senderName"] == nil || row["senderName"] == "" || row["event"] != "member_removed" {
		t.Errorf("preview = %v", row)
	}
	people := page["people"].(map[string]any)
	for _, u := range []string{ada, bob, cy} {
		p, ok := people[u].(map[string]any)
		if !ok || p["fullName"] == "" || p["available"] != true {
			t.Errorf("people[%s] = %v", u, people[u])
		}
	}
}

// FR-9.5: group_added names the group by its current name, in the list and in the real-time
// event.
func TestGroupAddedSubject(t *testing.T) {
	e := newMediaEnv(t)
	srv := serve(t, e.router)
	ada, bob, cy := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	id := e.newGroup(t, ada, bob)
	items, _ := e.notifications(t, bob, "")
	if s, ok := items[0]["subject"].(map[string]any); !ok || s["type"] != "conversation" || s["id"] != id || s["title"] != "Lab" {
		t.Fatalf("subject = %v", items[0])
	}
	if code, body := e.call(t, ada, http.MethodPatch, groupPath(id), `{"name":"Lab 2"}`); code != http.StatusOK {
		t.Fatalf("rename: %d %v", code, body)
	}
	items, _ = e.notifications(t, bob, "")
	if s := items[0]["subject"].(map[string]any); s["title"] != "Lab 2" {
		t.Errorf("after the rename = %v", s)
	}

	e.link(t, ada, cy)
	cyTab := dial(t, srv, cy)
	if code, body := e.addMembers(t, ada, id, cy); code != http.StatusOK {
		t.Fatalf("add: %d %v", code, body)
	}
	ev := cyTab.expect(t, realtimedomain.EventNotificationCreated)
	if s, ok := ev.Data["subject"].(map[string]any); !ok || s["title"] != "Lab 2" {
		t.Errorf("event = %v", ev.Data)
	}
}
