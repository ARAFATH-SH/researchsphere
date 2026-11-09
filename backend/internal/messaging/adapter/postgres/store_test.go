//go:build integration

package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
)

func newTestStore(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	pool := testenv.DB(t)
	return NewStore(pool), pool
}

// newUser stores a verified user with a profile named name.
func newUser(t *testing.T, pool *pgxpool.Pool, name string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(t.Context(),
		"INSERT INTO users (email, password_hash, email_verified_at) VALUES ($1, 'hash', now()) RETURNING id", testenv.Email(t)).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "INSERT INTO profiles (user_id, full_name, headline) VALUES ($1, $2, 'Chemist')", id, name); err != nil {
		t.Fatal(err)
	}
	return id
}

func clientID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	const hex = "0123456789abcdef"
	out := make([]byte, 0, 36)
	for i, v := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out = append(out, '-')
		}
		out = append(out, hex[v>>4], hex[v&0x0f])
	}
	return string(out)
}

func create(t *testing.T, s *Store, a, b string, state domain.State, requester *string) domain.Conversation {
	t.Helper()
	c, created, err := s.CreateDirect(t.Context(), a, b, state, requester)
	if err != nil || !created {
		t.Fatalf("CreateDirect: %v, %v", created, err)
	}
	return c
}

func add(t *testing.T, s *Store, c, sender, body string) domain.Message {
	t.Helper()
	m, added, err := s.AddMessage(t.Context(), domain.Message{ConversationID: c, SenderID: sender, Body: &body, ClientID: clientID()})
	if err != nil || !added {
		t.Fatalf("AddMessage: %v, %v", added, err)
	}
	return m
}

func TestCreateDirectIsOnePerPair(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
	c := create(t, s, ada, bob, domain.StateActive, nil)
	if _, created, err := s.CreateDirect(t.Context(), bob, ada, domain.StateActive, nil); err != nil || created {
		t.Fatalf("the other way round: created %v, %v", created, err)
	}
	if ok, err := s.DirectExists(t.Context(), bob, ada); err != nil || !ok {
		t.Errorf("DirectExists: %v, %v", ok, err)
	}
	got, err := s.FindConversation(t.Context(), c.ID, ada)
	if err != nil || got.OtherID != bob {
		t.Errorf("FindConversation: %+v, %v", got, err)
	}
}

func TestCreateDirectRace(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		created int
	)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			x, y := ada, bob
			if i%2 == 1 {
				x, y = bob, ada
			}
			_, ok, err := s.CreateDirect(t.Context(), x, y, domain.StateActive, nil)
			if err != nil {
				t.Error(err)
			}
			if ok {
				mu.Lock()
				created++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if created != 1 {
		t.Errorf("created %d times", created)
	}
	var rows, members int
	if err := pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM conversations), (SELECT count(*) FROM conversation_members)").Scan(&rows, &members); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || members != 2 {
		t.Errorf("%d conversations, %d members", rows, members)
	}
}

func TestAddMessage(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
	c := create(t, s, ada, bob, domain.StateActive, nil)

	m := add(t, s, c.ID, ada, "line one\nline two")
	if m.ID[14] != '7' {
		t.Errorf("ID %s is not a UUIDv7", m.ID)
	}
	got, err := s.FindConversation(ctx, c.ID, bob)
	if err != nil || !got.LastMessageAt.Equal(m.CreatedAt) {
		t.Errorf("last_message_at = %s, want %s (%v)", got.LastMessageAt, m.CreatedAt, err)
	}
	if got.OtherReadID == nil || *got.OtherReadID != m.ID {
		t.Errorf("the sender's marker = %v, want %s", got.OtherReadID, m.ID)
	}

	// The same client ID stores nothing and changes nothing.
	if _, added, err := s.AddMessage(ctx, domain.Message{ConversationID: c.ID, SenderID: ada, Body: str("again"), ClientID: m.ClientID}); err != nil || added {
		t.Fatalf("second add: %v, %v", added, err)
	}
	sent, found, err := s.FindSent(ctx, ada, m.ClientID)
	if err != nil || !found || sent.Message.ID != m.ID || *sent.Message.Body != *m.Body || sent.State != domain.StateActive {
		t.Errorf("FindSent: %+v, %v, %v", sent, found, err)
	}
	if _, found, _ := s.FindSent(ctx, bob, m.ClientID); found {
		t.Error("found another sender's client ID")
	}
}

