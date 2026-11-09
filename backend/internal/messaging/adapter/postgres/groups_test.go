//go:build integration

package postgres

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
)

// inTx runs fn in one transaction, as the usecase does for every group write.
func inTx(t *testing.T, pool *pgxpool.Pool, fn func(ctx context.Context) error) {
	t.Helper()
	if err := postgres.NewTransactor(pool).InTx(t.Context(), fn); err != nil {
		t.Fatal(err)
	}
}

func newGroup(t *testing.T, s *Store, pool *pgxpool.Pool, creator string, members ...string) domain.Message {
	t.Helper()
	var created domain.Message
	inTx(t, pool, func(ctx context.Context) error {
		var (
			ok  bool
			err error
		)
		created, ok, err = s.CreateGroup(ctx, domain.NewGroup{CreatorID: creator, Name: "Lab", ClientID: clientID(), MemberIDs: members})
		if err == nil && !ok {
			err = errors.New("not created")
		}
		return err
	})
	return created
}

// removeMember writes the exit's message, then removes the member, as the usecase does.
func removeMember(t *testing.T, s *Store, pool *pgxpool.Pool, id, actor, user string, ev domain.EventType) {
	t.Helper()
	inTx(t, pool, func(ctx context.Context) error {
		if _, err := s.LockConversation(ctx, id); err != nil {
			return err
		}
		if _, err := s.AddSystemMessage(ctx, id, actor, domain.SystemEvent{Type: ev, UserIDs: []string{user}}); err != nil {
			return err
		}
		removed, err := s.RemoveGroupMember(ctx, id, user)
		if err == nil && !removed {
			err = errors.New("not removed")
		}
		return err
	})
}

func role(t *testing.T, pool *pgxpool.Pool, id, user string) string {
	t.Helper()
	var r string
	if err := pool.QueryRow(t.Context(), "SELECT role FROM conversation_members WHERE conversation_id = $1 AND user_id = $2", id, user).Scan(&r); err != nil {
		return ""
	}
	return r
}

// FR-16.6: the creator owns the group, everyone joined at its group_created message, and each
// stay is recorded.
func TestCreateGroupStore(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob, cy := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Cy")
	created := newGroup(t, s, pool, ada, bob, cy)
	id := created.ConversationID

	if created.Kind != domain.KindSystem || created.Event.Type != domain.EventGroupCreated || created.SenderID != ada ||
		created.Body != nil || created.ClientID != "" || !slices.Equal(created.Event.UserIDs, []string{bob, cy}) || *created.Event.NewName != "Lab" {
		t.Errorf("created = %+v %+v", created, created.Event)
	}
	c, err := s.FindConversation(ctx, id, bob)
	if err != nil || c.Kind != domain.KindGroup || c.Name != "Lab" || c.MyRole != domain.RoleMember || !c.JoinedAt.Equal(created.CreatedAt) || c.OtherID != "" {
		t.Errorf("FindConversation = %+v, %v", c, err)
	}
	if role(t, pool, id, ada) != "owner" {
		t.Errorf("creator is %q", role(t, pool, id, ada))
	}
	var stays int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM conversation_memberships WHERE conversation_id = $1 AND joined_at = $2 AND left_at IS NULL",
		id, created.CreatedAt).Scan(&stays); err != nil || stays != 3 {
		t.Errorf("stays = %d, %v", stays, err)
	}
	members, err := s.ListGroupMembers(ctx, id)
	if err != nil || len(members) != 3 || members[0].UserID != ada || members[0].Role != domain.RoleOwner || members[1].FullName == "" {
		t.Errorf("ListGroupMembers = %+v, %v", members, err)
	}
	if found, ok, err := s.FindGroupByClient(ctx, bob, clientID()); err != nil || ok {
		t.Errorf("FindGroupByClient of another = %s %v %v", found, ok, err)
	}
}

// One group per creator and client ID: a second create stores nothing.
func TestCreateGroupClientIDOnce(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
	client := clientID()
	var first domain.Message
	for i := range 2 {
		inTx(t, pool, func(ctx context.Context) error {
			m, ok, err := s.CreateGroup(ctx, domain.NewGroup{CreatorID: ada, Name: "Lab", ClientID: client, MemberIDs: []string{bob}})
			if i == 0 {
				first = m
				if !ok {
					return errors.New("first not created")
				}
			} else if ok {
				return errors.New("second created")
			}
			return err
		})
	}
	id, found, err := s.FindGroupByClient(t.Context(), ada, client)
	if err != nil || !found || id != first.ConversationID {
		t.Errorf("FindGroupByClient = %s %v %v", id, found, err)
	}
}

