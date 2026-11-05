//go:build integration

package postgres

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/notification/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
)

func newTestStore(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	pool := testenv.DB(t)
	return NewStore(pool), pool
}

// newUser stores a verified user with a profile.
func newUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	err := pool.QueryRow(t.Context(),
		"INSERT INTO users (email, password_hash, email_verified_at) VALUES ($1, 'hash', now()) RETURNING id", testenv.Email(t)).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, pool, "INSERT INTO profiles (user_id, full_name) VALUES ($1, $2)", id, testenv.Word(t))
	return id
}

func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

// newRequest stores a pending connection request and returns its ID.
func newRequest(t *testing.T, pool *pgxpool.Pool, from, to string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(t.Context(), "INSERT INTO connections (requester_id, receiver_id) VALUES ($1, $2) RETURNING id", from, to).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func put(t *testing.T, s *Store, recipient, actor string, typ domain.Type, subject string) {
	t.Helper()
	if _, err := s.Put(t.Context(), domain.Notification{RecipientID: recipient, ActorID: actor, Type: typ, SubjectID: subject}); err != nil {
		t.Fatal(err)
	}
}

func list(t *testing.T, s *Store, user string) []domain.Item {
	t.Helper()
	items, err := s.List(t.Context(), user, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func unread(t *testing.T, s *Store, user string) int {
	t.Helper()
	n, err := s.CountUnread(t.Context(), user)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// Put replaces a connection notification of the same pair and type: one remains, about the
// new request, unread (FR-9.1).
func TestPutReplacesPerPair(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool), newUser(t, pool)
	r1 := newRequest(t, pool, ada, bob)
	put(t, s, bob, ada, domain.TypeConnectionRequestReceived, r1)
	if _, err := s.MarkAllRead(t.Context(), bob); err != nil {
		t.Fatal(err)
	}
	exec(t, pool, "UPDATE connections SET status = 'rejected' WHERE id = $1", r1)
	r2 := newRequest(t, pool, ada, bob)
	put(t, s, bob, ada, domain.TypeConnectionRequestReceived, r2)

	items := list(t, s, bob)
	if len(items) != 1 || items[0].SubjectID != r2 || items[0].ReadAt != nil || !*items[0].RequestPending {
		t.Fatalf("bob has %+v, want one unread pending notification about %s", items, r2)
	}
	if it := items[0]; it.ActorID != ada || it.Actor.UserID != ada || it.Actor.FullName == "" || it.Type != domain.TypeConnectionRequestReceived {
		t.Errorf("item %+v does not name ada as the actor", it)
	}
	put(t, s, bob, ada, domain.TypeConnectionRequestAccepted, r2)
	if n := len(list(t, s, bob)); n != 2 {
		t.Errorf("bob has %d notifications, want the received and the accepted one", n)
	}
}

// RequestPending follows the connection request: false once accepted or gone.
func TestRequestPending(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool), newUser(t, pool)
	r := newRequest(t, pool, ada, bob)
	put(t, s, bob, ada, domain.TypeConnectionRequestReceived, r)
	exec(t, pool, "UPDATE connections SET status = 'accepted', responded_at = now() WHERE id = $1", r)
	if items := list(t, s, bob); *items[0].RequestPending {
		t.Error("an accepted request reads as pending")
	}
	exec(t, pool, "DELETE FROM connections WHERE id = $1", r)
	if items := list(t, s, bob); *items[0].RequestPending {
		t.Error("a removed connection reads as pending")
	}
}

// DeleteBySubject removes only that type's notifications about that item.
func TestDeleteBySubject(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob, cy := newUser(t, pool), newUser(t, pool), newUser(t, pool)
	r1, r2 := newRequest(t, pool, ada, bob), newRequest(t, pool, cy, bob)
	put(t, s, bob, ada, domain.TypeConnectionRequestReceived, r1)
	put(t, s, bob, cy, domain.TypeConnectionRequestReceived, r2)
	put(t, s, ada, bob, domain.TypeConnectionRequestAccepted, r1)
	if _, err := s.DeleteBySubject(t.Context(), domain.TypeConnectionRequestReceived, r1); err != nil {
		t.Fatal(err)
	}
	if items := list(t, s, bob); len(items) != 1 || items[0].SubjectID != r2 {
		t.Errorf("bob has %+v, want only cy's request", items)
	}
	if len(list(t, s, ada)) != 1 {
		t.Error("the accepted notification about the same item was deleted")
	}
	if _, err := s.DeleteBySubject(t.Context(), domain.TypeConnectionRequestReceived, "not-a-uuid"); err != nil {
		t.Errorf("DeleteBySubject of a malformed ID: %v", err)
	}
}

// A suspended or unverified actor's notifications are left out of the list and the count,
// and cannot be marked read; a deleted actor's are gone (FR-2.8).
func TestHiddenActors(t *testing.T) {
	s, pool := newTestStore(t)
	bob, sam, una, dee := newUser(t, pool), newUser(t, pool), newUser(t, pool), newUser(t, pool)
	for _, actor := range []string{sam, una, dee} {
		put(t, s, bob, actor, domain.TypeConnectionRequestReceived, newRequest(t, pool, actor, bob))
	}
	exec(t, pool, "UPDATE users SET suspended_at = now() WHERE id = $1", sam)
	exec(t, pool, "UPDATE users SET email_verified_at = NULL WHERE id = $1", una)
	exec(t, pool, "DELETE FROM users WHERE id = $1", dee)

	if items := list(t, s, bob); len(items) != 0 {
		t.Errorf("bob sees %+v, want nothing", items)
	}
	if n := unread(t, s, bob); n != 0 {
		t.Errorf("unread count %d, want 0", n)
	}
	var samsID string
	if err := pool.QueryRow(t.Context(), "SELECT id FROM notifications WHERE actor_id = $1", sam).Scan(&samsID); err != nil {
		t.Fatal(err)
	}
	if found, err := s.MarkRead(t.Context(), samsID, bob); err != nil || found {
		t.Errorf("MarkRead of a suspended actor's notification = %v, %v; want not found", found, err)
	}
	if n, err := s.MarkAllRead(t.Context(), bob); err != nil || n != 0 {
		t.Errorf("MarkAllRead = %d, %v; want 0", n, err)
	}
	exec(t, pool, "UPDATE users SET suspended_at = NULL WHERE id = $1", sam)
	if n := unread(t, s, bob); n != 1 {
		t.Errorf("after the suspension is lifted the unread count is %d, want 1", n)
	}
}

// Pages of 20 over 25 notifications, some created at the same moment, are newest first,
// do not overlap and end with the 5 oldest (FR-9.5).
func TestListCursorPaging(t *testing.T) {
	s, pool := newTestStore(t)
	bob := newUser(t, pool)
	for i := range 25 {
		actor := newUser(t, pool)
		put(t, s, bob, actor, domain.TypeConnectionRequestReceived, newRequest(t, pool, actor, bob))
		if i%5 == 0 { // several share a created_at, so the ID must break ties
			exec(t, pool, "UPDATE notifications SET created_at = '2026-11-02T09:00:00Z' WHERE actor_id = $1", actor)
		}
	}
	page1, err := s.List(t.Context(), bob, nil, 20)
	if err != nil || len(page1) != 20 {
		t.Fatalf("page 1: %d items, %v", len(page1), err)
	}
	last := page1[19]
	page2, err := s.List(t.Context(), bob, &domain.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}, 20)
	if err != nil || len(page2) != 5 {
		t.Fatalf("page 2: %d items, %v", len(page2), err)
	}
	all := append(page1, page2...)
	seen := map[string]bool{}
	for i, it := range all {
		if seen[it.ID] {
			t.Errorf("%s appears twice", it.ID)
		}
		seen[it.ID] = true
		if i > 0 {
			prev := all[i-1]
			if it.CreatedAt.After(prev.CreatedAt) || it.CreatedAt.Equal(prev.CreatedAt) && it.ID > prev.ID {
				t.Errorf("item %d is out of order", i)
			}
		}
	}
	if len(seen) != 25 {
		t.Errorf("saw %d notifications, want 25", len(seen))
	}
}

// MarkRead works only for the recipient, keeps the first read time, and MarkAllRead
// counts what it marked (FR-9.6).
func TestMarkRead(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob, cy := newUser(t, pool), newUser(t, pool), newUser(t, pool)
	put(t, s, bob, ada, domain.TypeConnectionRequestReceived, newRequest(t, pool, ada, bob))
	put(t, s, bob, cy, domain.TypeConnectionRequestReceived, newRequest(t, pool, cy, bob))
	id := list(t, s, bob)[0].ID

	for _, user := range []string{ada, cy} {
		if found, err := s.MarkRead(t.Context(), id, user); err != nil || found {
			t.Errorf("MarkRead by %s = %v, %v; want not found", user, found, err)
		}
	}
	if found, err := s.MarkRead(t.Context(), id, bob); err != nil || !found {
		t.Fatalf("MarkRead by bob = %v, %v", found, err)
	}
	first := list(t, s, bob)[0].ReadAt
	if found, err := s.MarkRead(t.Context(), id, bob); err != nil || !found {
		t.Fatalf("second MarkRead = %v, %v", found, err)
	}
	if again := list(t, s, bob)[0].ReadAt; first == nil || again == nil || !again.Equal(*first) {
		t.Errorf("read time %v then %v, want it kept", first, again)
	}
	if found, err := s.MarkRead(t.Context(), "not-a-uuid", bob); err != nil || found {
		t.Errorf("MarkRead of a malformed ID = %v, %v", found, err)
	}
	if n := unread(t, s, bob); n != 1 {
		t.Errorf("unread %d, want 1", n)
	}
	if n, err := s.MarkAllRead(t.Context(), bob); err != nil || n != 1 {
		t.Errorf("MarkAllRead = %d, %v; want 1", n, err)
	}
	if n := unread(t, s, bob); n != 0 {
		t.Errorf("unread %d, want 0", n)
	}
}

// FR-16.9: both directions, every type, nobody else's.
func TestDeleteBetween(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob, cy := newUser(t, pool), newUser(t, pool), newUser(t, pool)
	put(t, s, ada, bob, domain.TypeConnectionRequestAccepted, newRequest(t, pool, ada, bob))
	put(t, s, bob, ada, domain.TypeConnectionRequestReceived, newRequest(t, pool, ada, cy))
	put(t, s, ada, cy, domain.TypeConnectionRequestAccepted, newRequest(t, pool, cy, bob))
	if err := s.DeleteBetween(ctx, bob, ada); err != nil {
		t.Fatal(err)
	}
	if got := list(t, s, bob); len(got) != 0 {
		t.Errorf("bob keeps %+v", got)
	}
	if got := list(t, s, ada); len(got) != 1 || got[0].ActorID != cy {
		t.Errorf("ada keeps %+v", got)
	}
	if err := s.DeleteBetween(ctx, "not-a-uuid", ada); err != nil {
		t.Errorf("bad ID: %v", err)
	}
}

// A system notification has no actor and is never hidden; an actor's is hidden while the actor
// is suspended, so the count always matches the list (FR-9.5, FR-15.5).
func TestSystemNotifications(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool), newUser(t, pool)
	warning := "00000000-0000-4000-8000-000000000001"
	put(t, s, ada, "", domain.TypeAccountWarning, warning)
	put(t, s, ada, bob, domain.TypeConnectionRequestReceived, newRequest(t, pool, bob, ada))

	if got := list(t, s, ada); len(got) != 2 {
		t.Fatalf("list = %+v", got)
	}
	exec(t, pool, "UPDATE users SET suspended_at = now() WHERE id = $1", bob)

	items := list(t, s, ada)
	if len(items) != 1 || items[0].Type != domain.TypeAccountWarning || items[0].Actor != nil || items[0].ActorID != "" {
		t.Fatalf("list = %+v", items)
	}
	if got := unread(t, s, ada); got != 1 {
		t.Errorf("unread = %d, want 1", got)
	}
	found, err := s.MarkRead(t.Context(), items[0].ID, ada)
	if err != nil || !found {
		t.Fatalf("MarkRead: found %v, error %v", found, err)
	}
	if got := unread(t, s, ada); got != 0 {
		t.Errorf("unread after marking = %d, want 0", got)
	}

	put(t, s, ada, "", domain.TypeAccountWarning, warning)
	n, err := s.MarkAllRead(t.Context(), ada)
	if err != nil || n != 1 {
		t.Errorf("MarkAllRead = %d, %v; want 1: the suspended actor's stays out", n, err)
	}
}

func TestAnActorNotificationNeedsItsActor(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool), newUser(t, pool)
	warning := "00000000-0000-4000-8000-000000000001"
	if _, err := s.Put(t.Context(), domain.Notification{RecipientID: ada, Type: domain.TypeConnectionRequestReceived, SubjectID: warning}); err == nil {
		t.Error("a connection notification without an actor was stored")
	}
	if _, err := s.Put(t.Context(), domain.Notification{RecipientID: ada, ActorID: bob, Type: domain.TypeAccountWarning, SubjectID: warning}); err == nil {
		t.Error("a system notification with an actor was stored")
	}
}
