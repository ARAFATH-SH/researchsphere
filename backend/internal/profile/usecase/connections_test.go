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

func (f *fakeConnections) Count(_ context.Context, userID string) (int, error) {
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
