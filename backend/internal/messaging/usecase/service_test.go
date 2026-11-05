package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

type harness struct {
	svc     *Service
	store   *fakeStore
	conns   fakeConnections
	blocks  *fakeBlocks
	limiter *fakeLimiter
	pub     *fakePublisher
	client  int
}

var testLimits = Limits{MessagesPerMinute: 30, MessagesPerDay: 1000, RequestsPerMinute: 5, RequestsPerDay: 20}

func newHarness() *harness {
	h := &harness{store: newFakeStore(), conns: fakeConnections{}, blocks: newFakeBlocks(), limiter: newFakeLimiter(), pub: &fakePublisher{}}
	h.svc = New(h.store, fakeTx{h.store}, h.conns, h.blocks, h.limiter, h.pub, testLimits, slog.New(slog.DiscardHandler))
	return h
}

// noLimits replaces the limits with ones a test that sends many messages cannot reach.
func (h *harness) noLimits() {
	h.svc = New(h.store, fakeTx{h.store}, h.conns, h.blocks, h.limiter, h.pub,
		Limits{MessagesPerMinute: 1 << 20, MessagesPerDay: 1 << 20, RequestsPerMinute: 1 << 20, RequestsPerDay: 1 << 20},
		slog.New(slog.DiscardHandler))
}

func (h *harness) connect(a, b string) { h.conns[a+"|"+b] = true }

func (h *harness) clientID() string {
	h.client++
	return fmt.Sprintf("client-%d", h.client)
}

func (h *harness) direct(t *testing.T, from, to, body string) domain.Sent {
	t.Helper()
	s, err := h.svc.SendDirect(t.Context(), from, to, body, h.clientID())
	if err != nil {
		t.Fatalf("SendDirect(%s, %s): %v", from, to, err)
	}
	return s
}

// FR-16.1: connected users get an active conversation.
func TestSendDirectToConnectionIsActive(t *testing.T) {
	h := newHarness()
	h.connect("ada", "bob")
	s := h.direct(t, "ada", "bob", "  hello\nthere \n")
	if s.State != domain.StateActive || s.Replayed || s.Message.Body != "hello\nthere" {
		t.Fatalf("got %+v", s)
	}
	if c := h.store.pair("ada", "bob"); c.requestedBy != nil {
		t.Errorf("requestedBy = %v, want nil", *c.requestedBy)
	}
	// Both can go on, in either form, and only one conversation exists.
	h.direct(t, "bob", "ada", "hi")
	h.direct(t, "ada", "bob", "again")
	if _, err := h.svc.Send(t.Context(), "bob", s.Message.ConversationID, "by id", h.clientID()); err != nil {
		t.Fatal(err)
	}
	if n := len(h.store.conversations); n != 1 {
		t.Errorf("%d conversations, want 1", n)
	}
	if n := len(h.store.messages); n != 4 {
		t.Errorf("%d messages, want 4", n)
	}
}

