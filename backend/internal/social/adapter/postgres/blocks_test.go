//go:build integration

package postgres

import (
	"slices"
	"testing"
)

func TestBlocks(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob, cy := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Cy")

	for _, want := range []bool{true, false} {
		added, err := s.AddBlock(ctx, ada, bob)
		if err != nil || added != want {
			t.Fatalf("AddBlock = %v, %v; want %v", added, err, want)
		}
	}
	if st, err := s.BlockBetween(ctx, ada, bob); err != nil || !st.ByMe || st.ByThem {
		t.Errorf("BlockBetween(ada, bob) = %+v, %v", st, err)
	}
	if st, err := s.BlockBetween(ctx, bob, ada); err != nil || st.ByMe || !st.ByThem {
		t.Errorf("BlockBetween(bob, ada) = %+v, %v", st, err)
	}
	if st, err := s.BlockBetween(ctx, "not-a-uuid", bob); err != nil || st.ByMe || st.ByThem {
		t.Errorf("BlockBetween with a bad ID = %+v, %v", st, err)
	}

	// Newest first; a suspended user is left out of the list and the total.
	if _, err := s.AddBlock(ctx, ada, cy); err != nil {
		t.Fatal(err)
	}
	exec(t, pool, "UPDATE blocks SET created_at = now() + interval '1 minute' WHERE blocked_id = $1", cy)
	items, total, err := s.ListBlocks(ctx, ada, 20, 0)
	if err != nil || total != 2 || items[0].Other.UserID != cy || items[1].Other.FullName != "Bob" {
		t.Fatalf("ListBlocks = %+v, %d, %v", items, total, err)
	}
	exec(t, pool, "UPDATE users SET suspended_at = now() WHERE id = $1", cy)
	if items, total, err := s.ListBlocks(ctx, ada, 20, 0); err != nil || total != 1 || len(items) != 1 {
		t.Errorf("ListBlocks with a suspended user = %+v, %d, %v", items, total, err)
	}

	if err := s.RemoveBlock(ctx, bob, ada); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.BlockBetween(ctx, ada, bob); !st.ByMe {
		t.Error("the blocked person's RemoveBlock removed the block")
	}
	if err := s.RemoveBlock(ctx, ada, "not-a-uuid"); err != nil {
		t.Errorf("RemoveBlock with a bad ID: %v", err)
	}
	if err := s.RemoveBlock(ctx, ada, bob); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.BlockBetween(ctx, ada, bob); st.ByMe {
		t.Error("RemoveBlock left the block")
	}
}

func TestRemoveLiveLink(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob, cy := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Cy")
	pending := create(t, s, ada, bob)
	rejected := create(t, s, ada, cy)
	if _, err := s.RejectRequest(ctx, rejected.ID, cy); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveLiveLink(ctx, bob, ada); err != nil {
		t.Fatal(err)
	}
	if _, found := liveLink(t, s, ada, bob); found {
		t.Errorf("request %s is still live", pending.ID)
	}
	if err := s.RemoveLiveLink(ctx, ada, cy); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM connections WHERE id = $1", rejected.ID).Scan(&n); err != nil || n != 1 {
		t.Errorf("the rejected row: %d, %v", n, err)
	}
}

// A connection list is what the viewer may see: people who blocked the viewer are left out of
// the page, the list's total and the count.
func TestConnectionsLeaveOutWhoBlockedTheViewer(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob, cy, dee := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Cy"), newUser(t, pool, "Dee")
	for _, u := range []string{ada, bob} {
		r := create(t, s, u, cy)
		if _, _, err := s.AcceptRequest(ctx, r.ID, cy); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AddBlock(ctx, ada, dee); err != nil {
		t.Fatal(err)
	}
	if items, total, err := s.ListConnections(ctx, dee, cy, 20, 0); err != nil || total != 1 || items[0].Other.UserID != bob {
		t.Errorf("the blocked viewer: %+v, %d, %v", items, total, err)
	}
	if n, err := s.CountConnections(ctx, dee, cy); err != nil || n != 1 {
		t.Errorf("count for the blocked viewer: %d, %v", n, err)
	}
	if n, err := s.CountConnections(ctx, ada, cy); err != nil || n != 2 {
		t.Errorf("count for the blocker: %d, %v", n, err)
	}
}

// The key of the LockPair query does not depend on the order of the pair, and holding it
// keeps another transaction out.
func TestLockPairKeyIsOrderless(t *testing.T) {
	_, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
	first, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Rollback(ctx) }()
	var key int64
	if err := first.QueryRow(ctx, `SELECT hashtextextended(LEAST($1::uuid, $2::uuid)::text || GREATEST($1::uuid, $2::uuid)::text, 0)`, ada, bob).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", key); err != nil {
		t.Fatal(err)
	}
	second, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Rollback(ctx) }()
	var got bool
	if err := second.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1)", key).Scan(&got); err != nil || got {
		t.Errorf("the second lock was granted: %v, %v", got, err)
	}
}

// BlockedAmong finds blocks either way between the user and the listed IDs, and nothing else
// (FR-16.6: a blocked pair cannot be put in a new group).
func TestBlockedAmong(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob, cy, dee, eli := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Cy"), newUser(t, pool, "Dee"), newUser(t, pool, "Eli")
	for _, b := range [][2]string{{ada, bob}, {cy, ada}, {dee, eli}} {
		if _, err := s.AddBlock(ctx, b[0], b[1]); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.BlockedAmong(ctx, ada, []string{bob, cy, dee, eli, "not-a-uuid"})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	want := []string{bob, cy}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("BlockedAmong = %v, want %v", got, want)
	}
	if got, err := s.BlockedAmong(ctx, ada, nil); err != nil || len(got) != 0 {
		t.Errorf("no IDs: %v, %v", got, err)
	}
}
