package usecase

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

// fakeStore keeps conversations in memory, with the rules the real store's queries apply.
// IDs are UUIDs that count up, so messages sort by age, as UUIDv7 values do.
type fakeStore struct {
	hidden        map[string]bool // users who are suspended or not verified
	conversations map[string]*fakeConversation
	messages      []domain.Message
	seq           int
	failAdd       error
	reports       []domain.Report
	views         [][2]string // report, admin
	failView      error
	duplicateAdd  bool // AddMessage reports a client ID used by a racing send
	calls         []string
	versions      []domain.MessageVersion
	clock         func() time.Time // the time of edits and deletions
	onLockMessage func()           // runs inside LockOwnMessage, for a change that commits meanwhile
	onLockConv    func()           // runs inside LockConversation, for a change that commits meanwhile
	stays         []fakeStay       // conversation_memberships
	membersAt     map[string][]string
}

// fakeStay is one stay in a group, as conversation_memberships keeps it.
type fakeStay struct {
	conversation, user string
	joined             time.Time
	left               *time.Time
}

// fakeActors gives each user a standing; nobody listed is in good standing.
type fakeActors map[string]domain.Standing

func (f fakeActors) Standing(_ context.Context, userID string) (domain.Standing, error) {
	return f[userID], nil
}

type fakeConversation struct {
	id, a, b    string
	state       domain.State
	requestedBy *string
	lastAt      time.Time
	read        map[string]string
	// A group has these instead of a and b.
	kind            domain.Kind
	name            string
	creator, client string
	members         []fakeMember
	closed          bool
}

type fakeMember struct {
	user   string
	role   domain.Role
	joined time.Time
}

func (c *fakeConversation) group() bool { return c.kind == domain.KindGroup }

func (c *fakeConversation) find(user string) (fakeMember, bool) {
	for _, m := range c.members {
		if m.user == user {
			return m, true
		}
	}
	return fakeMember{}, false
}

func (c *fakeConversation) has(user string) bool {
	if c.group() {
		_, ok := c.find(user)
		return ok
	}
	return c.a == user || c.b == user
}

// since is when the member joined: the zero time in a direct conversation.
func (c *fakeConversation) since(user string) time.Time {
	m, _ := c.find(user)
	return m.joined
}

func newFakeStore() *fakeStore {
	return &fakeStore{hidden: map[string]bool{}, conversations: map[string]*fakeConversation{},
		clock: func() time.Time { return time.Date(2026, 11, 4, 10, 5, 0, 0, time.UTC) }}
}

func (f *fakeStore) pair(a, b string) *fakeConversation {
	for _, c := range f.conversations {
		if (c.a == a && c.b == b) || (c.a == b && c.b == a) {
			return c
		}
	}
	return nil
}

func (c *fakeConversation) view(member string) domain.Conversation {
	if c.group() {
		m, _ := c.find(member)
		return domain.Conversation{ID: c.id, Kind: domain.KindGroup, State: c.state, LastMessageAt: c.lastAt, Name: c.name,
			MyRole: m.role, JoinedAt: m.joined}
	}
	other := c.a
	if member == c.a {
		other = c.b
	}
	v := domain.Conversation{ID: c.id, Kind: domain.KindDirect, State: c.state, RequestedBy: c.requestedBy, LastMessageAt: c.lastAt, OtherID: other}
	if r, ok := c.read[other]; ok {
		v.OtherReadID = &r
	}
	return v
}

func (f *fakeStore) UserVisible(_ context.Context, id string) (bool, error) {
	return !f.hidden[id] && id != "ghost", nil
}

func (f *fakeStore) FindSent(_ context.Context, senderID, clientID string) (domain.Sent, bool, error) {
	for _, m := range f.messages {
		if m.SenderID == senderID && m.ClientID == clientID {
			return domain.Sent{Message: m, State: f.conversations[m.ConversationID].state}, true, nil
		}
	}
	return domain.Sent{}, false, nil
}

func (f *fakeStore) DirectExists(_ context.Context, a, b string) (bool, error) {
	return f.pair(a, b) != nil, nil
}

func (f *fakeStore) CreateDirect(_ context.Context, a, b string, state domain.State, requestedBy *string) (domain.Conversation, bool, error) {
	if f.pair(a, b) != nil {
		return domain.Conversation{}, false, nil
	}
	c := &fakeConversation{id: fmt.Sprintf("00000000-0000-0000-0000-%012d", len(f.conversations)+1), a: a, b: b, state: state,
		requestedBy: requestedBy, read: map[string]string{}}
	f.conversations[c.id] = c
	return c.view(a), true, nil
}

