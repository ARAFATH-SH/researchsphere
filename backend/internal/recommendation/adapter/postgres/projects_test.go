//go:build integration

package postgres

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
)

// project stores a project with owner (in one statement, so the owner check at commit
// passes) and the given status, and returns its ID.
func (d db) project(owner, title, status string) string {
	d.t.Helper()
	var id string
	if err := d.pool.QueryRow(d.t.Context(), `WITH p AS (
			INSERT INTO projects (title, description, status) VALUES ($1, 'About ' || $1, $2::project_status) RETURNING id),
		m AS (INSERT INTO project_members (project_id, user_id, role) SELECT id, $3, 'owner' FROM p)
		SELECT id::text FROM p`, title, status, owner).Scan(&id); err != nil {
		d.t.Fatal(err)
	}
	return id
}

// embedProject stores the project's embedding at cosine cos from vector(1), made by model.
func (d db) embedProject(projectID string, cos float64, model string) {
	d.t.Helper()
	d.exec(`INSERT INTO project_embeddings (project_id, model, content_hash, embedding)
		VALUES ($1, $2, repeat('b', 64), $3::text::vector)
		ON CONFLICT (project_id) DO UPDATE SET model = EXCLUDED.model, embedding = EXCLUDED.embedding`, projectID, model, vector(cos))
}

func (d db) member(projectID, userID, role string) {
	d.t.Helper()
	d.exec("INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, $3::project_role)", projectID, userID, role)
}

func (d db) projectSkill(projectID, name string) {
	d.t.Helper()
	d.exec("INSERT INTO skills (name) VALUES ($1) ON CONFLICT (name) DO NOTHING", name)
	d.exec("INSERT INTO project_skills (project_id, skill_id) SELECT $1, id FROM skills WHERE name = $2", projectID, name)
}

func (d db) block(blocker, blocked string) {
	d.t.Helper()
	d.exec("INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)", blocker, blocked)
}

func projectIDs(cands []domain.ProjectCandidate) []string {
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.ProjectID
	}
	return out
}

// AI-3.1: each kind of project that must be left out is, alone: the caller's own, one they
// belong to, one they asked to join (a pending request row, as slice 20 will write it),
// closed and archived ones, a suspended or unverified owner, a block either way, another
// model and no embedding. A rejected join request, an invitation and an ended timed
// suspension leave a project in.
func TestRankProjectsExclusions(t *testing.T) {
	d := newDB(t)
	s := NewStore(d.pool)
	me := d.user("Me")
	d.embed(me, 1, modelA)
	owner := d.user("Owner")

	keep := d.project(owner, "Keep", "open")
	d.embedProject(keep, 0.5, modelA)

	excluded := map[string]string{}
	add := func(why string, setup func() string) { excluded[why] = setup() }
	add("my own", func() string { p := d.project(me, "Mine", "open"); d.embedProject(p, 0.99, modelA); return p })
	add("member", func() string {
		p := d.project(owner, "Joined", "open")
		d.member(p, me, "editor")
		d.embedProject(p, 0.99, modelA)
		return p
	})
	add("asked to join", func() string {
		p := d.project(owner, "Asked", "open")
		d.exec("INSERT INTO project_join_requests (project_id, user_id, kind) VALUES ($1, $2, 'request')", p, me)
		d.embedProject(p, 0.98, modelA)
		return p
	})
	add("closed", func() string { p := d.project(owner, "Closed", "closed"); d.embedProject(p, 0.98, modelA); return p })
	add("archived", func() string {
		p := d.project(owner, "Archived", "archived")
		d.embedProject(p, 0.98, modelA)
		return p
	})
	add("owner suspended", func() string {
		o := d.user("Suspended owner")
		d.exec("UPDATE users SET suspended_at = now(), suspension_reason = 'reason long enough' WHERE id = $1", o)
		p := d.project(o, "Suspended", "open")
		d.embedProject(p, 0.97, modelA)
		return p
	})
	add("owner unverified", func() string {
		o := d.user("Unverified owner")
		d.exec("UPDATE users SET email_verified_at = NULL WHERE id = $1", o)
		p := d.project(o, "Unverified", "open")
		d.embedProject(p, 0.97, modelA)
		return p
	})
	add("I blocked the owner", func() string {
		o := d.user("Blocked owner")
		d.block(me, o)
		p := d.project(o, "Blocked", "open")
		d.embedProject(p, 0.96, modelA)
		return p
	})
	add("the owner blocked me", func() string {
		o := d.user("Blocking owner")
		d.block(o, me)
		p := d.project(o, "Blocking", "open")
		d.embedProject(p, 0.96, modelA)
		return p
	})
	add("other model", func() string {
		p := d.project(owner, "Other model", "open")
		d.embedProject(p, 0.999, modelB)
		return p
	})
	add("no embedding", func() string { return d.project(owner, "Not embedded", "open") })

	rejected := d.project(owner, "Rejected request", "open")
	d.exec("INSERT INTO project_join_requests (project_id, user_id, kind, status) VALUES ($1, $2, 'request', 'rejected')", rejected, me)
	d.embedProject(rejected, 0.7, modelA)
	invited := d.project(owner, "Invited", "open")
	d.exec("INSERT INTO project_join_requests (project_id, user_id, kind) VALUES ($1, $2, 'invitation')", invited, me)
	d.embedProject(invited, 0.6, modelA)
	ended := d.user("Ended suspension")
	d.exec("UPDATE users SET suspended_at = now() - interval '2 days', suspended_until = now() - interval '1 hour', suspension_reason = 'reason long enough' WHERE id = $1", ended)
	back := d.project(ended, "Owner is back", "open")
	d.embedProject(back, 0.55, modelA)

	got, err := s.RankProjects(t.Context(), me, 10)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{rejected, invited, back, keep}; strings.Join(projectIDs(got), ",") != strings.Join(want, ",") {
		t.Errorf("RankProjects = %v, want %v", projectIDs(got), want)
	}
	for why, id := range excluded {
		if contains(projectIDs(got), id) {
			t.Errorf("%s: recommended, want left out", why)
		}
	}

	ok, err := s.ProjectsStillEligible(t.Context(), me, append([]string{keep, rejected, invited, back, "not-a-uuid"}, mapValues(excluded)...))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{keep, rejected, invited, back} {
		if !ok[id] {
			t.Errorf("ProjectsStillEligible left out %s", id)
		}
	}
	for why, id := range excluded {
		if ok[id] {
			t.Errorf("ProjectsStillEligible kept %s", why)
		}
	}
}

