package usecase

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/notification/domain"
)

var ctx = context.Background()

// fakeStore keeps notifications in memory and applies the schema's rule of one
// notification per recipient, actor and connection type (an upsert). hidden holds actors
// who are suspended or not verified. A test sets err to make every call fail.
type fakeStore struct {
	rows    []*domain.Notification
	hidden  map[string]bool
	pending map[string]bool // connection requests still pending, by ID
	clock   time.Time
	next    int
	err     error
	lastLim int
}

var _ domain.Store = (*fakeStore)(nil)

func newFakeStore() *fakeStore {
	return &fakeStore{hidden: map[string]bool{}, pending: map[string]bool{},
		clock: time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)}
}

func (s *fakeStore) visible(n *domain.Notification) bool { return !s.hidden[n.ActorID] }

func (s *fakeStore) DeleteBetween(_ context.Context, a, b string) error {
	if s.err != nil {
		return s.err
	}
	s.rows = slices.DeleteFunc(s.rows, func(n *domain.Notification) bool {
		return (n.RecipientID == a && n.ActorID == b) || (n.RecipientID == b && n.ActorID == a)
	})
	return nil
}

func (s *fakeStore) Put(_ context.Context, n domain.Notification) error {
	if s.err != nil {
		return s.err
	}
	s.clock = s.clock.Add(time.Second)
	for _, r := range s.rows {
		if r.RecipientID == n.RecipientID && r.ActorID == n.ActorID && r.Type == n.Type {
			r.SubjectID, r.CreatedAt, r.ReadAt = n.SubjectID, s.clock, nil
			return nil
		}
	}
	s.next++
	n.ID = fmt.Sprintf("00000000-0000-0000-0000-%012d", s.next)
	n.CreatedAt, n.ReadAt = s.clock, nil
	s.rows = append(s.rows, &n)
	return nil
}

func (s *fakeStore) DeleteBySubject(_ context.Context, t domain.Type, subjectID string) error {
	if s.err != nil {
		return s.err
	}
	s.rows = slices.DeleteFunc(s.rows, func(r *domain.Notification) bool { return r.Type == t && r.SubjectID == subjectID })
	return nil
}

func (s *fakeStore) List(_ context.Context, recipientID string, after *domain.Cursor, limit int) ([]domain.Item, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.lastLim = limit
	var mine []*domain.Notification
	for _, r := range s.rows {
		if r.RecipientID == recipientID && s.visible(r) {
			mine = append(mine, r)
		}
	}
	slices.SortFunc(mine, func(a, b *domain.Notification) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})
	var out []domain.Item
	for _, r := range mine {
		if after != nil && !olderThan(r, after) {
			continue
		}
		if len(out) == limit {
			break
		}
		p := s.pending[r.SubjectID]
		out = append(out, domain.Item{Notification: *r, Actor: domain.Actor{UserID: r.ActorID, FullName: "Name of " + r.ActorID}, RequestPending: &p})
	}
	return out, nil
}

// olderThan reports whether r comes after the cursor in the list order.
func olderThan(r *domain.Notification, c *domain.Cursor) bool {
	return r.CreatedAt.Before(c.CreatedAt) || r.CreatedAt.Equal(c.CreatedAt) && r.ID < c.ID
}

func (s *fakeStore) CountUnread(_ context.Context, recipientID string) (int, error) {
	n := 0
	for _, r := range s.rows {
		if r.RecipientID == recipientID && r.ReadAt == nil && s.visible(r) {
			n++
		}
	}
	return n, s.err
}

func (s *fakeStore) MarkRead(_ context.Context, id, recipientID string) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	for _, r := range s.rows {
		if r.ID == id && r.RecipientID == recipientID && s.visible(r) {
			if r.ReadAt == nil {
				t := s.clock
				r.ReadAt = &t
			}
			return true, nil
		}
	}
	return false, nil
}

func (s *fakeStore) MarkAllRead(_ context.Context, recipientID string) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	n := 0
	for _, r := range s.rows {
		if r.RecipientID == recipientID && r.ReadAt == nil && s.visible(r) {
			t := s.clock
			r.ReadAt = &t
			n++
		}
	}
	return n, nil
}

const (
	ada  = "00000000-0000-0000-0000-00000000000a"
	bob  = "00000000-0000-0000-0000-00000000000b"
	cy   = "00000000-0000-0000-0000-00000000000c"
	req1 = "00000000-0000-0000-0000-0000000000d1"
	req2 = "00000000-0000-0000-0000-0000000000d2"
)

