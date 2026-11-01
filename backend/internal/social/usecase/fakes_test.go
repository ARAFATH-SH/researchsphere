package usecase

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/social/domain"
)

// fakeRow is one row of the connections table.
type fakeRow struct {
	id, requester, receiver string
	status                  string // pending, accepted or rejected
	createdAt, respondedAt  time.Time
}

// fakeStore keeps users and connection rows in memory and applies the database's rule of
// one live link per pair. A test sets err to make every call fail.
type fakeStore struct {
	visible map[string]bool          // users who are verified and not suspended
	people  map[string]domain.Person // every user, by ID
	rows    []*fakeRow
	next    int
	clock   time.Time // each new row or answer moves it on a second
	err     error
	// vanish makes the next n FindLiveLink calls miss the live link, as if it was
	// cancelled or rejected between the refused insert and the read.
	vanish int
	// creates counts CreateRequest calls.
	creates int
}

var _ domain.Store = (*fakeStore)(nil)

func newFakeStore() *fakeStore {
	return &fakeStore{visible: map[string]bool{}, people: map[string]domain.Person{},
		clock: time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)}
}

// addUser adds a user who is visible unless hidden is true (suspended or not verified).
func (s *fakeStore) addUser(id string, hidden bool) {
	s.people[id] = domain.Person{UserID: id, FullName: "Name of " + id}
	s.visible[id] = !hidden
}

func (s *fakeStore) tick() time.Time {
	s.clock = s.clock.Add(time.Second)
	return s.clock
}

func (s *fakeStore) live(a, b string) *fakeRow {
	for _, r := range s.rows {
		pair := (r.requester == a && r.receiver == b) || (r.requester == b && r.receiver == a)
		if pair && (r.status == "pending" || r.status == "accepted") {
			return r
		}
	}
	return nil
}

func (s *fakeStore) byID(id string) *fakeRow {
	for _, r := range s.rows {
		if r.id == id {
			return r
		}
	}
	return nil
}

func (s *fakeStore) UserVisible(_ context.Context, userID string) (bool, error) {
	return s.visible[userID], s.err
}

func (s *fakeStore) FindPerson(_ context.Context, userID string) (domain.Person, error) {
	if s.err != nil {
		return domain.Person{}, s.err
	}
	p, ok := s.people[userID]
	if !ok {
		return domain.Person{}, domain.ErrNotFound
	}
	return p, nil
}

func (s *fakeStore) CreateRequest(_ context.Context, requesterID, receiverID string) (domain.Request, bool, error) {
	s.creates++
	if s.err != nil {
		return domain.Request{}, false, s.err
	}
	if s.live(requesterID, receiverID) != nil {
		return domain.Request{}, false, nil
	}
	s.next++
	r := &fakeRow{id: fmt.Sprintf("req-%d", s.next), requester: requesterID, receiver: receiverID,
		status: "pending", createdAt: s.tick()}
	s.rows = append(s.rows, r)
	return domain.Request{ID: r.id, RequesterID: requesterID, ReceiverID: receiverID, CreatedAt: r.createdAt}, true, nil
}

func (s *fakeStore) FindLiveLink(_ context.Context, a, b string) (domain.LiveLink, bool, error) {
	if s.err != nil {
		return domain.LiveLink{}, false, s.err
	}
	if s.vanish > 0 {
		s.vanish--
		return domain.LiveLink{}, false, nil
	}
	r := s.live(a, b)
	if r == nil {
		return domain.LiveLink{}, false, nil
	}
	return domain.LiveLink{ID: r.id, RequesterID: r.requester, Accepted: r.status == "accepted"}, true, nil
}

// answerable is the pending request receiverID received from a visible sender.
func (s *fakeStore) answerable(requestID, receiverID string) *fakeRow {
	r := s.byID(requestID)
	if r == nil || r.receiver != receiverID || r.status != "pending" || !s.visible[r.requester] {
		return nil
	}
	return r
}

func (s *fakeStore) AcceptRequest(_ context.Context, requestID, receiverID string) (domain.Request, time.Time, error) {
	if s.err != nil {
		return domain.Request{}, time.Time{}, s.err
	}
	r := s.answerable(requestID, receiverID)
	if r == nil {
		return domain.Request{}, time.Time{}, domain.ErrNotFound
	}
	r.status, r.respondedAt = "accepted", s.tick()
	return domain.Request{ID: r.id, RequesterID: r.requester, ReceiverID: r.receiver, CreatedAt: r.createdAt}, r.respondedAt, nil
}

