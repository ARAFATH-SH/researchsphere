package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/domain"
)

// fakeBus delivers what is published back to the hub, as Redis would, and only for
// subscribed users.
type fakeBus struct {
	mu         sync.Mutex
	subscribed map[string]bool
	calls      []string
	sent       []domain.Delivery
	failSub    error
	ch         chan domain.Delivery
}

func newFakeBus() *fakeBus {
	return &fakeBus{subscribed: map[string]bool{}, ch: make(chan domain.Delivery, 256)}
}

func (b *fakeBus) Publish(_ context.Context, userID string, payload []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	d := domain.Delivery{UserID: userID, Payload: payload}
	b.sent = append(b.sent, d)
	if b.subscribed[userID] {
		b.ch <- d
	}
	return nil
}

// published reports whether something containing text was published to the user.
func (b *fakeBus) published(userID, text string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.ContainsFunc(b.sent, func(d domain.Delivery) bool {
		return d.UserID == userID && strings.Contains(string(d.Payload), text)
	})
}

func (b *fakeBus) Subscribe(_ context.Context, userID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failSub != nil {
		return b.failSub
	}
	b.subscribed[userID] = true
	b.calls = append(b.calls, "sub "+userID)
	return nil
}

func (b *fakeBus) Unsubscribe(_ context.Context, userID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subscribed, userID)
	b.calls = append(b.calls, "unsub "+userID)
	return nil
}

func (b *fakeBus) Deliveries() <-chan domain.Delivery { return b.ch }

func (b *fakeBus) isSubscribed(userID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.subscribed[userID]
}

// fakePresence keeps sockets in memory; IDs sort by age, as the hub makes them.
type fakePresence struct {
	mu      sync.Mutex
	sockets map[string][]string
	refresh int
}

func newFakePresence() *fakePresence { return &fakePresence{sockets: map[string][]string{}} }

func (p *fakePresence) Add(_ context.Context, userID, socketID string, _ time.Time) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if slices.Contains(p.sockets[userID], socketID) {
		p.refresh++
	} else {
		p.sockets[userID] = append(p.sockets[userID], socketID)
		slices.Sort(p.sockets[userID])
	}
	return slices.Clone(p.sockets[userID]), nil
}

func (p *fakePresence) Remove(_ context.Context, userID, socketID string, _ time.Time) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sockets[userID] = slices.DeleteFunc(p.sockets[userID], func(s string) bool { return s == socketID })
	return len(p.sockets[userID]), nil
}

func (p *fakePresence) Count(_ context.Context, userID string, _ time.Time) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.sockets[userID]), nil
}

func (p *fakePresence) Online(_ context.Context, userIDs []string, _ time.Time) (map[string]bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := map[string]bool{}
	for _, id := range userIDs {
		out[id] = len(p.sockets[id]) > 0
	}
	return out, nil
}

// fakeAccess: conversations maps a conversation to its pair; a pair listed in blocked is
// refused. Peers come from conversations. groups maps a group to its members, and blocked
// names pairs ("a|b") that hear no typing from each other in a group.
type fakeAccess struct {
	mu            sync.Mutex
	conversations map[string][2]string
	refused       map[string]bool // conversations that are not active, or have a block
	groups        map[string][]string
	blocked       map[string]bool
	suspended     map[string]*domain.Suspension
	checks        int
}

func newFakeAccess() *fakeAccess {
	return &fakeAccess{
		conversations: map[string][2]string{"c1": {"ada", "bob"}},
		refused:       map[string]bool{}, suspended: map[string]*domain.Suspension{},
		groups: map[string][]string{}, blocked: map[string]bool{},
	}
}

func (a *fakeAccess) TypingPeers(_ context.Context, userID, id string) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.checks++
	if members, ok := a.groups[id]; ok {
		if !slices.Contains(members, userID) {
			return nil, nil
		}
		var out []string
		for _, m := range members {
			if m != userID && !a.blocked[userID+"|"+m] && !a.blocked[m+"|"+userID] {
				out = append(out, m)
			}
		}
		return out, nil
	}
	pair, ok := a.conversations[id]
	if !ok || a.refused[id] {
		return nil, nil
	}
	switch userID {
	case pair[0]:
		return []string{pair[1]}, nil
	case pair[1]:
		return []string{pair[0]}, nil
	}
	return nil, nil
}

func (a *fakeAccess) Peers(_ context.Context, userID string) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for id, pair := range a.conversations {
		if a.refused[id] {
			continue
		}
		if pair[0] == userID {
			out = append(out, pair[1])
		}
		if pair[1] == userID {
			out = append(out, pair[0])
		}
	}
	return out, nil
}

func (a *fakeAccess) Visible(ctx context.Context, viewerID string, userIDs []string) ([]string, error) {
	peers, err := a.Peers(ctx, viewerID)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, id := range userIDs {
		if slices.Contains(peers, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

func (a *fakeAccess) Suspension(_ context.Context, userID string) (*domain.Suspension, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.suspended[userID], nil
}

func (a *fakeAccess) checkCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.checks
}

// fakeSocket records frames. A blocked socket hangs in Write until it is closed, as a client
// that stopped reading does.
type fakeSocket struct {
	frames  chan []byte
	closed  chan domain.CloseCode
	once    sync.Once
	block   chan struct{}
	failPin bool
}

func newFakeSocket() *fakeSocket {
	return &fakeSocket{frames: make(chan []byte, 256), closed: make(chan domain.CloseCode, 1)}
}

func newStuckSocket() *fakeSocket {
	s := newFakeSocket()
	s.block = make(chan struct{})
	return s
}

func (s *fakeSocket) Write(ctx context.Context, frame []byte) error {
	if s.block != nil {
		select {
		case <-s.block:
			return errors.New("closed")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.frames <- frame
	return nil
}

func (s *fakeSocket) Ping(context.Context) error {
	if s.failPin {
		return errors.New("no pong")
	}
	return nil
}

func (s *fakeSocket) Close(code domain.CloseCode) error {
	s.once.Do(func() {
		s.closed <- code
		if s.block != nil {
			close(s.block)
		}
	})
	return nil
}

// wait is how long a test waits for something that should happen at once.
const wait = 2 * time.Second

func (s *fakeSocket) next(t *testing.T) domain.Event {
	t.Helper()
	select {
	case f := <-s.frames:
		var raw struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(f, &raw); err != nil {
			t.Fatal(err)
		}
		return domain.Event{Type: raw.Type, Data: string(raw.Data)}
	case <-time.After(wait):
		t.Fatal("no frame arrived")
		return domain.Event{}
	}
}

// silent fails if a frame arrives within d.
func (s *fakeSocket) silent(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case f := <-s.frames:
		t.Fatalf("unexpected frame %s", f)
	case <-time.After(d):
	}
}

func (s *fakeSocket) wantClosed(t *testing.T, code domain.CloseCode) {
	t.Helper()
	select {
	case got := <-s.closed:
		if got != code {
			t.Fatalf("closed with %d, want %d", got, code)
		}
	case <-time.After(wait):
		t.Fatalf("not closed, want %d", code)
	}
}

func (s *fakeSocket) wantOpen(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case got := <-s.closed:
		t.Fatalf("closed with %d", got)
	case <-time.After(d):
	}
}
