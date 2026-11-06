package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// fakeConnections answers every viewer with relation (or byUser's entry, for Relations)
// and every user with count, and remembers the calls.
type fakeConnections struct {
	relation  domain.ConnectionRelation
	byUser    map[string]domain.ConnectionRelation
	count     int
	err       error
	relations [][2]string
	lists     [][]string
	counted   []string
	block     domain.BlockState
}

func (f *fakeConnections) Block(context.Context, string, string) (domain.BlockState, error) {
	return f.block, f.err
}

func (f *fakeConnections) Relations(_ context.Context, _ string, userIDs []string) (map[string]domain.ConnectionRelation, error) {
	f.lists = append(f.lists, userIDs)
	out := map[string]domain.ConnectionRelation{}
	for _, id := range userIDs {
		out[id] = f.byUser[id]
	}
	return out, f.err
}

func (f *fakeConnections) Relation(_ context.Context, viewerID, ownerID string) (domain.ConnectionRelation, error) {
	f.relations = append(f.relations, [2]string{viewerID, ownerID})
	return f.relation, f.err
}

func (f *fakeConnections) Count(_ context.Context, _, userID string) (int, error) {
	f.counted = append(f.counted, userID)
	return f.count, f.err
}

// A viewed profile carries the viewer's connection status with the owner and the owner's
// number of connections (FR-7).
func TestViewShowsConnection(t *testing.T) {
	id := "00000000-0000-0000-0000-0000000000a1"
	conns := &fakeConnections{relation: domain.ConnectionRelation{Status: domain.ConnectionPendingReceived, RequestID: &id}, count: 312}
	svc := New(newFakeStore(testAccounts()...), WithConnections(conns))
	p, err := svc.View(t.Context(), eveID, adaID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Connection.Status != domain.ConnectionPendingReceived || p.Connection.RequestID == nil || *p.Connection.RequestID != id {
		t.Errorf("Connection = %+v, want pending_received with the request ID", p.Connection)
	}
	if p.ConnectionCount != 312 {
		t.Errorf("ConnectionCount = %d, want 312", p.ConnectionCount)
	}
	if len(conns.relations) != 1 || conns.relations[0] != [2]string{eveID, adaID} {
		t.Errorf("Relation calls %v, want one for eve viewing ada", conns.relations)
	}
	if len(conns.counted) != 1 || conns.counted[0] != adaID {
		t.Errorf("Count calls %v, want one for ada", conns.counted)
	}
}

// The own profile's relation comes from the lookup too, which answers self, and it shows
// its count.
func TestOwnProfileAsksTheLookup(t *testing.T) {
	conns := &fakeConnections{relation: domain.ConnectionRelation{Status: domain.ConnectionSelf}, count: 4}
	svc := New(newFakeStore(testAccounts()...), WithConnections(conns))
	p, err := svc.GetOwn(t.Context(), adaID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Connection.Status != domain.ConnectionSelf || p.Connection.RequestID != nil || p.ConnectionCount != 4 {
		t.Errorf("own profile: %+v, count %d; want self and 4", p.Connection, p.ConnectionCount)
	}
	if len(conns.relations) != 1 || conns.relations[0] != [2]string{adaID, adaID} {
		t.Errorf("Relation calls %v, want one for ada viewing ada", conns.relations)
	}
}

// Without a lookup, the own profile reads as self and anyone else's as none.
func TestNoLookupDefaults(t *testing.T) {
	svc := New(newFakeStore(testAccounts()...))
	own, _ := svc.GetOwn(t.Context(), adaID)
	other, _ := svc.View(t.Context(), eveID, adaID)
	if own.Connection.Status != domain.ConnectionSelf || other.Connection.Status != domain.ConnectionNone {
		t.Errorf("own %s, other %s; want self and none", own.Connection.Status, other.Connection.Status)
	}
}

func TestConnectionLookupError(t *testing.T) {
	boom := errors.New("boom")
	svc := New(newFakeStore(testAccounts()...), WithConnections(&fakeConnections{err: boom}))
	if _, err := svc.View(t.Context(), eveID, adaID); !errors.Is(err, boom) {
		t.Errorf("View: %v, want the lookup error", err)
	}
	if _, err := svc.GetOwn(t.Context(), adaID); !errors.Is(err, boom) {
		t.Errorf("GetOwn: %v, want the lookup error", err)
	}
}

// FR-16.9: the owner who blocked the viewer does not exist for them, on the profile or the
// publications; the blocker still opens the blocked person's profile.
func TestBlockedViewer(t *testing.T) {
	blockedBy := New(newFakeStore(testAccounts()...), WithConnections(&fakeConnections{block: domain.BlockState{ByOwner: true}}))
	if _, err := blockedBy.View(t.Context(), eveID, adaID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("View by the blocked person: %v, want ErrNotFound", err)
	}
	if _, err := blockedBy.ListPublications(t.Context(), eveID, adaID, 1, 10); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("ListPublications by the blocked person: %v, want ErrNotFound", err)
	}

	blocker := New(newFakeStore(testAccounts()...), WithConnections(&fakeConnections{block: domain.BlockState{ByViewer: true}}))
	p, err := blocker.View(t.Context(), eveID, adaID)
	if err != nil {
		t.Fatalf("View by the blocker: %v", err)
	}
	if !p.BlockedByMe {
		t.Error("BlockedByMe is false for the blocker")
	}
	if _, err := blocker.ListPublications(t.Context(), eveID, adaID, 1, 10); err != nil {
		t.Errorf("ListPublications by the blocker: %v", err)
	}
}

// fakeConversations answers every lookup with link, or err, and records who asked.
type fakeConversations struct {
	link  *domain.ConversationLink
	err   error
	calls [][2]string
}

func (f *fakeConversations) Between(_ context.Context, viewerID, ownerID string) (*domain.ConversationLink, error) {
	f.calls = append(f.calls, [2]string{viewerID, ownerID})
	return f.link, f.err
}

// A profile carries the viewer's conversation with its owner, nil when there is none.
func TestViewShowsConversation(t *testing.T) {
	link := &domain.ConversationLink{ID: "00000000-0000-0000-0000-0000000000c1", State: "active", CanMessage: true}
	for _, tc := range []struct {
		name string
		link *domain.ConversationLink
	}{{"one exists", link}, {"none", nil}} {
		t.Run(tc.name, func(t *testing.T) {
			chats := &fakeConversations{link: tc.link}
			svc := New(newFakeStore(testAccounts()...), WithConversations(chats))
			p, err := svc.View(t.Context(), eveID, adaID)
			if err != nil {
				t.Fatal(err)
			}
			if p.Conversation != tc.link {
				t.Errorf("Conversation = %+v, want %+v", p.Conversation, tc.link)
			}
			if len(chats.calls) != 1 || chats.calls[0] != [2]string{eveID, adaID} {
				t.Errorf("Between calls %v, want one for eve viewing ada", chats.calls)
			}
		})
	}
}

// One's own profile has no conversation and asks for none, by either route.
func TestOwnProfileHasNoConversation(t *testing.T) {
	chats := &fakeConversations{link: &domain.ConversationLink{ID: "x"}}
	svc := New(newFakeStore(testAccounts()...), WithConversations(chats))
	own, err := svc.GetOwn(t.Context(), adaID)
	if err != nil {
		t.Fatal(err)
	}
	viewed, err := svc.View(t.Context(), adaID, adaID)
	if err != nil {
		t.Fatal(err)
	}
	if own.Conversation != nil || viewed.Conversation != nil || len(chats.calls) != 0 {
		t.Errorf("own %+v, viewed %+v, %d lookups; want none", own.Conversation, viewed.Conversation, len(chats.calls))
	}
}

// A failed lookup fails the view rather than hiding a conversation that exists.
func TestConversationLookupError(t *testing.T) {
	boom := errors.New("boom")
	svc := New(newFakeStore(testAccounts()...), WithConversations(&fakeConversations{err: boom}))
	if _, err := svc.View(t.Context(), eveID, adaID); !errors.Is(err, boom) {
		t.Errorf("View error = %v, want boom", err)
	}
}
