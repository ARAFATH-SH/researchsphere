// Package usecase holds the realtime module's logic: the hub of open sockets, fan-out through
// the bus, typing and presence (FR-16.1, FR-16.3, FR-16.4, FR-9.4; docs/realtime.md). It
// imports only the realtime domain.
package usecase

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/domain"
)

// Limits (docs/realtime.md).
const (
	MaxSocketsPerUser = 5
	MaxPresenceIDs    = 50
	// MaxClientEvents is how many frames a socket may send per ClientEventWindow; the rest
	// are ignored. The typing throttle is per conversation, so without this a client could
	// buy one database read per made-up conversation ID.
	MaxClientEvents   = 20
	ClientEventWindow = 10 * time.Second
)

// Config holds the hub's timings. Tests shorten them.
type Config struct {
	PingInterval   time.Duration // how often a socket is pinged and its presence refreshed
	PongTimeout    time.Duration // how long a pong may take
	WriteTimeout   time.Duration // how long one frame may take to write
	CallTimeout    time.Duration // how long a call to Redis or the database may take off a request
	SendBuffer     int           // frames a socket may have waiting
	TypingThrottle time.Duration // at most one start per conversation in this time
	TypingTimeout  time.Duration // isTyping=false follows this long after the last start
	OfflineGrace   time.Duration // how long a user with no socket left stays online
}

// DefaultConfig returns the values of docs/realtime.md.
func DefaultConfig() Config {
	return Config{
		PingInterval:   25 * time.Second,
		PongTimeout:    10 * time.Second,
		WriteTimeout:   5 * time.Second,
		CallTimeout:    2 * time.Second,
		SendBuffer:     32,
		TypingThrottle: 3 * time.Second,
		TypingTimeout:  6 * time.Second,
		OfflineGrace:   5 * time.Second,
	}
}

// Hub holds this instance's sockets. Every event goes through the bus, even to a socket on
// this instance, so one path serves one instance or many.
type Hub struct {
	bus      domain.Bus
	presence domain.Presence
	access   domain.Access
	cfg      Config
	now      func() time.Time
	logger   *slog.Logger

	mu      sync.Mutex
	users   map[string]*member
	offline map[string]*time.Timer // users waiting out the offline delay
	closed  bool
	wg      sync.WaitGroup // sockets being closed

	typingMu sync.Mutex
	typing   map[typingKey]*typingState
}

// member is one user on this instance. refs counts sockets and reservations; the channel
// stays subscribed while it is above 0.
type member struct {
	refs    int
	ready   chan struct{} // closed when the subscription has an answer
	err     error         // that answer; read after ready
	sockets map[string]*Client
	closes  int              // commands that closed every socket so far
	last    domain.CloseCode // the code of the latest one
}

// NewHub returns a Hub. Call Run to start delivering.
func NewHub(bus domain.Bus, presence domain.Presence, access domain.Access, cfg Config, now func() time.Time, logger *slog.Logger) *Hub {
	return &Hub{
		bus: bus, presence: presence, access: access, cfg: cfg, now: now, logger: logger,
		users: map[string]*member{}, offline: map[string]*time.Timer{}, typing: map[typingKey]*typingState{},
	}
}

// Run delivers what the bus receives until ctx is cancelled.
func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case d, ok := <-h.bus.Deliveries():
			if !ok {
				return
			}
			h.dispatch(ctx, d)
		}
	}
}

// dispatch hands one envelope to the user's sockets on this instance. It never waits for a
// client: a socket whose buffer is full is closed, so one slow client delays nobody.
func (h *Hub) dispatch(ctx context.Context, d domain.Delivery) {
	var env domain.Envelope
	if err := json.Unmarshal(d.Payload, &env); err != nil {
		h.logger.WarnContext(ctx, "realtime: bad envelope", slog.Any("error", err))
		return
	}
	h.mu.Lock()
	m := h.users[d.UserID]
	if m == nil {
		h.mu.Unlock()
		return
	}
	if env.Close != nil && env.Close.Socket == "" {
		m.closes++
		m.last = env.Close.Code
	}
	targets := make([]*Client, 0, len(m.sockets))
	for _, c := range m.sockets {
		targets = append(targets, c)
	}
	h.mu.Unlock()

	for _, c := range targets {
		if env.Close != nil {
			if env.Close.Socket == "" || env.Close.Socket == c.id {
				c.close(env.Close.Code)
			}
			continue
		}
		select {
		case c.send <- []byte(env.Event):
		default:
			h.logger.InfoContext(ctx, "realtime: send buffer full", slog.String("user_id", d.UserID))
			c.close(domain.CloseTryLater)
		}
	}
}

// Reservation holds a user's subscription while the upgrade finishes. Call Attach or Cancel.
type Reservation struct {
	hub    *Hub
	userID string
	member *member
	closes int
}

