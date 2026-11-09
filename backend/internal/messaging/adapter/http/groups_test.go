package http

import (
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

const memberID = "7f3a1c2e-8888-4e8b-b7c1-2d3e4f5a6b7c"

var group = domain.Item{
	Conversation: domain.Conversation{ID: convID, Kind: domain.KindGroup, State: domain.StateActive, LastMessageAt: at,
		Name: "Lab", MyRole: domain.RoleOwner, JoinedAt: at},
	Group: &domain.GroupInfo{
		Name: "Lab", MemberCount: 3, MyRole: domain.RoleOwner,
		Avatars: []domain.Person{{UserID: otherID, FullName: "Bob", PhotoFileID: ptr(fileID)}},
		Members: []domain.GroupMember{
			{UserID: caller, FullName: "Ada", Role: domain.RoleOwner, JoinedAt: at, Available: true},
			{UserID: otherID, FullName: "Bob", Role: domain.RoleMember, JoinedAt: at, Available: false},
		},
	},
	LastMessage: &domain.Preview{Kind: domain.KindSystem, Event: eventPtr(domain.EventMemberAdded), CreatedAt: at},
}

func eventPtr(e domain.EventType) *domain.EventType { return &e }

const createBody = `{"name":"Lab","memberIds":["` + otherID + `","` + memberID + `"],"clientId":"` + clientID + `"}`

// FR-16.6: a group as the API shows it. otherMember is null, group carries the name, the
// caller's role, avatars and members, and a system preview has its event key, not English.
func TestGroupBody(t *testing.T) {
	rec := send(t, &fakeService{group: group}, http.MethodPost, "/api/v1/conversations/groups", createBody, caller)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	b := decode(t, rec)
	if b["kind"] != "group" || b["otherMember"] != nil || b["blockedByMe"] != false || b["canMessage"] != true {
		t.Errorf("body = %v", b)
	}
	g := b["group"].(map[string]any)
	if g["name"] != "Lab" || g["memberCount"] != 3.0 || g["myRole"] != "owner" {
		t.Errorf("group = %v", g)
	}
	avatars := g["avatars"].([]any)
	if len(avatars) != 1 || avatars[0].(map[string]any)["photoUrl"] != "/api/v1/media/"+fileID {
		t.Errorf("avatars = %v", avatars)
	}
	members := g["members"].([]any)
	if len(members) != 2 || members[0].(map[string]any)["role"] != "owner" || members[1].(map[string]any)["available"] != false {
		t.Errorf("members = %v", members)
	}
	last := b["lastMessage"].(map[string]any)
	if last["kind"] != "system" || last["event"] != "member_added" || last["body"] != nil || last["senderId"] != nil {
		t.Errorf("lastMessage = %v", last)
	}
}

// A direct conversation keeps its shape: kind direct, group null.
func TestDirectBodyHasNoGroup(t *testing.T) {
	rec := send(t, &fakeService{}, http.MethodGet, "/api/v1/conversations/"+convID, "", caller)
	b := decode(t, rec)
	if b["kind"] != "direct" || b["group"] != nil || b["otherMember"] == nil {
		t.Errorf("body = %v", b)
	}
	if last := b["lastMessage"].(map[string]any); last["kind"] != "text" || last["event"] != nil {
		t.Errorf("lastMessage = %v", last)
	}
}

func TestCreateGroupAnswers(t *testing.T) {
	path := "/api/v1/conversations/groups"
	for _, tc := range []struct {
		name     string
		body     string
		err      error
		replayed bool
		status   int
		code     string
		field    string
	}{
		{name: "created", body: createBody, status: http.StatusCreated},
		{name: "replayed client ID", body: createBody, replayed: true, status: http.StatusOK},
		{name: "zero client ID", body: `{"name":"Lab","memberIds":[],"clientId":"00000000-0000-0000-0000-000000000000"}`, status: http.StatusBadRequest, code: "bad_request", field: "clientId"},
		{name: "bad name", body: createBody, err: domain.ErrInvalidGroupName, status: http.StatusBadRequest, code: "invalid_group_name", field: "name"},
		{name: "bad members", body: createBody, err: &domain.InvalidFieldError{Field: "memberIds"}, status: http.StatusBadRequest, code: "bad_request", field: "memberIds"},
		{name: "suspended", body: createBody, err: &domain.SuspendedError{}, status: http.StatusForbidden, code: "account_suspended"},
		{name: "replay of a group the caller left", body: createBody, err: domain.ErrNotFound, status: http.StatusNotFound, code: "not_found"},
		{name: "limited", body: createBody, err: &domain.RateLimitedError{RetryAfter: 90 * time.Second}, status: http.StatusTooManyRequests, code: "rate_limited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeService{group: group, err: tc.err, replayed: tc.replayed}
			rec := send(t, svc, http.MethodPost, path, tc.body, caller)
			if rec.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.status, rec.Body)
			}
			if tc.code == "" {
				return
			}
			b := decode(t, rec)
			if b["code"] != tc.code || (tc.field != "" && b["field"] != tc.field) {
				t.Errorf("body = %v", b)
			}
			if tc.status == http.StatusTooManyRequests && rec.Header().Get("Retry-After") != "90" {
				t.Errorf("Retry-After = %q", rec.Header().Get("Retry-After"))
			}
		})
	}
	t.Run("passes the members and client ID", func(t *testing.T) {
		svc := &fakeService{group: group}
		send(t, svc, http.MethodPost, path, createBody, caller)
		want := domain.GroupInput{Name: "Lab", MemberIDs: []string{otherID, memberID}, ClientID: clientID}
		if svc.args[0] != caller || !reflect.DeepEqual(svc.args[1], want) {
			t.Errorf("args = %v", svc.args)
		}
	})
}