// The list of a group: kind, name, size, up to 3 avatars other than the viewer, and a system
// preview with its event; system messages and messages before joining are never unread.
func TestGroupItems(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob, cy, dee, eli := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Cy"), newUser(t, pool, "Dee"), newUser(t, pool, "Eli")
	id := newGroup(t, s, pool, ada, bob, cy, dee).ConversationID
	inTx(t, pool, func(ctx context.Context) error {
		_, _, err := s.AddMessage(ctx, domain.Message{ConversationID: id, SenderID: ada, Body: ptr("before eli"), ClientID: clientID()})
		return err
	})
	inTx(t, pool, func(ctx context.Context) error {
		ev, err := s.AddSystemMessage(ctx, id, ada, domain.SystemEvent{Type: domain.EventMemberAdded, UserIDs: []string{eli}})
		if err != nil {
			return err
		}
		return s.AddGroupMembers(ctx, id, []string{eli}, ev.CreatedAt)
	})

	it, err := s.FindItem(ctx, id, eli)
	if err != nil {
		t.Fatal(err)
	}
	if it.Kind != domain.KindGroup || it.Group == nil || it.Group.Name != "Lab" || it.Group.MemberCount != 5 || it.Group.MyRole != domain.RoleMember {
		t.Fatalf("item = %+v %+v", it, it.Group)
	}
	// The first four joined at one instant, so the lower IDs come first; eli is the viewer.
	first := []string{ada, bob, cy, dee}
	slices.Sort(first)
	var got []string
	for _, p := range it.Group.Avatars {
		got = append(got, p.UserID)
	}
	if !slices.Equal(got, first[:3]) {
		t.Errorf("avatars = %v, want %v", got, first[:3])
	}
	if it.UnreadCount != 0 || it.LastMessage == nil || it.LastMessage.Kind != domain.KindSystem ||
		it.LastMessage.Event == nil || *it.LastMessage.Event != domain.EventMemberAdded || it.LastMessage.Body != nil {
		t.Errorf("eli: unread %d, preview %+v", it.UnreadCount, it.LastMessage)
	}
	if bobs, _ := s.FindItem(ctx, id, bob); bobs.UnreadCount != 1 {
		t.Errorf("bob's unread = %d, want 1 (not the system messages)", bobs.UnreadCount)
	}
	if n, _ := s.CountUnread(ctx, eli); n.Total != 0 {
		t.Errorf("eli's total = %d", n.Total)
	}
	if n, _ := s.CountUnread(ctx, bob); n.Total != 1 {
		t.Errorf("bob's total = %d", n.Total)
	}
	items, err := s.ListItems(ctx, eli, domain.BoxInbox, nil, 10)
	if err != nil || len(items) != 1 || items[0].Group == nil || len(items[0].Group.Avatars) != 3 {
		t.Errorf("ListItems = %+v, %v", items, err)
	}
	// Eli sees only from the member_added message on.
	c, _ := s.FindConversation(ctx, id, eli)
	list, err := s.ListMessages(ctx, id, c.JoinedAt, "", 10)
	if err != nil || len(list) != 1 || list[0].Event.Type != domain.EventMemberAdded {
		t.Errorf("eli's messages = %+v, %v", list, err)
	}
	all, _ := s.ListMessages(ctx, id, time.Time{}, "", 10)
	before := all[1]
	if in, _ := s.MessageIn(ctx, id, before.ID, c.JoinedAt); in {
		t.Error("eli may mark an earlier message read")
	}
	if _, found, _ := s.FindMessageSender(ctx, id, before.ID, c.JoinedAt); found {
		t.Error("eli may report an earlier message")
	}
	if _, found, _ := s.FindMessageSender(ctx, id, list[0].ID, time.Time{}); found {
		t.Error("a system message can be reported")
	}
	if _, err := s.FindOwnMessage(ctx, id, list[0].ID, ada); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a system message is the actor's own: %v", err)
	}
	readers, err := s.ListReaders(ctx, id, ada)
	others := []string{bob, cy, dee}
	slices.Sort(others)
	var order []string
	for _, r := range readers {
		order = append(order, r.UserID)
		if r.LastReadMessageID != nil {
			t.Errorf("%s has read %s", r.UserID, *r.LastReadMessageID)
		}
	}
	if err != nil || !slices.Equal(order, append(others, eli)) {
		t.Errorf("ListReaders = %v, %v", order, err)
	}
	// Presence stays with direct conversations.
	if peers, _ := s.ActivePeers(ctx, ada); len(peers) != 0 {
		t.Errorf("ActivePeers = %v", peers)
	}
	if partners, _ := s.Partners(ctx, ada); len(partners) != 0 {
		t.Errorf("Partners = %v", partners)
	}
}