// Reserve subscribes to the user's channel and returns once it is live. Doing this before the
// upgrade means a client that refetches after 101 misses nothing, and a suspension that
// commits after the check that follows Reserve is heard.
func (h *Hub) Reserve(ctx context.Context, userID string) (*Reservation, error) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, domain.ErrClosed
	}
	m := h.users[userID]
	first := m == nil
	if first {
		m = &member{ready: make(chan struct{}), sockets: map[string]*Client{}}
		h.users[userID] = m
	}
	m.refs++
	closes := m.closes
	h.mu.Unlock()

	if first {
		m.err = h.bus.Subscribe(ctx, userID)
		close(m.ready)
	}
	select {
	case <-m.ready:
	case <-ctx.Done():
		h.release(ctx, userID, m)
		return nil, ctx.Err()
	}
	if m.err != nil {
		h.release(ctx, userID, m)
		return nil, fmt.Errorf("subscribe: %w", m.err)
	}
	return &Reservation{hub: h, userID: userID, member: m, closes: closes}, nil
}

// release gives up one reference. The unsubscribe is written under the lock, so a Subscribe
// for a new member always comes after it.
func (h *Hub) release(ctx context.Context, userID string, m *member) {
	h.mu.Lock()
	defer h.mu.Unlock()
	m.refs--
	if m.refs > 0 || h.users[userID] != m {
		return
	}
	delete(h.users, userID)
	if err := h.bus.Unsubscribe(ctx, userID); err != nil {
		h.logger.WarnContext(ctx, "realtime: unsubscribe", slog.Any("error", err))
	}
}

// Cancel gives the reservation up: the upgrade did not happen.
func (r *Reservation) Cancel(ctx context.Context) { r.hub.release(ctx, r.userID, r.member) }

// closedSince reports whether a command closed all the user's sockets since Reserve.
func (r *Reservation) closedSince() bool {
	r.hub.mu.Lock()
	defer r.hub.mu.Unlock()
	return r.member.closes != r.closes
}

// Attach makes the upgraded socket live until expires, when the access token ends. The caller
// reads from the socket, passes each frame to Client.Receive and calls Client.Done when the
// read fails.
func (r *Reservation) Attach(sock domain.Socket, expires time.Time) *Client {
	h := r.hub
	c := &Client{
		hub: h, member: r.member, userID: r.userID, id: h.newSocketID(), sock: sock, expires: expires,
		send: make(chan []byte, h.cfg.SendBuffer), done: make(chan struct{}),
	}
	h.mu.Lock()
	r.member.sockets[c.id] = c
	code, closed := r.member.last, r.member.closes != r.closes
	if h.closed {
		code, closed = domain.CloseGoingAway, true
	}
	timer, waiting := h.offline[r.userID]
	if waiting {
		timer.Stop()
		delete(h.offline, r.userID)
	}
	h.mu.Unlock()

	if closed {
		c.close(code)
		return c
	}
	go c.writeLoop()
	go c.heartbeat()
	h.joined(c, !waiting)
	return c
}

// newSocketID makes an ID that sorts by age, to the millisecond, so the oldest socket can be
// found in Redis.
func (h *Hub) newSocketID() string {
	var b [6]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never returns an error since Go 1.24.
	return fmt.Sprintf("%012x-%s", h.now().UnixMilli(), hex.EncodeToString(b[:]))
}

// joined records the socket, tells peers when it is the user's first, and closes the oldest
// sockets past the limit. The limit holds across instances: the list comes from Redis and the
// command goes to whichever instance has the socket.
func (h *Hub) joined(c *Client, announce bool) {
	ctx, cancel := h.callContext()
	defer cancel()
	sockets, err := h.presence.Add(ctx, c.userID, c.id, h.now())
	if err != nil {
		h.logger.WarnContext(ctx, "realtime: record presence", slog.Any("error", err))
		return
	}
	if len(sockets) == 1 && announce {
		h.announce(ctx, c.userID, true)
	}
	for i := range len(sockets) - MaxSocketsPerUser {
		h.publishClose(ctx, c.userID, domain.CloseCommand{Socket: sockets[i], Code: domain.CloseReplaced})
	}
}

// left runs when a socket has closed: it forgets the socket and, when it was the user's last,
// starts the offline delay. A token expiring forces a reconnect every 15 minutes, and without
// the delay everyone would flicker offline each time. While shutting down nothing would
// survive to end the delay, so peers hear at once.
func (h *Hub) left(c *Client) {
	ctx, cancel := h.callContext()
	defer cancel()
	h.mu.Lock()
	delete(c.member.sockets, c.id)
	h.mu.Unlock()
	h.release(ctx, c.userID, c.member)
	left, err := h.presence.Remove(ctx, c.userID, c.id, h.now())
	if err != nil {
		h.logger.WarnContext(ctx, "realtime: forget presence", slog.Any("error", err))
		return
	}
	if left > 0 {
		return
	}
	h.mu.Lock()
	closed := h.closed
	if !closed {
		if t, ok := h.offline[c.userID]; ok {
			t.Stop()
		}
		h.offline[c.userID] = time.AfterFunc(h.cfg.OfflineGrace, func() { h.offlineDue(c.userID) })
	}
	h.mu.Unlock()
	if closed {
		h.announce(ctx, c.userID, false)
	}
}

