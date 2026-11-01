//go:build integration

package postgres

import (
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/social/domain"
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
	if _, err := pool.Exec(t.Context(),
		"INSERT INTO profiles (user_id, full_name, headline, institution) VALUES ($1, $2, 'Researcher', 'BUET')", id, name); err != nil {
		t.Fatal(err)
	}
	return id
}

func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func create(t *testing.T, s *Store, from, to string) domain.Request {
	t.Helper()
	r, created, err := s.CreateRequest(t.Context(), from, to)
	if err != nil || !created {
		t.Fatalf("CreateRequest: %v, created %v", err, created)
	}
	return r
}

func liveLink(t *testing.T, s *Store, a, b string) (domain.LiveLink, bool) {
	t.Helper()
	l, found, err := s.FindLiveLink(t.Context(), a, b)
	if err != nil {
		t.Fatal(err)
	}
	return l, found
}

// FR-7.1, FR-7.3, FR-7.4, FR-7.5: send, accept, list, remove.
func TestSendAcceptListRemove(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")

	r := create(t, s, ada, bob)
	if l, found := liveLink(t, s, bob, ada); !found || l.ID != r.ID || l.RequesterID != ada || l.Accepted {
		t.Errorf("live link %+v, %v; want ada's pending request", l, found)
	}
	sent, total, err := s.ListSent(ctx, ada, 20, 0)
	if err != nil || total != 1 || len(sent) != 1 || sent[0].ID != r.ID || sent[0].Other.UserID != bob ||
		sent[0].Other.FullName != "Bob" || *sent[0].Other.Headline != "Researcher" || *sent[0].Other.Institution != "BUET" {
		t.Fatalf("ada's sent list: %+v, %d, %v", sent, total, err)
	}
	received, total, _ := s.ListReceived(ctx, bob, 20, 0)
	if total != 1 || received[0].Other.UserID != ada {
		t.Fatalf("bob's received list: %+v, %d", received, total)
	}

	accepted, at, err := s.AcceptRequest(ctx, r.ID, bob)
	if err != nil || accepted.RequesterID != ada || at.IsZero() {
		t.Fatalf("AcceptRequest: %+v, %v, %v", accepted, at, err)
	}
	if l, _ := liveLink(t, s, ada, bob); !l.Accepted {
		t.Error("the link is not accepted")
	}
	for _, pair := range [][2]string{{ada, bob}, {bob, ada}} {
		conns, total, err := s.ListConnections(ctx, pair[0], 20, 0)
		if err != nil || total != 1 || conns[0].Other.UserID != pair[1] || !conns[0].ConnectedAt.Equal(at) {
			t.Errorf("connections of %s: %+v, %d, %v", pair[0], conns, total, err)
		}
		if n, _ := s.CountConnections(ctx, pair[0]); n != 1 {
			t.Errorf("CountConnections = %d, want 1", n)
		}
	}
	if _, total, _ := s.ListSent(ctx, ada, 20, 0); total != 0 {
		t.Error("the accepted request is still in the sent list")
	}

	// Either side may remove it; then it is gone for both.
	if err := s.RemoveConnection(ctx, bob, ada); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveConnection(ctx, ada, bob); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second removal: %v, want ErrNotFound", err)
	}
	if _, found := liveLink(t, s, ada, bob); found {
		t.Error("a link is left after the removal")
	}
	create(t, s, bob, ada) // a new request is allowed
}

// FR-7.6: a rejected request leaves the lists, stays as history and does not block a new
// request in either direction.
func TestRejectThenSendAgain(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")

	r := create(t, s, ada, bob)
	if err := s.RejectRequest(ctx, r.ID, ada); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("reject by the sender: %v, want ErrNotFound", err)
	}
	if err := s.RejectRequest(ctx, r.ID, bob); err != nil {
		t.Fatal(err)
	}
	if err := s.RejectRequest(ctx, r.ID, bob); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second reject: %v, want ErrNotFound", err)
	}
	if _, _, err := s.AcceptRequest(ctx, r.ID, bob); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("accept after reject: %v, want ErrNotFound", err)
	}
	if _, found := liveLink(t, s, ada, bob); found {
		t.Error("the rejected request is still live")
	}
	if _, total, _ := s.ListSent(ctx, ada, 20, 0); total != 0 {
		t.Error("the rejected request is in the sender's list")
	}
	if _, total, _ := s.ListReceived(ctx, bob, 20, 0); total != 0 {
		t.Error("the rejected request is in the receiver's list")
	}
	var status string
	if err := pool.QueryRow(ctx, "SELECT status FROM connections WHERE id = $1", r.ID).Scan(&status); err != nil || status != "rejected" {
		t.Errorf("stored status %q, %v; want rejected kept as history", status, err)
	}
	again := create(t, s, ada, bob)
	if again.ID == r.ID {
		t.Error("the new request reused the rejected row")
	}
}