// The 400 for members who cannot be added lists them and says nothing more (FR-16.6).
func TestIneligibleMembersListsThem(t *testing.T) {
	svc := &fakeService{err: &domain.IneligibleError{UserIDs: []string{memberID, otherID}}}
	rec := send(t, svc, http.MethodPost, "/api/v1/conversations/groups", createBody, caller)
	b := decode(t, rec)
	ids, _ := b["userIds"].([]any)
	if rec.Code != http.StatusBadRequest || b["code"] != "ineligible_members" || len(ids) != 2 || ids[0] != memberID || ids[1] != otherID {
		t.Errorf("%d %v", rec.Code, b)
	}
	if _, ok := b["field"]; ok {
		t.Errorf("field = %v", b["field"])
	}
}

func TestGroupOwnerOperationsAnswers(t *testing.T) {
	base := "/api/v1/conversations/" + convID
	ops := []struct {
		name, method, path, body string
		ok                       int
	}{
		{"rename", http.MethodPatch, base, `{"name":"Lab 2"}`, http.StatusOK},
		{"add", http.MethodPost, base + "/members", `{"userIds":["` + memberID + `"]}`, http.StatusOK},
		{"remove", http.MethodDelete, base + "/members/" + memberID, "", http.StatusNoContent},
	}
	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			for _, tc := range []struct {
				err    error
				status int
				code   string
			}{
				{nil, op.ok, ""},
				{domain.ErrNotOwner, http.StatusForbidden, "not_group_owner"},
				{&domain.SuspendedError{}, http.StatusForbidden, "account_suspended"},
				{domain.ErrNotFound, http.StatusNotFound, "not_found"},
			} {
				rec := send(t, &fakeService{group: group, err: tc.err}, op.method, op.path, op.body, caller)
				if rec.Code != tc.status {
					t.Errorf("%v: status %d, want %d: %s", tc.err, rec.Code, tc.status, rec.Body)
					continue
				}
				if tc.code != "" && decode(t, rec)["code"] != tc.code {
					t.Errorf("%v: body %s", tc.err, rec.Body)
				}
			}
		})
	}
	t.Run("add: full and limited", func(t *testing.T) {
		rec := send(t, &fakeService{err: domain.ErrGroupFull}, http.MethodPost, base+"/members", `{"userIds":["`+memberID+`"]}`, caller)
		if rec.Code != http.StatusConflict || decode(t, rec)["code"] != "group_full" {
			t.Errorf("full: %d %s", rec.Code, rec.Body)
		}
		rec = send(t, &fakeService{err: &domain.RateLimitedError{RetryAfter: time.Hour}}, http.MethodPost, base+"/members", `{"userIds":["`+memberID+`"]}`, caller)
		if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "3600" {
			t.Errorf("limited: %d %v", rec.Code, rec.Header())
		}
		rec = send(t, &fakeService{err: &domain.IneligibleError{UserIDs: []string{memberID}}}, http.MethodPost, base+"/members", `{"userIds":["`+memberID+`"]}`, caller)
		if rec.Code != http.StatusBadRequest || decode(t, rec)["code"] != "ineligible_members" {
			t.Errorf("ineligible: %d %s", rec.Code, rec.Body)
		}
	})
	t.Run("remove: oneself", func(t *testing.T) {
		rec := send(t, &fakeService{err: domain.ErrRemoveSelf}, http.MethodDelete, base+"/members/"+caller, "", caller)
		if rec.Code != http.StatusBadRequest || decode(t, rec)["code"] != "cannot_remove_self" {
			t.Errorf("%d %s", rec.Code, rec.Body)
		}
	})
	t.Run("rename: bad name", func(t *testing.T) {
		rec := send(t, &fakeService{err: domain.ErrInvalidGroupName}, http.MethodPatch, base, `{"name":""}`, caller)
		if b := decode(t, rec); rec.Code != http.StatusBadRequest || b["code"] != "invalid_group_name" || b["field"] != "name" {
			t.Errorf("%d %v", rec.Code, b)
		}
	})
}

func TestLeaveGroupAnswers(t *testing.T) {
	path := "/api/v1/conversations/" + convID + "/leave"
	svc := &fakeService{}
	if rec := send(t, svc, http.MethodPost, path, "", caller); rec.Code != http.StatusNoContent {
		t.Errorf("leave: %d", rec.Code)
	}
	if svc.args[0] != caller || svc.args[1] != convID {
		t.Errorf("args = %v", svc.args)
	}
	if rec := send(t, &fakeService{err: domain.ErrNotFound}, http.MethodPost, path, "", caller); rec.Code != http.StatusNotFound {
		t.Errorf("not a member: %d", rec.Code)
	}
	if rec := send(t, &fakeService{}, http.MethodPost, path, "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no session: %d", rec.Code)
	}
}

