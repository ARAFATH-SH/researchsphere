package usecase

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

// Group members need UUIDs: memberIDs checks them, as the contract does.
func uid(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", n) }

var (
	owner = uid(1)
	ben   = uid(2)
	cat   = uid(3)
	dan   = uid(4)
	eve2  = uid(5)
)

// groupHarness is a harness whose owner is connected to ben, cat, dan and eve2.
func groupHarness() *harness {
	h := newHarness()
	for _, u := range []string{ben, cat, dan, eve2} {
		h.connect(owner, u)
	}
	return h
}

// group creates a group of the owner with the members and returns its ID.
func (h *harness) group(t *testing.T, members ...string) string {
	t.Helper()
	it, replayed, err := h.svc.CreateGroup(t.Context(), owner, domain.GroupInput{Name: "Lab", MemberIDs: members, ClientID: h.clientID()})
	if err != nil || replayed {
		t.Fatalf("CreateGroup: %v %v", replayed, err)
	}
	return it.ID
}

func (h *harness) say(t *testing.T, from, id, body string) domain.Message {
	t.Helper()
	s, err := h.svc.Send(t.Context(), from, id, body, h.clientID())
	if err != nil {
		t.Fatalf("Send(%s): %v", from, err)
	}
	return s.Message
}

func events(ms []domain.Message) []domain.EventType {
	var out []domain.EventType
	for _, m := range slices.Backward(ms) {
		if m.Event != nil {
			out = append(out, m.Event.Type)
		}
	}
	return out
}

func roleOf(t *testing.T, h *harness, id, user string) domain.Role {
	t.Helper()
	it, err := h.svc.Get(t.Context(), user, id)
	if err != nil {
		t.Fatalf("Get(%s): %v", user, err)
	}
	return it.Group.MyRole
}

// FR-16.6: the creator owns the group; the others are members who joined at its first
// message; each is notified; everyone hears of it; the limits count one group and its members.
func TestCreateGroup(t *testing.T) {
	h := groupHarness()
	it, replayed, err := h.svc.CreateGroup(t.Context(), owner, domain.GroupInput{Name: "  Lab  ", MemberIDs: []string{cat, ben, cat}, ClientID: "c1"})
	if err != nil || replayed {
		t.Fatal(replayed, err)
	}
	if it.Kind != domain.KindGroup || it.Group == nil || it.Group.Name != "Lab" || it.Group.MyRole != domain.RoleOwner || it.Group.MemberCount != 3 || !it.CanMessage() {
		t.Fatalf("item = %+v %+v", it, it.Group)
	}
	if len(it.Group.Members) != 3 || it.Group.Members[0].UserID != owner || it.Group.Members[0].Role != domain.RoleOwner {
		t.Errorf("members = %+v", it.Group.Members)
	}
	page := h.mustMessages(t, ben, it.ID)
	if len(page.Items) != 1 || page.Items[0].Event.Type != domain.EventGroupCreated || page.Items[0].SenderID != owner ||
		!slices.Equal(page.Items[0].Event.UserIDs, []string{ben, cat}) || *page.Items[0].Event.NewName != "Lab" {
		t.Errorf("first message = %+v", page.Items)
	}
	if want := fmt.Sprintf("added %s by %s: [%s %s]", it.ID, owner, ben, cat); !slices.Equal(h.notifier.events, []string{want}) {
		t.Errorf("notifier = %v", h.notifier.events)
	}
	if !slices.Equal(h.blocks.pairs, []string{owner + "|" + ben, owner + "|" + cat}) {
		t.Errorf("pair locks = %v, want both members in sorted order", h.blocks.pairs)
	}
	if h.limiter.used["groups:day|"+owner] != 1 || h.limiter.used["group-members:day|"+owner] != 2 {
		t.Errorf("limits = %v", h.limiter.used)
	}
	everyone := fmt.Sprint([]string{owner, ben, cat})
	if len(h.pub.events) != 2 || !strings.HasSuffix(h.pub.events[0], everyone) ||
		!strings.Contains(h.pub.events[1], "members "+it.ID+" added") || !strings.HasSuffix(h.pub.events[1], everyone) {
		t.Errorf("events = %v", h.pub.events)
	}
}

func TestCreateGroupValidation(t *testing.T) {
	long, max := strings.Repeat("é", MaxGroupNameLength+1), strings.Repeat("é", MaxGroupNameLength)
	forty9 := make([]string, 49)
	fifty := make([]string, 50)
	for i := range fifty {
		fifty[i] = uid(100 + i)
	}
	copy(forty9, fifty)
	for _, tc := range []struct {
		name    string
		group   string
		members []string
		want    error
	}{
		{"empty name", "   ", []string{ben}, domain.ErrInvalidGroupName},
		{"name too long", long, []string{ben}, domain.ErrInvalidGroupName},
		{"two lines", "Lab\nTwo", []string{ben}, domain.ErrInvalidGroupName},
		{"a tab", "Lab\tTwo", []string{ben}, domain.ErrInvalidGroupName},
		{"a line separator", "Lab Two", []string{ben}, domain.ErrInvalidGroupName},
		{"not text", "Lab\xff", []string{ben}, domain.ErrInvalidGroupName},
		{"no members", "Lab", nil, &domain.InvalidFieldError{Field: "memberIds"}},
		{"oneself", "Lab", []string{ben, owner}, &domain.InvalidFieldError{Field: "memberIds"}},
		{"not a UUID", "Lab", []string{"bob"}, &domain.InvalidFieldError{Field: "memberIds"}},
		{"50 others", "Lab", fifty, &domain.InvalidFieldError{Field: "memberIds"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := groupHarness()
			_, _, err := h.svc.CreateGroup(t.Context(), owner, domain.GroupInput{Name: tc.group, MemberIDs: tc.members, ClientID: "c1"})
			var field *domain.InvalidFieldError
			if errors.As(tc.want, &field) {
				var got *domain.InvalidFieldError
				if !errors.As(err, &got) || got.Field != field.Field {
					t.Fatalf("err = %v, want field %s", err, field.Field)
				}
			} else if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(h.store.conversations) != 0 || len(h.limiter.used) != 0 {
				t.Error("something was stored or counted")
			}
		})
	}
	t.Run("100 characters and 49 others are allowed", func(t *testing.T) {
		h := groupHarness()
		h.limiter.used["group-members:day|"+owner] = 0
		for _, u := range forty9 {
			h.connect(owner, u)
		}
		it, _, err := h.svc.CreateGroup(t.Context(), owner, domain.GroupInput{Name: max, MemberIDs: forty9, ClientID: "c1"})
		if err != nil || it.Group.MemberCount != domain.MaxGroupMembers {
			t.Fatalf("%v %+v", err, it.Group)
		}
	})
}

// Only the creator's connections who are verified, not suspended and in no block either way
// with the creator; every failing ID is listed, and nothing says why.
func TestCreateGroupRefusesIneligibleMembers(t *testing.T) {
	h := groupHarness()
	stranger, hidden, blockedByOwner, blockedOwner := uid(20), dan, cat, eve2
	h.store.hidden[hidden] = true
	h.blocks.set[owner+"|"+blockedByOwner] = true
	h.blocks.set[blockedOwner+"|"+owner] = true
	_, _, err := h.svc.CreateGroup(t.Context(), owner, domain.GroupInput{Name: "Lab", MemberIDs: []string{ben, stranger, hidden, blockedByOwner, blockedOwner}, ClientID: "c1"})
	var bad *domain.IneligibleError
	if !errors.As(err, &bad) {
		t.Fatalf("err = %v", err)
	}
	want := []string{cat, dan, eve2, stranger}
	if !slices.Equal(bad.UserIDs, want) {
		t.Errorf("ineligible = %v, want %v", bad.UserIDs, want)
	}
	if len(h.store.conversations) != 0 || len(h.limiter.used) != 0 || len(h.notifier.events) != 0 {
		t.Error("something was stored, counted or notified")
	}
}

// A block that commits while the create waits for the pair locks is not passed.
func TestCreateGroupBlockedMeanwhile(t *testing.T) {
	h := groupHarness()
	h.blocks.onLock = func() { h.blocks.set[cat+"|"+owner] = true }
	_, _, err := h.svc.CreateGroup(t.Context(), owner, domain.GroupInput{Name: "Lab", MemberIDs: []string{ben, cat}, ClientID: "c1"})
	var bad *domain.IneligibleError
	if !errors.As(err, &bad) || !slices.Equal(bad.UserIDs, []string{cat}) {
		t.Fatalf("err = %v", err)
	}
	if len(h.store.conversations) != 0 {
		t.Error("the group was stored")
	}
}

// A used client ID returns the first group with replayed set, using no quota; once the creator
// has left it, the answer is not found.
func TestCreateGroupReplay(t *testing.T) {
	h := groupHarness()
	in := domain.GroupInput{Name: "Lab", MemberIDs: []string{ben}, ClientID: "same"}
	first, _, err := h.svc.CreateGroup(t.Context(), owner, in)
	if err != nil {
		t.Fatal(err)
	}
	again, replayed, err := h.svc.CreateGroup(t.Context(), owner, in)
	if err != nil || !replayed || again.ID != first.ID || len(h.store.conversations) != 1 || h.limiter.used["groups:day|"+owner] != 1 {
		t.Fatalf("replay: %v %v %s", replayed, err, again.ID)
	}
	if err := h.svc.Leave(t.Context(), owner, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.svc.CreateGroup(t.Context(), owner, in); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("after leaving: %v", err)
	}
}

// A suspended creator is refused; a messaging restriction does not stop creating (FR-15.6).
func TestCreateGroupStanding(t *testing.T) {
	h := groupHarness()
	h.actors[owner] = domain.Standing{Suspended: true}
	var suspended *domain.SuspendedError
	if _, _, err := h.svc.CreateGroup(t.Context(), owner, domain.GroupInput{Name: "Lab", MemberIDs: []string{ben}, ClientID: "c1"}); !errors.As(err, &suspended) {
		t.Errorf("suspended: %v", err)
	}
	until := h.now.Add(time.Hour)
	h.actors[owner] = domain.Standing{RestrictedUntil: &until}
	if _, _, err := h.svc.CreateGroup(t.Context(), owner, domain.GroupInput{Name: "Lab", MemberIDs: []string{ben}, ClientID: "c2"}); err != nil {
		t.Errorf("restricted: %v", err)
	}
}

// FR-16.10: 10 groups a day, and 100 added members a day over creating and adding.
func TestGroupLimits(t *testing.T) {
	var limited *domain.RateLimitedError
	t.Run("groups", func(t *testing.T) {
		h := groupHarness()
		h.limiter.used["groups:day|"+owner] = GroupsPerDay
		_, _, err := h.svc.CreateGroup(t.Context(), owner, domain.GroupInput{Name: "Lab", MemberIDs: []string{ben}, ClientID: "c1"})
		if !errors.As(err, &limited) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("members at creation", func(t *testing.T) {
		h := groupHarness()
		h.limiter.used["group-members:day|"+owner] = GroupMembersPerDay - 1
		_, _, err := h.svc.CreateGroup(t.Context(), owner, domain.GroupInput{Name: "Lab", MemberIDs: []string{ben, cat}, ClientID: "c1"})
		if !errors.As(err, &limited) {
			t.Errorf("err = %v", err)
		}
		if _, _, err := h.svc.CreateGroup(t.Context(), owner, domain.GroupInput{Name: "Lab", MemberIDs: []string{ben}, ClientID: "c2"}); err != nil {
			t.Errorf("the 100th: %v", err)
		}
	})
	t.Run("members added later", func(t *testing.T) {
		h := groupHarness()
		id := h.group(t, ben)
		h.limiter.used["group-members:day|"+owner] = GroupMembersPerDay
		if _, err := h.svc.AddMembers(t.Context(), owner, id, []string{cat}); !errors.As(err, &limited) {
			t.Errorf("err = %v", err)
		}
	})
}

// Only the owner renames; the same name changes nothing; a rename writes old and new.
func TestRenameGroup(t *testing.T) {
	h := groupHarness()
	id := h.group(t, ben)
	h.pub.events = nil
	if _, err := h.svc.RenameGroup(t.Context(), ben, id, "Mine"); !errors.Is(err, domain.ErrNotOwner) {
		t.Errorf("member: %v", err)
	}
	if _, err := h.svc.RenameGroup(t.Context(), cat, id, "Mine"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("non-member: %v", err)
	}
	if _, err := h.svc.RenameGroup(t.Context(), owner, id, "\n"); !errors.Is(err, domain.ErrInvalidGroupName) {
		t.Errorf("bad name: %v", err)
	}
	if _, err := h.svc.RenameGroup(t.Context(), owner, id, " Lab "); err != nil || len(h.pub.events) != 0 {
		t.Errorf("same name: %v %v", err, h.pub.events)
	}
	it, err := h.svc.RenameGroup(t.Context(), owner, id, "Lab 2")
	if err != nil || it.Group.Name != "Lab 2" {
		t.Fatalf("%v %+v", err, it.Group)
	}
	last := h.mustMessages(t, ben, id).Items[0]
	if last.Event.Type != domain.EventGroupRenamed || *last.Event.OldName != "Lab" || *last.Event.NewName != "Lab 2" || last.SenderID != owner {
		t.Errorf("event = %+v", last.Event)
	}
	if len(h.pub.events) != 2 || !strings.Contains(h.pub.events[1], `renamed by "`+owner+`" [] "Lab 2"`) {
		t.Errorf("events = %v", h.pub.events)
	}
	h.actors[owner] = domain.Standing{Suspended: true}
	var suspended *domain.SuspendedError
	if _, err := h.svc.RenameGroup(t.Context(), owner, id, "Lab 3"); !errors.As(err, &suspended) {
		t.Errorf("suspended: %v", err)
	}
}

// Rename, add, remove and leave are for groups only: a direct conversation answers not found.
func TestGroupOperationsOnADirectConversation(t *testing.T) {
	h := groupHarness()
	id := h.direct(t, owner, ben, "hi").Message.ConversationID
	ctx := t.Context()
	if _, err := h.svc.RenameGroup(ctx, owner, id, "x"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("rename: %v", err)
	}
	if _, err := h.svc.AddMembers(ctx, owner, id, []string{cat}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("add: %v", err)
	}
	if err := h.svc.RemoveMember(ctx, owner, id, ben); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("remove: %v", err)
	}
	if err := h.svc.Leave(ctx, owner, id); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("leave: %v", err)
	}
}

// Adding later follows the rules of creating, against the current owner; people already in the
// group are left alone; the total stays at most 50.
func TestAddMembers(t *testing.T) {
	h := groupHarness()
	id := h.group(t, ben)
	h.notifier.events, h.pub.events = nil, nil

	if _, err := h.svc.AddMembers(t.Context(), ben, id, []string{cat}); !errors.Is(err, domain.ErrNotOwner) {
		t.Errorf("member: %v", err)
	}
	var bad *domain.IneligibleError
	if _, err := h.svc.AddMembers(t.Context(), owner, id, []string{cat, uid(30)}); !errors.As(err, &bad) || !slices.Equal(bad.UserIDs, []string{uid(30)}) {
		t.Errorf("stranger: %v", err)
	}
	it, err := h.svc.AddMembers(t.Context(), owner, id, []string{ben, cat})
	if err != nil || it.Group.MemberCount != 3 {
		t.Fatalf("%v %+v", err, it.Group)
	}
	last := h.mustMessages(t, cat, id).Items[0]
	if last.Event.Type != domain.EventMemberAdded || !slices.Equal(last.Event.UserIDs, []string{cat}) {
		t.Errorf("event = %+v", last.Event)
	}
	if !slices.Equal(h.notifier.events, []string{fmt.Sprintf("added %s by %s: [%s]", id, owner, cat)}) {
		t.Errorf("notifier = %v", h.notifier.events)
	}
	if h.limiter.used["group-members:day|"+owner] != 2 {
		t.Errorf("members counted = %d, want 1 at creation and 1 now", h.limiter.used["group-members:day|"+owner])
	}
	// All already members: nothing changes and no quota is used.
	h.pub.events = nil
	if _, err := h.svc.AddMembers(t.Context(), owner, id, []string{ben, cat}); err != nil || len(h.pub.events) != 0 || h.limiter.used["group-members:day|"+owner] != 2 {
		t.Errorf("no change: %v %v", err, h.pub.events)
	}
	if _, err := h.svc.AddMembers(t.Context(), owner, id, []string{owner}); err == nil {
		t.Error("adding oneself was accepted")
	}
}

func TestAddMembersGroupFull(t *testing.T) {
	h := groupHarness()
	h.noLimits()
	var first []string
	for i := range 48 {
		u := uid(200 + i)
		h.connect(owner, u)
		first = append(first, u)
	}
	id := h.group(t, first...) // 49 members
	h.limiter.used = map[string]int{}
	if _, err := h.svc.AddMembers(t.Context(), owner, id, []string{ben, cat}); !errors.Is(err, domain.ErrGroupFull) {
		t.Fatalf("51st: %v", err)
	}
	if it, err := h.svc.AddMembers(t.Context(), owner, id, []string{ben}); err != nil || it.Group.MemberCount != 50 {
		t.Fatalf("50th: %v", err)
	}
	if _, err := h.svc.AddMembers(t.Context(), owner, id, []string{cat}); !errors.Is(err, domain.ErrGroupFull) {
		t.Errorf("full: %v", err)
	}
}

// A new member sees only messages from the moment they joined: the list, the read marker, the
// report and the unread count all stop there.
func TestNewMemberSeesOnlyLaterMessages(t *testing.T) {
	h := groupHarness()
	id := h.group(t, ben)
	before := h.say(t, ben, id, "before cat")
	if _, err := h.svc.AddMembers(t.Context(), owner, id, []string{cat}); err != nil {
		t.Fatal(err)
	}
	after := h.say(t, ben, id, "after cat")

	page := h.mustMessages(t, cat, id)
	if len(page.Items) != 2 || page.Items[0].ID != after.ID || page.Items[1].Event.Type != domain.EventMemberAdded {
		t.Errorf("cat sees %+v", page.Items)
	}
	if err := h.svc.MarkRead(t.Context(), cat, id, before.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("mark an earlier message read: %v", err)
	}
	if _, err := h.svc.Report(t.Context(), cat, id, domain.ReportInput{Reason: domain.ReasonSpam, MessageID: &before.ID}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("report an earlier message: %v", err)
	}
	if n, _ := h.svc.UnreadCounts(t.Context(), cat); n.Total != 1 {
		t.Errorf("cat's unread = %d, want 1", n.Total)
	}
	if n, _ := h.svc.UnreadCounts(t.Context(), owner); n.Total != 2 {
		t.Errorf("owner's unread = %d, want 2", n.Total)
	}
}

// Messages in a group go to every other current member; blocks between members stop nobody;
// standing, limits and the client ID work as in a direct conversation.
func TestSendInGroup(t *testing.T) {
	h := groupHarness()
	id := h.group(t, ben, cat)
	h.blocks.set[ben+"|"+cat] = true
	h.pub.events = nil
	m := h.say(t, cat, id, "hello")
	if want := fmt.Sprintf("sent %s to %v", m.ID, []string{owner, ben}); !slices.Equal(h.pub.events, []string{want}) {
		t.Errorf("events = %v", h.pub.events)
	}
	if again, err := h.svc.Send(t.Context(), cat, id, "hello", m.ClientID); err != nil || !again.Replayed || again.Message.ID != m.ID {
		t.Errorf("replay: %v %+v", err, again)
	}
	until := h.now.Add(time.Hour)
	h.actors[cat] = domain.Standing{RestrictedUntil: &until}
	var restricted *domain.RestrictedError
	if _, err := h.svc.Send(t.Context(), cat, id, "again", h.clientID()); !errors.As(err, &restricted) {
		t.Errorf("restricted: %v", err)
	}
	h.limiter.used["messages:minute|"+ben] = testLimits.MessagesPerMinute
	var limited *domain.RateLimitedError
	if _, err := h.svc.Send(t.Context(), ben, id, "again", h.clientID()); !errors.As(err, &limited) {
		t.Errorf("limited: %v", err)
	}
	if _, err := h.svc.Send(t.Context(), dan, id, "hi", h.clientID()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("non-member: %v", err)
	}
}

// A removal that commits while a send waits for the group's lock wins (NFR-13).
func TestSendInGroupRemovedMeanwhile(t *testing.T) {
	h := groupHarness()
	id := h.group(t, ben)
	h.store.onLockConv = func() { _, _ = h.store.RemoveGroupMember(t.Context(), id, ben) }
	if _, err := h.svc.Send(t.Context(), ben, id, "hi", h.clientID()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if slices.ContainsFunc(h.store.messages, func(m domain.Message) bool { return m.Body != nil }) {
		t.Error("the message was stored")
	}
}

// System messages never count as unread and are no one's to edit, delete or report.
func TestSystemMessages(t *testing.T) {
	h := groupHarness()
	id := h.group(t, ben)
	if _, err := h.svc.RenameGroup(t.Context(), owner, id, "Lab 2"); err != nil {
		t.Fatal(err)
	}
	event := h.mustMessages(t, ben, id).Items[0]
	if !event.System() {
		t.Fatalf("latest = %+v", event)
	}
	if n, _ := h.svc.UnreadCounts(t.Context(), ben); n.Total != 0 {
		t.Errorf("unread = %d", n.Total)
	}
	if _, err := h.svc.Edit(t.Context(), owner, id, event.ID, "x"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("edit: %v", err)
	}
	if err := h.svc.Delete(t.Context(), owner, id, event.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("delete: %v", err)
	}
	if _, err := h.svc.Report(t.Context(), ben, id, domain.ReportInput{Reason: domain.ReasonSpam, MessageID: &event.ID}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("report: %v", err)
	}
	if it, _ := h.svc.Get(t.Context(), ben, id); it.UnreadCount != 0 || it.LastMessage.Kind != domain.KindSystem || *it.LastMessage.Event != domain.EventGroupRenamed {
		t.Errorf("item = %+v", it.LastMessage)
	}
}

// Edits and deletions in a group reach every other member; a former member can do neither.
func TestEditAndDeleteInGroup(t *testing.T) {
	h := groupHarness()
	id := h.group(t, ben, cat)
	m := h.say(t, ben, id, "typo")
	h.pub.events = nil
	if _, err := h.svc.Edit(t.Context(), ben, id, m.ID, "fixed"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Delete(t.Context(), ben, id, m.ID); err != nil {
		t.Fatal(err)
	}
	others := fmt.Sprint([]string{owner, cat})
	if len(h.pub.events) != 2 || !strings.HasSuffix(h.pub.events[0], others) || !strings.HasSuffix(h.pub.events[1], others) {
		t.Errorf("events = %v", h.pub.events)
	}
	other := h.say(t, cat, id, "mine")
	if err := h.svc.Leave(t.Context(), cat, id); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Delete(t.Context(), cat, id, other.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("former member deletes: %v", err)
	}
	if _, err := h.svc.Edit(t.Context(), cat, id, other.ID, "x"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("former member edits: %v", err)
	}
}

// FR-16.3: each member has a read marker; the messages page lists the other members' markers;
// moving one tells every other member.
func TestGroupReadMarkers(t *testing.T) {
	h := groupHarness()
	id := h.group(t, ben, cat)
	m := h.say(t, owner, id, "hello")
	h.pub.events = nil
	if err := h.svc.MarkRead(t.Context(), ben, id, m.ID); err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("read %s %s to %v", ben, m.ID, []string{owner, cat}); !slices.Equal(h.pub.events, []string{want}) {
		t.Errorf("events = %v", h.pub.events)
	}
	page := h.mustMessages(t, owner, id)
	if page.OtherReadID != nil || len(page.Readers) != 2 {
		t.Fatalf("page = %+v", page)
	}
	seen := 0
	for _, r := range page.Readers {
		if r.LastReadMessageID != nil && *r.LastReadMessageID >= m.ID {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("seen by %d, want 1: %+v", seen, page.Readers)
	}
	if d := h.direct(t, owner, dan, "direct"); h.mustMessages(t, owner, d.Message.ConversationID).Readers != nil {
		t.Error("a direct conversation lists readers")
	}
}

// Unread counts include groups, per conversation and in total.
func TestUnreadCountsWithGroups(t *testing.T) {
	h := groupHarness()
	id := h.group(t, ben)
	h.say(t, owner, id, "one")
	h.say(t, owner, id, "two")
	h.direct(t, owner, ben, "direct")
	if n, _ := h.svc.UnreadCounts(t.Context(), ben); n.Total != 3 {
		t.Errorf("total = %d, want 3", n.Total)
	}
	if it, _ := h.svc.Get(t.Context(), ben, id); it.UnreadCount != 2 {
		t.Errorf("group unread = %d, want 2", it.UnreadCount)
	}
	page, err := h.svc.List(t.Context(), ben, "", "", nil)
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("inbox: %v %d", err, len(page.Items))
	}
}

// Only the owner removes; not themselves; the removed member loses all access at once, their
// notification is taken back and their open thread hears it.
func TestRemoveMember(t *testing.T) {
	h := groupHarness()
	id := h.group(t, ben, cat)
	h.notifier.events, h.pub.events = nil, nil
	if err := h.svc.RemoveMember(t.Context(), ben, id, cat); !errors.Is(err, domain.ErrNotOwner) {
		t.Errorf("member removes: %v", err)
	}
	if err := h.svc.RemoveMember(t.Context(), owner, id, owner); !errors.Is(err, domain.ErrRemoveSelf) {
		t.Errorf("owner removes themselves: %v", err)
	}
	if err := h.svc.RemoveMember(t.Context(), owner, id, dan); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("non-member: %v", err)
	}
	if err := h.svc.RemoveMember(t.Context(), owner, id, cat); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Get(t.Context(), cat, id); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("removed member reads the group: %v", err)
	}
	if _, err := h.svc.Messages(t.Context(), cat, id, "", nil); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("removed member reads the messages: %v", err)
	}
	if page, _ := h.svc.List(t.Context(), cat, "", "", nil); len(page.Items) != 0 {
		t.Errorf("still listed for the removed member: %+v", page.Items)
	}
	last := h.mustMessages(t, ben, id).Items[0]
	if last.Event.Type != domain.EventMemberRemoved || last.SenderID != owner || !slices.Equal(last.Event.UserIDs, []string{cat}) {
		t.Errorf("event = %+v", last.Event)
	}
	if !slices.Equal(h.notifier.events, []string{"lost " + id + " " + cat}) {
		t.Errorf("notifier = %v", h.notifier.events)
	}
	remaining := fmt.Sprint([]string{owner, ben})
	if len(h.pub.events) != 3 || !strings.HasSuffix(h.pub.events[0], remaining) ||
		!strings.Contains(h.pub.events[1], "removed by") || !strings.HasSuffix(h.pub.events[1], remaining) ||
		h.pub.events[2] != fmt.Sprintf("lost %s %v", id, []string{cat}) {
		t.Errorf("events = %v", h.pub.events)
	}
}

