//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/authz"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

type db struct {
	t     *testing.T
	pool  *pgxpool.Pool
	store *Store
}

func newDB(t *testing.T) db {
	t.Helper()
	pool := testenv.DB(t)
	return db{t: t, pool: pool, store: NewStore(pool)}
}

func (d db) exec(sql string, args ...any) {
	d.t.Helper()
	if _, err := d.pool.Exec(d.t.Context(), sql, args...); err != nil {
		d.t.Fatalf("%s: %v", sql, err)
	}
}

func (d db) count(sql string, args ...any) int {
	d.t.Helper()
	var n int
	if err := d.pool.QueryRow(d.t.Context(), sql, args...).Scan(&n); err != nil {
		d.t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// user stores a verified user with a profile named name and returns its ID.
func (d db) user(name string) string {
	d.t.Helper()
	var id string
	if err := d.pool.QueryRow(d.t.Context(), `INSERT INTO users (email, password_hash, email_verified_at)
		VALUES ($1, 'hash', now()) RETURNING id::text`, testenv.Email(d.t)).Scan(&id); err != nil {
		d.t.Fatal(err)
	}
	d.exec("INSERT INTO profiles (user_id, full_name, headline) VALUES ($1, $2, 'Researcher')", id, name)
	return id
}

// project creates a project the way the usecase does: project, owner and skills in one
// transaction.
func (d db) project(owner, title, description string, skills ...string) string {
	d.t.Helper()
	var id string
	err := postgres.NewTransactor(d.pool).InTx(d.t.Context(), func(ctx context.Context) error {
		var err error
		if id, err = d.store.Create(ctx, domain.NewProject{OwnerID: owner, Title: title, Description: description}); err != nil {
			return err
		}
		return d.store.SetSkills(ctx, id, skills)
	})
	if err != nil {
		d.t.Fatal(err)
	}
	return id
}

func (d db) status(id string, s domain.Status) {
	d.exec("UPDATE projects SET status = $2::project_status WHERE id = $1", id, string(s))
}

func (d db) jobs(id string) int {
	return d.count("SELECT count(*) FROM embedding_jobs WHERE owner_type = 'project' AND owner_id = $1", id)
}

// FR-10.1, FR-10.2, FR-10.6: create with the owner as first member, read, edit, delete; the
// embedding job follows every change of the text and goes with the project (AI-1.2).
func TestProjectLifecycle(t *testing.T) {
	d := newDB(t)
	ctx := t.Context()
	ada, bob := d.user("Ada"), d.user("Bob")
	d.exec("INSERT INTO skills (name) VALUES ('PyTorch') ON CONFLICT (name) DO NOTHING")
	id := d.project(ada, "Graph learning", "A study", "pytorch", "Go")

	if id[14] != '7' {
		t.Errorf("id %s is not a UUIDv7", id)
	}
	p, err := d.store.Get(ctx, id, ada)
	if err != nil {
		t.Fatal(err)
	}
	if p.Owner.UserID != ada || p.Owner.FullName != "Ada" || p.MyRole != domain.RoleOwner || p.MemberCount != 1 || p.Status != domain.StatusOpen {
		t.Errorf("project = %+v", p)
	}
	if len(p.Skills) != 2 || p.Skills[0].Name != "Go" || p.Skills[1].Name != "PyTorch" {
		t.Errorf("skills = %+v, want Go and the existing spelling PyTorch, by name", p.Skills)
	}
	if n := d.count("SELECT count(*) FROM project_members WHERE project_id = $1 AND user_id = $2 AND role = 'owner'", id, ada); n != 1 {
		t.Errorf("owner rows = %d", n)
	}
	if other, _ := d.store.Get(ctx, id, bob); other.MyRole != domain.RoleNone || other.Access.OwnerAvailable != true {
		t.Errorf("as bob: role %q, access %+v", other.MyRole, other.Access)
	}
	if d.jobs(id) != 1 {
		t.Error("a new project was not queued for its embedding")
	}

	// A status change queues nothing; a text or skill change does.
	d.exec("DELETE FROM embedding_jobs")
	if err := d.store.Update(ctx, id, domain.Changes{Status: ptr(domain.StatusClosed)}); err != nil {
		t.Fatal(err)
	}
	if d.jobs(id) != 0 {
		t.Error("a status change queued the project")
	}
	err = postgres.NewTransactor(d.pool).InTx(ctx, func(ctx context.Context) error {
		if _, err := d.store.Lock(ctx, id, ada); err != nil {
			return err
		}
		if err := d.store.Update(ctx, id, domain.Changes{Title: ptr("Graph learning at scale"), SetAbstract: true, Abstract: ptr("We scale.")}); err != nil {
			return err
		}
		return d.store.SetSkills(ctx, id, []string{"go", "Rust"})
	})
	if err != nil {
		t.Fatal(err)
	}
	p, _ = d.store.Get(ctx, id, ada)
	if p.Title != "Graph learning at scale" || p.Abstract == nil || *p.Abstract != "We scale." || p.Status != domain.StatusClosed || p.Description != "A study" {
		t.Errorf("after update: %+v", p)
	}
	if len(p.Skills) != 2 || p.Skills[0].Name != "Go" || p.Skills[1].Name != "Rust" {
		t.Errorf("skills after replace = %+v", p.Skills)
	}
	if d.jobs(id) != 1 {
		t.Error("a text change did not queue the project")
	}
	if err := d.store.Update(ctx, id, domain.Changes{SetAbstract: true}); err != nil {
		t.Fatal(err)
	}
	if p, _ = d.store.Get(ctx, id, ada); p.Abstract != nil {
		t.Errorf("abstract not cleared: %v", *p.Abstract)
	}

	// 21 skills fail at commit and change nothing.
	names := make([]string, 21)
	for i := range names {
		names[i] = fmt.Sprintf("Skill %02d", i)
	}
	err = postgres.NewTransactor(d.pool).InTx(ctx, func(ctx context.Context) error { return d.store.SetSkills(ctx, id, names) })
	if err == nil {
		t.Error("21 skills were stored")
	}

	// Delete: the project, its members, skills and job go; the shared skill tags stay.
	d.exec("INSERT INTO project_embeddings (project_id, model, content_hash, embedding) VALUES ($1, 'm@1', repeat('a', 64), array_fill(0.1::real, ARRAY[768])::vector)", id)
	if err := d.store.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := d.store.Get(ctx, id, ada); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("after delete: %v", err)
	}
	if n := d.count(`SELECT (SELECT count(*) FROM project_members WHERE project_id = $1) + (SELECT count(*) FROM project_skills WHERE project_id = $1)
		+ (SELECT count(*) FROM project_embeddings WHERE project_id = $1) + (SELECT count(*) FROM embedding_jobs WHERE owner_id = $1)`, id); n != 0 {
		t.Errorf("%d rows left after delete", n)
	}
	if d.count("SELECT count(*) FROM skills WHERE name IN ('Go', 'Rust')") != 2 {
		t.Error("deleting a project deleted shared skill tags")
	}
	if _, err := d.store.Get(ctx, "not-a-uuid", ada); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("bad id: %v", err)
	}
}