// FR-16.2: the first message to someone who is not a connection is a request; nobody can
// send until the receiver answers.
func TestRequestFlow(t *testing.T) {
	ctx := context.Background()

	start := func(t *testing.T) (*harness, string) {
		h := newHarness()
		s := h.direct(t, "ada", "bob", "may I?")
		if s.State != domain.StateRequest {
			t.Fatalf("state = %s", s.State)
		}
		return h, s.Message.ConversationID
	}

	t.Run("nobody can send while pending", func(t *testing.T) {
		h, id := start(t)
		for _, tc := range []struct {
			name string
			send func() (domain.Sent, error)
		}{
			{"sender, direct", func() (domain.Sent, error) { return h.svc.SendDirect(ctx, "ada", "bob", "x", h.clientID()) }},
			{"sender, by id", func() (domain.Sent, error) { return h.svc.Send(ctx, "ada", id, "x", h.clientID()) }},
			{"receiver, direct", func() (domain.Sent, error) { return h.svc.SendDirect(ctx, "bob", "ada", "x", h.clientID()) }},
			{"receiver, by id", func() (domain.Sent, error) { return h.svc.Send(ctx, "bob", id, "x", h.clientID()) }},
		} {
			if _, err := tc.send(); !errors.Is(err, domain.ErrRequestPending) {
				t.Errorf("%s: err = %v, want ErrRequestPending", tc.name, err)
			}
		}
		if len(h.store.messages) != 1 {
			t.Errorf("%d messages stored", len(h.store.messages))
		}
	})

	t.Run("accept opens it", func(t *testing.T) {
		h, id := start(t)
		it, err := h.svc.Accept(ctx, "bob", id)
		if err != nil || it.State != domain.StateActive {
			t.Fatalf("Accept: %+v, %v", it, err)
		}
		if !slices.Contains(h.pub.events, "accepted "+id+" to [ada]") {
			t.Errorf("events = %v", h.pub.events)
		}
		h.direct(t, "ada", "bob", "thanks")
		h.direct(t, "bob", "ada", "welcome")
	})

	t.Run("decline closes it for both", func(t *testing.T) {
		h, id := start(t)
		it, err := h.svc.Decline(ctx, "bob", id)
		if err != nil || it.State != domain.StateDeclined {
			t.Fatalf("Decline: %+v, %v", it, err)
		}
		if !slices.Contains(h.pub.events, "declined "+id+" to [ada]") {
			t.Errorf("events = %v", h.pub.events)
		}
		if _, err := h.svc.SendDirect(ctx, "ada", "bob", "x", h.clientID()); !errors.Is(err, domain.ErrClosed) {
			t.Errorf("sender: %v", err)
		}
		if _, err := h.svc.Send(ctx, "bob", id, "x", h.clientID()); !errors.Is(err, domain.ErrClosed) {
			t.Errorf("receiver: %v", err)
		}
	})

	t.Run("only the receiver of a pending request can answer", func(t *testing.T) {
		h, id := start(t)
		for _, tc := range []struct {
			name, user string
			answer     func(context.Context, string, string) (domain.Item, error)
		}{
			{"sender accepts", "ada", h.svc.Accept},
			{"sender declines", "ada", h.svc.Decline},
			{"stranger accepts", "eve", h.svc.Accept},
			{"stranger declines", "eve", h.svc.Decline},
		} {
			if _, err := tc.answer(ctx, tc.user, id); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("%s: %v", tc.name, err)
			}
		}
		if _, err := h.svc.Accept(ctx, "bob", id); err != nil {
			t.Fatal(err)
		}
		if _, err := h.svc.Accept(ctx, "bob", id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("second accept: %v", err)
		}
		if _, err := h.svc.Decline(ctx, "bob", id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("decline after accept: %v", err)
		}
	})

	t.Run("a request from a sender now suspended cannot be answered", func(t *testing.T) {
		h, id := start(t)
		h.store.hidden["ada"] = true
		if _, err := h.svc.Accept(ctx, "bob", id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("accept: %v", err)
		}
		if n, _ := h.svc.UnreadCounts(ctx, "bob"); n.Requests != 0 {
			t.Errorf("requests = %d", n.Requests)
		}
		if p, _ := h.svc.List(ctx, "bob", "requests", "", nil); len(p.Items) != 0 {
			t.Errorf("requests box = %v", p.Items)
		}
	})
}

// Connecting turns a request into a chat; a declined conversation stays closed.
func TestConnectionMade(t *testing.T) {
	ctx := context.Background()

	t.Run("activates a request", func(t *testing.T) {
		h := newHarness()
		h.direct(t, "ada", "bob", "may I?")
		if err := h.svc.ConnectionMade(ctx, "bob", "ada"); err != nil {
			t.Fatal(err)
		}
		if s := h.direct(t, "ada", "bob", "connected now"); s.State != domain.StateActive {
			t.Errorf("state = %s", s.State)
		}
	})

	t.Run("leaves a declined conversation closed", func(t *testing.T) {
		h := newHarness()
		id := h.direct(t, "ada", "bob", "may I?").Message.ConversationID
		if _, err := h.svc.Decline(ctx, "bob", id); err != nil {
			t.Fatal(err)
		}
		h.connect("ada", "bob")
		if err := h.svc.ConnectionMade(ctx, "ada", "bob"); err != nil {
			t.Fatal(err)
		}
		if _, err := h.svc.SendDirect(ctx, "ada", "bob", "x", h.clientID()); !errors.Is(err, domain.ErrClosed) {
			t.Errorf("err = %v, want ErrClosed", err)
		}
	})

	t.Run("does nothing for a pair with no conversation", func(t *testing.T) {
		h := newHarness()
		if err := h.svc.ConnectionMade(ctx, "ada", "bob"); err != nil {
			t.Fatal(err)
		}
		if len(h.store.conversations) != 0 {
			t.Error("a conversation appeared")
		}
	})

	// A connection accepted while the request was being created misses ConnectionMade; the
	// next send finds the two connected and opens the conversation.
	t.Run("a send heals a request between connected users", func(t *testing.T) {
		h := newHarness()
		s := h.direct(t, "ada", "bob", "may I?")
		h.connect("ada", "bob")
		if got := h.direct(t, "bob", "ada", "yes"); got.State != domain.StateActive {
			t.Errorf("state = %s", got.State)
		}
		if c := h.store.conversations[s.Message.ConversationID]; c.state != domain.StateActive {
			t.Errorf("stored state = %s", c.state)
		}
	})

	// Decided with the developer: removing a connection does not close a conversation.
	t.Run("an active conversation survives a removed connection", func(t *testing.T) {
		h := newHarness()
		h.connect("ada", "bob")
		h.direct(t, "ada", "bob", "hi")
		delete(h.conns, "ada|bob")
		if s := h.direct(t, "ada", "bob", "still here"); s.State != domain.StateActive {
			t.Errorf("state = %s", s.State)
		}
	})
}