func TestLocksAndActivation(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob, cy := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Cy")
	c := create(t, s, ada, bob, domain.StateRequest, &ada)
	if err := s.ActivateRequest(ctx, bob, ada); err != nil {
		t.Fatal(err)
	}
	got, err := s.LockDirect(ctx, ada, bob)
	if err != nil || got.ID != c.ID || got.State != domain.StateActive || got.RequestedBy == nil || *got.RequestedBy != ada {
		t.Errorf("LockDirect: %+v, %v", got, err)
	}
	if _, err := s.LockDirect(ctx, ada, cy); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("no conversation: %v", err)
	}
	if _, err := s.LockConversation(ctx, cy); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown ID: %v", err)
	}
	if _, err := s.LockConversation(ctx, "nonsense"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("bad ID: %v", err)
	}

	// A declined conversation stays closed.
	d := create(t, s, ada, cy, domain.StateRequest, &ada)
	if _, found, err := s.AnswerRequest(ctx, d.ID, cy, domain.StateDeclined); err != nil || !found {
		t.Fatalf("decline: %v, %v", found, err)
	}
	if err := s.ActivateRequest(ctx, ada, cy); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.LockDirect(ctx, ada, cy); got.State != domain.StateDeclined {
		t.Errorf("state = %s", got.State)
	}
}

// idMicrosecond splits a message ID into its Unix millisecond and its 12 sub-millisecond bits.
func idMicrosecond(t *testing.T, id string) (ms int64, fraction int64) {
	t.Helper()
	plain := strings.ReplaceAll(id, "-", "")
	ms, err := strconv.ParseInt(plain[:12], 16, 64)
	if err != nil {
		t.Fatal(err)
	}
	fraction, err = strconv.ParseInt(plain[13:16], 16, 64)
	if err != nil {
		t.Fatal(err)
	}
	return ms, fraction
}

// A send takes the lock, then makes its ID and its time from one instant, so the order the
// lock grants, the IDs and the times always agree. Ten sends rarely share a millisecond by
// chance, so the second case forces it: with last_message_at ahead of the clock, each send
// lands one microsecond after the last, the way it would if the clock had stepped back.
func TestLockedSendsAreInOrder(t *testing.T) {
	cases := []struct {
		name string
		lead string
	}{
		{"on the clock", ""},
		{"in one millisecond", "UPDATE conversations SET last_message_at = date_trunc('milliseconds', now()) + interval '1 hour' WHERE id = $1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, pool := newTestStore(t)
			tx := postgres.NewTransactor(pool)
			ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
			c := create(t, s, ada, bob, domain.StateActive, nil)
			if tc.lead != "" {
				if _, err := pool.Exec(t.Context(), tc.lead, c.ID); err != nil {
					t.Fatal(err)
				}
			}
			const sends = 10
			var (
				wg      sync.WaitGroup
				mu      sync.Mutex
				granted []string // Bodies, in the order the lock was granted.
			)
			for i := range sends {
				wg.Add(1)
				go func() {
					defer wg.Done()
					body := strconv.Itoa(i)
					err := tx.InTx(t.Context(), func(ctx context.Context) error {
						if _, err := s.LockConversation(ctx, c.ID); err != nil {
							return err
						}
						mu.Lock()
						granted = append(granted, body)
						mu.Unlock()
						_, _, err := s.AddMessage(ctx, domain.Message{ConversationID: c.ID, SenderID: ada, Body: &body, ClientID: clientID()})
						return err
					})
					if err != nil {
						t.Error(err)
					}
				}()
			}
			wg.Wait()
			newestFirst, err := s.ListMessages(t.Context(), c.ID, time.Time{}, "", sends)
			if err != nil || len(newestFirst) != sends {
				t.Fatalf("%d messages, %v", len(newestFirst), err)
			}
			for i, m := range newestFirst {
				if want := granted[sends-1-i]; *m.Body != want {
					t.Errorf("position %d holds send %s, want %s (lock order %v)", i, *m.Body, want, granted)
				}
				ms, fraction := idMicrosecond(t, m.ID)
				at := m.CreatedAt.UnixMicro()
				if ms != at/1000 || fraction != (at%1000)*4096/1000 {
					t.Errorf("ID %s does not carry created_at %s", m.ID, m.CreatedAt.Format(time.RFC3339Nano))
				}
				if i == 0 {
					continue
				}
				if newer := newestFirst[i-1]; m.ID >= newer.ID || !m.CreatedAt.Before(newer.CreatedAt) {
					t.Errorf("message %d (%s, %s) is not older than the one before it (%s, %s)", i, m.ID, m.CreatedAt.Format(time.RFC3339Nano), newer.ID, newer.CreatedAt.Format(time.RFC3339Nano))
				}
			}
			if tc.lead != "" && newestFirst[0].CreatedAt.UnixMilli() != newestFirst[sends-1].CreatedAt.UnixMilli() {
				t.Errorf("the sends left the millisecond: %s to %s", newestFirst[sends-1].CreatedAt, newestFirst[0].CreatedAt)
			}
			got, err := s.FindConversation(t.Context(), c.ID, bob)
			if err != nil {
				t.Fatal(err)
			}
			if !got.LastMessageAt.Equal(newestFirst[0].CreatedAt) {
				t.Errorf("last_message_at = %s, want %s", got.LastMessageAt, newestFirst[0].CreatedAt)
			}
			if got.OtherReadID == nil || *got.OtherReadID != newestFirst[0].ID {
				t.Errorf("the sender's read marker = %v, want the newest message %s", got.OtherReadID, newestFirst[0].ID)
			}
		})
	}
}