// A system message has no body and no client ID; owner_changed has no sender. Readers carry
// every other member's marker in a group (FR-16.3).
func TestGroupMessagesAndReaders(t *testing.T) {
	system := domain.Message{ID: msgID, ConversationID: convID, Kind: domain.KindSystem, CreatedAt: at,
		Event: &domain.SystemEvent{Type: domain.EventOwnerChanged, UserIDs: []string{otherID}}}
	page := domain.MessagePage{Items: []domain.Message{system}, Readers: []domain.Reader{{UserID: otherID, LastReadMessageID: ptr(msgID)}, {UserID: memberID}}}
	rec := send(t, &fakeService{page: &page}, http.MethodGet, "/api/v1/conversations/"+convID+"/messages", "", caller)
	b := decode(t, rec)
	m := b["items"].([]any)[0].(map[string]any)
	ev := m["event"].(map[string]any)
	if m["kind"] != "system" || m["senderId"] != nil || m["body"] != nil || ev["type"] != "owner_changed" || ev["actorId"] != nil {
		t.Errorf("message = %v", m)
	}
	if users := ev["userIds"].([]any); len(users) != 1 || users[0] != otherID {
		t.Errorf("userIds = %v", users)
	}
	readers := b["readers"].([]any)
	if len(readers) != 2 || readers[0].(map[string]any)["lastReadMessageId"] != msgID || readers[1].(map[string]any)["lastReadMessageId"] != nil {
		t.Errorf("readers = %v", readers)
	}
	if b["otherLastReadMessageId"] != nil {
		t.Errorf("otherLastReadMessageId = %v", b["otherLastReadMessageId"])
	}

	// A direct conversation has no readers list.
	rec = send(t, &fakeService{}, http.MethodGet, "/api/v1/conversations/"+convID+"/messages", "", caller)
	if b := decode(t, rec); b["readers"] != nil {
		t.Errorf("direct readers = %v", b["readers"])
	}
}

// A report of a whole group names nobody (FR-16.9).
func TestGroupReportReceiptNamesNobody(t *testing.T) {
	svc := &fakeService{receipt: &domain.ReportReceipt{ID: reportID, CreatedAt: at}}
	rec := send(t, svc, http.MethodPost, "/api/v1/conversations/"+convID+"/reports", `{"reason":"spam"}`, caller)
	b := decode(t, rec)
	if rec.Code != http.StatusCreated || b["reportedUserId"] != nil || b["blockedByMe"] != false {
		t.Errorf("%d %v", rec.Code, b)
	}
}

// The admin detail of a group report: no reported user, the group's name, everyone who was a
// member at the report, and system messages with their events (NFR-13).
func TestGroupReportDetail(t *testing.T) {
	r := report
	r.Reported, r.ConversationKind, r.GroupName = nil, domain.KindGroup, ptr("Lab")
	actor := caller
	d := domain.ReportDetail{
		Report:          r,
		MembersAtReport: []domain.ReportPerson{{UserID: caller, FullName: "Ada", Role: "researcher"}, {UserID: otherID, FullName: "Bob", Role: "researcher"}},
		Messages: []domain.ReportedMessage{{ID: msgID, ConversationID: convID, SenderID: actor, Kind: domain.KindSystem, CreatedAt: at,
			Event: &domain.SystemEvent{Type: domain.EventGroupRenamed, UserIDs: []string{}, OldName: ptr("Lab"), NewName: ptr("Lab 2")}}},
	}
	rec := send(t, &fakeService{detail: &d}, http.MethodGet, "/api/v1/admin/message-reports/"+reportID, "", caller)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	b := decode(t, rec)
	rep := b["report"].(map[string]any)
	if rep["reportedUser"] != nil || rep["conversationKind"] != "group" || rep["groupName"] != "Lab" {
		t.Errorf("report = %v", rep)
	}
	if members := b["membersAtReport"].([]any); len(members) != 2 || members[1].(map[string]any)["userId"] != otherID {
		t.Errorf("membersAtReport = %v", members)
	}
	m := b["messages"].([]any)[0].(map[string]any)
	ev := m["event"].(map[string]any)
	if m["kind"] != "system" || ev["type"] != "group_renamed" || ev["oldName"] != "Lab" || ev["newName"] != "Lab 2" || ev["actorId"] != caller {
		t.Errorf("message = %v", m)
	}

	// A direct report keeps membersAtReport null and its reported user.
	rec = send(t, &fakeService{}, http.MethodGet, "/api/v1/admin/message-reports/"+reportID, "", caller)
	b = decode(t, rec)
	if b["membersAtReport"] != nil || b["report"].(map[string]any)["reportedUser"] == nil || b["report"].(map[string]any)["conversationKind"] != "direct" {
		t.Errorf("direct detail = %v", b)
	}
}