// When the owner leaves, the member who joined earliest becomes owner, after the left message,
// with an owner_changed message of no actor; when the last member leaves, the group is closed.
func TestLeaveAndOwnerHandOver(t *testing.T) {
	h := groupHarness()
	id := h.group(t, cat)
	if _, err := h.svc.AddMembers(t.Context(), owner, id, []string{ben}); err != nil {
		t.Fatal(err)
	}
	h.pub.events = nil
	if err := h.svc.Leave(t.Context(), owner, id); err != nil {
		t.Fatal(err)
	}
	if role := roleOf(t, h, id, cat); role != domain.RoleOwner {
		t.Errorf("cat, who joined first, is %s", role)
	}
	if role := roleOf(t, h, id, ben); role != domain.RoleMember {
		t.Errorf("ben is %s", role)
	}
	ms := h.mustMessages(t, cat, id).Items
	if got := events(ms)[len(events(ms))-2:]; !slices.Equal(got, []domain.EventType{domain.EventMemberLeft, domain.EventOwnerChanged}) {
		t.Errorf("last events = %v", got)
	}
	if ms[0].SenderID != "" || !slices.Equal(ms[0].Event.UserIDs, []string{cat}) || ms[1].SenderID != owner {
		t.Errorf("hand-over = %+v, left = %+v", ms[0], ms[1])
	}
	if !strings.Contains(strings.Join(h.pub.events, "\n"), "owner_changed by \"\" ["+cat+"]") {
		t.Errorf("events = %v", h.pub.events)
	}
	if _, err := h.svc.Get(t.Context(), owner, id); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("former owner reads the group: %v", err)
	}
	// The new owner manages the group.
	if _, err := h.svc.RenameGroup(t.Context(), cat, id, "Cat's"); err != nil {
		t.Errorf("new owner renames: %v", err)
	}

	if err := h.svc.Leave(t.Context(), ben, id); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Leave(t.Context(), cat, id); err != nil {
		t.Fatal(err)
	}
	if !h.store.conversations[id].closed {
		t.Error("the last member left and the group is open")
	}
	if err := h.svc.Leave(t.Context(), cat, id); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("leave twice: %v", err)
	}
}