// AI-3.1 and AI-2.4: the cosine order, the limit and the cosine itself; a project with many
// members ranks below a more similar one with only its owner. Skills come with each
// candidate in name order; the cards carry the member count and the owner.
func TestRankProjectsOrderSkillsAndCards(t *testing.T) {
	d := newDB(t)
	s := NewStore(d.pool)
	me := d.user("Me")
	d.embed(me, 1, modelA)
	owner := d.user("Owner")
	byCos := map[float64]string{}
	for _, c := range []float64{0.3, 0.9, 0.5, 0.7, 0.2, 0.8, 0.4, 0.6, 0.1, 0.85, 0.95, 0.75} {
		p := d.project(owner, "Proj", "open")
		d.embedProject(p, c, modelA)
		byCos[c] = p
	}
	popular := byCos[0.85]
	for range 30 {
		d.member(popular, d.user("Member"), "viewer")
	}
	d.projectSkill(byCos[0.95], "remote sensing")
	d.projectSkill(byCos[0.95], "GIS")

	got, err := s.RankProjects(t.Context(), me, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []float64{0.95, 0.9, 0.85, 0.8, 0.75, 0.7, 0.6, 0.5, 0.4, 0.3}
	if len(got) != len(want) {
		t.Fatalf("%d candidates, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].ProjectID != byCos[w] || math.Abs(got[i].Cosine-w) > 1e-4 {
			t.Errorf("#%d = %s (cos %.4f), want cos %.2f", i, got[i].ProjectID, got[i].Cosine, w)
		}
	}
	if strings.Join(got[0].Skills, "|") != "GIS|remote sensing" || got[1].Skills == nil || len(got[1].Skills) != 0 {
		t.Errorf("skills %v and %v", got[0].Skills, got[1].Skills)
	}
	if got, err := s.RankProjects(t.Context(), me, 3); err != nil || len(got) != 3 {
		t.Errorf("limit 3: %d, %v", len(got), err)
	}
	if got, err := s.RankProjects(t.Context(), d.user("No embedding"), 10); err != nil || len(got) != 0 {
		t.Errorf("caller without an embedding: %d, %v", len(got), err)
	}

	cards, err := s.ProjectCards(t.Context(), []string{popular, byCos[0.95], "not-a-uuid"})
	if err != nil {
		t.Fatal(err)
	}
	if c := cards[popular]; c.MemberCount != 31 || c.Owner.UserID != owner || c.Owner.FullName != "Owner" || c.Title != "Proj" || c.Description != "About Proj" {
		t.Errorf("card = %+v", c)
	}
	if len(cards) != 2 {
		t.Errorf("%d cards, want 2", len(cards))
	}
}

// AI-2.3: the project's members, unverified and suspended users, a block either way with
// the owner, another model and no embedding are left out; a block involving another member
// is not the owner's and leaves the user in. ProjectSelf reads the owner, skills and stamp.
func TestRankResearchersExclusions(t *testing.T) {
	d := newDB(t)
	s := NewStore(d.pool)
	owner := d.user("Owner")
	d.embed(owner, 0.999, modelA)
	p := d.project(owner, "Flood maps", "closed")
	d.projectSkill(p, "Python")
	d.projectSkill(p, "GIS")

	if self, err := s.ProjectSelf(t.Context(), p); err != nil || self.Embedding != nil || self.OwnerID != owner || strings.Join(self.Skills, "|") != "GIS|Python" {
		t.Fatalf("ProjectSelf before embedding = %+v, %v", self, err)
	}
	d.embedProject(p, 1, modelA)
	if self, err := s.ProjectSelf(t.Context(), p); err != nil || self.Embedding == nil || self.Embedding.Model != modelA {
		t.Fatalf("ProjectSelf = %+v, %v", self, err)
	}
	for _, missing := range []string{"not-a-uuid", "00000000-0000-4000-8000-000000000000"} {
		if _, err := s.ProjectSelf(t.Context(), missing); !errors.Is(err, domain.ErrProjectNotFound) {
			t.Errorf("ProjectSelf(%s): %v", missing, err)
		}
	}

	keep := d.user("Keep")
	d.embed(keep, 0.5, modelA)
	d.skill(keep, "python") // the shared citext tag: the project's "Python" row
	editor := d.user("Other member")
	d.member(p, editor, "editor")
	d.embed(editor, 0.98, modelA)
	blockedByMember := d.user("Blocked by a member")
	d.block(editor, blockedByMember)
	d.embed(blockedByMember, 0.6, modelA)

	excluded := map[string]string{"owner": owner, "member": editor}
	add := func(why string, setup func(id string)) {
		id := d.user(why)
		setup(id)
		excluded[why] = id
	}
	add("unverified", func(id string) {
		d.embed(id, 0.97, modelA)
		d.exec("UPDATE users SET email_verified_at = NULL WHERE id = $1", id)
	})
	add("suspended", func(id string) {
		d.exec("UPDATE users SET suspended_at = now(), suspension_reason = 'reason long enough' WHERE id = $1", id)
		d.embed(id, 0.97, modelA)
	})
	add("owner blocked them", func(id string) { d.embed(id, 0.96, modelA); d.block(owner, id) })
	add("they blocked the owner", func(id string) { d.embed(id, 0.96, modelA); d.block(id, owner) })
	add("other model", func(id string) { d.embed(id, 0.999, modelB) })
	add("no embedding", func(string) {})

	got, err := s.RankResearchers(t.Context(), p, 10)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{blockedByMember, keep}; strings.Join(ids(got), ",") != strings.Join(want, ",") {
		t.Errorf("RankResearchers = %v, want %v", ids(got), want)
	}
	if len(got) == 2 && strings.Join(got[1].Skills, "|") != "Python" {
		t.Errorf("researcher skills = %v", got[1].Skills)
	}
	ok, err := s.ResearchersStillEligible(t.Context(), p, append([]string{keep, blockedByMember}, mapValues(excluded)...))
	if err != nil {
		t.Fatal(err)
	}
	if !ok[keep] || !ok[blockedByMember] {
		t.Errorf("ResearchersStillEligible left out an eligible user: %v", ok)
	}
	for why, id := range excluded {
		if ok[id] {
			t.Errorf("ResearchersStillEligible kept %s", why)
		}
	}
	owned, err := s.OwnedProjects(t.Context(), owner, editor, "not-a-uuid")
	if err != nil || len(owned) != 1 || owned[0] != p {
		t.Errorf("OwnedProjects = %v, %v; want only %s", owned, err, p)
	}
}

// AI-2.4 for AI-2.3: a less similar researcher with many connections and publications ranks
// below a more similar newcomer.
func TestRankResearchersIgnoresPopularity(t *testing.T) {
	d := newDB(t)
	s := NewStore(d.pool)
	owner := d.user("Owner")
	p := d.project(owner, "Proj", "open")
	d.embedProject(p, 1, modelA)
	newcomer := d.user("Newcomer")
	d.embed(newcomer, 0.9, modelA)
	famous := d.user("Famous")
	d.embed(famous, 0.8, modelA)
	for range 20 {
		d.connect(d.user("Fan"), famous, "accepted")
	}
	got, err := s.RankResearchers(t.Context(), p, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 || got[0].UserID != newcomer || got[1].UserID != famous {
		t.Errorf("order %v, want the newcomer first", ids(got))
	}
}