// The owner trigger (migration 000021): the member who joined earliest, then the lower ID,
// becomes owner with an owner_changed message that has no actor, after the left message; a
// non-owner leaving hands nothing over; the last member leaving closes the group.
func TestOwnerHandOverTrigger(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob, cy, dee := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Cy"), newUser(t, pool, "Dee")
	id := newGroup(t, s, pool, ada, bob, cy).ConversationID
	inTx(t, pool, func(ctx context.Context) error {
		ev, err := s.AddSystemMessage(ctx, id, ada, domain.SystemEvent{Type: domain.EventMemberAdded, UserIDs: []string{dee}})
		if err != nil {
			return err
		}
		return s.AddGroupMembers(ctx, id, []string{dee}, ev.CreatedAt)
	})
	heir := min(bob, cy) // bob and cy joined together; the lower ID wins

	removeMember(t, s, pool, id, dee, dee, domain.EventMemberLeft)
	if role(t, pool, id, ada) != "owner" {
		t.Error("a member leaving moved ownership")
	}
	last, _ := s.ListMessages(ctx, id, time.Time{}, "", 1)
	if last[0].Event.Type != domain.EventMemberLeft {
		t.Errorf("latest = %+v", last[0].Event)
	}

	removeMember(t, s, pool, id, ada, ada, domain.EventMemberLeft)
	if role(t, pool, id, heir) != "owner" || role(t, pool, id, max(bob, cy)) != "member" {
		t.Errorf("roles: %s %s", role(t, pool, id, bob), role(t, pool, id, cy))
	}
	ms, _ := s.ListMessages(ctx, id, time.Time{}, "", 2)
	if ms[0].Event.Type != domain.EventOwnerChanged || ms[0].SenderID != "" || !slices.Equal(ms[0].Event.UserIDs, []string{heir}) ||
		ms[1].Event.Type != domain.EventMemberLeft || ms[1].SenderID != ada || ms[0].ID <= ms[1].ID {
		t.Errorf("messages = %+v %+v", ms[0], ms[1])
	}
	after, err := s.SystemMessagesAfter(ctx, id, ms[1].ID)
	if err != nil || len(after) != 1 || after[0].ID != ms[0].ID {
		t.Errorf("SystemMessagesAfter = %+v, %v", after, err)
	}
	var lastAt time.Time
	_ = pool.QueryRow(ctx, "SELECT last_message_at FROM conversations WHERE id = $1", id).Scan(&lastAt)
	if !lastAt.Equal(ms[0].CreatedAt) {
		t.Errorf("last_message_at = %s, want the hand-over's %s", lastAt, ms[0].CreatedAt)
	}
	var left int
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM conversation_memberships WHERE conversation_id = $1 AND left_at IS NOT NULL", id).Scan(&left)
	if left != 2 {
		t.Errorf("ended stays = %d, want 2", left)
	}

	removeMember(t, s, pool, id, heir, max(bob, cy), domain.EventMemberRemoved)
	removeMember(t, s, pool, id, heir, heir, domain.EventMemberLeft)
	var closed *time.Time
	if err := pool.QueryRow(ctx, "SELECT closed_at FROM conversations WHERE id = $1", id).Scan(&closed); err != nil || closed == nil {
		t.Errorf("closed_at = %v, %v", closed, err)
	}
	if removed, err := s.RemoveGroupMember(ctx, id, heir); err != nil || removed {
		t.Errorf("remove twice: %v %v", removed, err)
	}
}

