package usecase

import (
	"errors"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

const (
	ada = "u-ada" // owns the projects
	bob = "u-bob" // another verified user
)

// FR-10.1: the field rules, each refusal naming its field, and the caller as the owner.
func TestCreateValidates(t *testing.T) {
	long := func(n int) string { return strings.Repeat("x", n) }
	tests := []struct {
		name  string
		in    CreateInput
		field string
	}{
		{"title too short", CreateInput{Title: "ab", Description: "d"}, "title"},
		{"title too short after trimming", CreateInput{Title: "  ab  ", Description: "d"}, "title"},
		{"title too long", CreateInput{Title: long(151), Description: "d"}, "title"},
		{"title with NUL", CreateInput{Title: "ab\x00c", Description: "d"}, "title"},
		{"no description", CreateInput{Title: "Title", Description: "  "}, "description"},
		{"description too long", CreateInput{Title: "Title", Description: long(5001)}, "description"},
		{"abstract too long", CreateInput{Title: "Title", Description: "d", Abstract: ptr(long(3001))}, "abstract"},
		{"empty skill", CreateInput{Title: "Title", Description: "d", Skills: []string{"Go", "  "}}, "requiredSkills[1]"},
		{"skill too long", CreateInput{Title: "Title", Description: "d", Skills: []string{long(61)}}, "requiredSkills[0]"},
		{"21 skills", CreateInput{Title: "Title", Description: "d", Skills: manySkills(21)}, "requiredSkills"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			_, err := e.service.Create(t.Context(), ada, tt.in)
			var fe *domain.FieldError
			if !errors.As(err, &fe) || fe.Field != tt.field {
				t.Fatalf("err = %v, want a FieldError for %s", err, tt.field)
			}
			if len(e.store.projects) != 0 || e.create.used[ada] != 0 {
				t.Error("an invalid project was stored or counted")
			}
		})
	}
}

func manySkills(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "Skill " + string(rune('A'+i))
	}
	return out
}

