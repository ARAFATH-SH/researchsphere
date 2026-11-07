package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/domain"
)

type env struct {
	hub      *Hub
	bus      *fakeBus
	presence *fakePresence
	access   *fakeAccess
	nowNanos *atomic.Int64
}

func testConfig() Config {
	c := DefaultConfig()
	c.PingInterval = time.Hour
	c.OfflineGrace = 20 * time.Millisecond
	c.TypingThrottle = time.Hour // tests move the clock instead
	c.TypingTimeout = 40 * time.Millisecond
	return c
}

func newEnv(t *testing.T, change ...func(*Config)) *env {
	t.Helper()
	cfg := testConfig()
	for _, f := range change {
		f(&cfg)
	}
	e := &env{bus: newFakeBus(), presence: newFakePresence(), access: newFakeAccess(), nowNanos: &atomic.Int64{}}
	e.nowNanos.Store(time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC).UnixNano())
	e.hub = NewHub(e.bus, e.presence, e.access, cfg, func() time.Time { return time.Unix(0, e.nowNanos.Load()) },
		slog.New(slog.DiscardHandler))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.hub.Run(ctx)
	}()
	t.Cleanup(func() {
		sctx, stop := context.WithTimeout(context.Background(), wait)
		defer stop()
		e.hub.Shutdown(sctx)
		cancel()
		<-done
	})
	return e
}

func (e *env) advance(d time.Duration) { e.nowNanos.Add(int64(d)) }

// connect opens a socket for the user that lasts as long as the test.
func (e *env) connect(t *testing.T, userID string) (*Client, *fakeSocket) {
	t.Helper()
	s := newFakeSocket()
	return e.attach(t, userID, s), s
}

func (e *env) attach(t *testing.T, userID string, s domain.Socket) *Client {
	t.Helper()
	r, err := e.hub.Reserve(t.Context(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return r.Attach(s, time.Unix(0, e.nowNanos.Load()).Add(time.Hour))
}

func TestFanOut(t *testing.T) {
	e := newEnv(t)
	_, bob := e.connect(t, "bob")
	_, ada1 := e.connect(t, "ada")
	_, ada2 := e.connect(t, "ada")
	bob.next(t) // ada's presence

	e.hub.Send(t.Context(), "ada", domain.Event{Type: domain.EventUnreadChanged, Data: map[string]int{"total": 1}})

	for name, s := range map[string]*fakeSocket{"first": ada1, "second": ada2} {
		if ev := s.next(t); ev.Type != domain.EventUnreadChanged || ev.Data != `{"total":1}` {
			t.Errorf("%s socket got %+v", name, ev)
		}
	}
	bob.silent(t, 30*time.Millisecond)
}

// A client that stops reading is disconnected; others keep getting their frames, on time.
func TestSlowClientIsDisconnectedAndBlocksNobody(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.SendBuffer = 2; c.WriteTimeout = time.Hour })
	stuck := newStuckSocket()
	e.attach(t, "ada", stuck)
	_, healthy := e.connect(t, "ada")

	start := time.Now()
	for i := range 20 {
		e.hub.Send(t.Context(), "ada", domain.Event{Type: domain.EventUnreadChanged, Data: i})
		healthy.next(t)
	}
	stuck.wantClosed(t, domain.CloseTryLater)
	if d := time.Since(start); d > wait/2 {
		t.Errorf("the healthy socket waited %s", d)
	}
}

// The sixth socket closes the oldest, with the code that tells the client not to reconnect.
func TestMaxSockets(t *testing.T) {
	e := newEnv(t)
	sockets := make([]*fakeSocket, MaxSocketsPerUser+2)
	for i := range sockets {
		sockets[i] = newFakeSocket()
		e.attach(t, "ada", sockets[i])
		e.advance(time.Millisecond) // socket IDs sort by age, to the millisecond
	}
	sockets[0].wantClosed(t, domain.CloseReplaced)
	sockets[1].wantClosed(t, domain.CloseReplaced)
	for _, s := range sockets[2:] {
		s.wantOpen(t, 10*time.Millisecond)
	}
}

func TestSocketClosesWhenItsTokenExpires(t *testing.T) {
	e := newEnv(t)
	r, err := e.hub.Reserve(t.Context(), "ada")
	if err != nil {
		t.Fatal(err)
	}
	s := newFakeSocket()
	r.Attach(s, time.Unix(0, e.nowNanos.Load()).Add(20*time.Millisecond))
	s.wantClosed(t, domain.CloseTokenExpired)
}