func (f *fakeStore) LockDirect(_ context.Context, a, b string) (domain.Conversation, error) {
	f.calls = append(f.calls, "lock")
	c := f.pair(a, b)
	if c == nil {
		return domain.Conversation{}, domain.ErrNotFound
	}
	return domain.Conversation{ID: c.id, State: c.state, RequestedBy: c.requestedBy}, nil
}

func (f *fakeStore) LockConversation(_ context.Context, id string) (domain.Conversation, error) {
	f.calls = append(f.calls, "lock")
	if f.onLockConv != nil {
		f.onLockConv()
		f.onLockConv = nil
	}
	c, ok := f.conversations[id]
	if !ok {
		return domain.Conversation{}, domain.ErrNotFound
	}
	return domain.Conversation{ID: c.id, State: c.state, RequestedBy: c.requestedBy}, nil
}

func (f *fakeStore) ActivateRequest(_ context.Context, a, b string) error {
	if c := f.pair(a, b); c != nil && c.state == domain.StateRequest {
		c.state = domain.StateActive
	}
	return nil
}

func (f *fakeStore) AddMessage(_ context.Context, m domain.Message) (domain.Message, bool, error) {
	if f.failAdd != nil {
		return domain.Message{}, false, f.failAdd
	}
	if f.duplicateAdd {
		return domain.Message{}, false, nil
	}
	f.seq++
	m.ID = fmt.Sprintf("00000000-0000-7000-8000-%012d", f.seq)
	m.CreatedAt = time.Date(2026, 11, 4, 10, 0, f.seq, 0, time.UTC)
	f.messages = append(f.messages, m)
	c := f.conversations[m.ConversationID]
	c.lastAt = m.CreatedAt
	c.read[m.SenderID] = m.ID
	return m, true, nil
}

func (f *fakeStore) member(id, memberID string) (*fakeConversation, error) {
	c, ok := f.conversations[id]
	if !ok || !c.has(memberID) {
		return nil, domain.ErrNotFound
	}
	return c, nil
}

func (f *fakeStore) FindConversation(_ context.Context, id, memberID string) (domain.Conversation, error) {
	c, err := f.member(id, memberID)
	if err != nil {
		return domain.Conversation{}, err
	}
	return c.view(memberID), nil
}

func (f *fakeStore) item(c *fakeConversation, memberID string) domain.Item {
	it := domain.Item{Conversation: c.view(memberID)}
	if c.group() {
		it.Group = &domain.GroupInfo{Name: c.name, MemberCount: len(c.members), MyRole: it.MyRole, Avatars: []domain.Person{}}
		for _, m := range f.sortedMembers(c) {
			if m.user != memberID && len(it.Group.Avatars) < 3 {
				it.Group.Avatars = append(it.Group.Avatars, domain.Person{UserID: m.user})
			}
		}
	} else {
		it.Other = domain.Person{UserID: it.OtherID, Available: !f.hidden[it.OtherID]}
	}
	since := c.since(memberID)
	for _, m := range f.messages {
		if m.ConversationID != c.id || m.CreatedAt.Before(since) {
			continue
		}
		it.LastMessage = &domain.Preview{SenderID: m.SenderID, Kind: m.Kind, Body: m.Body, Deleted: m.Deleted(), CreatedAt: m.CreatedAt}
		if m.Event != nil {
			it.LastMessage.Event = &m.Event.Type
		}
		if !m.System() && m.SenderID != memberID && m.ID > c.read[memberID] && !m.Deleted() {
			it.UnreadCount++
		}
	}
	return it
}

func (f *fakeStore) FindItem(_ context.Context, id, memberID string) (domain.Item, error) {
	c, err := f.member(id, memberID)
	if err != nil {
		return domain.Item{}, err
	}
	return f.item(c, memberID), nil
}

