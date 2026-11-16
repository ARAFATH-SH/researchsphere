package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/authz"
	profileusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/usecase"
	projectdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
	projectusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/project/usecase"
)

// okProjects answers every project operation with a fixed project, report or refusal, so
// the router tests reach the handlers.
type okProjects struct{}

const okProjectID = "33333333-3333-4333-8333-333333333333"

var okProject = projectdomain.Project{
	ID: okProjectID, Title: "Graph learning", Description: "A study", Status: projectdomain.StatusOpen,
	Skills: []projectdomain.Skill{}, Owner: projectdomain.Person{UserID: okUserID, FullName: "Ada"},
	MyRole: projectdomain.RoleOwner, MemberCount: 1,
}

var okProjectReport = projectdomain.Report{
	ID: "44444444-4444-4444-8444-444444444444", ProjectID: okProjectID, ProjectTitle: "Graph learning", Reason: "spam",
	Reporter: projectdomain.ReportPerson{Person: projectdomain.Person{UserID: okUserID, FullName: "Ada"}, Role: "researcher"},
	Owner:    projectdomain.ReportPerson{Person: projectdomain.Person{UserID: okUserID, FullName: "Ada"}, Role: "researcher"},
}

func (okProjects) Create(context.Context, string, projectusecase.CreateInput) (projectdomain.Project, error) {
	return okProject, nil
}

func (okProjects) Get(_ context.Context, _, id string) (projectdomain.Project, error) {
	if id != okProjectID {
		return projectdomain.Project{}, projectdomain.ErrNotFound
	}
	return okProject, nil
}

func (okProjects) Update(_ context.Context, _, id string, _ projectusecase.UpdateInput) (projectdomain.Project, error) {
	if id != okProjectID {
		return projectdomain.Project{}, projectdomain.ErrNotFound
	}
	return okProject, nil
}

func (okProjects) Delete(_ context.Context, _, id string, confirm *string) error {
	if id != okProjectID {
		return projectdomain.ErrNotFound
	}
	if confirm == nil || *confirm != okProject.Title {
		return projectdomain.ErrConfirmMismatch
	}
	return nil
}

func (okProjects) ListMine(_ context.Context, _ string, page, size int) (projectusecase.Page, error) {
	if page < 1 || size < 1 {
		return projectusecase.Page{}, projectdomain.ErrInvalidPage
	}
	return projectusecase.Page{Items: []projectdomain.Summary{}, Page: page, PageSize: size}, nil
}

func (okProjects) Search(_ context.Context, _ string, q *string, page int) (projectdomain.SearchResult, error) {
	if q == nil || len(*q) < 2 {
		return projectdomain.SearchResult{}, projectdomain.ErrInvalidQuery
	}
	if page < 1 {
		return projectdomain.SearchResult{}, projectdomain.ErrInvalidPage
	}
	return projectdomain.SearchResult{Items: []projectdomain.Summary{}}, nil
}

func (okProjects) Report(context.Context, string, string, projectusecase.ReportInput) (projectdomain.ReportReceipt, error) {
	return projectdomain.ReportReceipt{ID: okProjectReport.ID, CreatedAt: time.Now()}, nil
}

func (okProjects) ListReports(_ context.Context, status *string, page int) (projectusecase.ReportPage, error) {
	if status != nil && *status != "open" && *status != "resolved" {
		return projectusecase.ReportPage{}, &projectdomain.FieldError{Field: "status"}
	}
	return projectusecase.ReportPage{Items: []projectdomain.Report{okProjectReport}, Page: page, PageSize: 20, Total: 1}, nil
}

func (okProjects) GetReport(context.Context, string) (projectdomain.Report, projectdomain.ReportedProject, error) {
	return okProjectReport, projectdomain.ReportedProject{ID: okProjectID, Title: "Graph learning", Status: projectdomain.StatusArchived}, nil
}

func (okProjects) ResolveReport(context.Context, string, string, string, *string) (projectdomain.Report, error) {
	return projectdomain.Report{}, projectdomain.ErrAlreadyResolved
}