func TestCloseUserReachesEverySocket(t *testing.T) {
	e := newEnv(t)
	_, a1 := e.connect(t, "ada")
	_, a2 := e.connect(t, "ada")
	_, bob := e.connect(t, "bob")
	e.hub.CloseUser(t.Context(), "ada", domain.CloseSessionEnded)
	a1.wantClosed(t, domain.CloseSessionEnded)
	a2.wantClosed(t, domain.CloseSessionEnded)
	bob.wantOpen(t, 20*time.Millisecond)
}

// A session that ends between the subscription and the upgrade still closes the socket.
func TestCloseWhileReservedClosesTheSocket(t *testing.T) {
	e := newEnv(t)
	r, err := e.hub.Reserve(t.Context(), "ada")
	if err != nil {
		t.Fatal(err)
	}
	e.hub.CloseUser(t.Context(), "ada", domain.CloseSessionEnded)
	deadline := time.Now().Add(wait)
	for !r.closedSince() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	s := newFakeSocket()
	r.Attach(s, time.Unix(0, e.nowNanos.Load()).Add(time.Hour))
	s.wantClosed(t, domain.CloseSessionEnded)
}

func TestDeadConnectionIsDropped(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.PingInterval = 5 * time.Millisecond })
	s := newFakeSocket()
	s.failPin = true
	e.attach(t, "ada", s)
	s.wantClosed(t, domain.CloseTryLater)
}

func TestHeartbeatRefreshesPresence(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.PingInterval = 5 * time.Millisecond })
	e.connect(t, "ada")
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		e.presence.mu.Lock()
		n := e.presence.refresh
		e.presence.mu.Unlock()
		if n >= 2 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the heartbeat did not refresh the socket")
}

// The subscription follows the sockets, and a failed one refuses the socket.
func TestSubscription(t *testing.T) {
	e := newEnv(t)
	c, _ := e.connect(t, "ada")
	if !e.bus.isSubscribed("ada") {
		t.Fatal("not subscribed after connecting")
	}
	c.Done()
	deadline := time.Now().Add(wait)
	for e.bus.isSubscribed("ada") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if e.bus.isSubscribed("ada") {
		t.Error("still subscribed with no socket left")
	}

	e.bus.failSub = errors.New("redis is down")
	if _, err := e.hub.Reserve(t.Context(), "bob"); err == nil {
		t.Error("Reserve() = nil with Redis down")
	}
}

func TestTypingIsRelayedToTheOtherMemberOnly(t *testing.T) {
	e := newEnv(t)
	_, bob := e.connect(t, "bob")
	_, eve := e.connect(t, "eve")
	ada, adaSocket := e.connect(t, "ada")
	bob.next(t) // ada's presence

	ada.Receive(t.Context(), []byte(`{"type":"typing.start","data":{"conversationId":"c1"}}`))

	ev := bob.next(t)
	if ev.Type != domain.EventTyping || ev.Data != `{"conversationId":"c1","userId":"ada","isTyping":true}` {
		t.Errorf("got %+v", ev)
	}
	adaSocket.silent(t, 10*time.Millisecond)
	eve.silent(t, 10*time.Millisecond)
}

func TestTypingRefused(t *testing.T) {
	for _, tc := range []struct {
		name    string
		user    string
		id      string
		refused bool
	}{
		{name: "not a member", user: "eve", id: "c1"},
		{name: "unknown conversation", user: "ada", id: "nope"},
		{name: "blocked or not active", user: "ada", id: "c1", refused: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.access.refused["c1"] = tc.refused
			_, bob := e.connect(t, "bob")
			sender, _ := e.connect(t, tc.user)
			if !tc.refused && tc.user == "ada" {
				bob.next(t)
			}
			sender.Receive(t.Context(), fmt.Appendf(nil, `{"type":"typing.start","data":{"conversationId":%q}}`, tc.id))
			bob.silent(t, 30*time.Millisecond)
		})
	}
}