// Anyone may leave, also while suspended (they only take themselves out).
func TestLeaveWhileSuspended(t *testing.T) {
	h := groupHarness()
	id := h.group(t, ben)
	h.actors[ben] = domain.Standing{Suspended: true}
	if err := h.svc.Leave(t.Context(), ben, id); err != nil {
		t.Errorf("err = %v", err)
	}
}

// Typing in a group goes to every other member except those in a block with the typer.
func TestTypingPeersInGroup(t *testing.T) {
	h := groupHarness()
	id := h.group(t, ben, cat, dan)
	h.blocks.set[dan+"|"+ben] = true
	peers, err := h.svc.TypingPeers(t.Context(), ben, id)
	if err != nil || !slices.Equal(peers, []string{owner, cat}) {
		t.Errorf("peers = %v %v", peers, err)
	}
	if peers, _ := h.svc.TypingPeers(t.Context(), eve2, id); len(peers) != 0 {
		t.Errorf("non-member peers = %v", peers)
	}
}

// FR-16.9 in groups: any member reports the group (nobody named) or a message (its sender).
func TestReportInGroup(t *testing.T) {
	h := groupHarness()
	id := h.group(t, ben, cat)
	m := h.say(t, ben, id, "rude")

	whole, err := h.svc.Report(t.Context(), cat, id, domain.ReportInput{Reason: domain.ReasonOther})
	if err != nil || whole.ReportedID != nil || whole.BlockedByMe {
		t.Fatalf("whole group: %+v %v", whole, err)
	}
	one, err := h.svc.Report(t.Context(), cat, id, domain.ReportInput{Reason: domain.ReasonHarassment, MessageID: &m.ID})
	if err != nil || one.ReportedID == nil || *one.ReportedID != ben {
		t.Fatalf("message: %+v %v", one, err)
	}
	if _, err := h.svc.Report(t.Context(), dan, id, domain.ReportInput{Reason: domain.ReasonOther}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("non-member reports: %v", err)
	}

	d, err := h.svc.ReportDetail(t.Context(), "admin", whole.ID, "")
	if err != nil || d.Report.Reported != nil || d.Report.ConversationKind != domain.KindGroup || len(d.MembersAtReport) != 3 {
		t.Fatalf("detail: %+v %v", d, err)
	}
	if !slices.ContainsFunc(d.Messages, func(m domain.ReportedMessage) bool { return m.Kind == domain.KindSystem }) {
		t.Error("the detail leaves out system messages")
	}
}