func TestCreateStoresTheOwnerAndNormalizedSkills(t *testing.T) {
	e := newEnv()
	p, err := e.service.Create(t.Context(), ada, CreateInput{
		Title: "  Graph learning ", Description: " A study ", Abstract: ptr("   "),
		Skills: []string{" Machine   learning ", "machine learning", "Go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Graph learning" || p.Description != "A study" || p.Abstract != nil || p.Status != domain.StatusOpen {
		t.Errorf("project = %+v", p)
	}
	if p.MyRole != domain.RoleOwner || p.Owner.UserID != ada {
		t.Errorf("owner = %+v, role %q; want the caller as owner", p.Owner, p.MyRole)
	}
	if len(p.Skills) != 2 || p.Skills[0].Name != "Machine learning" || p.Skills[1].Name != "Go" {
		t.Errorf("skills = %+v, want the first spelling of each, once", p.Skills)
	}
	// 20 distinct skills are allowed; duplicates do not count.
	if _, err := e.service.Create(t.Context(), ada, CreateInput{Title: "Title", Description: "d", Skills: append(manySkills(20), "skill a")}); err != nil {
		t.Errorf("20 skills and a duplicate: %v", err)
	}
}

// Ten projects a day; the eleventh is refused and stores nothing. Only created projects
// count, and when the limit cannot be read the project is still created.
func TestCreateRateLimit(t *testing.T) {
	e := newEnv()
	for i := range CreatesPerWindow {
		if _, err := e.service.Create(t.Context(), ada, CreateInput{Title: "Title", Description: "d"}); err != nil {
			t.Fatalf("create %d: %v", i+1, err)
		}
	}
	_, err := e.service.Create(t.Context(), ada, CreateInput{Title: "Title", Description: "d"})
	var limited *domain.RateLimitedError
	if !errors.As(err, &limited) || limited.RetryAfter <= 0 {
		t.Fatalf("11th create: %v, want RateLimitedError with a wait", err)
	}
	if len(e.store.projects) != CreatesPerWindow {
		t.Errorf("%d projects stored, want %d", len(e.store.projects), CreatesPerWindow)
	}
	if _, err := e.service.Create(t.Context(), bob, CreateInput{Title: "Title", Description: "d"}); err != nil {
		t.Errorf("another user's create: %v", err)
	}

	e = newEnv()
	e.store.failTx = errBoom
	if _, err := e.service.Create(t.Context(), ada, CreateInput{Title: "Title", Description: "d"}); !errors.Is(err, errBoom) {
		t.Fatalf("err = %v", err)
	}
	if e.create.used[ada] != 0 {
		t.Error("a failed create was counted")
	}

	e = newEnv()
	e.create.err = errBoom
	if _, err := e.service.Create(t.Context(), ada, CreateInput{Title: "Title", Description: "d"}); err != nil {
		t.Errorf("create with the limit unavailable: %v, want it allowed", err)
	}
}

// SRS 2.3 and decisions.md 2026-11-16: members see the project whatever its status; others
// only an open or closed project whose owner is usable and has not blocked them. The abstract
// is for members only.
func TestGetVisibility(t *testing.T) {
	tests := []struct {
		name    string
		status  domain.Status
		setup   func(s *memStore, id string)
		viewer  string
		visible bool
	}{
		{"open, other user", domain.StatusOpen, nil, bob, true},
		{"closed, other user", domain.StatusClosed, nil, bob, true},
		{"archived, other user", domain.StatusArchived, nil, bob, false},
		{"archived, owner", domain.StatusArchived, nil, ada, true},
		{"archived, viewer member", domain.StatusArchived, func(s *memStore, id string) { s.members[id][bob] = domain.RoleViewer }, bob, true},
		{"owner blocked the viewer", domain.StatusOpen, func(s *memStore, _ string) { s.blocks[[2]string{ada, bob}] = true }, bob, false},
		{"viewer blocked the owner (one-way)", domain.StatusOpen, func(s *memStore, _ string) { s.blocks[[2]string{bob, ada}] = true }, bob, true},
		{"owner suspended, other user", domain.StatusOpen, func(s *memStore, _ string) { s.away[ada] = true }, bob, false},
		{"owner suspended, member", domain.StatusOpen, func(s *memStore, id string) { s.away[ada] = true; s.members[id][bob] = domain.RoleEditor }, bob, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			id := e.store.add(ada, tt.status)
			if tt.setup != nil {
				tt.setup(e.store, id)
			}
			p, err := e.service.Get(t.Context(), tt.viewer, id)
			if !tt.visible {
				if !errors.Is(err, domain.ErrNotFound) {
					t.Fatalf("err = %v, want ErrNotFound", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			member := p.MyRole != domain.RoleNone
			if member != (p.Abstract != nil) {
				t.Errorf("role %q sees abstract %v; the abstract is for members only", p.MyRole, p.Abstract)
			}
		})
	}
	e := newEnv()
	if _, err := e.service.Get(t.Context(), ada, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown project: %v", err)
	}
}

// FR-10.2: the owner edits details and status; an editor only the abstract; anyone else is
// refused (403) when they can see the project, and gets 404 when they cannot.
func TestUpdatePermissions(t *testing.T) {
	tests := []struct {
		name string
		role domain.Role
		in   UpdateInput
		want error
	}{
		{"owner, title", domain.RoleOwner, UpdateInput{Title: ptr("New title")}, nil},
		{"owner, status", domain.RoleOwner, UpdateInput{Status: ptr("archived")}, nil},
		{"owner, abstract", domain.RoleOwner, UpdateInput{SetAbstract: true, Abstract: ptr("New")}, nil},
		{"editor, abstract", domain.RoleEditor, UpdateInput{SetAbstract: true}, nil},
		{"editor, title", domain.RoleEditor, UpdateInput{Title: ptr("New title")}, domain.ErrForbidden},
		{"editor, abstract and status", domain.RoleEditor, UpdateInput{SetAbstract: true, Status: ptr("closed")}, domain.ErrForbidden},
		{"viewer, abstract", domain.RoleViewer, UpdateInput{SetAbstract: true}, domain.ErrForbidden},
		{"non-member, status", domain.RoleNone, UpdateInput{Status: ptr("closed")}, domain.ErrForbidden},
		{"non-member, nothing", domain.RoleNone, UpdateInput{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			id := e.store.add(ada, domain.StatusOpen)
			caller := ada
			if tt.role != domain.RoleOwner {
				caller = bob
				if tt.role != domain.RoleNone {
					e.store.members[id][bob] = tt.role
				}
			}
			_, err := e.service.Update(t.Context(), caller, id, tt.in)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if tt.want != nil && len(e.store.updates) != 0 {
				t.Error("a refused edit changed the project")
			}
		})
	}
}

func TestUpdateRulesAndStatus(t *testing.T) {
	e := newEnv()
	id := e.store.add(ada, domain.StatusOpen)
	for _, in := range []struct {
		in    UpdateInput
		field string
	}{
		{UpdateInput{Status: ptr("deleted")}, "status"},
		{UpdateInput{Title: ptr("x")}, "title"},
		{UpdateInput{Skills: ptr(manySkills(21))}, "requiredSkills"},
	} {
		var fe *domain.FieldError
		if _, err := e.service.Update(t.Context(), ada, id, in.in); !errors.As(err, &fe) || fe.Field != in.field {
			t.Errorf("Update(%+v) = %v, want FieldError %s", in.in, err, in.field)
		}
	}
	// Any status to any status, and back.
	for _, st := range []string{"archived", "closed", "open", "archived", "open"} {
		p, err := e.service.Update(t.Context(), ada, id, UpdateInput{Status: ptr(st)})
		if err != nil || string(p.Status) != st {
			t.Fatalf("set %s: %v, %q", st, err, p.Status)
		}
	}
	p, err := e.service.Update(t.Context(), ada, id, UpdateInput{SetAbstract: true, Skills: ptr([]string{"Go"})})
	if err != nil || p.Abstract != nil || len(p.Skills) != 1 {
		t.Errorf("clear abstract and set skills: %v, %+v", err, p)
	}
	// An archived project is still the owner's to edit, and gone for others.
	if _, err := e.service.Update(t.Context(), ada, id, UpdateInput{Status: ptr("archived")}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.service.Update(t.Context(), bob, id, UpdateInput{Status: ptr("open")}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("non-member edit of an archived project: %v, want ErrNotFound", err)
	}
}

// FR-10.6: only the owner, only with the exact title.
func TestDelete(t *testing.T) {
	e := newEnv()
	id := e.store.add(ada, domain.StatusOpen)
	e.store.members[id][bob] = domain.RoleEditor
	if err := e.service.Delete(t.Context(), bob, id, ptr("Graph learning")); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("editor delete: %v, want ErrForbidden", err)
	}
	for _, c := range []*string{nil, ptr(""), ptr("graph learning"), ptr("Graph")} {
		if err := e.service.Delete(t.Context(), ada, id, c); !errors.Is(err, domain.ErrConfirmMismatch) {
			t.Errorf("confirm %v: %v, want ErrConfirmMismatch", c, err)
		}
	}
	if len(e.store.deleted) != 0 {
		t.Fatal("a refused delete deleted")
	}
	if err := e.service.Delete(t.Context(), ada, id, ptr("  Graph learning ")); err != nil {
		t.Fatalf("owner delete: %v", err)
	}
	if err := e.service.Delete(t.Context(), ada, id, ptr("Graph learning")); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second delete: %v, want ErrNotFound", err)
	}
	archived := e.store.add(ada, domain.StatusArchived)
	if err := e.service.Delete(t.Context(), bob, archived, ptr("Graph learning")); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("non-member delete of an archived project: %v, want ErrNotFound", err)
	}
}

func TestListMinePages(t *testing.T) {
	e := newEnv()
	for range 3 {
		e.store.add(ada, domain.StatusArchived)
	}
	page, err := e.service.ListMine(t.Context(), ada, 2, 2)
	if err != nil || page.Total != 3 || len(page.Items) != 1 || page.PageSize != 2 {
		t.Errorf("page 2 = %+v, %v", page, err)
	}
	if page, _ := e.service.ListMine(t.Context(), ada, 1, 500); page.PageSize != MaxPageSize {
		t.Errorf("page size = %d, want it capped at %d", page.PageSize, MaxPageSize)
	}
	for _, p := range [][2]int{{0, 20}, {1, 0}} {
		if _, err := e.service.ListMine(t.Context(), ada, p[0], p[1]); !errors.Is(err, domain.ErrInvalidPage) {
			t.Errorf("ListMine(%v) = %v", p, err)
		}
	}
}

// FR-10.3: 2 to 100 characters in keyword mode, pages 1 to 50 of 20, 60 searches a minute;
// invalid ones are not counted.
func TestSearchRules(t *testing.T) {
	e := newEnv()
	for _, q := range []*string{nil, ptr(" a "), ptr(strings.Repeat("x", 101)), ptr("a\x00b")} {
		if _, err := e.service.Search(t.Context(), ada, q, "keyword", 1); !errors.Is(err, domain.ErrInvalidQuery) {
			t.Errorf("q %v: %v, want ErrInvalidQuery", q, err)
		}
	}
	for _, p := range []int{0, 51} {
		if _, err := e.service.Search(t.Context(), ada, ptr("graph"), "keyword", p); !errors.Is(err, domain.ErrInvalidPage) {
			t.Errorf("page %d: %v, want ErrInvalidPage", p, err)
		}
	}
	if e.search.used[ada] != 0 {
		t.Fatal("invalid searches were counted")
	}
	if _, err := e.service.Search(t.Context(), ada, ptr("  graph  "), "keyword", 3); err != nil {
		t.Fatal(err)
	}
	if got := e.store.searches[0]; got.Q != "graph" || got.Limit != 20 || got.Offset != 40 || got.ViewerID != ada {
		t.Errorf("query = %+v", got)
	}
	e.search.used[ada] = SearchesPerWindow
	var limited *domain.RateLimitedError
	if _, err := e.service.Search(t.Context(), ada, ptr("graph"), "keyword", 1); !errors.As(err, &limited) {
		t.Errorf("61st search: %v, want RateLimitedError", err)
	}
}

// FR-15.11: report checks in order: visible, not suspended, fields, not the owner, once.
func TestReport(t *testing.T) {
	e := newEnv()
	id := e.store.add(ada, domain.StatusOpen)
	archived := e.store.add(ada, domain.StatusArchived)
	if _, err := e.service.Report(t.Context(), bob, archived, ReportInput{Reason: "spam"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("report of a hidden project: %v", err)
	}
	var fe *domain.FieldError
	if _, err := e.service.Report(t.Context(), bob, id, ReportInput{Reason: "boring"}); !errors.As(err, &fe) || fe.Field != "reason" {
		t.Errorf("bad reason: %v", err)
	}
	if _, err := e.service.Report(t.Context(), bob, id, ReportInput{Reason: "spam", Details: ptr(strings.Repeat("x", 1001))}); !errors.As(err, &fe) || fe.Field != "details" {
		t.Errorf("long details: %v", err)
	}
	if _, err := e.service.Report(t.Context(), ada, id, ReportInput{Reason: "spam"}); !errors.Is(err, domain.ErrCannotReportOwn) {
		t.Errorf("own project: %v", err)
	}
	if _, err := e.service.Report(t.Context(), bob, id, ReportInput{Reason: "spam", Details: ptr("  ")}); err != nil {
		t.Fatalf("report: %v", err)
	}
	if _, err := e.service.Report(t.Context(), bob, id, ReportInput{Reason: "other"}); !errors.Is(err, domain.ErrAlreadyReported) {
		t.Errorf("second report: %v", err)
	}
	e.actors.suspended[bob] = true
	var suspended *domain.SuspendedError
	if _, err := e.service.Report(t.Context(), bob, id, ReportInput{Reason: "boring"}); !errors.As(err, &suspended) || suspended.Reason == nil {
		t.Errorf("suspended reporter: %v, want SuspendedError before the field check", err)
	}
}

func TestReportAdminInputs(t *testing.T) {
	e := newEnv()
	var fe *domain.FieldError
	if _, err := e.service.ListReports(t.Context(), ptr("all"), 1); !errors.As(err, &fe) || fe.Field != "status" {
		t.Errorf("status all: %v", err)
	}
	if _, err := e.service.ListReports(t.Context(), nil, 0); !errors.Is(err, domain.ErrInvalidPage) {
		t.Errorf("page 0: %v", err)
	}
	if p, err := e.service.ListReports(t.Context(), ptr("resolved"), 2); err != nil || p.Page != 2 || p.PageSize != ReportPageSize {
		t.Errorf("resolved page 2: %+v, %v", p, err)
	}
	if _, err := e.service.ResolveReport(t.Context(), ada, "r1", "deleted", nil); !errors.As(err, &fe) || fe.Field != "outcome" {
		t.Errorf("bad outcome: %v", err)
	}
	if _, err := e.service.ResolveReport(t.Context(), ada, "r1", "dismissed", ptr(strings.Repeat("x", 1001))); !errors.As(err, &fe) || fe.Field != "note" {
		t.Errorf("long note: %v", err)
	}
}

func TestNormalizeSkillName(t *testing.T) {
	for in, want := range map[string]string{" Deep \t learning ": "Deep learning", "Go": "Go", strings.Repeat("é", 60): strings.Repeat("é", 60)} {
		if got, ok := NormalizeSkillName(in); !ok || got != want {
			t.Errorf("NormalizeSkillName(%q) = %q, %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "   ", strings.Repeat("é", 61), "a\x00b"} {
		if _, ok := NormalizeSkillName(in); ok {
			t.Errorf("NormalizeSkillName(%q) accepted", in)
		}
	}
}