func (f *fakeStore) ListItems(_ context.Context, memberID string, box domain.Box, after *domain.Cursor, limit int) ([]domain.Item, error) {
	var items []domain.Item
	for _, c := range f.conversations {
		if !c.has(memberID) {
			continue
		}
		it := f.item(c, memberID)
		mine := c.requestedBy != nil && *c.requestedBy == memberID
		in := c.state == domain.StateActive || (c.state == domain.StateRequest && mine)
		if box == domain.BoxRequests {
			in = c.state == domain.StateRequest && !mine && !f.hidden[it.OtherID]
		}
		if in {
			items = append(items, it)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].LastMessageAt.After(items[j].LastMessageAt) })
	if after != nil {
		items = slices.DeleteFunc(items, func(it domain.Item) bool { return !it.LastMessageAt.Before(after.LastMessageAt) })
	}
	return items[:min(limit, len(items))], nil
}

func (f *fakeStore) ListMessages(_ context.Context, conversationID string, since time.Time, before string, limit int) ([]domain.Message, error) {
	var out []domain.Message
	for _, m := range slices.Backward(f.messages) {
		if m.ConversationID == conversationID && !m.CreatedAt.Before(since) && (before == "" || m.ID < before) {
			out = append(out, m)
		}
	}
	return out[:min(limit, len(out))], nil
}

func (f *fakeStore) MessageIn(_ context.Context, conversationID, messageID string, since time.Time) (bool, error) {
	return slices.ContainsFunc(f.messages, func(m domain.Message) bool {
		return m.ID == messageID && m.ConversationID == conversationID && !m.CreatedAt.Before(since)
	}), nil
}

func (f *fakeStore) MoveReadMarker(_ context.Context, conversationID, memberID, messageID string) (bool, error) {
	c := f.conversations[conversationID]
	if messageID <= c.read[memberID] {
		return false, nil
	}
	c.read[memberID] = messageID
	return true, nil
}

func (f *fakeStore) AnswerRequest(_ context.Context, id, receiverID string, state domain.State) (string, bool, error) {
	c, err := f.member(id, receiverID)
	if err != nil || c.state != domain.StateRequest || *c.requestedBy == receiverID || f.hidden[*c.requestedBy] {
		return "", false, nil
	}
	c.state = state
	return *c.requestedBy, true, nil
}

func (f *fakeStore) CountUnread(_ context.Context, memberID string) (domain.UnreadCounts, error) {
	var n domain.UnreadCounts
	for _, c := range f.conversations {
		if !c.has(memberID) {
			continue
		}
		switch {
		case c.state == domain.StateActive:
			n.Total += f.item(c, memberID).UnreadCount
		case c.state == domain.StateRequest && *c.requestedBy != memberID && !f.hidden[*c.requestedBy]:
			n.Requests++
		}
	}
	return n, nil
}

// fakeTx restores conversations and messages when fn fails, as a rollback would.
type fakeTx struct{ store *fakeStore }

func (t fakeTx) InTx(ctx context.Context, fn func(context.Context) error) error {
	s := t.store
	saved := map[string]fakeConversation{}
	for id, c := range s.conversations {
		cp := *c
		cp.members = slices.Clone(c.members)
		cp.read = maps.Clone(c.read)
		saved[id] = cp
	}
	messages, versions, stays := slices.Clone(s.messages), len(s.versions), slices.Clone(s.stays)
	err := fn(ctx)
	if err != nil {
		s.messages, s.versions, s.stays = messages, s.versions[:versions], stays
		s.conversations = map[string]*fakeConversation{}
		for id, c := range saved {
			s.conversations[id] = &c
		}
	}
	return err
}

// fakeBlocks holds blocks as blocker|blocked, and counts locks. onLock runs under the first
// lock, for a block that commits while a send waits for it.
type fakeBlocks struct {
	set    map[string]bool
	locks  int
	pairs  []string // the pairs locked, in order
	onLock func()
}

func newFakeBlocks() *fakeBlocks { return &fakeBlocks{set: map[string]bool{}} }

func (b *fakeBlocks) Lock(_ context.Context, x, y string) error {
	b.locks++
	b.pairs = append(b.pairs, x+"|"+y)
	if b.onLock != nil {
		b.onLock()
		b.onLock = nil
	}
	return nil
}

func (b *fakeBlocks) Between(_ context.Context, me, other string) (domain.BlockState, error) {
	return domain.BlockState{ByMe: b.set[me+"|"+other], ByThem: b.set[other+"|"+me]}, nil
}

func (b *fakeBlocks) BlockedAmong(_ context.Context, userID string, ids []string) ([]string, error) {
	var out []string
	for _, id := range ids {
		if b.set[userID+"|"+id] || b.set[id+"|"+userID] {
			out = append(out, id)
		}
	}
	return out, nil
}