func TestReadMarkerOnlyMovesForward(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob, cy := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Cy")
	c := create(t, s, ada, bob, domain.StateActive, nil)
	first, second := add(t, s, c.ID, ada, "one"), add(t, s, c.ID, ada, "two")

	if moved, err := s.MoveReadMarker(ctx, c.ID, bob, second.ID); err != nil || !moved {
		t.Fatalf("forward: %v, %v", moved, err)
	}
	for _, id := range []string{first.ID, second.ID} {
		if moved, err := s.MoveReadMarker(ctx, c.ID, bob, id); err != nil || moved {
			t.Errorf("to %s: moved %v, %v", id, moved, err)
		}
	}
	got, _ := s.FindConversation(ctx, c.ID, ada)
	if *got.OtherReadID != second.ID {
		t.Errorf("marker = %s", *got.OtherReadID)
	}
	var at *time.Time
	if err := pool.QueryRow(ctx, "SELECT last_read_at FROM conversation_members WHERE conversation_id = $1 AND user_id = $2", c.ID, bob).Scan(&at); err != nil || at == nil {
		t.Errorf("last_read_at = %v, %v", at, err)
	}

	// The database refuses a marker that points into another conversation.
	d := create(t, s, ada, cy, domain.StateActive, nil)
	foreign := add(t, s, d.ID, ada, "elsewhere")
	if in, err := s.MessageIn(ctx, c.ID, foreign.ID, time.Time{}); err != nil || in {
		t.Errorf("MessageIn: %v, %v", in, err)
	}
	_, err := s.MoveReadMarker(ctx, c.ID, bob, "ffffffff-ffff-7fff-bfff-ffffffffffff")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Errorf("marker to a message that does not exist: %v", err)
	}
}

func TestMembershipIsChecked(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob, eve := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Eve")
	c := create(t, s, ada, bob, domain.StateRequest, &ada)
	if _, err := s.FindConversation(ctx, c.ID, eve); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("FindConversation: %v", err)
	}
	if _, err := s.FindItem(ctx, c.ID, eve); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("FindItem: %v", err)
	}
	if _, err := s.FindConversation(ctx, "nonsense", ada); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("bad ID: %v", err)
	}
	if _, found, err := s.AnswerRequest(ctx, c.ID, eve, domain.StateActive); err != nil || found {
		t.Errorf("a stranger answered: %v, %v", found, err)
	}
	if _, found, err := s.AnswerRequest(ctx, c.ID, ada, domain.StateActive); err != nil || found {
		t.Errorf("the sender answered: %v, %v", found, err)
	}
	requester, found, err := s.AnswerRequest(ctx, c.ID, bob, domain.StateActive)
	if err != nil || !found || requester != ada {
		t.Errorf("the receiver: %s, %v, %v", requester, found, err)
	}
	if _, found, _ := s.AnswerRequest(ctx, c.ID, bob, domain.StateDeclined); found {
		t.Error("answered twice")
	}
}