// The visibility fields: the owner's account and blocks (one-way).
func TestProjectAccessFields(t *testing.T) {
	d := newDB(t)
	ctx := t.Context()
	ada, bob := d.user("Ada"), d.user("Bob")
	id := d.project(ada, "Graph learning", "A study")
	d.exec("INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)", bob, ada)
	if a, _ := d.store.Lock(ctx, id, bob); a.BlockedByOwner {
		t.Error("bob's block of ada hides ada's project from bob")
	}
	d.exec("INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)", ada, bob)
	if a, _ := d.store.Lock(ctx, id, bob); !a.BlockedByOwner {
		t.Error("ada's block of bob is not seen")
	}
	d.exec("UPDATE users SET suspended_at = now() WHERE id = $1", ada)
	if p, _ := d.store.Get(ctx, id, bob); p.Access.OwnerAvailable {
		t.Error("a suspended owner counts as available")
	}
}

func TestListMine(t *testing.T) {
	d := newDB(t)
	ctx := t.Context()
	ada, bob := d.user("Ada"), d.user("Bob")
	a1 := d.project(ada, "First project", "One", "Go")
	a2 := d.project(ada, "Second project", "Two")
	d.status(a2, domain.StatusArchived)
	b1 := d.project(bob, "Bob's project", "Three")
	d.exec("INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'viewer')", b1, ada)
	d.project(bob, "Not Ada's", "Four")
	d.exec("UPDATE projects SET updated_at = now() + interval '1 minute' WHERE id = $1", a1)

	items, total, err := d.store.ListMine(ctx, ada, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(items) != 2 || items[0].ID != a1 || items[0].MyRole != domain.RoleOwner || len(items[0].Skills) != 1 {
		t.Fatalf("page 1 = %+v, total %d", items, total)
	}
	rest, _, _ := d.store.ListMine(ctx, ada, 2, 2)
	roles := map[string]domain.Role{}
	for _, it := range append(items, rest...) {
		roles[it.ID] = it.MyRole
	}
	if len(roles) != 3 || roles[a2] != domain.RoleOwner || roles[b1] != domain.RoleViewer {
		t.Errorf("roles = %v; want the archived project and the viewer membership too", roles)
	}
	if role, _ := d.store.ProjectRole(ctx, b1, ada); role != authz.ProjectViewer {
		t.Errorf("ProjectRole = %q", role)
	}
	if role, _ := d.store.ProjectRole(ctx, a1, bob); role != authz.ProjectNonMember {
		t.Errorf("ProjectRole of a non-member = %q", role)
	}
}

func searchAs(t *testing.T, d db, viewer, q string, page int) domain.SearchResult {
	t.Helper()
	res, err := d.store.Search(t.Context(), domain.SearchQuery{ViewerID: viewer, Q: q, Limit: 20, Offset: (page - 1) * 20})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func ids(items []domain.Summary) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

// FR-10.3: title, skill and description matches, ranked; open projects only; the owner's
// account and blocks; a typo in the title.
func TestSearch(t *testing.T) {
	d := newDB(t)
	ada, bob, cy := d.user("Ada"), d.user("Bob"), d.user("Cy")
	w := testenv.Word(t)
	exact := d.project(ada, w, "First")
	prefix := d.project(ada, w+" networks", "Second")
	skill := d.project(ada, "Other title", "Third", w+" skill")
	desc := d.project(ada, "Plain", "About "+strings.ToLower(w)+" things")
	closed := d.project(ada, w+" closed", "x")
	d.status(closed, domain.StatusClosed)
	archived := d.project(ada, w+" archived", "x")
	d.status(archived, domain.StatusArchived)
	cyProject := d.project(cy, w+" by Cy", "x")

	res := searchAs(t, d, bob, w, 1)
	want := []string{exact, cyProject, prefix, skill, desc}
	got := ids(res.Items)
	if res.Total != 5 || res.Fuzzy {
		t.Fatalf("total %d fuzzy %v, items %v", res.Total, res.Fuzzy, got)
	}
	if got[0] != exact || got[3] != skill || got[4] != desc {
		t.Errorf("order = %v, want exact, the prefixes, skill, description (%v)", got, want)
	}
	if res.Items[3].MatchedOn != "skill" || res.Items[4].MatchedOn != "description" || res.Items[0].MatchedOn != "title" {
		t.Errorf("matchedOn = %s %s %s", res.Items[0].MatchedOn, res.Items[3].MatchedOn, res.Items[4].MatchedOn)
	}
	if res.Items[0].Owner.FullName != "Ada" || res.Items[0].MyRole != domain.RoleNone {
		t.Errorf("first = %+v", res.Items[0])
	}
	if mine := searchAs(t, d, ada, w, 1); mine.Items[0].MyRole != domain.RoleOwner {
		t.Errorf("own project role = %q", mine.Items[0].MyRole)
	}

	// Cy blocked Bob: Cy's project is gone for Bob only (one-way, FR-16.9).
	d.exec("INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)", cy, bob)
	if res := searchAs(t, d, bob, w, 1); res.Total != 4 || contains(ids(res.Items), cyProject) {
		t.Errorf("blocked: %v", ids(res.Items))
	}
	if res := searchAs(t, d, ada, w, 1); res.Total != 5 {
		t.Errorf("another user's results changed: %d", res.Total)
	}
	// A suspended owner's projects are hidden from everyone.
	d.exec("UPDATE users SET suspended_at = now() WHERE id = $1", ada)
	if res := searchAs(t, d, cy, w, 1); res.Total != 1 || res.Items[0].ID != cyProject {
		t.Errorf("suspended owner: %v", ids(res.Items))
	}
	d.exec("UPDATE users SET suspended_at = NULL, email_verified_at = NULL WHERE id = $1", ada)
	if res := searchAs(t, d, cy, w, 1); res.Total != 1 {
		t.Errorf("unverified owner: %v", ids(res.Items))
	}
	d.exec("UPDATE users SET email_verified_at = now() WHERE id = $1", ada)

	// A typo in one letter of a title word finds the title and says which (fuzzy).
	typo := w[:4] + "x" + w[5:] + " networks"
	res = searchAs(t, d, bob, typo, 1)
	if !res.Fuzzy || res.CorrectedQuery == nil || len(res.Items) == 0 || !strings.HasPrefix(*res.CorrectedQuery, w) {
		t.Errorf("typo search = %+v", res)
	}
	// A wildcard matches only itself.
	if res := searchAs(t, d, bob, "%_", 1); res.Total != 0 {
		t.Errorf("wildcards matched %d", res.Total)
	}
}

func TestSearchPages(t *testing.T) {
	d := newDB(t)
	ada := d.user("Ada")
	w := testenv.Word(t)
	for i := range 25 {
		d.project(ada, fmt.Sprintf("%s project %02d", w, i), "x")
	}
	p1, p2 := searchAs(t, d, ada, w, 1), searchAs(t, d, ada, w, 2)
	if p1.Total != 25 || len(p1.Items) != 20 || len(p2.Items) != 5 {
		t.Fatalf("pages: %d+%d of %d", len(p1.Items), len(p2.Items), p1.Total)
	}
	seen := map[string]bool{}
	for _, id := range append(ids(p1.Items), ids(p2.Items)...) {
		seen[id] = true
	}
	if len(seen) != 25 {
		t.Errorf("%d distinct results over two pages, want 25", len(seen))
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// FR-15.11: one open report per reporter and project; listing, reading and resolving.
func TestReports(t *testing.T) {
	d := newDB(t)
	ctx := t.Context()
	ada, bob := d.user("Ada"), d.user("Bob")
	admin := d.user("Admin")
	id := d.project(ada, "Graph learning", "A study", "Go")
	d.status(id, domain.StatusArchived)

	r, err := d.store.CreateReport(ctx, domain.NewReport{ProjectID: id, ReporterID: bob, Reason: "spam", Details: ptr("Ads")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.store.CreateReport(ctx, domain.NewReport{ProjectID: id, ReporterID: bob, Reason: "other"}); !errors.Is(err, domain.ErrAlreadyReported) {
		t.Errorf("second open report: %v", err)
	}
	items, total, err := d.store.ListReports(ctx, true, 20, 0)
	if err != nil || total != 1 || items[0].ID != r.ID || items[0].Reporter.UserID != bob || items[0].Owner.UserID != ada ||
		items[0].Reporter.Role != "researcher" || items[0].ProjectTitle != "Graph learning" {
		t.Fatalf("open list = %+v, %d, %v", items, total, err)
	}
	rep, proj, err := d.store.GetReport(ctx, r.ID)
	if err != nil || rep.Details == nil || *rep.Details != "Ads" || proj.Status != domain.StatusArchived || len(proj.Skills) != 1 || proj.MemberCount != 1 {
		t.Errorf("detail = %+v %+v, %v", rep, proj, err)
	}
	resolved, err := d.store.ResolveReport(ctx, r.ID, admin, "dismissed", ptr("Not spam"))
	if err != nil || resolved.ResolvedAt == nil || *resolved.Outcome != "dismissed" || *resolved.Note != "Not spam" {
		t.Fatalf("resolve = %+v, %v", resolved, err)
	}
	if _, err := d.store.ResolveReport(ctx, r.ID, admin, "action_taken", nil); !errors.Is(err, domain.ErrAlreadyResolved) {
		t.Errorf("resolve twice: %v", err)
	}
	if _, err := d.store.ResolveReport(ctx, "00000000-0000-4000-8000-000000000000", admin, "dismissed", nil); !errors.Is(err, domain.ErrReportNotFound) {
		t.Errorf("unknown report: %v", err)
	}
	if _, _, err := d.store.GetReport(ctx, "nope"); !errors.Is(err, domain.ErrReportNotFound) {
		t.Errorf("bad id: %v", err)
	}
	if _, n, _ := d.store.ListReports(ctx, false, 20, 0); n != 1 {
		t.Errorf("resolved count = %d", n)
	}
	if _, err := d.store.CreateReport(ctx, domain.NewReport{ProjectID: id, ReporterID: bob, Reason: "other"}); err != nil {
		t.Errorf("report again after resolution: %v", err)
	}
}

func ptr[T any](v T) *T { return &v }
