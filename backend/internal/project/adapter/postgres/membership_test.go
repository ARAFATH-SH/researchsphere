//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// inTx runs fn in one transaction, as the usecase does.
func (d db) inTx(fn func(ctx context.Context) error) error {
	return postgres.NewTransactor(d.pool).InTx(d.t.Context(), fn)
}

// FR-11.1, FR-11.3: what Relation reads, one pending row per user and project of either
// kind, and the rows' fields.
func TestRelationAndRequests(t *testing.T) {
	d := newDB(t)
	ctx := t.Context()
	ada, bob, cy := d.user("Ada"), d.user("Bob"), d.user("Cy")
	p := d.project(ada, "Graph learning", "A study")

	rel, err := d.store.Relation(ctx, p, bob)
	if err != nil || !rel.Exists || !rel.Available || rel.Blocked || rel.Role != domain.RoleNone || rel.Pending != nil {
		t.Fatalf("relation = %+v, %v", rel, err)
	}
	if rel, _ := d.store.Relation(ctx, p, ada); rel.Role != domain.RoleOwner {
		t.Errorf("owner's role = %q", rel.Role)
	}
	if rel, _ := d.store.Relation(ctx, p, "00000000-0000-0000-0000-000000000000"); rel.Exists {
		t.Error("an unknown user exists")
	}
	if rel, _ := d.store.Relation(ctx, p, "nope"); rel.Exists {
		t.Error("a user that is not a UUID exists")
	}

	msg := "Hello"
	r, err := d.store.CreateRequest(ctx, domain.NewJoinRequest{ProjectID: p, UserID: bob, Kind: domain.KindRequest, Message: &msg})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != domain.RequestPending || r.Kind != domain.KindRequest || r.Role != domain.RoleNone || r.Message == nil || *r.Message != msg || r.DecidedAt != nil {
		t.Errorf("request = %+v", r)
	}
	if _, err := d.store.CreateRequest(ctx, domain.NewJoinRequest{ProjectID: p, UserID: bob, Kind: domain.KindInvitation, Role: domain.RoleViewer}); !errors.Is(err, domain.ErrRequestPending) {
		t.Errorf("a second pending row: %v", err)
	}
	if rel, _ := d.store.Relation(ctx, p, bob); rel.Pending == nil || rel.Pending.ID != r.ID {
		t.Errorf("pending = %+v", rel.Pending)
	}
	inv, err := d.store.CreateRequest(ctx, domain.NewJoinRequest{ProjectID: p, UserID: cy, Kind: domain.KindInvitation, Role: domain.RoleEditor})
	if err != nil || inv.Role != domain.RoleEditor || inv.Message != nil {
		t.Fatalf("invitation = %+v, %v", inv, err)
	}

	// Blocks either way.
	d.exec("INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)", cy, ada)
	if rel, _ := d.store.Relation(ctx, p, cy); !rel.Blocked {
		t.Error("cy's block of the owner is not seen")
	}
	// Suspended now; a suspension that ended no longer counts.
	d.exec("UPDATE users SET suspended_at = now() WHERE id = $1", bob)
	if rel, _ := d.store.Relation(ctx, p, bob); rel.Available {
		t.Error("a suspended user is available")
	}
	d.exec("UPDATE users SET suspended_at = now() - interval '2 hours', suspended_until = now() - interval '1 hour' WHERE id = $1", bob)
	if rel, _ := d.store.Relation(ctx, p, bob); !rel.Available {
		t.Error("a suspension that ended still counts")
	}

	// Lock by kind and project only.
	if _, err := d.store.LockRequest(ctx, p, r.ID, domain.KindInvitation); !errors.Is(err, domain.ErrRequestNotFound) {
		t.Errorf("a request locked as an invitation: %v", err)
	}
	other := d.project(cy, "Other", "x")
	if _, err := d.store.LockRequest(ctx, other, r.ID, domain.KindRequest); !errors.Is(err, domain.ErrRequestNotFound) {
		t.Errorf("a request locked in another project: %v", err)
	}
	if _, err := d.store.LockRequest(ctx, p, "nope", domain.KindRequest); !errors.Is(err, domain.ErrRequestNotFound) {
		t.Errorf("an ID that is not a UUID: %v", err)
	}
	if err := d.inTx(func(ctx context.Context) error {
		got, err := d.store.LockRequest(ctx, p, r.ID, domain.KindRequest)
		if err != nil || got.ID != r.ID {
			t.Errorf("lock = %+v, %v", got, err)
		}
		return d.store.DecideRequest(ctx, r.ID, domain.RequestRejected)
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := d.store.LockRequest(ctx, p, r.ID, domain.KindRequest)
	if got.Status != domain.RequestRejected || got.DecidedAt == nil {
		t.Errorf("decided = %+v", got)
	}
	// After a rejection a new request is allowed; a cancel deletes the row.
	r2, err := d.store.CreateRequest(ctx, domain.NewJoinRequest{ProjectID: p, UserID: bob, Kind: domain.KindRequest})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.store.DeleteRequest(ctx, r2.ID); err != nil {
		t.Fatal(err)
	}
	if n := d.count("SELECT count(*) FROM project_join_requests WHERE id = $1", r2.ID); n != 0 {
		t.Error("the cancelled request is still there")
	}
}

// FR-11.2 to FR-11.4, FR-10.5: members, roles, removal and a transfer that keeps exactly one
// owner; the database refuses a second owner and no owner.
func TestMembersAndTransfer(t *testing.T) {
	d := newDB(t)
	ctx := t.Context()
	ada, bob, cy := d.user("Ada"), d.user("Bob"), d.user("Cy")
	p := d.project(ada, "Graph learning", "A study")

	m, err := d.store.AddMember(ctx, p, bob, domain.RoleEditor)
	if err != nil || m.UserID != bob || m.Role != domain.RoleEditor || m.FullName != "Bob" || m.JoinedAt.IsZero() {
		t.Fatalf("member = %+v, %v", m, err)
	}
	if _, err := d.store.AddMember(ctx, p, bob, domain.RoleViewer); !errors.Is(err, domain.ErrAlreadyMember) {
		t.Errorf("added twice: %v", err)
	}
	if _, err := d.store.AddMember(ctx, p, cy, domain.RoleViewer); err != nil {
		t.Fatal(err)
	}
	if _, err := d.store.Member(ctx, p, d.user("Dee")); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("a non-member: %v", err)
	}
	ms, total, err := d.store.Members(ctx, p, 2, 0)
	if err != nil || total != 3 || len(ms) != 2 || ms[0].UserID != ada || ms[0].Role != domain.RoleOwner || ms[1].UserID != bob {
		t.Errorf("members = %+v (%d), %v", ms, total, err)
	}

	if err := d.store.SetRole(ctx, p, cy, domain.RoleEditor); err != nil {
		t.Fatal(err)
	}
	if m, _ := d.store.Member(ctx, p, cy); m.Role != domain.RoleEditor {
		t.Errorf("cy's role = %q", m.Role)
	}

	// Promoting before demoting breaks the one-owner index; no owner breaks the trigger.
	if err := d.inTx(func(ctx context.Context) error { return d.store.SetRole(ctx, p, bob, domain.RoleOwner) }); err == nil {
		t.Error("a second owner was allowed")
	}
	if err := d.inTx(func(ctx context.Context) error { return d.store.SetRole(ctx, p, ada, domain.RoleEditor) }); err == nil {
		t.Error("a project was left without an owner")
	}
	// The transfer: demote, then promote, in one transaction.
	if err := d.inTx(func(ctx context.Context) error {
		if err := d.store.SetRole(ctx, p, ada, domain.RoleEditor); err != nil {
			return err
		}
		return d.store.SetRole(ctx, p, bob, domain.RoleOwner)
	}); err != nil {
		t.Fatal(err)
	}
	if n := d.count("SELECT count(*) FROM project_members WHERE project_id = $1 AND role = 'owner'", p); n != 1 {
		t.Errorf("%d owners", n)
	}
	if a, _ := d.store.Lock(ctx, p, ada); a.OwnerID != bob || a.Role != domain.RoleEditor {
		t.Errorf("access after the transfer = %+v", a)
	}

	if err := d.store.RemoveMember(ctx, p, cy); err != nil {
		t.Fatal(err)
	}
	if _, total, _ := d.store.Members(ctx, p, 50, 0); total != 2 {
		t.Errorf("%d members after the removal", total)
	}
}

// The owner's lists put pending rows first and leave out suspended users; the user's own
// lists carry the project; a block hides an invitation and deletes the pending rows.
func TestMembershipListsAndWithdraw(t *testing.T) {
	d := newDB(t)
	ctx := t.Context()
	ada, bob, cy, dee := d.user("Ada"), d.user("Bob"), d.user("Cy"), d.user("Dee")
	p := d.project(ada, "Graph learning", "A study")
	q := d.project(bob, "Corpus", "Texts")

	old, _ := d.store.CreateRequest(ctx, domain.NewJoinRequest{ProjectID: p, UserID: cy, Kind: domain.KindRequest})
	if err := d.store.DecideRequest(ctx, old.ID, domain.RequestRejected); err != nil {
		t.Fatal(err)
	}
	newer, _ := d.store.CreateRequest(ctx, domain.NewJoinRequest{ProjectID: p, UserID: cy, Kind: domain.KindRequest})
	first, _ := d.store.CreateRequest(ctx, domain.NewJoinRequest{ProjectID: p, UserID: bob, Kind: domain.KindRequest})
	gone, _ := d.store.CreateRequest(ctx, domain.NewJoinRequest{ProjectID: p, UserID: dee, Kind: domain.KindRequest})
	d.exec("UPDATE users SET suspended_at = now() WHERE id = $1", dee)

	items, total, err := d.store.ProjectRequests(ctx, p, domain.KindRequest, 50, 0)
	if err != nil || total != 3 || len(items) != 3 {
		t.Fatalf("requests = %+v (%d), %v", items, total, err)
	}
	if items[0].ID != newer.ID || items[1].ID != first.ID || items[2].ID != old.ID || items[0].User.FullName != "Cy" {
		t.Errorf("order = %s %s %s, want pending oldest first then decided", items[0].ID, items[1].ID, items[2].ID)
	}
	for _, it := range items {
		if it.ID == gone.ID {
			t.Error("a suspended user's request is listed")
		}
	}
	mine, total, err := d.store.MyRequests(ctx, cy, 50, 0)
	if err != nil || total != 2 || mine[0].ID != newer.ID || mine[0].Project.Title != "Graph learning" || mine[0].Project.Owner.UserID != ada {
		t.Errorf("cy's requests = %+v (%d), %v", mine, total, err)
	}

	inv, _ := d.store.CreateRequest(ctx, domain.NewJoinRequest{ProjectID: q, UserID: ada, Kind: domain.KindInvitation, Role: domain.RoleViewer})
	invs, total, err := d.store.MyInvitations(ctx, ada, 50, 0)
	if err != nil || total != 1 || invs[0].ID != inv.ID || invs[0].Role != domain.RoleViewer || invs[0].Project.Title != "Corpus" {
		t.Errorf("ada's invitations = %+v (%d), %v", invs, total, err)
	}
	d.exec("INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)", ada, bob)
	if _, total, _ := d.store.MyInvitations(ctx, ada, 50, 0); total != 0 {
		t.Error("an invitation from a blocked owner is listed")
	}

	w, err := d.store.WithdrawBetween(ctx, ada, bob)
	if err != nil {
		t.Fatal(err)
	}
	if len(w) != 2 {
		t.Errorf("withdrawn = %+v, want bob's request and ada's invitation", w)
	}
	if n := d.count("SELECT count(*) FROM project_join_requests WHERE id = ANY($1::uuid[])", []string{first.ID, inv.ID}); n != 0 {
		t.Error("a pending row between them is left")
	}
	if n := d.count("SELECT count(*) FROM project_join_requests WHERE id = ANY($1::uuid[])", []string{old.ID, newer.ID, gone.ID}); n != 3 {
		t.Error("another user's row was withdrawn")
	}
}

// FR-9.5: a title is given only for a project the viewer may see.
func TestVisibleTitles(t *testing.T) {
	d := newDB(t)
	ctx := t.Context()
	ada, bob := d.user("Ada"), d.user("Bob")
	open := d.project(ada, "Open one", "x")
	archived := d.project(ada, "Archived one", "x")
	d.status(archived, domain.StatusArchived)
	got, err := d.store.VisibleTitles(ctx, bob, []string{open, archived, "nope"})
	if err != nil || len(got) != 1 || got[open] != "Open one" {
		t.Errorf("titles = %v, %v", got, err)
	}
	if _, err := d.store.AddMember(ctx, archived, bob, domain.RoleViewer); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.store.VisibleTitles(ctx, bob, []string{archived}); got[archived] != "Archived one" {
		t.Errorf("a member: %v", got)
	}
	d.exec("INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)", ada, bob)
	d.exec("DELETE FROM project_members WHERE user_id = $1", bob)
	if got, _ := d.store.VisibleTitles(ctx, bob, []string{open}); len(got) != 0 {
		t.Errorf("blocked by the owner: %v", got)
	}
}