func TestSendRefusals(t *testing.T) {
	ctx := context.Background()
	long := strings.Repeat("é", 5000)

	t.Run("body", func(t *testing.T) {
		for _, tc := range []struct {
			name, body string
			want       error
		}{
			{"empty", "", domain.ErrInvalidBody},
			{"only whitespace", " \n\t ", domain.ErrInvalidBody},
			{"5001 characters", long + "é", domain.ErrInvalidBody},
			{"NUL", "a\x00b", domain.ErrInvalidBody},
			{"not UTF-8", "a\xffb", domain.ErrInvalidBody},
			{"5000 characters", long, nil},
			{"5000 after trimming", "  " + long + "\n", nil},
			{"one character", "a", nil},
		} {
			t.Run(tc.name, func(t *testing.T) {
				h := newHarness()
				h.connect("ada", "bob")
				if _, err := h.svc.SendDirect(ctx, "ada", "bob", tc.body, "c1"); !errors.Is(err, tc.want) {
					t.Errorf("err = %v, want %v", err, tc.want)
				}
			})
		}
	})

	t.Run("oneself", func(t *testing.T) {
		h := newHarness()
		if _, err := h.svc.SendDirect(ctx, "ada", "ada", "hi", "c1"); !errors.Is(err, domain.ErrSelf) {
			t.Errorf("err = %v", err)
		}
	})

	for _, who := range []string{"ghost", "hidden"} {
		t.Run("unknown or suspended receiver: "+who, func(t *testing.T) {
			h := newHarness()
			h.store.hidden["hidden"] = true
			if _, err := h.svc.SendDirect(ctx, "ada", who, "hi", "c1"); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("err = %v", err)
			}
			if len(h.store.conversations) != 0 {
				t.Error("a conversation was stored")
			}
		})
	}

	t.Run("a refused send leaves no conversation behind", func(t *testing.T) {
		h := newHarness()
		h.store.failAdd = errors.New("boom")
		if _, err := h.svc.SendDirect(ctx, "ada", "bob", "hi", "c1"); err == nil {
			t.Fatal("no error")
		}
		if len(h.store.conversations) != 0 {
			t.Error("the conversation outlived its first message")
		}
		if h.limiter.used["messages:minute|ada"] != 0 || len(h.pub.events) != 0 {
			t.Errorf("counted %v, published %v", h.limiter.used, h.pub.events)
		}
	})
}

// NFR-14: every read and write checks membership; a stranger cannot tell a conversation
// exists. Administrators are not a special case here: the usecase knows no roles.
func TestNonMemberGetsNotFound(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	h.connect("ada", "bob")
	s := h.direct(t, "ada", "bob", "private")
	id := s.Message.ConversationID
	calls := map[string]func() error{
		"Send":     func() error { _, err := h.svc.Send(ctx, "eve", id, "hi", "c9"); return err },
		"Get":      func() error { _, err := h.svc.Get(ctx, "eve", id); return err },
		"Messages": func() error { _, err := h.svc.Messages(ctx, "eve", id, "", nil); return err },
		"MarkRead": func() error { return h.svc.MarkRead(ctx, "eve", id, s.Message.ID) },
		"Accept":   func() error { _, err := h.svc.Accept(ctx, "eve", id); return err },
		"Decline":  func() error { _, err := h.svc.Decline(ctx, "eve", id); return err },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("err = %v, want ErrNotFound", err)
			}
		})
	}
	// Unknown IDs get the same answer.
	if _, err := h.svc.Get(ctx, "ada", "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown ID: %v", err)
	}
	// The check comes before the client ID: a stranger's replay reveals nothing.
	if _, err := h.svc.Send(ctx, "eve", id, "hi", s.Message.ClientID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("replay by a stranger: %v", err)
	}
}

// Decided with the developer: a conversation with a suspended member stays readable, but
// nobody can send in it.
func TestSuspendedMemberIsReadOnly(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	h.connect("ada", "bob")
	s := h.direct(t, "bob", "ada", "hi")
	id := s.Message.ConversationID
	h.store.hidden["bob"] = true

	if _, err := h.svc.Send(ctx, "ada", id, "hello?", h.clientID()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Send: %v", err)
	}
	if _, err := h.svc.SendDirect(ctx, "ada", "bob", "hello?", h.clientID()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("SendDirect: %v", err)
	}
	it, err := h.svc.Get(ctx, "ada", id)
	if err != nil || it.Other.Available {
		t.Errorf("Get: %+v, %v", it, err)
	}
	p, err := h.svc.List(ctx, "ada", "", "", nil)
	if err != nil || len(p.Items) != 1 {
		t.Errorf("List: %+v, %v", p, err)
	}
	if m, err := h.svc.Messages(ctx, "ada", id, "", nil); err != nil || len(m.Items) != 1 {
		t.Errorf("Messages: %+v, %v", m, err)
	}
	if err := h.svc.MarkRead(ctx, "ada", id, s.Message.ID); err != nil {
		t.Errorf("MarkRead: %v", err)
	}
}