type fakeConnections map[string]bool

func (f fakeConnections) Connected(_ context.Context, a, b string) (bool, error) {
	return f[a+"|"+b] || f[b+"|"+a], nil
}

func (f fakeConnections) ConnectedAmong(_ context.Context, userID string, ids []string) ([]string, error) {
	var out []string
	for _, id := range ids {
		if f[userID+"|"+id] || f[id+"|"+userID] {
			out = append(out, id)
		}
	}
	return out, nil
}

// fakeNotifier records what it hears, one string per call.
type fakeNotifier struct{ events []string }

func (n *fakeNotifier) MembersAdded(_ context.Context, conversationID, ownerID string, userIDs []string) error {
	n.events = append(n.events, fmt.Sprintf("added %s by %s: %v", conversationID, ownerID, userIDs))
	return nil
}

func (n *fakeNotifier) AccessLost(_ context.Context, conversationID, userID string) error {
	n.events = append(n.events, fmt.Sprintf("lost %s %s", conversationID, userID))
	return nil
}

// fakeLimiter counts per window and user. left is the time every window reports.
type fakeLimiter struct {
	used   map[string]int
	left   map[string]time.Duration
	failed bool
}

func newFakeLimiter() *fakeLimiter {
	return &fakeLimiter{used: map[string]int{}, left: map[string]time.Duration{}}
}

func (f *fakeLimiter) Used(_ context.Context, name, userID string) (int, time.Duration, error) {
	if f.failed {
		return 0, 0, errors.New("redis down")
	}
	return f.used[name+"|"+userID], f.left[name], nil
}

func (f *fakeLimiter) Record(_ context.Context, name, userID string, n int, _ time.Duration) error {
	if f.failed {
		return errors.New("redis down")
	}
	f.used[name+"|"+userID] += n
	return nil
}

// fakePublisher records what it hears, one string per event.
type fakePublisher struct{ events []string }

func (f *fakePublisher) MessageSent(_ context.Context, s domain.Sent, to []string) {
	f.events = append(f.events, fmt.Sprintf("sent %s to %v", s.Message.ID, to))
}

func (f *fakePublisher) MessageEdited(_ context.Context, m domain.Message, to []string) {
	f.events = append(f.events, fmt.Sprintf("edited %s to %v", m.ID, to))
}

func (f *fakePublisher) MessageDeleted(_ context.Context, m domain.Message, to []string) {
	f.events = append(f.events, fmt.Sprintf("deleted %s to %v", m.ID, to))
}

func (f *fakePublisher) ReadMoved(_ context.Context, _, readerID, messageID string, to []string) {
	f.events = append(f.events, fmt.Sprintf("read %s %s to %v", readerID, messageID, to))
}

func (f *fakePublisher) RequestAccepted(_ context.Context, id string, to []string) {
	f.events = append(f.events, fmt.Sprintf("accepted %s to %v", id, to))
}

func (f *fakePublisher) RequestDeclined(_ context.Context, id string, to []string) {
	f.events = append(f.events, fmt.Sprintf("declined %s to %v", id, to))
}

func (f *fakePublisher) PairChanged(_ context.Context, a, b string) {
	f.events = append(f.events, fmt.Sprintf("pair %s %s", a, b))
}

func (f *fakePublisher) MembersChanged(_ context.Context, id string, c domain.MemberChange, to []string) {
	f.events = append(f.events, fmt.Sprintf("members %s %s by %q %v %q to %v", id, c.Kind, c.ActorID, c.UserIDs, c.Name, to))
}

func (f *fakePublisher) AccessLost(_ context.Context, id string, users []string) {
	f.events = append(f.events, fmt.Sprintf("lost %s %v", id, users))
}

func (f *fakeStore) ActivePeers(context.Context, string) ([]string, error) { return nil, nil }

func (f *fakeStore) VisiblePeers(context.Context, string, []string) ([]string, error) {
	return nil, nil
}

func (f *fakeStore) Partners(context.Context, string) ([]domain.Partner, error) { return nil, nil }

func (f *fakeStore) DirectID(_ context.Context, a, b string) (string, bool, error) {
	if c := f.pair(a, b); c != nil {
		return c.id, true, nil
	}
	return "", false, nil
}

func (f *fakeStore) DeclineRequests(_ context.Context, a, b string) error {
	if c := f.pair(a, b); c != nil && c.state == domain.StateRequest {
		c.state = domain.StateDeclined
	}
	return nil
}