// At most one start per throttle window per conversation, and a refused start costs one
// database read in that window too.
func TestTypingThrottle(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.TypingThrottle = 3 * time.Second; c.TypingTimeout = time.Hour })
	_, bob := e.connect(t, "bob")
	ada, _ := e.connect(t, "ada")
	bob.next(t)
	start := []byte(`{"type":"typing.start","data":{"conversationId":"c1"}}`)

	ada.Receive(t.Context(), start)
	bob.next(t)
	ada.Receive(t.Context(), start)
	bob.silent(t, 20*time.Millisecond)
	if n := e.access.checkCount(); n != 1 {
		t.Errorf("%d access checks, want 1", n)
	}

	e.advance(3 * time.Second)
	ada.Receive(t.Context(), start)
	if ev := bob.next(t); ev.Type != domain.EventTyping {
		t.Errorf("got %+v", ev)
	}
}

func TestTypingAutoStop(t *testing.T) {
	e := newEnv(t)
	_, bob := e.connect(t, "bob")
	ada, _ := e.connect(t, "ada")
	bob.next(t)
	ada.Receive(t.Context(), []byte(`{"type":"typing.start","data":{"conversationId":"c1"}}`))
	bob.next(t)
	ev := bob.next(t)
	if ev.Type != domain.EventTyping || !strings.Contains(ev.Data.(string), `"isTyping":false`) {
		t.Errorf("got %+v", ev)
	}
	bob.silent(t, 80*time.Millisecond)
}

func TestTypingStop(t *testing.T) {
	t.Run("stops at once and cancels the auto-stop", func(t *testing.T) {
		e := newEnv(t, func(c *Config) { c.TypingTimeout = 50 * time.Millisecond })
		_, bob := e.connect(t, "bob")
		ada, _ := e.connect(t, "ada")
		bob.next(t)
		ada.Receive(t.Context(), []byte(`{"type":"typing.start","data":{"conversationId":"c1"}}`))
		bob.next(t)
		ada.Receive(t.Context(), []byte(`{"type":"typing.stop","data":{"conversationId":"c1"}}`))
		if ev := bob.next(t); !strings.Contains(ev.Data.(string), `"isTyping":false`) {
			t.Errorf("got %+v", ev)
		}
		bob.silent(t, 100*time.Millisecond)
	})

	t.Run("without a start it relays nothing", func(t *testing.T) {
		e := newEnv(t)
		_, bob := e.connect(t, "bob")
		ada, _ := e.connect(t, "ada")
		bob.next(t)
		ada.Receive(t.Context(), []byte(`{"type":"typing.stop","data":{"conversationId":"c1"}}`))
		bob.silent(t, 20*time.Millisecond)
	})

	// A block between start and stop refuses the stop, and the auto-stop still clears the
	// indicator it left behind.
	t.Run("refused after a block, the auto-stop clears it", func(t *testing.T) {
		e := newEnv(t)
		_, bob := e.connect(t, "bob")
		ada, _ := e.connect(t, "ada")
		bob.next(t)
		ada.Receive(t.Context(), []byte(`{"type":"typing.start","data":{"conversationId":"c1"}}`))
		bob.next(t)
		e.access.mu.Lock()
		e.access.refused["c1"] = true
		e.access.mu.Unlock()
		ada.Receive(t.Context(), []byte(`{"type":"typing.stop","data":{"conversationId":"c1"}}`))
		if ev := bob.next(t); !strings.Contains(ev.Data.(string), `"isTyping":false`) {
			t.Errorf("got %+v", ev)
		}
	})
}

// Junk is ignored, and the socket stays open.
func TestAnythingElseIsIgnored(t *testing.T) {
	e := newEnv(t)
	_, bob := e.connect(t, "bob")
	ada, s := e.connect(t, "ada")
	bob.next(t)
	for _, f := range []string{`not json`, `{}`, `{"type":"message.send"}`, `{"type":"typing.start"}`, `[]`} {
		ada.Receive(t.Context(), []byte(f))
	}
	bob.silent(t, 20*time.Millisecond)
	s.wantOpen(t, time.Millisecond)
}

// Past 20 events in 10 s a socket's events are ignored, so random conversation IDs cannot
// buy a database read each; the next window starts afresh.
func TestIncomingEventLimit(t *testing.T) {
	e := newEnv(t)
	ada, _ := e.connect(t, "ada")
	for i := range MaxClientEvents + 10 {
		ada.Receive(t.Context(), fmt.Appendf(nil, `{"type":"typing.start","data":{"conversationId":"x%d"}}`, i))
	}
	if n := e.access.checkCount(); n != MaxClientEvents {
		t.Errorf("%d access checks, want %d", n, MaxClientEvents)
	}
	e.advance(ClientEventWindow)
	ada.Receive(t.Context(), []byte(`{"type":"typing.start","data":{"conversationId":"fresh"}}`))
	if n := e.access.checkCount(); n != MaxClientEvents+1 {
		t.Errorf("%d access checks after the window, want %d", n, MaxClientEvents+1)
	}
}