func TestIdempotentClientID(t *testing.T) {
	ctx := context.Background()

	t.Run("a repeat returns the original", func(t *testing.T) {
		h := newHarness()
		h.connect("ada", "bob")
		first, err := h.svc.SendDirect(ctx, "ada", "bob", "hi", "c1")
		if err != nil {
			t.Fatal(err)
		}
		again, err := h.svc.SendDirect(ctx, "ada", "bob", "different text", "c1")
		if err != nil || !again.Replayed || again.Message.ID != first.Message.ID || again.Message.Body != "hi" {
			t.Fatalf("got %+v, %v", again, err)
		}
		byID, err := h.svc.Send(ctx, "ada", first.Message.ConversationID, "hi", "c1")
		if err != nil || !byID.Replayed || byID.Message.ID != first.Message.ID {
			t.Fatalf("by ID: %+v, %v", byID, err)
		}
		if len(h.store.messages) != 1 {
			t.Errorf("%d messages", len(h.store.messages))
		}
		if h.limiter.used["messages:minute|ada"] != 1 || len(h.pub.events) != 1 {
			t.Errorf("counted %v, published %v", h.limiter.used, h.pub.events)
		}
	})

	t.Run("another sender may use the same value", func(t *testing.T) {
		h := newHarness()
		h.connect("ada", "bob")
		a, _ := h.svc.SendDirect(ctx, "ada", "bob", "one", "c1")
		b, err := h.svc.SendDirect(ctx, "bob", "ada", "two", "c1")
		if err != nil || b.Replayed || b.Message.ID == a.Message.ID {
			t.Fatalf("got %+v, %v", b, err)
		}
	})

	t.Run("a retry of a request gets its message, not a refusal", func(t *testing.T) {
		h := newHarness()
		first, _ := h.svc.SendDirect(ctx, "ada", "bob", "may I?", "c1")
		again, err := h.svc.SendDirect(ctx, "ada", "bob", "may I?", "c1")
		if err != nil || !again.Replayed || again.State != domain.StateRequest || again.Message.ID != first.Message.ID {
			t.Fatalf("got %+v, %v", again, err)
		}
	})

	t.Run("a replay comes before the limits and the state", func(t *testing.T) {
		h := newHarness()
		first, _ := h.svc.SendDirect(ctx, "ada", "bob", "may I?", "c1")
		if _, err := h.svc.Decline(ctx, "bob", first.Message.ConversationID); err != nil {
			t.Fatal(err)
		}
		h.limiter.used["messages:minute|ada"] = 30
		again, err := h.svc.SendDirect(ctx, "ada", "bob", "may I?", "c1")
		if err != nil || !again.Replayed || again.State != domain.StateDeclined {
			t.Fatalf("got %+v, %v", again, err)
		}
	})

	t.Run("losing a race for the client ID rolls back and returns the winner", func(t *testing.T) {
		h := newHarness()
		h.connect("ada", "carol")
		h.connect("ada", "bob")
		winner, _ := h.svc.SendDirect(ctx, "ada", "carol", "first", "c1")
		// A second send got past the lookup before the first committed.
		found := h.store.messages
		h.store.messages = nil
		lookups := &racingFind{fakeStore: h.store, hidden: true, saved: found}
		svc := New(lookups, fakeTx{h.store}, h.conns, h.blocks, h.limiter, h.pub, testLimits, slog.New(slog.DiscardHandler))
		h.store.duplicateAdd = true
		got, err := svc.SendDirect(ctx, "ada", "bob", "second", "c1")
		if err != nil || !got.Replayed || got.Message.ID != winner.Message.ID {
			t.Fatalf("got %+v, %v", got, err)
		}
		if h.store.pair("ada", "bob") != nil {
			t.Error("the losing send left an empty conversation")
		}
		if h.limiter.used["messages:minute|ada"] != 1 {
			t.Errorf("counted %v", h.limiter.used)
		}
	})

	t.Run("a retry that races its own first message gets it, not a refusal", func(t *testing.T) {
		h := newHarness()
		winner, _ := h.svc.SendDirect(ctx, "ada", "bob", "may I?", "c1")
		// The retry got past the lookup before the first send committed, then found the
		// request it had started itself.
		lookups := &racingFind{fakeStore: h.store, hidden: true, saved: h.store.messages}
		svc := New(lookups, fakeTx{h.store}, h.conns, h.blocks, h.limiter, h.pub, testLimits, slog.New(slog.DiscardHandler))
		got, err := svc.SendDirect(ctx, "ada", "bob", "may I?", "c1")
		if err != nil || !got.Replayed || got.Message.ID != winner.Message.ID {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
}

// racingFind hides the racing send's message from the first lookup only.
type racingFind struct {
	*fakeStore
	hidden bool
	saved  []domain.Message
}

func (r *racingFind) FindSent(ctx context.Context, senderID, clientID string) (domain.Sent, bool, error) {
	if r.hidden {
		r.hidden = false
		return domain.Sent{}, false, nil
	}
	r.messages = r.saved
	return r.fakeStore.FindSent(ctx, senderID, clientID)
}

// FR-16.10.
func TestRateLimits(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name, window string
		used         int
		left         time.Duration
		toStranger   bool
	}{
		{"messages per minute", "messages:minute", 30, 40 * time.Second, false},
		{"messages per day", "messages:day", 1000, time.Hour, false},
		{"requests per minute", "requests:minute", 5, 40 * time.Second, true},
		{"requests per day", "requests:day", 20, time.Hour, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness()
			h.connect("ada", "bob")
			to := "bob"
			if tc.toStranger {
				to = "carol"
			}
			h.limiter.used[tc.window+"|ada"] = tc.used - 1
			h.limiter.left[tc.window] = tc.left
			if _, err := h.svc.SendDirect(ctx, "ada", to, "under the limit", "c1"); err != nil {
				t.Fatalf("one below the limit: %v", err)
			}
			if tc.toStranger {
				// A second message to the same person is not a new request.
				if _, err := h.svc.SendDirect(ctx, "ada", to, "x", "c2"); !errors.Is(err, domain.ErrRequestPending) {
					t.Fatalf("second send: %v", err)
				}
				to = "dan"
			}
			_, err := h.svc.SendDirect(ctx, "ada", to, "over", "c3")
			var limited *domain.RateLimitedError
			if !errors.As(err, &limited) || limited.RetryAfter != tc.left {
				t.Fatalf("err = %v, want RetryAfter %s", err, tc.left)
			}
			if tc.toStranger && h.store.pair("ada", to) != nil {
				t.Error("a refused request was stored")
			}
		})
	}

	t.Run("the longest wait wins when two windows are full", func(t *testing.T) {
		h := newHarness()
		h.connect("ada", "bob")
		h.limiter.used["messages:minute|ada"] = 30
		h.limiter.left["messages:minute"] = 10 * time.Second
		h.limiter.used["messages:day|ada"] = 1000
		h.limiter.left["messages:day"] = time.Hour
		_, err := h.svc.SendDirect(ctx, "ada", "bob", "x", "c1")
		var limited *domain.RateLimitedError
		if !errors.As(err, &limited) || limited.RetryAfter != time.Hour {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("the limit applies in a conversation too", func(t *testing.T) {
		h := newHarness()
		h.connect("ada", "bob")
		id := h.direct(t, "ada", "bob", "hi").Message.ConversationID
		h.limiter.used["messages:minute|ada"] = 30
		var limited *domain.RateLimitedError
		if _, err := h.svc.Send(ctx, "ada", id, "x", h.clientID()); !errors.As(err, &limited) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("a first message counts against both limits", func(t *testing.T) {
		h := newHarness()
		h.direct(t, "ada", "bob", "may I?")
		for _, w := range []string{"messages:minute", "messages:day", "requests:minute", "requests:day"} {
			if h.limiter.used[w+"|ada"] != 1 {
				t.Errorf("%s = %d", w, h.limiter.used[w+"|ada"])
			}
		}
	})

	t.Run("a message to a connection is not a request", func(t *testing.T) {
		h := newHarness()
		h.connect("ada", "bob")
		h.direct(t, "ada", "bob", "hi")
		if n := h.limiter.used["requests:minute|ada"]; n != 0 {
			t.Errorf("requests counted: %d", n)
		}
	})

	t.Run("a refused send uses no quota", func(t *testing.T) {
		h := newHarness()
		h.direct(t, "ada", "bob", "may I?")
		before := h.limiter.used["messages:minute|ada"]
		if _, err := h.svc.SendDirect(ctx, "ada", "bob", "again", "c9"); !errors.Is(err, domain.ErrRequestPending) {
			t.Fatal(err)
		}
		if h.limiter.used["messages:minute|ada"] != before {
			t.Error("a refused send was counted")
		}
	})

	t.Run("limits are per user", func(t *testing.T) {
		h := newHarness()
		h.connect("ada", "bob")
		h.limiter.used["messages:minute|ada"] = 30
		h.direct(t, "bob", "ada", "fine")
	})

	t.Run("sends go through when the limiter is down", func(t *testing.T) {
		h := newHarness()
		h.limiter.failed = true
		h.direct(t, "ada", "bob", "may I?")
	})

	t.Run("the numbers come from the configuration", func(t *testing.T) {
		h := newHarness()
		h.connect("ada", "bob")
		h.svc = New(h.store, fakeTx{h.store}, h.conns, h.blocks, h.limiter, h.pub,
			Limits{MessagesPerMinute: 1, MessagesPerDay: 1000, RequestsPerMinute: 5, RequestsPerDay: 20}, slog.New(slog.DiscardHandler))
		h.direct(t, "ada", "bob", "one")
		var limited *domain.RateLimitedError
		if _, err := h.svc.SendDirect(ctx, "ada", "bob", "two", "c2"); !errors.As(err, &limited) {
			t.Fatalf("err = %v", err)
		}
	})
}

// FR-16.3.
func TestReadMarkers(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	h.connect("ada", "bob")
	first := h.direct(t, "ada", "bob", "one").Message
	second := h.direct(t, "ada", "bob", "two").Message
	id := first.ConversationID
	h.pub.events = nil

	if n, _ := h.svc.UnreadCounts(ctx, "bob"); n.Total != 2 {
		t.Fatalf("unread = %d", n.Total)
	}
	if n, _ := h.svc.UnreadCounts(ctx, "ada"); n.Total != 0 {
		t.Errorf("the sender's own messages are unread: %d", n.Total)
	}

	if err := h.svc.MarkRead(ctx, "bob", id, second.ID); err != nil {
		t.Fatal(err)
	}
	want := "read bob " + second.ID + " to [ada]"
	if !slices.Equal(h.pub.events, []string{want}) {
		t.Errorf("events = %v", h.pub.events)
	}
	if n, _ := h.svc.UnreadCounts(ctx, "bob"); n.Total != 0 {
		t.Errorf("unread after read = %d", n.Total)
	}

	// Backward and repeated calls succeed and change nothing.
	h.pub.events = nil
	for _, m := range []string{first.ID, second.ID} {
		if err := h.svc.MarkRead(ctx, "bob", id, m); err != nil {
			t.Fatal(err)
		}
	}
	if got := *h.mustMessages(t, "ada", id).OtherReadID; got != second.ID {
		t.Errorf("marker = %s, want %s", got, second.ID)
	}
	if len(h.pub.events) != 0 {
		t.Errorf("an unmoved marker was published: %v", h.pub.events)
	}

	// A message of another conversation is not found.
	h.connect("bob", "carol")
	other := h.direct(t, "carol", "bob", "elsewhere").Message
	if err := h.svc.MarkRead(ctx, "bob", id, other.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign message: %v", err)
	}
}

func (h *harness) mustMessages(t *testing.T, user, id string) domain.MessagePage {
	t.Helper()
	p, err := h.svc.Messages(t.Context(), user, id, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUnreadCountsSeparateRequests(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	h.connect("ada", "bob")
	h.direct(t, "ada", "bob", "hi")
	h.direct(t, "carol", "bob", "may I?")
	h.direct(t, "bob", "dan", "may I?")
	n, err := h.svc.UnreadCounts(ctx, "bob")
	if err != nil || n.Total != 1 || n.Requests != 1 {
		t.Errorf("got %+v, %v; want total 1, requests 1", n, err)
	}
}

func TestPublishesSentMessage(t *testing.T) {
	h := newHarness()
	h.connect("ada", "bob")
	m := h.direct(t, "ada", "bob", "hi").Message
	if want := []string{"sent " + m.ID + " to [bob]"}; !slices.Equal(h.pub.events, want) {
		t.Errorf("events = %v, want %v", h.pub.events, want)
	}
}

func TestListBoxes(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	h.connect("ada", "bob")
	active := h.direct(t, "ada", "bob", "hi").Message.ConversationID
	sent := h.direct(t, "ada", "carol", "may I?").Message.ConversationID
	received := h.direct(t, "dan", "ada", "may I?").Message.ConversationID
	declined := h.direct(t, "erin", "ada", "may I?").Message.ConversationID
	if _, err := h.svc.Decline(ctx, "ada", declined); err != nil {
		t.Fatal(err)
	}

	ids := func(box string) []string {
		p, err := h.svc.List(ctx, "ada", box, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, it := range p.Items {
			out = append(out, it.ID)
		}
		slices.Sort(out)
		return out
	}
	want := []string{active, sent}
	slices.Sort(want)
	if got := ids("inbox"); !slices.Equal(got, want) {
		t.Errorf("inbox = %v, want %v", got, want)
	}
	if got := ids(""); !slices.Equal(got, want) {
		t.Errorf("default box = %v, want %v", got, want)
	}
	if got := ids("requests"); !slices.Equal(got, []string{received}) {
		t.Errorf("requests = %v, want %v", got, []string{received})
	}
	if _, err := h.svc.List(ctx, "ada", "archive", "", nil); !errors.Is(err, domain.ErrInvalidBox) {
		t.Errorf("unknown box: %v", err)
	}
}

func TestListPaging(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	for i := range 5 {
		other := fmt.Sprintf("u%d", i)
		h.connect("ada", other)
		h.direct(t, "ada", other, "hi")
	}
	limit := 2
	seen := map[string]bool{}
	cursor := ""
	for page := 0; ; page++ {
		p, err := h.svc.List(ctx, "ada", "inbox", cursor, &limit)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range p.Items {
			seen[it.ID] = true
		}
		if p.NextCursor == nil {
			if page != 2 {
				t.Errorf("ended after page %d", page)
			}
			break
		}
		cursor = *p.NextCursor
	}
	if len(seen) != 5 {
		t.Errorf("saw %d conversations", len(seen))
	}
	if _, err := h.svc.List(ctx, "ada", "", "not-a-cursor", nil); !errors.Is(err, domain.ErrInvalidCursor) {
		t.Errorf("bad cursor: %v", err)
	}
	zero := 0
	if _, err := h.svc.List(ctx, "ada", "", "", &zero); !errors.Is(err, domain.ErrInvalidLimit) {
		t.Errorf("limit 0: %v", err)
	}
}

func TestMessagesPaging(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	h.noLimits()
	h.connect("ada", "bob")
	var id string
	for i := range 35 {
		id = h.direct(t, "ada", "bob", fmt.Sprintf("m%d", i)).Message.ConversationID
	}

	p, err := h.svc.Messages(ctx, "ada", id, "", nil)
	if err != nil || len(p.Items) != DefaultMessageLimit || p.NextBefore == nil {
		t.Fatalf("first page: %d items, next %v, %v", len(p.Items), p.NextBefore, err)
	}
	if p.Items[0].Body != "m34" || p.Items[29].Body != "m5" {
		t.Errorf("not newest first: %s .. %s", p.Items[0].Body, p.Items[29].Body)
	}
	last, err := h.svc.Messages(ctx, "ada", id, *p.NextBefore, nil)
	if err != nil || len(last.Items) != 5 || last.NextBefore != nil || last.Items[0].Body != "m4" {
		t.Fatalf("second page: %d items, next %v, %v", len(last.Items), last.NextBefore, err)
	}
}

func TestMessagesLimit(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	h.noLimits()
	h.connect("ada", "bob")
	var id string
	for range 105 {
		id = h.direct(t, "ada", "bob", "x").Message.ConversationID
	}
	big := 500
	p, err := h.svc.Messages(ctx, "ada", id, "", &big)
	if err != nil || len(p.Items) != MaxMessageLimit {
		t.Errorf("%d items, %v; want the cap %d", len(p.Items), err, MaxMessageLimit)
	}
	zero := 0
	if _, err := h.svc.Messages(ctx, "ada", id, "", &zero); !errors.Is(err, domain.ErrInvalidLimit) {
		t.Errorf("limit 0: %v", err)
	}
	if _, err := h.svc.Messages(ctx, "ada", id, "not-a-uuid", nil); !errors.Is(err, domain.ErrInvalidCursor) {
		t.Errorf("bad before: %v", err)
	}
}

func TestCursorRoundTrip(t *testing.T) {
	c := domain.Cursor{LastMessageAt: time.Date(2026, 11, 4, 10, 0, 0, 123456000, time.UTC), ID: "3b1f6c2e-9a4d-4e8b-b7c1-2d3e4f5a6b7c"}
	got, err := DecodeCursor(EncodeCursor(c))
	if err != nil || !got.LastMessageAt.Equal(c.LastMessageAt) || got.ID != c.ID {
		t.Errorf("got %+v, %v", got, err)
	}
	for _, bad := range []string{"", "!!!", "bm8tc2VwYXJhdG9y"} {
		if _, err := DecodeCursor(bad); !errors.Is(err, domain.ErrInvalidCursor) {
			t.Errorf("DecodeCursor(%q) = %v", bad, err)
		}
	}
}

// FR-16.9: both directions, with and without a conversation.
func TestBlockRefusesSends(t *testing.T) {
	const blocker, blocked = "ada", "bob"
	h := newHarness()
	h.connect(blocker, blocked)
	id := h.direct(t, blocker, blocked, "hello").Message.ConversationID
	h.blocks.set[blocker+"|"+blocked] = true

	tests := []struct {
		name string
		send func() error
		want error
	}{
		{"blocked, in the conversation", func() error { _, err := h.svc.Send(t.Context(), blocked, id, "hi", h.clientID()); return err }, domain.ErrCannotMessage},
		{"blocked, direct", func() error {
			_, err := h.svc.SendDirect(t.Context(), blocked, blocker, "hi", h.clientID())
			return err
		}, domain.ErrCannotMessage},
		{"blocker, in the conversation", func() error { _, err := h.svc.Send(t.Context(), blocker, id, "hi", h.clientID()); return err }, domain.ErrYouBlocked},
		{"blocker, direct", func() error {
			_, err := h.svc.SendDirect(t.Context(), blocker, blocked, "hi", h.clientID())
			return err
		}, domain.ErrYouBlocked},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.send(); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
	if len(h.store.messages) != 1 {
		t.Errorf("%d messages stored, want 1", len(h.store.messages))
	}
}

// A first message from a blocked person gets the 404 of an unknown user, so it does not
// reveal the block; the blocker is told. Nothing is created or counted.
func TestBlockWithoutConversation(t *testing.T) {
	h := newHarness()
	h.blocks.set["ada|bob"] = true
	if _, err := h.svc.SendDirect(t.Context(), "bob", "ada", "hi", h.clientID()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("blocked person: %v", err)
	}
	if _, err := h.svc.SendDirect(t.Context(), "ada", "bob", "hi", h.clientID()); !errors.Is(err, domain.ErrYouBlocked) {
		t.Errorf("blocker: %v", err)
	}
	h.blocks.set["bob|ada"] = true
	if _, err := h.svc.SendDirect(t.Context(), "bob", "ada", "hi", h.clientID()); !errors.Is(err, domain.ErrYouBlocked) {
		t.Errorf("blocked both ways: %v", err)
	}
	if len(h.store.conversations) != 0 || len(h.limiter.used) != 0 {
		t.Errorf("conversations %v, counted %v", h.store.conversations, h.limiter.used)
	}
}

// A block that commits while a send waits for the pair's lock refuses that send, and the
// conversation a first message would have created is not left behind.
func TestBlockCommittedWhileSendWaits(t *testing.T) {
	h := newHarness()
	h.blocks.onLock = func() { h.blocks.set["ada|bob"] = true }
	if _, err := h.svc.SendDirect(t.Context(), "bob", "ada", "hi", h.clientID()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v", err)
	}
	if h.blocks.locks != 1 || len(h.store.conversations) != 0 || len(h.store.messages) != 0 {
		t.Errorf("locks %d, conversations %d, messages %d", h.blocks.locks, len(h.store.conversations), len(h.store.messages))
	}
}

// A retry of a message stored before the block still returns the original (14A.1).
func TestBlockKeepsReplay(t *testing.T) {
	h := newHarness()
	h.connect("ada", "bob")
	sent, err := h.svc.SendDirect(t.Context(), "bob", "ada", "hi", "retry")
	if err != nil {
		t.Fatal(err)
	}
	h.blocks.set["ada|bob"] = true
	again, err := h.svc.SendDirect(t.Context(), "bob", "ada", "hi", "retry")
	if err != nil || !again.Replayed || again.Message.ID != sent.Message.ID {
		t.Errorf("retry: %+v, %v", again, err)
	}
}

// FR-16.9: a block declines a pending request, which leaves the boxes; an active conversation
// stays.
func TestDeclineRequests(t *testing.T) {
	h := newHarness()
	h.direct(t, "ada", "bob", "may I?")
	if err := h.svc.DeclineRequests(t.Context(), "bob", "ada"); err != nil {
		t.Fatal(err)
	}
	if s := h.store.pair("ada", "bob").state; s != domain.StateDeclined {
		t.Errorf("state = %s", s)
	}
	h.connect("ada", "cy")
	h.direct(t, "ada", "cy", "hello")
	if err := h.svc.DeclineRequests(t.Context(), "ada", "cy"); err != nil {
		t.Fatal(err)
	}
	if s := h.store.pair("ada", "cy").state; s != domain.StateActive {
		t.Errorf("active conversation became %s", s)
	}
}

func TestCanMessage(t *testing.T) {
	ok := domain.Item{Conversation: domain.Conversation{State: domain.StateActive}, Other: domain.Person{Available: true}}
	if !ok.CanMessage() {
		t.Error("an active conversation refuses")
	}
	tests := map[string]func(*domain.Item){
		"blocked by me":   func(i *domain.Item) { i.BlockedByMe = true },
		"blocked me":      func(i *domain.Item) { i.BlockedMe = true },
		"unavailable":     func(i *domain.Item) { i.Other.Available = false },
		"pending request": func(i *domain.Item) { i.State = domain.StateRequest },
		"declined":        func(i *domain.Item) { i.State = domain.StateDeclined },
	}
	for name, change := range tests {
		it := ok
		change(&it)
		if it.CanMessage() {
			t.Errorf("%s: can message", name)
		}
	}
}