func TestItemsAndUnreadCounts(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob, cy, dan := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Cy"), newUser(t, pool, "Dan")

	active := create(t, s, ada, bob, domain.StateActive, nil)
	add(t, s, active.ID, ada, "from me")
	long := make([]rune, 200)
	for i := range long {
		long[i] = 'é'
	}
	last := add(t, s, active.ID, bob, string(long))
	add(t, s, active.ID, bob, "newest")
	if moved, err := s.MoveReadMarker(ctx, active.ID, ada, last.ID); err != nil || !moved {
		t.Fatal(moved, err)
	}
	request := create(t, s, ada, cy, domain.StateRequest, &cy)
	add(t, s, request.ID, cy, "may I?")
	mine := create(t, s, ada, dan, domain.StateRequest, &ada)
	add(t, s, mine.ID, ada, "may I?")

	inbox, err := s.ListItems(ctx, ada, domain.BoxInbox, nil, 10)
	if err != nil || len(inbox) != 2 {
		t.Fatalf("inbox: %d, %v", len(inbox), err)
	}
	if inbox[0].ID != mine.ID || inbox[1].ID != active.ID {
		t.Errorf("order: %s, %s", inbox[0].ID, inbox[1].ID)
	}
	it := inbox[1]
	if it.UnreadCount != 1 || it.LastMessage == nil || *it.LastMessage.Body != "newest" || it.LastMessage.SenderID != bob {
		t.Errorf("item = %+v", it)
	}
	if it.Other.UserID != bob || it.Other.FullName != "Bob" || it.Other.Headline == nil || *it.Other.Headline != "Chemist" || !it.Other.Available {
		t.Errorf("other = %+v", it.Other)
	}

	requests, err := s.ListItems(ctx, ada, domain.BoxRequests, nil, 10)
	if err != nil || len(requests) != 1 || requests[0].ID != request.ID || requests[0].UnreadCount != 1 {
		t.Fatalf("requests: %+v, %v", requests, err)
	}

	// The preview is the first 120 characters, not bytes.
	add(t, s, active.ID, bob, string(long))
	one, err := s.FindItem(ctx, active.ID, ada)
	if err != nil || len([]rune(*one.LastMessage.Body)) != 120 {
		t.Errorf("preview has %d characters, %v", len([]rune(*one.LastMessage.Body)), err)
	}

	n, err := s.CountUnread(ctx, ada)
	if err != nil || n.Total != 2 || n.Requests != 1 {
		t.Errorf("counts = %+v, %v; want 2 and 1", n, err)
	}

	// A suspended member's conversation stays; a request from them leaves the box.
	if _, err := pool.Exec(ctx, "UPDATE users SET suspended_at = now() WHERE id = ANY($1::uuid[])", []string{bob, cy}); err != nil {
		t.Fatal(err)
	}
	inbox, _ = s.ListItems(ctx, ada, domain.BoxInbox, nil, 10)
	if len(inbox) != 2 || inbox[0].Other.UserID != bob || inbox[0].Other.Available {
		t.Errorf("inbox after suspension: %+v", inbox)
	}
	if requests, _ = s.ListItems(ctx, ada, domain.BoxRequests, nil, 10); len(requests) != 0 {
		t.Errorf("requests after suspension: %+v", requests)
	}
	if n, _ = s.CountUnread(ctx, ada); n.Requests != 0 {
		t.Errorf("request count after suspension: %+v", n)
	}
	if _, found, _ := s.AnswerRequest(ctx, request.ID, ada, domain.StateActive); found {
		t.Error("answered a request from a suspended user")
	}
}

func TestListCursorKeepsItsPlace(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada := newUser(t, pool, "Ada")
	at := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	var ids []string
	for range 4 {
		c := create(t, s, ada, newUser(t, pool, "Other"), domain.StateActive, nil)
		ids = append(ids, c.ID)
		// All four have the same activity, so only the ID separates them.
		if _, err := pool.Exec(ctx, "UPDATE conversations SET last_message_at = $1 WHERE id = $2", at, c.ID); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.ListItems(ctx, ada, domain.BoxInbox, nil, 2)
	if err != nil || len(first) != 2 {
		t.Fatal(len(first), err)
	}
	second, err := s.ListItems(ctx, ada, domain.BoxInbox, &domain.Cursor{LastMessageAt: first[1].LastMessageAt, ID: first[1].ID}, 2)
	if err != nil || len(second) != 2 {
		t.Fatal(len(second), err)
	}
	seen := map[string]bool{}
	for _, it := range append(first, second...) {
		seen[it.ID] = true
	}
	if len(seen) != len(ids) {
		t.Errorf("saw %d of %d", len(seen), len(ids))
	}
}

func TestListMessagesPages(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
	c := create(t, s, ada, bob, domain.StateActive, nil)
	var sent []domain.Message
	for range 5 {
		sent = append(sent, add(t, s, c.ID, ada, "x"))
	}
	page, err := s.ListMessages(ctx, c.ID, time.Time{}, "", 2)
	if err != nil || len(page) != 2 || page[0].ID != sent[4].ID || page[1].ID != sent[3].ID {
		t.Fatalf("first page: %+v, %v", page, err)
	}
	page, err = s.ListMessages(ctx, c.ID, time.Time{}, page[1].ID, 10)
	if err != nil || len(page) != 3 || page[0].ID != sent[2].ID {
		t.Fatalf("second page: %+v, %v", page, err)
	}
}

func str(s string) *string { return &s }
