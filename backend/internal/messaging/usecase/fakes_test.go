package usecase

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
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
}

func newFakeStore() *fakeStore {
	return &fakeStore{hidden: map[string]bool{}, conversations: map[string]*fakeConversation{}}
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
	other := c.a
	if member == c.a {
		other = c.b
	}
	v := domain.Conversation{ID: c.id, State: c.state, RequestedBy: c.requestedBy, LastMessageAt: c.lastAt, OtherID: other}
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
	if !ok || (c.a != memberID && c.b != memberID) {
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
	it.Other = domain.Person{UserID: it.OtherID, Available: !f.hidden[it.OtherID]}
	for _, m := range f.messages {
		if m.ConversationID == c.id {
			it.LastMessage = &domain.Preview{SenderID: m.SenderID, Body: m.Body, CreatedAt: m.CreatedAt}
			if m.SenderID != memberID && m.ID > c.read[memberID] {
				it.UnreadCount++
			}
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
		if c.a != memberID && c.b != memberID {
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

func (f *fakeStore) ListMessages(_ context.Context, conversationID, before string, limit int) ([]domain.Message, error) {
	var out []domain.Message
	for _, m := range slices.Backward(f.messages) {
		if m.ConversationID == conversationID && (before == "" || m.ID < before) {
			out = append(out, m)
		}
	}
	return out[:min(limit, len(out))], nil
}

func (f *fakeStore) MessageIn(_ context.Context, conversationID, messageID string) (bool, error) {
	return slices.ContainsFunc(f.messages, func(m domain.Message) bool {
		return m.ID == messageID && m.ConversationID == conversationID
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
		if c.a != memberID && c.b != memberID {
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
		saved[id] = *c
	}
	n := len(s.messages)
	err := fn(ctx)
	if err != nil {
		s.messages = s.messages[:n]
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
	onLock func()
}

func newFakeBlocks() *fakeBlocks { return &fakeBlocks{set: map[string]bool{}} }

func (b *fakeBlocks) Lock(context.Context, string, string) error {
	b.locks++
	if b.onLock != nil {
		b.onLock()
		b.onLock = nil
	}
	return nil
}

func (b *fakeBlocks) Between(_ context.Context, me, other string) (domain.BlockState, error) {
	return domain.BlockState{ByMe: b.set[me+"|"+other], ByThem: b.set[other+"|"+me]}, nil
}

type fakeConnections map[string]bool

func (f fakeConnections) Connected(_ context.Context, a, b string) (bool, error) {
	return f[a+"|"+b] || f[b+"|"+a], nil
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

func (f *fakeLimiter) Record(_ context.Context, name, userID string, _ time.Duration) error {
	if f.failed {
		return errors.New("redis down")
	}
	f.used[name+"|"+userID]++
	return nil
}

// fakePublisher records what it hears, one string per event.
type fakePublisher struct{ events []string }

func (f *fakePublisher) MessageSent(_ context.Context, m domain.Message, to []string) {
	f.events = append(f.events, fmt.Sprintf("sent %s to %v", m.ID, to))
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

func (f *fakeStore) DeclineRequests(_ context.Context, a, b string) error {
	if c := f.pair(a, b); c != nil && c.state == domain.StateRequest {
		c.state = domain.StateDeclined
	}
	return nil
}

func (f *fakeStore) FindMessageSender(_ context.Context, conversationID, messageID string) (string, bool, error) {
	for _, m := range f.messages {
		if m.ConversationID == conversationID && m.ID == messageID {
			return m.SenderID, true, nil
		}
	}
	return "", false, nil
}

func (f *fakeStore) InsertReport(_ context.Context, r domain.NewReport) (string, time.Time, error) {
	f.seq++
	id, at := fmt.Sprintf("00000000-0000-0000-0000-9%011d", f.seq), reportClock(f.seq)
	f.reports = append(f.reports, domain.Report{ID: id, ConversationID: r.ConversationID, Reason: r.Reason, Details: r.Details,
		MessageID: r.MessageID, CreatedAt: at, Reporter: domain.ReportPerson{UserID: r.ReporterID}, Reported: domain.ReportPerson{UserID: r.ReportedID}})
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

// ListReportedMessages ignores the moment: the real query's cut-off has its own test.
func (f *fakeStore) ListReportedMessages(ctx context.Context, reportID, before string, limit int) ([]domain.Message, error) {
	r, err := f.FindReport(ctx, reportID)
	if err != nil {
		return nil, err
	}
	return f.ListMessages(ctx, r.ConversationID, before, limit)
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