func (s *fakeStore) RejectRequest(_ context.Context, requestID, receiverID string) error {
	if s.err != nil {
		return s.err
	}
	r := s.answerable(requestID, receiverID)
	if r == nil {
		return domain.ErrNotFound
	}
	r.status, r.respondedAt = "rejected", s.tick()
	return nil
}

func (s *fakeStore) CancelRequest(_ context.Context, requestID, requesterID string) error {
	if s.err != nil {
		return s.err
	}
	r := s.byID(requestID)
	if r == nil || r.requester != requesterID || r.status != "pending" {
		return domain.ErrNotFound
	}
	s.rows = slices.DeleteFunc(s.rows, func(x *fakeRow) bool { return x == r })
	return nil
}

func (s *fakeStore) RemoveConnection(_ context.Context, userID, otherID string) error {
	if s.err != nil {
		return s.err
	}
	r := s.live(userID, otherID)
	if r == nil || r.status != "accepted" {
		return domain.ErrNotFound
	}
	s.rows = slices.DeleteFunc(s.rows, func(x *fakeRow) bool { return x == r })
	return nil
}

// page returns items[offset:offset+limit], newest (last added) first.
func page[T any](items []T, limit, offset int) []T {
	slices.Reverse(items)
	if offset >= len(items) {
		return []T{}
	}
	return items[offset:min(len(items), offset+limit)]
}

func (s *fakeStore) ListConnections(_ context.Context, userID string, limit, offset int) ([]domain.Connection, int, error) {
	if s.err != nil {
		return nil, 0, s.err
	}
	var all []domain.Connection
	for _, r := range s.rows {
		other := r.receiver
		if r.receiver == userID {
			other = r.requester
		}
		if r.status == "accepted" && (r.requester == userID || r.receiver == userID) && s.visible[other] {
			all = append(all, domain.Connection{ConnectedAt: r.respondedAt, Other: s.people[other]})
		}
	}
	return page(all, limit, offset), len(all), nil
}

func (s *fakeStore) listPending(userID string, received bool, limit, offset int) ([]domain.PendingRequest, int, error) {
	if s.err != nil {
		return nil, 0, s.err
	}
	var all []domain.PendingRequest
	for _, r := range s.rows {
		me, other := r.requester, r.receiver
		if received {
			me, other = r.receiver, r.requester
		}
		if r.status == "pending" && me == userID && s.visible[other] {
			all = append(all, domain.PendingRequest{ID: r.id, CreatedAt: r.createdAt, Other: s.people[other]})
		}
	}
	return page(all, limit, offset), len(all), nil
}

func (s *fakeStore) ListReceived(_ context.Context, userID string, limit, offset int) ([]domain.PendingRequest, int, error) {
	return s.listPending(userID, true, limit, offset)
}

func (s *fakeStore) ListSent(_ context.Context, userID string, limit, offset int) ([]domain.PendingRequest, int, error) {
	return s.listPending(userID, false, limit, offset)
}

func (s *fakeStore) CountConnections(ctx context.Context, userID string) (int, error) {
	_, n, err := s.ListConnections(ctx, userID, 0, 0)
	return n, err
}

// fakeLimiter counts requests per user. A test sets err to make it fail, or used and
// left to report a window.
type fakeLimiter struct {
	used     map[string]int
	left     time.Duration
	err      error
	recorded []time.Duration // the window of each Record call
}

func newFakeLimiter() *fakeLimiter { return &fakeLimiter{used: map[string]int{}} }

func (l *fakeLimiter) Used(_ context.Context, userID string) (int, time.Duration, error) {
	return l.used[userID], l.left, l.err
}

func (l *fakeLimiter) Record(_ context.Context, userID string, window time.Duration) error {
	if l.err != nil {
		return l.err
	}
	l.used[userID]++
	l.recorded = append(l.recorded, window)
	return nil
}

// fakeNotifier remembers what it heard. A test sets err to make it fail.
type fakeNotifier struct {
	sent, accepted []domain.Request
	err            error
}

func (n *fakeNotifier) RequestSent(_ context.Context, r domain.Request) error {
	n.sent = append(n.sent, r)
	return n.err
}

func (n *fakeNotifier) RequestAccepted(_ context.Context, r domain.Request) error {
	n.accepted = append(n.accepted, r)
	return n.err
}