func presenceOf(t *testing.T, s *fakeSocket) string {
	t.Helper()
	ev := s.next(t)
	if ev.Type != domain.EventPresence {
		t.Fatalf("got %+v", ev)
	}
	return ev.Data.(string)
}

func TestPresenceReachesPeersOnly(t *testing.T) {
	e := newEnv(t)
	_, bob := e.connect(t, "bob")
	_, eve := e.connect(t, "eve")
	c, _ := e.connect(t, "ada")
	if got := presenceOf(t, bob); got != `{"userId":"ada","online":true}` {
		t.Errorf("online: %s", got)
	}
	c.Done()
	if got := presenceOf(t, bob); got != `{"userId":"ada","online":false}` {
		t.Errorf("offline: %s", got)
	}
	eve.silent(t, 30*time.Millisecond)
}

// The forced reconnect every 15 minutes must not flicker: a socket back within the delay
// sends no offline event.
func TestReconnectWithinTheDelayStaysOnline(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.OfflineGrace = 60 * time.Millisecond })
	_, bob := e.connect(t, "bob")
	c, _ := e.connect(t, "ada")
	presenceOf(t, bob)
	c.Done()
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if n, _ := e.presence.Count(t.Context(), "ada", time.Time{}); n == 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	e.connect(t, "ada")
	bob.silent(t, 120*time.Millisecond)
}

func TestSecondSocketAnnouncesNothing(t *testing.T) {
	e := newEnv(t)
	_, bob := e.connect(t, "bob")
	e.connect(t, "ada")
	presenceOf(t, bob)
	e.connect(t, "ada")
	bob.silent(t, 30*time.Millisecond)
}

func TestHidePresence(t *testing.T) {
	e := newEnv(t)
	_, bob := e.connect(t, "bob")
	_, ada := e.connect(t, "ada")
	presenceOf(t, bob)
	e.hub.HidePresence(t.Context(), "ada", "bob")
	if got := presenceOf(t, bob); got != `{"userId":"ada","online":false}` {
		t.Errorf("bob got %s", got)
	}
	if got := presenceOf(t, ada); got != `{"userId":"bob","online":false}` {
		t.Errorf("ada got %s", got)
	}
}

func TestOnline(t *testing.T) {
	e := newEnv(t)
	e.connect(t, "bob")
	e.connect(t, "eve")

	got, err := e.hub.Online(t.Context(), "ada", []string{"bob", "eve", "bob", "zed"})
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Status{{UserID: "bob", Online: true}, {UserID: "eve"}, {UserID: "zed"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Online() = %v, want %v (eve shares no conversation with ada)", got, want)
	}

	if _, err := e.hub.Online(t.Context(), "ada", nil); !errors.Is(err, domain.ErrNoIDs) {
		t.Errorf("no IDs: %v", err)
	}
	many := make([]string, MaxPresenceIDs+1)
	if _, err := e.hub.Online(t.Context(), "ada", many); !errors.Is(err, domain.ErrTooManyIDs) {
		t.Errorf("%d IDs: %v", len(many), err)
	}
}

func TestShutdownClosesSocketsAndRefusesNewOnes(t *testing.T) {
	e := newEnv(t)
	_, ada := e.connect(t, "ada")
	_, bob := e.connect(t, "bob")
	presenceOf(t, ada)
	ctx, cancel := context.WithTimeout(t.Context(), wait)
	defer cancel()
	e.hub.Shutdown(ctx)
	ada.wantClosed(t, domain.CloseGoingAway)
	bob.wantClosed(t, domain.CloseGoingAway)
	if _, err := e.hub.Reserve(t.Context(), "ada"); !errors.Is(err, domain.ErrClosed) {
		t.Errorf("Reserve() after shutdown: %v", err)
	}
}

// Shutting down tells peers at once: no timer would survive to do it.
func TestShutdownAnnouncesOfflineAtOnce(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.OfflineGrace = time.Hour })
	e.connect(t, "ada")
	ctx, cancel := context.WithTimeout(t.Context(), wait)
	defer cancel()
	e.hub.Shutdown(ctx)
	if !e.bus.published("bob", `"online":false`) {
		t.Error("bob was not told that ada went offline")
	}
}
