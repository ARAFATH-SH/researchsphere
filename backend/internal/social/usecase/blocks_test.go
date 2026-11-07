package usecase

import (
	"errors"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/social/domain"
)

func (e env) block(t *testing.T, blocker, blocked string) {
	t.Helper()
	if err := e.svc.Block(ctx, blocker, blocked); err != nil {
		t.Fatalf("Block %s -> %s: %v", blocker, blocked, err)
	}
}

func TestBlockRefusals(t *testing.T) {
	e := newEnv()
	if err := e.svc.Block(ctx, "ada", "ada"); !errors.Is(err, domain.ErrBlockSelf) {
		t.Errorf("oneself: %v", err)
	}
	for _, u := range []string{"sam", "ghost"} {
		if err := e.svc.Block(ctx, "ada", u); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s: %v", u, err)
		}
	}
	if len(e.store.blocks) != 0 || len(e.notifier.blocked) != 0 {
		t.Errorf("a refused block changed something: %v %v", e.store.blocks, e.notifier.blocked)
	}
}

// FR-16.9: a connection goes, in the transaction that tells the notifier; a second block
// changes nothing.
func TestBlockRemovesConnection(t *testing.T) {
	e := newEnv()
	r := e.send(t, "ada", "bob")
	if _, err := e.svc.Accept(ctx, "bob", r.ID); err != nil {
		t.Fatal(err)
	}
	e.block(t, "bob", "ada")
	for _, p := range [][2]string{{"ada", "bob"}, {"bob", "ada"}} {
		if s := e.relation(t, p[0], p[1]).Status; s != domain.StatusNone {
			t.Errorf("%s sees %s", p[0], s)
		}
	}
	if len(e.notifier.blocked) != 1 || e.notifier.blocked[0] != "bob|ada" || e.notifier.outsideTx != 0 {
		t.Errorf("notifier: %v, %d outside the transaction", e.notifier.blocked, e.notifier.outsideTx)
	}
	e.block(t, "bob", "ada")
	if len(e.notifier.blocked) != 1 {
		t.Errorf("second block notified again: %v", e.notifier.blocked)
	}
}

// FR-16.9: pending requests go, both ways.
func TestBlockCancelsPendingRequests(t *testing.T) {
	for name, tc := range map[string][2]string{"sent by the blocker": {"ada", "bob"}, "sent by the blocked": {"bob", "ada"}} {
		t.Run(name, func(t *testing.T) {
			e := newEnv()
			e.send(t, tc[0], tc[1])
			e.block(t, "ada", "bob")
			if e.store.live("ada", "bob") != nil {
				t.Error("the request is still live")
			}
		})
	}
}

// A rejected row stays as history (FR-7.6).
func TestBlockKeepsRejectedRow(t *testing.T) {
	e := newEnv()
	r := e.send(t, "ada", "bob")
	if err := e.svc.Reject(ctx, "bob", r.ID); err != nil {
		t.Fatal(err)
	}
	e.block(t, "ada", "bob")
	if e.store.byID(r.ID) == nil {
		t.Error("the rejected row is gone")
	}
}

// FR-16.9: the blocked person hears the same 404 as for an unknown user, the blocker a 409.
func TestSendRefusedByBlock(t *testing.T) {
	e := newEnv()
	e.block(t, "ada", "bob")
	if _, err := e.svc.Send(ctx, "bob", "ada"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("blocked person: %v", err)
	}
	if _, err := e.svc.Send(ctx, "ada", "bob"); !errors.Is(err, domain.ErrYouBlocked) {
		t.Errorf("blocker: %v", err)
	}
	e.block(t, "bob", "ada")
	if _, err := e.svc.Send(ctx, "bob", "ada"); !errors.Is(err, domain.ErrYouBlocked) {
		t.Errorf("blocked both ways: %v", err)
	}
	if len(e.limiter.recorded) != 0 || e.store.creates != 0 {
		t.Errorf("a refused request was counted: %v, %d", e.limiter.recorded, e.store.creates)
	}
}

// FR-16.9: unblocking restores nothing; the two may then connect again.
func TestUnblockRestoresNothing(t *testing.T) {
	e := newEnv()
	r := e.send(t, "ada", "bob")
	if _, err := e.svc.Accept(ctx, "bob", r.ID); err != nil {
		t.Fatal(err)
	}
	e.block(t, "ada", "bob")
	for range 2 {
		if err := e.svc.Unblock(ctx, "ada", "bob"); err != nil {
			t.Fatal(err)
		}
	}
	if s := e.relation(t, "ada", "bob").Status; s != domain.StatusNone {
		t.Errorf("status = %s", s)
	}
	e.send(t, "bob", "ada")
}

// The other user's block cannot be undone by the blocked person.
func TestUnblockIsOneDirection(t *testing.T) {
	e := newEnv()
	e.block(t, "ada", "bob")
	if err := e.svc.Unblock(ctx, "bob", "ada"); err != nil {
		t.Fatal(err)
	}
	if s, _ := e.svc.BlockBetween(ctx, "ada", "bob"); !s.ByMe {
		t.Error("the blocked person removed the block")
	}
}

// FR-16.9: on a third user's list the blocked viewer misses the blocker, and the total and
// count agree; the blocker's own list and profile answer 404.
func TestConnectionListsHideBlocker(t *testing.T) {
	e := newEnv()
	for _, u := range []string{"ada", "bob"} {
		r := e.send(t, u, "cy")
		if _, err := e.svc.Accept(ctx, "cy", r.ID); err != nil {
			t.Fatal(err)
		}
	}
	e.block(t, "ada", "bob")
	p, err := e.svc.ListUserConnections(ctx, "bob", "cy", 1)
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 1 || len(p.Items) != 1 || p.Items[0].Other.UserID != "bob" {
		t.Errorf("the blocked viewer sees %+v", p)
	}
	if n, _ := e.svc.CountConnections(ctx, "bob", "cy"); n != 1 {
		t.Errorf("count = %d, want 1", n)
	}
	if p, _ := e.svc.ListUserConnections(ctx, "ada", "cy", 1); p.Total != 2 {
		t.Errorf("the blocker sees total %d, want 2", p.Total)
	}
	if _, err := e.svc.ListUserConnections(ctx, "bob", "ada", 1); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("the blocker's list for the blocked person: %v", err)
	}
	if _, err := e.svc.ListUserConnections(ctx, "ada", "bob", 1); err != nil {
		t.Errorf("the blocked person's list for the blocker: %v", err)
	}
}

func TestListBlocks(t *testing.T) {
	e := newEnv()
	e.block(t, "ada", "bob")
	e.block(t, "ada", "cy")
	e.block(t, "cy", "ada")
	e.store.visible["cy"] = false
	p, err := e.svc.ListBlocks(ctx, "ada", 1)
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 1 || p.Items[0].Other.UserID != "bob" || p.PageSize != PageSize {
		t.Errorf("page = %+v", p)
	}
	for _, page := range []int{0, -1, 1 << 40} {
		if _, err := e.svc.ListBlocks(ctx, "ada", page); !errors.Is(err, domain.ErrInvalidPage) {
			t.Errorf("page %d: %v", page, err)
		}
	}
}