func TestCancel(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob, cy := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Cy")
	r := create(t, s, ada, bob)
	for _, who := range []string{bob, cy} {
		if err := s.CancelRequest(ctx, r.ID, who); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("cancel by %s: %v, want ErrNotFound", who, err)
		}
	}
	if err := s.CancelRequest(ctx, "not-a-uuid", ada); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("cancel of a malformed ID: %v, want ErrNotFound", err)
	}
	if err := s.CancelRequest(ctx, r.ID, ada); err != nil {
		t.Fatal(err)
	}
	if err := s.CancelRequest(ctx, r.ID, ada); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second cancel: %v, want ErrNotFound", err)
	}
	if _, total, _ := s.ListReceived(ctx, bob, 20, 0); total != 0 {
		t.Error("the cancelled request is still received")
	}
	create(t, s, bob, ada)

	// An accepted request cannot be cancelled.
	r2 := create(t, s, ada, cy)
	if _, _, err := s.AcceptRequest(ctx, r2.ID, cy); err != nil {
		t.Fatal(err)
	}
	if err := s.CancelRequest(ctx, r2.ID, ada); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("cancel of a connection: %v, want ErrNotFound", err)
	}
}

// FR-7.2: the database refuses a second live link in either direction, and CreateRequest
// reports it as not created.
func TestCreateRefusedByLiveLink(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
	create(t, s, ada, bob)
	for _, pair := range [][2]string{{ada, bob}, {bob, ada}} {
		if _, created, err := s.CreateRequest(t.Context(), pair[0], pair[1]); err != nil || created {
			t.Errorf("CreateRequest %v: created %v, %v; want refused", pair, created, err)
		}
	}
	var n int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM connections").Scan(&n); err != nil || n != 1 {
		t.Errorf("%d rows, want 1", n)
	}
}

// Two users who send each other a request at the same moment get one request; the other
// insert returns no row and sees the stored one (FR-7.2).
func TestSimultaneousRequests(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	const rounds = 20
	for range rounds {
		ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
		var (
			wg      sync.WaitGroup
			start   = make(chan struct{})
			created [2]bool
			errs    [2]error
		)
		for i, pair := range [][2]string{{ada, bob}, {bob, ada}} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, created[i], errs[i] = s.CreateRequest(ctx, pair[0], pair[1])
			}()
		}
		close(start)
		wg.Wait()
		if errs[0] != nil || errs[1] != nil {
			t.Fatalf("errors %v", errs)
		}
		if created[0] == created[1] {
			t.Fatalf("created %v, want exactly one request", created)
		}
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM connections WHERE requester_id IN ($1, $2)", ada, bob).Scan(&n); err != nil || n != 1 {
			t.Fatalf("%d rows for the pair, want 1", n)
		}
		winner := ada
		if created[1] {
			winner = bob
		}
		if l, found := liveLink(t, s, ada, bob); !found || l.RequesterID != winner {
			t.Fatalf("live link %+v, want the winner's request", l)
		}
	}
}