func newEnv() (*Service, *fakeStore) {
	s := newFakeStore()
	return New(s), s
}

func list(t *testing.T, svc *Service, user string) []domain.Item {
	t.Helper()
	p, err := svc.List(ctx, user, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return p.Items
}

func count(t *testing.T, svc *Service, user string) int {
	t.Helper()
	n, err := svc.UnreadCount(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// Every type has a registry entry with a subject kind and a link (FR-9.5).
func TestRegistryCoversEveryType(t *testing.T) {
	for _, typ := range []domain.Type{domain.TypeConnectionRequestReceived, domain.TypeConnectionRequestAccepted} {
		spec, ok := domain.Registry[typ]
		if !ok || spec.SubjectKind == "" || spec.Link == nil {
			t.Errorf("type %s has no complete registry entry", typ)
		}
	}
}

// A sent request notifies the receiver, from the sender (FR-9.1).
func TestConnectionRequestSent(t *testing.T) {
	svc, s := newEnv()
	s.pending[req1] = true
	if err := svc.ConnectionRequestSent(ctx, req1, ada, bob); err != nil {
		t.Fatal(err)
	}
	items := list(t, svc, bob)
	if len(items) != 1 {
		t.Fatalf("bob has %d notifications, want 1", len(items))
	}
	it := items[0]
	if it.Type != domain.TypeConnectionRequestReceived || it.ActorID != ada || it.SubjectID != req1 || it.Link != "/network" ||
		it.RequestPending == nil || !*it.RequestPending || it.ReadAt != nil {
		t.Errorf("bob's notification %+v, want an unread pending request from ada linking to /network", it)
	}
	if len(list(t, svc, ada)) != 0 {
		t.Error("the sender was notified")
	}
	if count(t, svc, bob) != 1 {
		t.Error("bob's unread count is not 1")
	}
}

// An accepted request notifies the original sender, from the receiver (FR-9.1).
func TestConnectionRequestAccepted(t *testing.T) {
	svc, s := newEnv()
	s.pending[req1] = true
	if err := svc.ConnectionRequestSent(ctx, req1, ada, bob); err != nil {
		t.Fatal(err)
	}
	s.pending[req1] = false
	if err := svc.ConnectionRequestAccepted(ctx, req1, ada, bob); err != nil {
		t.Fatal(err)
	}
	items := list(t, svc, ada)
	if len(items) != 1 {
		t.Fatalf("ada has %d notifications, want 1", len(items))
	}
	it := items[0]
	if it.Type != domain.TypeConnectionRequestAccepted || it.ActorID != bob || it.Link != "/profile/"+bob || it.RequestPending != nil {
		t.Errorf("ada's notification %+v, want accepted by bob linking to bob's profile, without requestPending", it)
	}
	if got := list(t, svc, bob); len(got) != 1 || *got[0].RequestPending {
		t.Errorf("bob's received notification %+v, want it kept and no longer pending", got)
	}
}

// Cancelling or rejecting removes the received notification, and re-sending afterwards,
// or while a stale one remains, leaves exactly one.
func TestWithdrawAndResend(t *testing.T) {
	svc, _ := newEnv()
	if err := svc.ConnectionRequestSent(ctx, req1, ada, bob); err != nil {
		t.Fatal(err)
	}
	if err := svc.ConnectionRequestWithdrawn(ctx, req1); err != nil {
		t.Fatal(err)
	}
	if n := len(list(t, svc, bob)); n != 0 {
		t.Fatalf("bob has %d notifications after the withdrawal, want 0", n)
	}
	if count(t, svc, bob) != 0 {
		t.Error("the withdrawn notification is still counted")
	}
	if err := svc.ConnectionRequestSent(ctx, req2, ada, bob); err != nil {
		t.Fatal(err)
	}
	if err := svc.ConnectionRequestSent(ctx, req1, ada, bob); err != nil { // a stale one is replaced
		t.Fatal(err)
	}
	items := list(t, svc, bob)
	if len(items) != 1 || items[0].SubjectID != req1 {
		t.Errorf("bob has %+v, want one notification about the latest request", items)
	}
	if err := svc.ConnectionRequestWithdrawn(ctx, req2); err != nil {
		t.Fatal(err)
	}
	if len(list(t, svc, bob)) != 1 {
		t.Error("withdrawing another request removed this one")
	}
}

// A re-sent request after the first was read is unread again.
func TestResendIsUnread(t *testing.T) {
	svc, _ := newEnv()
	if err := svc.ConnectionRequestSent(ctx, req1, ada, bob); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MarkAllRead(ctx, bob); err != nil {
		t.Fatal(err)
	}
	if err := svc.ConnectionRequestSent(ctx, req2, ada, bob); err != nil {
		t.Fatal(err)
	}
	if count(t, svc, bob) != 1 {
		t.Error("the replaced notification is not unread")
	}
}

// Actors who are suspended or unverified are left out of the list and the count.
func TestHiddenActor(t *testing.T) {
	svc, s := newEnv()
	if err := svc.ConnectionRequestSent(ctx, req1, ada, bob); err != nil {
		t.Fatal(err)
	}
	s.hidden[ada] = true
	if len(list(t, svc, bob)) != 0 || count(t, svc, bob) != 0 {
		t.Error("a hidden actor's notification is shown or counted")
	}
}

// Pages are newest first, follow the cursor without overlap, and the last one has no
// cursor (FR-9.5).
func TestListPaging(t *testing.T) {
	svc, s := newEnv()
	for i := range 25 {
		actor := fmt.Sprintf("00000000-0000-0000-0001-%012d", i)
		if err := svc.ConnectionRequestSent(ctx, fmt.Sprintf("00000000-0000-0000-0002-%012d", i), actor, bob); err != nil {
			t.Fatal(err)
		}
	}
	p1, err := svc.List(ctx, bob, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p1.Items) != DefaultLimit || p1.NextCursor == nil || s.lastLim != DefaultLimit+1 {
		t.Fatalf("first page has %d items, cursor %v, asked for %d; want %d with a cursor", len(p1.Items), p1.NextCursor, s.lastLim, DefaultLimit)
	}
	p2, err := svc.List(ctx, bob, *p1.NextCursor, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p2.Items) != 5 || p2.NextCursor != nil {
		t.Fatalf("second page has %d items, cursor %v; want 5 and none", len(p2.Items), p2.NextCursor)
	}
	all := append(p1.Items, p2.Items...)
	seen := map[string]bool{}
	for i, it := range all {
		if seen[it.ID] {
			t.Errorf("%s is on both pages", it.ID)
		}
		seen[it.ID] = true
		if i > 0 && it.CreatedAt.After(all[i-1].CreatedAt) {
			t.Errorf("item %d is newer than the one before", i)
		}
	}
}

func TestListLimit(t *testing.T) {
	svc, s := newEnv()
	ptr := func(n int) *int { return &n }
	for _, tc := range []struct {
		limit *int
		want  int
	}{{nil, DefaultLimit}, {ptr(1), 1}, {ptr(50), 50}, {ptr(51), 50}, {ptr(1000), 50}} {
		if _, err := svc.List(ctx, bob, "", tc.limit); err != nil {
			t.Fatal(err)
		}
		if s.lastLim != tc.want+1 {
			t.Errorf("limit %v asked the store for %d, want %d", tc.limit, s.lastLim, tc.want+1)
		}
	}
	for _, n := range []int{0, -1} {
		if _, err := svc.List(ctx, bob, "", ptr(n)); !errors.Is(err, domain.ErrInvalidLimit) {
			t.Errorf("limit %d: %v, want ErrInvalidLimit", n, err)
		}
	}
}

func TestCursor(t *testing.T) {
	c := domain.Cursor{CreatedAt: time.Date(2026, 11, 2, 9, 0, 0, 123456000, time.UTC), ID: req1}
	got, err := DecodeCursor(EncodeCursor(c))
	if err != nil || !got.CreatedAt.Equal(c.CreatedAt) || got.ID != c.ID {
		t.Errorf("round trip gave %+v, %v; want %+v", got, err, c)
	}
	svc, _ := newEnv()
	for _, bad := range []string{"!!", "bm90aGluZw", EncodeCursor(domain.Cursor{CreatedAt: c.CreatedAt, ID: "x"}),
		"MjAyNi0xMS0wMnwwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwZDE"} {
		if _, err := svc.List(ctx, bob, bad, nil); !errors.Is(err, domain.ErrInvalidCursor) {
			t.Errorf("cursor %q: %v, want ErrInvalidCursor", bad, err)
		}
	}
}

// Marking one read is idempotent; another user's, a hidden one or an unknown ID is
// ErrNotFound (FR-9.6).
func TestMarkRead(t *testing.T) {
	svc, s := newEnv()
	if err := svc.ConnectionRequestSent(ctx, req1, ada, bob); err != nil {
		t.Fatal(err)
	}
	id := list(t, svc, bob)[0].ID
	for range 2 {
		if err := svc.MarkRead(ctx, bob, id); err != nil {
			t.Fatalf("MarkRead: %v", err)
		}
	}
	if count(t, svc, bob) != 0 || list(t, svc, bob)[0].ReadAt == nil {
		t.Error("the notification is not read")
	}
	for _, tc := range []struct{ user, id string }{{ada, id}, {cy, id}, {bob, req2}, {bob, "not-a-uuid"}} {
		if err := svc.MarkRead(ctx, tc.user, tc.id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("MarkRead(%s, %s): %v, want ErrNotFound", tc.user, tc.id, err)
		}
	}
	s.hidden[ada] = true
	if err := svc.MarkRead(ctx, bob, id); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("MarkRead of a hidden actor's notification: %v, want ErrNotFound", err)
	}
}

// Marking all read marks only the caller's visible unread ones and says how many (FR-9.6).
func TestMarkAllRead(t *testing.T) {
	svc, s := newEnv()
	for _, n := range [][3]string{{req1, ada, bob}, {req2, cy, bob}, {req1, bob, ada}} {
		if err := svc.ConnectionRequestSent(ctx, n[0], n[1], n[2]); err != nil {
			t.Fatal(err)
		}
	}
	s.hidden[cy] = true
	n, err := svc.MarkAllRead(ctx, bob)
	if err != nil || n != 1 {
		t.Fatalf("MarkAllRead = %d, %v; want 1", n, err)
	}
	if n, _ := svc.MarkAllRead(ctx, bob); n != 0 {
		t.Errorf("second MarkAllRead = %d, want 0", n)
	}
	if count(t, svc, ada) != 1 {
		t.Error("another user's notification was marked")
	}
}

func TestStoreErrors(t *testing.T) {
	svc, s := newEnv()
	boom := errors.New("boom")
	s.err = boom
	if err := svc.ConnectionRequestSent(ctx, req1, ada, bob); !errors.Is(err, boom) {
		t.Errorf("ConnectionRequestSent: %v", err)
	}
	if err := svc.ConnectionRequestAccepted(ctx, req1, ada, bob); !errors.Is(err, boom) {
		t.Errorf("ConnectionRequestAccepted: %v", err)
	}
	if err := svc.ConnectionRequestWithdrawn(ctx, req1); !errors.Is(err, boom) {
		t.Errorf("ConnectionRequestWithdrawn: %v", err)
	}
	if _, err := svc.List(ctx, bob, "", nil); !errors.Is(err, boom) {
		t.Errorf("List: %v", err)
	}
	if _, err := svc.UnreadCount(ctx, bob); !errors.Is(err, boom) {
		t.Errorf("UnreadCount: %v", err)
	}
	if err := svc.MarkRead(ctx, bob, req1); !errors.Is(err, boom) {
		t.Errorf("MarkRead: %v", err)
	}
	if _, err := svc.MarkAllRead(ctx, bob); !errors.Is(err, boom) {
		t.Errorf("MarkAllRead: %v", err)
	}
}

// A stored type without a registry entry fails the list instead of sending no link.
func TestUnknownTypeFails(t *testing.T) {
	svc, s := newEnv()
	s.rows = append(s.rows, &domain.Notification{ID: req1, RecipientID: bob, ActorID: ada, Type: "post_like", SubjectID: req2, CreatedAt: s.clock})
	if _, err := svc.List(ctx, bob, "", nil); err == nil || !strings.Contains(err.Error(), "registry") {
		t.Errorf("List: %v, want a missing registry entry error", err)
	}
}

// FR-16.9: a block takes back what the two were told about each other, both ways and of
// every type, and nothing about anyone else.
func TestRemoveBetween(t *testing.T) {
	store := newFakeStore()
	svc := New(store)
	ctx := t.Context()
	for _, n := range []domain.Notification{
		{RecipientID: "ada", ActorID: "bob", Type: domain.TypeConnectionRequestAccepted, SubjectID: "1"},
		{RecipientID: "bob", ActorID: "ada", Type: domain.TypeConnectionRequestReceived, SubjectID: "2"},
		{RecipientID: "ada", ActorID: "cy", Type: domain.TypeConnectionRequestAccepted, SubjectID: "3"},
	} {
		if err := store.Put(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.RemoveBetween(ctx, "bob", "ada"); err != nil {
		t.Fatal(err)
	}
	if len(store.rows) != 1 || store.rows[0].ActorID != "cy" {
		t.Errorf("rows left: %+v, want only cy's", store.rows)
	}
}