// Deleting the owner's account hands ownership over through the same trigger, with its
// owner_changed message (SRS 7, Retention). The deleted user's own messages go with them.
func TestOwnerAccountDeletedHandsOver(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob, cy := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Cy")
	id := newGroup(t, s, pool, ada, bob, cy).ConversationID
	add(t, s, id, ada, "from ada")
	add(t, s, id, bob, "from bob")

	if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id = $1", ada); err != nil {
		t.Fatal(err)
	}
	heir := min(bob, cy)
	if role(t, pool, id, heir) != "owner" {
		t.Errorf("heir is %q", role(t, pool, id, heir))
	}
	ms, _ := s.ListMessages(ctx, id, time.Time{}, "", 10)
	if len(ms) != 2 || ms[0].Event == nil || ms[0].Event.Type != domain.EventOwnerChanged || ms[0].SenderID != "" ||
		!slices.Equal(ms[0].Event.UserIDs, []string{heir}) || ms[1].SenderID != bob {
		t.Errorf("messages = %+v", ms)
	}
	var creator *string
	if err := pool.QueryRow(ctx, "SELECT created_by FROM conversations WHERE id = $1", id).Scan(&creator); err != nil || creator != nil {
		t.Errorf("created_by = %v, %v", creator, err)
	}
}

// Who was in a group at a report's moment: a stay that began at or before it and had not ended
// before it. Someone who joined later, or left earlier, was not.
func TestMembersAtReport(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob, cy, dee := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Cy"), newUser(t, pool, "Dee")
	id := newGroup(t, s, pool, ada, bob, cy).ConversationID
	removeMember(t, s, pool, id, cy, cy, domain.EventMemberLeft) // left before the report
	reportID := newReportAbout(t, s, id, bob, nil)
	removeMember(t, s, pool, id, ada, bob, domain.EventMemberRemoved) // left after the report
	inTx(t, pool, func(ctx context.Context) error {                   // joined after the report
		ev, err := s.AddSystemMessage(ctx, id, ada, domain.SystemEvent{Type: domain.EventMemberAdded, UserIDs: []string{dee}})
		if err != nil {
			return err
		}
		return s.AddGroupMembers(ctx, id, []string{dee}, ev.CreatedAt)
	})

	people, err := s.MembersAtReport(ctx, reportID)
	if err != nil || len(people) != 2 || people[0].UserID != ada || people[1].UserID != bob || people[0].FullName != "Ada" || people[0].Role == "" {
		t.Errorf("MembersAtReport = %+v, %v", people, err)
	}
	for user, want := range map[string]bool{ada: true, bob: true, cy: false, dee: false, "nope": false} {
		if got, err := s.MemberAtReport(ctx, reportID, user); err != nil || got != want {
			t.Errorf("MemberAtReport(%s) = %v, %v; want %v", user, got, err, want)
		}
	}

	r, err := s.FindReport(ctx, reportID)
	if err != nil || r.Reported != nil || r.ConversationKind != domain.KindGroup || r.GroupName == nil || *r.GroupName != "Lab" {
		t.Errorf("FindReport = %+v, %v", r, err)
	}
	// The admin read shows every message up to the report, system messages included.
	ms, err := s.ListReportedMessages(ctx, reportID, "", 10)
	if err != nil || len(ms) != 2 || ms[0].Kind != domain.KindSystem || ms[0].Event.Type != domain.EventMemberLeft || ms[1].Event.Type != domain.EventGroupCreated {
		t.Errorf("ListReportedMessages = %+v, %v", ms, err)
	}
}

// newReportAbout stores a report of the conversation; reported nil reports a whole group.
func newReportAbout(t *testing.T, s *Store, c, reporter string, reported *string) string {
	t.Helper()
	id, _, err := s.InsertReport(t.Context(), domain.NewReport{ConversationID: c, ReporterID: reporter, ReportedID: reported, Reason: domain.ReasonOther})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestRenameGroupStore(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
	id := newGroup(t, s, pool, ada, bob).ConversationID
	inTx(t, pool, func(ctx context.Context) error { return s.RenameGroup(ctx, id, "Lab 2") })
	if c, _ := s.FindConversation(t.Context(), id, bob); c.Name != "Lab 2" {
		t.Errorf("name = %q", c.Name)
	}
	ids, err := s.GroupMemberIDs(t.Context(), id)
	if err != nil || len(ids) != 2 {
		t.Errorf("GroupMemberIDs = %v, %v", ids, err)
	}
	if visible, err := s.UsersVisible(t.Context(), []string{ada, "not-a-uuid", clientID()}); err != nil || !slices.Equal(visible, []string{ada}) {
		t.Errorf("UsersVisible = %v, %v", visible, err)
	}
}

func ptr(s string) *string { return &s }