// Suspended and unverified users are left out of every list and the count, and their
// requests cannot be accepted.
func TestHiddenUsersLeftOut(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada := newUser(t, pool, "Ada")
	friend, sus, unv := newUser(t, pool, "Friend"), newUser(t, pool, "Suspended"), newUser(t, pool, "Unverified")
	for _, u := range []string{friend, sus, unv} {
		r := create(t, s, u, ada)
		if _, _, err := s.AcceptRequest(ctx, r.ID, ada); err != nil {
			t.Fatal(err)
		}
	}
	pendingFrom, pendingTo := newUser(t, pool, "Pending from"), newUser(t, pool, "Pending to")
	fromReq := create(t, s, pendingFrom, ada)
	create(t, s, ada, pendingTo)
	for _, u := range []string{sus, pendingFrom, pendingTo} {
		exec(t, pool, "UPDATE users SET suspended_at = now() WHERE id = $1", u)
	}
	exec(t, pool, "UPDATE users SET email_verified_at = NULL WHERE id = $1", unv)

	conns, total, err := s.ListConnections(ctx, ada, 20, 0)
	if err != nil || total != 1 || len(conns) != 1 || conns[0].Other.UserID != friend {
		t.Errorf("connections: %+v, %d, %v; want only Friend", conns, total, err)
	}
	if n, _ := s.CountConnections(ctx, ada); n != 1 {
		t.Errorf("CountConnections = %d, want 1", n)
	}
	if _, total, _ := s.ListReceived(ctx, ada, 20, 0); total != 0 {
		t.Errorf("received total %d, want the suspended sender left out", total)
	}
	if _, total, _ := s.ListSent(ctx, ada, 20, 0); total != 0 {
		t.Errorf("sent total %d, want the suspended receiver left out", total)
	}
	if _, _, err := s.AcceptRequest(ctx, fromReq.ID, ada); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("accept from a suspended sender: %v, want ErrNotFound", err)
	}
	for _, u := range []string{sus, unv, "not-a-uuid"} {
		if v, err := s.UserVisible(ctx, u); err != nil || v {
			t.Errorf("UserVisible(%s) = %v, %v; want false", u, v, err)
		}
	}
	if v, _ := s.UserVisible(ctx, friend); !v {
		t.Error("UserVisible(friend) = false")
	}
	// A connection with a suspended user can still be removed.
	if err := s.RemoveConnection(ctx, ada, sus); err != nil {
		t.Errorf("remove a suspended connection: %v", err)
	}
}

// Lists are newest first, paged, with the total of every page.
func TestListOrderAndPages(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada := newUser(t, pool, "Ada")
	var ids []string
	for range 5 {
		u := newUser(t, pool, testenv.Word(t))
		r := create(t, s, u, ada)
		// Spread the times so the order does not depend on the clock's resolution.
		exec(t, pool, "UPDATE connections SET created_at = now() - make_interval(mins => $2::int) WHERE id = $1", r.ID, 10-len(ids))
		ids = append(ids, r.ID)
	}
	page1, total, err := s.ListReceived(ctx, ada, 2, 0)
	if err != nil || total != 5 || len(page1) != 2 || page1[0].ID != ids[4] || page1[1].ID != ids[3] {
		t.Fatalf("page 1: %+v, %d, %v; want the two newest of 5", page1, total, err)
	}
	page3, total, _ := s.ListReceived(ctx, ada, 2, 4)
	if total != 5 || len(page3) != 1 || page3[0].ID != ids[0] {
		t.Errorf("page 3: %+v, %d; want the oldest", page3, total)
	}
	past, total, _ := s.ListReceived(ctx, ada, 2, 10)
	if total != 5 || len(past) != 0 || past == nil {
		t.Errorf("past the end: %+v, %d; want an empty list and the total", past, total)
	}

	// Connections are ordered by when they were accepted, not sent.
	for _, id := range []string{ids[0], ids[4], ids[2]} {
		if _, _, err := s.AcceptRequest(ctx, id, ada); err != nil {
			t.Fatal(err)
		}
	}
	exec(t, pool, "UPDATE connections SET responded_at = now() - make_interval(mins => 3) WHERE id = $1", ids[0])
	exec(t, pool, "UPDATE connections SET responded_at = now() - make_interval(mins => 2) WHERE id = $1", ids[4])
	exec(t, pool, "UPDATE connections SET responded_at = now() - make_interval(mins => 1) WHERE id = $1", ids[2])
	conns, _, _ := s.ListConnections(ctx, ada, 20, 0)
	var senders []string
	for _, c := range conns {
		senders = append(senders, c.Other.UserID)
	}
	var want []string
	for _, id := range []string{ids[2], ids[4], ids[0]} {
		var u string
		if err := pool.QueryRow(ctx, "SELECT requester_id FROM connections WHERE id = $1", id).Scan(&u); err != nil {
			t.Fatal(err)
		}
		want = append(want, u)
	}
	if len(senders) != 3 || senders[0] != want[0] || senders[1] != want[1] || senders[2] != want[2] {
		t.Errorf("connections in order %v, want %v", senders, want)
	}
}

func TestFindPerson(t *testing.T) {
	s, pool := newTestStore(t)
	ada := newUser(t, pool, "Ada")
	p, err := s.FindPerson(t.Context(), ada)
	if err != nil || p.FullName != "Ada" || p.PhotoFileID != nil {
		t.Errorf("FindPerson: %+v, %v", p, err)
	}
	for _, id := range []string{"not-a-uuid", "00000000-0000-0000-0000-000000000000"} {
		if _, err := s.FindPerson(t.Context(), id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("FindPerson(%s): %v, want ErrNotFound", id, err)
		}
	}
}