// offlineDue ends the delay: the user is offline unless a socket came back, also on another
// instance.
func (h *Hub) offlineDue(userID string) {
	h.mu.Lock()
	delete(h.offline, userID)
	h.mu.Unlock()
	ctx, cancel := h.callContext()
	defer cancel()
	n, err := h.presence.Count(ctx, userID, h.now())
	if err != nil {
		h.logger.WarnContext(ctx, "realtime: count presence", slog.Any("error", err))
		return
	}
	if n == 0 {
		h.announce(ctx, userID, false)
	}
}

// announce tells the people who may see it.
func (h *Hub) announce(ctx context.Context, userID string, online bool) {
	peers, err := h.access.Peers(ctx, userID)
	if err != nil {
		h.logger.WarnContext(ctx, "realtime: read peers", slog.Any("error", err))
		return
	}
	for _, p := range peers {
		h.Send(ctx, p, domain.Event{Type: domain.EventPresence, Data: domain.Status{UserID: userID, Online: online}})
	}
}

// Send publishes an event to the user's sockets on every instance. Delivery is best effort:
// a failure is logged and cannot fail the change it reports.
func (h *Hub) Send(ctx context.Context, userID string, e domain.Event) {
	raw, err := json.Marshal(e)
	if err != nil {
		h.logger.ErrorContext(ctx, "realtime: encode event", slog.String("type", e.Type), slog.Any("error", err))
		return
	}
	h.publish(ctx, userID, domain.Envelope{Event: raw})
}

// CloseUser closes all the user's sockets, on every instance.
func (h *Hub) CloseUser(ctx context.Context, userID string, code domain.CloseCode) {
	h.publishClose(ctx, userID, domain.CloseCommand{Code: code})
}

// HidePresence takes each of the two out of the other's view after a block (FR-16.9): the
// rule for who sees presence no longer holds, and an open page would keep the green dot.
func (h *Hub) HidePresence(ctx context.Context, a, b string) {
	h.Send(ctx, a, domain.Event{Type: domain.EventPresence, Data: domain.Status{UserID: b}})
	h.Send(ctx, b, domain.Event{Type: domain.EventPresence, Data: domain.Status{UserID: a}})
}

func (h *Hub) publishClose(ctx context.Context, userID string, cmd domain.CloseCommand) {
	h.publish(ctx, userID, domain.Envelope{Close: &cmd})
}

func (h *Hub) publish(ctx context.Context, userID string, env domain.Envelope) {
	payload, err := json.Marshal(env)
	if err != nil {
		h.logger.ErrorContext(ctx, "realtime: encode envelope", slog.Any("error", err))
		return
	}
	if err := h.bus.Publish(ctx, userID, payload); err != nil {
		h.logger.WarnContext(ctx, "realtime: publish", slog.Any("error", err))
	}
}

// Suspension returns the user's suspension, nil when there is none.
func (h *Hub) Suspension(ctx context.Context, userID string) (*domain.Suspension, error) {
	return h.access.Suspension(ctx, userID)
}

// Online answers whether each of userIDs is online, once per ID, in the order asked. Anyone
// viewerID may not see reads as offline, so the answer does not say who may be seen (FR-16.4).
func (h *Hub) Online(ctx context.Context, viewerID string, userIDs []string) ([]domain.Status, error) {
	if len(userIDs) == 0 {
		return nil, domain.ErrNoIDs
	}
	if len(userIDs) > MaxPresenceIDs {
		return nil, domain.ErrTooManyIDs
	}
	visible, err := h.access.Visible(ctx, viewerID, userIDs)
	if err != nil {
		return nil, err
	}
	online := map[string]bool{}
	if len(visible) > 0 {
		if online, err = h.presence.Online(ctx, visible, h.now()); err != nil {
			return nil, err
		}
	}
	seen := make(map[string]bool, len(userIDs))
	out := make([]domain.Status, 0, len(userIDs))
	for _, id := range userIDs {
		if !seen[id] {
			seen[id] = true
			out = append(out, domain.Status{UserID: id, Online: online[id]})
		}
	}
	return out, nil
}

// Shutdown closes every socket and waits for that, within ctx. New sockets are refused.
func (h *Hub) Shutdown(ctx context.Context) {
	h.mu.Lock()
	h.closed = true
	var clients []*Client
	for _, m := range h.users {
		for _, c := range m.sockets {
			clients = append(clients, c)
		}
	}
	waiting := make([]string, 0, len(h.offline))
	for id, t := range h.offline {
		t.Stop()
		delete(h.offline, id)
		waiting = append(waiting, id)
	}
	h.mu.Unlock()

	for _, c := range clients {
		c.close(domain.CloseGoingAway)
	}
	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		h.logger.WarnContext(ctx, "realtime: sockets still closing at shutdown")
	}
	for _, id := range waiting {
		h.offlineDue(id)
	}
	h.typingMu.Lock()
	for k, s := range h.typing {
		s.timer.Stop()
		delete(h.typing, k)
	}
	h.typingMu.Unlock()
}

// callContext bounds work no request is waiting on.
func (h *Hub) callContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), h.cfg.CallTimeout)
}