func (f *fakeStore) FindMessageSender(_ context.Context, conversationID, messageID string, since time.Time) (string, bool, error) {
	for _, m := range f.messages {
		if m.ConversationID == conversationID && m.ID == messageID && !m.System() && !m.CreatedAt.Before(since) {
			return m.SenderID, true, nil
		}
	}
	return "", false, nil
}

func (f *fakeStore) InsertReport(_ context.Context, r domain.NewReport) (string, time.Time, error) {
	f.seq++
	id, at := fmt.Sprintf("00000000-0000-0000-0000-9%011d", f.seq), reportClock(f.seq)
	report := domain.Report{ID: id, ConversationID: r.ConversationID, Reason: r.Reason, Details: r.Details, ConversationKind: domain.KindDirect,
		MessageID: r.MessageID, CreatedAt: at, Reporter: domain.ReportPerson{UserID: r.ReporterID}}
	if r.ReportedID != nil {
		report.Reported = &domain.ReportPerson{UserID: *r.ReportedID}
	}
	if c := f.conversations[r.ConversationID]; c.group() {
		report.ConversationKind, report.GroupName = domain.KindGroup, &c.name
		var at []string
		for _, m := range c.members {
			at = append(at, m.user)
		}
		if f.membersAt == nil {
			f.membersAt = map[string][]string{}
		}
		f.membersAt[id] = at
	}
	f.reports = append(f.reports, report)
	return id, at, nil
}

func reportClock(n int) time.Time { return time.Date(2026, 11, 5, 9, 0, n, 0, time.UTC) }

func (f *fakeStore) ListReports(_ context.Context, open bool, limit, offset int) ([]domain.Report, int, error) {
	var all []domain.Report
	for _, r := range f.reports {
		if (r.ResolvedAt == nil) == open {
			all = append(all, r)
		}
	}
	return all[min(offset, len(all)):min(offset+limit, len(all))], len(all), nil
}

func (f *fakeStore) FindReport(_ context.Context, id string) (domain.Report, error) {
	for _, r := range f.reports {
		if r.ID == id {
			return r, nil
		}
	}
	return domain.Report{}, domain.ErrNotFound
}

// ListReportedMessages ignores the moment and shows the current text: the real query's cut-off
// and text at the report's moment have their own tests.
func (f *fakeStore) ListReportedMessages(ctx context.Context, reportID, before string, limit int) ([]domain.ReportedMessage, error) {
	r, err := f.FindReport(ctx, reportID)
	if err != nil {
		return nil, err
	}
	ms, err := f.ListMessages(ctx, r.ConversationID, time.Time{}, before, limit)
	out := make([]domain.ReportedMessage, len(ms))
	for i, m := range ms {
		out[i] = domain.ReportedMessage{ID: m.ID, ConversationID: m.ConversationID, SenderID: m.SenderID, CreatedAt: m.CreatedAt,
			Kind: m.Kind, Event: m.Event, Body: m.Body, EditedAt: m.EditedAt, DeletedAt: m.DeletedAt}
	}
	return out, err
}

// ListReportedVersions ignores the moment, as ListReportedMessages does.
func (f *fakeStore) ListReportedVersions(_ context.Context, _ string, messageIDs []string) ([]domain.MessageVersion, error) {
	var out []domain.MessageVersion
	for _, v := range f.versions {
		if slices.Contains(messageIDs, v.MessageID) {
			out = append(out, v)
		}
	}
	return out, nil
}

func (f *fakeStore) message(id string) *domain.Message {
	for i := range f.messages {
		if f.messages[i].ID == id {
			return &f.messages[i]
		}
	}
	return nil
}

func (f *fakeStore) FindOwnMessage(_ context.Context, conversationID, messageID, senderID string) (domain.Message, error) {
	m := f.message(messageID)
	if m == nil || m.ConversationID != conversationID || m.SenderID != senderID || m.System() {
		return domain.Message{}, domain.ErrNotFound
	}
	return *m, nil
}

func (f *fakeStore) LockOwnMessage(ctx context.Context, conversationID, messageID, senderID string) (domain.Message, error) {
	f.calls = append(f.calls, "lock message")
	if f.onLockMessage != nil {
		f.onLockMessage()
		f.onLockMessage = nil
	}
	return f.FindOwnMessage(ctx, conversationID, messageID, senderID)
}