// A moderation action may resolve a group report when it names someone who was a member at the
// report's moment; anyone else is refused, and a direct report keeps its one reported user.
func TestResolveGroupReportForAction(t *testing.T) {
	h := groupHarness()
	id := h.group(t, ben, cat)
	r, err := h.svc.Report(t.Context(), cat, id, domain.ReportInput{Reason: domain.ReasonOther})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.ResolveForAction(t.Context(), "admin", r.ID, dan, nil); !errors.Is(err, domain.ErrReportAboutOther) {
		t.Errorf("non-member: %v", err)
	}
	if err := h.svc.ResolveForAction(t.Context(), "admin", r.ID, ben, nil); err != nil {
		t.Errorf("member at the report: %v", err)
	}

	m := h.say(t, ben, id, "rude")
	r2, err := h.svc.Report(t.Context(), cat, id, domain.ReportInput{Reason: domain.ReasonSpam, MessageID: &m.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.ResolveForAction(t.Context(), "admin", r2.ID, owner, nil); err != nil {
		t.Errorf("another member of a group report about a message: %v", err)
	}

	d := h.direct(t, owner, dan, "hi")
	r3, err := h.svc.Report(t.Context(), dan, d.Message.ConversationID, domain.ReportInput{Reason: domain.ReasonSpam})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.ResolveForAction(t.Context(), "admin", r3.ID, ben, nil); !errors.Is(err, domain.ErrReportAboutOther) {
		t.Errorf("direct report, someone else: %v", err)
	}
}