// Every project route needs the platform action platformGuards names (SRS 2.3): the admin
// routes refuse a researcher with the one forbidden body, and none of them answers an
// unverified session (FR-17.3).
func TestProjectRoutes(t *testing.T) {
	router := testRouter()
	const p = "/api/v1/projects/" + okProjectID
	tests := []struct {
		name, method, path, body, role string
		want                           int
		wantBody                       string
	}{
		{"create", http.MethodPost, "/api/v1/projects", `{"title":"Graph learning","description":"A study"}`, "researcher", http.StatusCreated, `"myRole":"owner"`},
		{"create, no session", http.MethodPost, "/api/v1/projects", `{"title":"x","description":"y"}`, "", http.StatusUnauthorized, `"unauthenticated"`},
		{"create, unknown field", http.MethodPost, "/api/v1/projects", `{"title":"x","description":"y","owner":"z"}`, "researcher", http.StatusBadRequest, `"bad_request"`},
		{"read", http.MethodGet, p, "", "researcher", http.StatusOK, `"title":"Graph learning"`},
		{"read, not a UUID", http.MethodGet, "/api/v1/projects/nope", "", "researcher", http.StatusNotFound, `"not_found"`},
		{"read as administrator", http.MethodGet, p, "", "admin", http.StatusOK, `"id":"` + okProjectID + `"`},
		{"patch", http.MethodPatch, p, `{"status":"closed"}`, "researcher", http.StatusOK, `"status":"open"`},
		{"patch, null title", http.MethodPatch, p, `{"title":null}`, "researcher", http.StatusBadRequest, `"field":"title"`},
		{"delete, no confirm", http.MethodDelete, p, "", "researcher", http.StatusBadRequest, `"code":"confirmation_mismatch","field":"confirm"`},
		{"delete", http.MethodDelete, p + "?confirm=Graph+learning", "", "researcher", http.StatusNoContent, ""},
		{"report", http.MethodPost, p + "/reports", `{"reason":"spam"}`, "researcher", http.StatusCreated, `"id":"` + okProjectReport.ID + `"`},
		{"my projects", http.MethodGet, "/api/v1/me/projects", "", "researcher", http.StatusOK, `"pageSize":20`},
		{"my projects, page 0", http.MethodGet, "/api/v1/me/projects?page=0", "", "researcher", http.StatusBadRequest, `"bad_request"`},
		{"search", http.MethodGet, "/api/v1/search/projects?q=graph", "", "researcher", http.StatusOK, `"pageSize":20`},
		{"search, no q", http.MethodGet, "/api/v1/search/projects", "", "researcher", http.StatusBadRequest, `"invalid_query"`},
		{"report list, researcher", http.MethodGet, "/api/v1/admin/project-reports", "", "researcher", http.StatusForbidden, `{"code":"forbidden","message":"You do not have permission to do this."}`},
		{"report list, administrator", http.MethodGet, "/api/v1/admin/project-reports", "", "admin", http.StatusOK, `"projectTitle":"Graph learning"`},
		{"report list, bad status", http.MethodGet, "/api/v1/admin/project-reports?status=all", "", "admin", http.StatusBadRequest, `"field":"status"`},
		{"report, researcher", http.MethodGet, "/api/v1/admin/project-reports/" + okProjectReport.ID, "", "researcher", http.StatusForbidden, `"forbidden"`},
		{"report, administrator", http.MethodGet, "/api/v1/admin/project-reports/" + okProjectReport.ID, "", "admin", http.StatusOK, `"status":"archived"`},
		{"resolve, researcher", http.MethodPost, "/api/v1/admin/project-reports/" + okProjectReport.ID + "/resolve", `{"outcome":"dismissed"}`, "researcher", http.StatusForbidden, `"forbidden"`},
		{"resolve twice", http.MethodPost, "/api/v1/admin/project-reports/" + okProjectReport.ID + "/resolve", `{"outcome":"dismissed"}`, "admin", http.StatusConflict, `"report_already_resolved"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body io.Reader
			ct := ""
			if tt.body != "" {
				body, ct = strings.NewReader(tt.body), "application/json"
			}
			rec := sendAs(t, router, tt.method, tt.path, body, ct, tt.role)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.want, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Errorf("body = %s, want it to contain %s", rec.Body, tt.wantBody)
			}
		})
	}
}

// The project actions map onto SRS 2.3's project table: an owner may do all four, an editor
// view and edit the abstract, a viewer and a non-member only view.
func TestProjectPermissions(t *testing.T) {
	perms := projectPermissions{}
	tests := []struct {
		role                         projectdomain.Role
		view, details, abstract, del bool
	}{
		{projectdomain.RoleOwner, true, true, true, true},
		{projectdomain.RoleEditor, true, false, true, false},
		{projectdomain.RoleViewer, true, false, false, false},
		{projectdomain.RoleNone, true, false, false, false},
		{projectdomain.Role("admin"), false, false, false, false},
	}
	for _, tt := range tests {
		for action, want := range map[projectdomain.Action]bool{
			projectdomain.ActView: tt.view, projectdomain.ActEditDetails: tt.details,
			projectdomain.ActEditAbstract: tt.abstract, projectdomain.ActDelete: tt.del,
		} {
			if got := perms.Can(tt.role, action); got != want {
				t.Errorf("Can(%q, %q) = %v, want %v", tt.role, action, got, want)
			}
		}
	}
	if perms.Can(projectdomain.RoleOwner, projectdomain.Action("unknown")) {
		t.Error("an unknown action is allowed")
	}
	// The table itself is authz's; this only checks the mapping.
	if !authz.CanInProject(authz.ProjectOwner, authz.TransferOrDeleteProject) {
		t.Fatal("authz table changed")
	}
}

// Required skills are profile skill tags (FR-4.2): the project module repeats the profile's
// name rule, since modules do not import each other, and this keeps the two the same.
func TestNormalizeSkillNameMatchesProfileTags(t *testing.T) {
	for _, in := range []string{"", " ", "Go", "  Deep \t\n learning ", strings.Repeat("a", 60), strings.Repeat("a", 61),
		strings.Repeat("é", 60), strings.Repeat("é", 61), "machine learning", "\u00a0x\u00a0"} {
		want, err := profileusecase.NormalizeTagName(in)
		got, ok := projectusecase.NormalizeSkillName(in)
		if got != want || ok != (err == nil) {
			t.Errorf("%q: project %q %v, profile %q %v", in, got, ok, want, err)
		}
	}
}