// replace keeps the message's text as a version ended by end, as the real queries do.
func (f *fakeStore) replace(messageID string, end domain.VersionEnd, body *string) (domain.Message, error) {
	m := f.message(messageID)
	if m == nil || m.Deleted() {
		return domain.Message{}, domain.ErrNotFound
	}
	at, from := f.clock(), m.CreatedAt
	if m.EditedAt != nil {
		from = *m.EditedAt
	}
	f.versions = append(f.versions, domain.MessageVersion{MessageID: m.ID, Body: *m.Body, ShownFrom: from, ShownUntil: at, EndedBy: end})
	m.Body = body
	if end == domain.EndedByEdit {
		m.EditedAt = &at
	} else {
		m.DeletedAt = &at
	}
	return *m, nil
}

func (f *fakeStore) EditMessage(_ context.Context, messageID, body string) (domain.Message, error) {
	f.calls = append(f.calls, "edit")
	return f.replace(messageID, domain.EndedByEdit, &body)
}

func (f *fakeStore) DeleteMessage(_ context.Context, messageID string) (domain.Message, error) {
	f.calls = append(f.calls, "delete")
	return f.replace(messageID, domain.EndedByDeletion, nil)
}

func (f *fakeStore) AddReportView(_ context.Context, reportID, adminID string) error {
	if f.failView != nil {
		return f.failView
	}
	f.views = append(f.views, [2]string{reportID, adminID})
	return nil
}

func (f *fakeStore) ResolveReport(_ context.Context, id, _ string, outcome domain.ReportOutcome, note *string) (bool, error) {
	for i := range f.reports {
		if r := &f.reports[i]; r.ID == id && r.ResolvedAt == nil {
			at := reportClock(59)
			r.ResolvedAt, r.Outcome, r.Note = &at, &outcome, note
			return true, nil
		}
	}
	return false, nil
}

// MembersAtReport returns who was in the group when the report was made: the fake records the
// members at InsertReport; the real rule (conversation_memberships) has its store tests.
func (f *fakeStore) MembersAtReport(_ context.Context, reportID string) ([]domain.ReportPerson, error) {
	var out []domain.ReportPerson
	for _, u := range f.membersAt[reportID] {
		out = append(out, domain.ReportPerson{UserID: u})
	}
	return out, nil
}

func (f *fakeStore) MemberAtReport(_ context.Context, reportID, userID string) (bool, error) {
	return slices.Contains(f.membersAt[reportID], userID), nil
}

func (f *fakeStore) UsersVisible(_ context.Context, ids []string) ([]string, error) {
	var out []string
	for _, id := range ids {
		if !f.hidden[id] && id != "ghost" {
			out = append(out, id)
		}
	}
	return out, nil
}

func (f *fakeStore) FindGroupByClient(_ context.Context, creatorID, clientID string) (string, bool, error) {
	for _, c := range f.conversations {
		if c.group() && c.creator == creatorID && c.client == clientID {
			return c.id, true, nil
		}
	}
	return "", false, nil
}

func (f *fakeStore) CreateGroup(ctx context.Context, g domain.NewGroup) (domain.Message, bool, error) {
	f.calls = append(f.calls, "create group")
	if _, found, _ := f.FindGroupByClient(ctx, g.CreatorID, g.ClientID); found {
		return domain.Message{}, false, nil
	}
	c := &fakeConversation{id: fmt.Sprintf("00000000-0000-0000-0000-%012d", len(f.conversations)+1), kind: domain.KindGroup,
		state: domain.StateActive, name: g.Name, creator: g.CreatorID, client: g.ClientID, read: map[string]string{}}
	f.conversations[c.id] = c
	name := g.Name
	created, err := f.AddSystemMessage(ctx, c.id, g.CreatorID, domain.SystemEvent{Type: domain.EventGroupCreated, UserIDs: g.MemberIDs, NewName: &name})
	if err != nil {
		return domain.Message{}, false, err
	}
	f.join(c, g.CreatorID, domain.RoleOwner, created.CreatedAt)
	for _, u := range g.MemberIDs {
		f.join(c, u, domain.RoleMember, created.CreatedAt)
	}
	return created, true, nil
}

func (f *fakeStore) join(c *fakeConversation, user string, role domain.Role, at time.Time) {
	c.members = append(c.members, fakeMember{user: user, role: role, joined: at})
	f.stays = append(f.stays, fakeStay{conversation: c.id, user: user, joined: at})
}

func (f *fakeStore) AddSystemMessage(_ context.Context, conversationID, actorID string, ev domain.SystemEvent) (domain.Message, error) {
	f.seq++
	m := domain.Message{ID: fmt.Sprintf("00000000-0000-7000-8000-%012d", f.seq), ConversationID: conversationID, SenderID: actorID,
		Kind: domain.KindSystem, Event: &ev, CreatedAt: time.Date(2026, 11, 4, 10, 0, f.seq, 0, time.UTC)}
	f.messages = append(f.messages, m)
	f.conversations[conversationID].lastAt = m.CreatedAt
	return m, nil
}

func (f *fakeStore) AddGroupMembers(_ context.Context, conversationID string, userIDs []string, joinedAt time.Time) error {
	f.calls = append(f.calls, "add members")
	c := f.conversations[conversationID]
	for _, u := range userIDs {
		f.join(c, u, domain.RoleMember, joinedAt)
	}
	return nil
}

// RemoveGroupMember does what the trigger of migration 000021 does: it ends the stay, hands
// ownership to the member who joined earliest with an owner_changed message that has no actor,
// or closes the group when nobody is left.
func (f *fakeStore) RemoveGroupMember(ctx context.Context, conversationID, userID string) (bool, error) {
	c := f.conversations[conversationID]
	gone, ok := c.find(userID)
	if !ok {
		return false, nil
	}
	c.members = slices.DeleteFunc(c.members, func(m fakeMember) bool { return m.user == userID })
	at := c.lastAt
	for i := range f.stays {
		if s := &f.stays[i]; s.conversation == conversationID && s.user == userID && s.left == nil {
			s.left = &at
		}
	}
	if len(c.members) == 0 {
		c.closed = true
		return true, nil
	}
	if gone.role != domain.RoleOwner {
		return true, nil
	}
	heir := f.sortedMembers(c)[0].user
	for i := range c.members {
		if c.members[i].user == heir {
			c.members[i].role = domain.RoleOwner
		}
	}
	_, err := f.AddSystemMessage(ctx, conversationID, "", domain.SystemEvent{Type: domain.EventOwnerChanged, UserIDs: []string{heir}})
	return true, err
}

func (f *fakeStore) sortedMembers(c *fakeConversation) []fakeMember {
	ms := slices.Clone(c.members)
	slices.SortFunc(ms, func(a, b fakeMember) int {
		if n := a.joined.Compare(b.joined); n != 0 {
			return n
		}
		return strings.Compare(a.user, b.user)
	})
	return ms
}

func (f *fakeStore) RenameGroup(_ context.Context, conversationID, name string) error {
	f.conversations[conversationID].name = name
	return nil
}

func (f *fakeStore) GroupMemberIDs(_ context.Context, conversationID string) ([]string, error) {
	c, ok := f.conversations[conversationID]
	if !ok {
		return nil, nil
	}
	var out []string
	for _, m := range f.sortedMembers(c) {
		out = append(out, m.user)
	}
	return out, nil
}

func (f *fakeStore) ListGroupMembers(_ context.Context, conversationID string) ([]domain.GroupMember, error) {
	var out []domain.GroupMember
	for _, m := range f.sortedMembers(f.conversations[conversationID]) {
		out = append(out, domain.GroupMember{UserID: m.user, Role: m.role, JoinedAt: m.joined, Available: !f.hidden[m.user]})
	}
	return out, nil
}

func (f *fakeStore) GroupAvatars(context.Context, string, []string) (map[string][]domain.Person, error) {
	return map[string][]domain.Person{}, nil
}

func (f *fakeStore) ListReaders(_ context.Context, conversationID, viewerID string) ([]domain.Reader, error) {
	c := f.conversations[conversationID]
	var out []domain.Reader
	for _, m := range f.sortedMembers(c) {
		if m.user == viewerID {
			continue
		}
		r := domain.Reader{UserID: m.user}
		if id, ok := c.read[m.user]; ok {
			r.LastReadMessageID = &id
		}
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeStore) SystemMessagesAfter(_ context.Context, conversationID, afterID string) ([]domain.Message, error) {
	var out []domain.Message
	for _, m := range f.messages {
		if m.ConversationID == conversationID && m.System() && m.ID > afterID {
			out = append(out, m)
		}
	}
	return out, nil
}
