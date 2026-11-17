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

func (okProjects) Search(_ context.Context, _ string, q *string, mode string, page int) (projectdomain.SearchResult, error) {
	if mode != "" && mode != "auto" && mode != "keyword" {
		return projectdomain.SearchResult{}, projectdomain.ErrInvalidSearchMode
	}
	if q == nil || len(*q) < 2 {
		return projectdomain.SearchResult{}, projectdomain.ErrInvalidQuery
	}
	if page < 1 {
		return projectdomain.SearchResult{}, projectdomain.ErrInvalidPage
	}
	m := projectdomain.ResultHybrid
	if mode == "keyword" {
		m = projectdomain.ResultKeyword
	}
	return projectdomain.SearchResult{Mode: m, Items: []projectdomain.Summary{}}, nil
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

const okJoinRequestID = "55555555-5555-4555-8555-555555555555"

var (
	okMember  = projectdomain.Member{Person: projectdomain.Person{UserID: okUserID, FullName: "Ada"}, Role: projectdomain.RoleEditor}
	okRequest = projectdomain.JoinRequest{ID: okJoinRequestID, ProjectID: okProjectID, Kind: projectdomain.KindRequest, Status: projectdomain.RequestPending, UserID: okUserID}
	okMine    = projectdomain.MyRequestItem{JoinRequest: okRequest, Project: projectdomain.MembershipProject{
		ID: okProjectID, Title: "Graph learning", Status: projectdomain.StatusOpen, Owner: projectdomain.Person{UserID: okUserID, FullName: "Ada"},
	}}
)

// The membership operations answer with fixed values, or a refusal where a test needs one:
// a role other than editor or viewer, and the owner leaving.
func (okProjects) RequestToJoin(context.Context, string, string, *string) (projectdomain.MyRequestItem, error) {
	return okMine, nil
}

func (okProjects) CancelRequest(context.Context, string, string, string) error { return nil }

func (okProjects) DecideRequest(_ context.Context, _, _, _ string, accept bool, role projectdomain.Role) (projectdomain.Member, error) {
	if accept && !projectdomain.MemberRoleValid(role) {
		return projectdomain.Member{}, &projectdomain.FieldError{Field: "role"}
	}
	return okMember, nil
}

func (okProjects) Invite(context.Context, string, string, projectusecase.InviteInput) (projectdomain.RequestItem, error) {
	inv := okRequest
	inv.Kind, inv.Role = projectdomain.KindInvitation, projectdomain.RoleViewer
	return projectdomain.RequestItem{JoinRequest: inv, User: okMember.Person}, nil
}

func (okProjects) CancelInvitation(context.Context, string, string, string) error {
	return projectdomain.ErrInvitationNotPending
}

func (okProjects) AnswerInvitation(context.Context, string, string, string, bool) (projectdomain.Member, error) {
	return okMember, nil
}

func (okProjects) ChangeRole(context.Context, string, string, string, projectdomain.Role) (projectdomain.Member, error) {
	return okMember, nil
}

func (okProjects) RemoveMember(context.Context, string, string, string) error { return nil }

func (okProjects) Leave(context.Context, string, string) error {
	return projectdomain.ErrOwnerMustTransfer
}

func (okProjects) Transfer(context.Context, string, string, string) (projectdomain.Project, error) {
	p := okProject
	p.MyRole = projectdomain.RoleEditor
	return p, nil
}

func (okProjects) Members(_ context.Context, _, _ string, page, size int) (projectusecase.MemberPage, error) {
	if page < 1 {
		return projectusecase.MemberPage{}, projectdomain.ErrInvalidPage
	}
	return projectusecase.MemberPage{Items: []projectdomain.Member{okMember}, Page: page, PageSize: size, Total: 1}, nil
}

func (okProjects) ProjectRequests(_ context.Context, _, _ string, kind projectdomain.RequestKind, page, size int) (projectusecase.RequestPage, error) {
	r := okRequest
	if kind == projectdomain.KindInvitation {
		r.Kind, r.Role = kind, projectdomain.RoleViewer
	}
	return projectusecase.RequestPage{Items: []projectdomain.RequestItem{{JoinRequest: r, User: okMember.Person}}, Page: page, PageSize: size, Total: 1}, nil
}

func (okProjects) MyRequests(_ context.Context, _ string, page, size int) (projectusecase.MyRequestPage, error) {
	return projectusecase.MyRequestPage{Items: []projectdomain.MyRequestItem{okMine}, Page: page, PageSize: size, Total: 1}, nil
}

func (okProjects) MyInvitations(_ context.Context, _ string, page, size int) (projectusecase.MyRequestPage, error) {
	inv := okMine
	inv.Kind, inv.Role = projectdomain.KindInvitation, projectdomain.RoleViewer
	return projectusecase.MyRequestPage{Items: []projectdomain.MyRequestItem{inv}, Page: page, PageSize: size, Total: 1}, nil
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
		{"search, hybrid by default", http.MethodGet, "/api/v1/search/projects?q=graph", "", "researcher", http.StatusOK, `"mode":"hybrid"`},
		{"search, keyword mode", http.MethodGet, "/api/v1/search/projects?q=graph&mode=keyword", "", "researcher", http.StatusOK, `"mode":"keyword"`},
		{"search, bad mode", http.MethodGet, "/api/v1/search/projects?q=graph&mode=semantic", "", "researcher", http.StatusBadRequest, `"field":"mode"`},
		{"report list, researcher", http.MethodGet, "/api/v1/admin/project-reports", "", "researcher", http.StatusForbidden, `{"code":"forbidden","message":"You do not have permission to do this."}`},
		{"report list, administrator", http.MethodGet, "/api/v1/admin/project-reports", "", "admin", http.StatusOK, `"projectTitle":"Graph learning"`},
		{"report list, bad status", http.MethodGet, "/api/v1/admin/project-reports?status=all", "", "admin", http.StatusBadRequest, `"field":"status"`},
		{"report, researcher", http.MethodGet, "/api/v1/admin/project-reports/" + okProjectReport.ID, "", "researcher", http.StatusForbidden, `"forbidden"`},
		{"report, administrator", http.MethodGet, "/api/v1/admin/project-reports/" + okProjectReport.ID, "", "admin", http.StatusOK, `"status":"archived"`},
		{"resolve, researcher", http.MethodPost, "/api/v1/admin/project-reports/" + okProjectReport.ID + "/resolve", `{"outcome":"dismissed"}`, "researcher", http.StatusForbidden, `"forbidden"`},
		{"ask to join", http.MethodPost, p + "/join-requests", `{"message":"Hi"}`, "researcher", http.StatusCreated, `"status":"pending"`},
		{"ask to join, no session", http.MethodPost, p + "/join-requests", `{}`, "", http.StatusUnauthorized, `"unauthenticated"`},
		{"cancel a request", http.MethodDelete, p + "/join-requests/" + okJoinRequestID, "", "researcher", http.StatusNoContent, ""},
		{"accept a request", http.MethodPost, p + "/join-requests/" + okJoinRequestID + "/accept", `{"role":"editor"}`, "researcher", http.StatusOK, `"role":"editor"`},
		{"accept as owner", http.MethodPost, p + "/join-requests/" + okJoinRequestID + "/accept", `{"role":"owner"}`, "researcher", http.StatusBadRequest, `"field":"role"`},
		{"reject a request", http.MethodPost, p + "/join-requests/" + okJoinRequestID + "/reject", "", "researcher", http.StatusNoContent, ""},
		{"project requests", http.MethodGet, p + "/join-requests", "", "researcher", http.StatusOK, `"status":"pending"`},
		{"invite", http.MethodPost, p + "/invitations", `{"userId":"` + okUserID + `","role":"viewer"}`, "researcher", http.StatusCreated, `"role":"viewer"`},
		{"invite, no user", http.MethodPost, p + "/invitations", `{"role":"viewer"}`, "researcher", http.StatusBadRequest, `"bad_request"`},
		{"project invitations", http.MethodGet, p + "/invitations", "", "researcher", http.StatusOK, `"role":"viewer"`},
		{"cancel an answered invitation", http.MethodDelete, p + "/invitations/" + okJoinRequestID, "", "researcher", http.StatusConflict, `"invitation_not_pending"`},
		{"accept an invitation", http.MethodPost, p + "/invitations/" + okJoinRequestID + "/accept", "", "researcher", http.StatusOK, `"role":"editor"`},
		{"reject an invitation", http.MethodPost, p + "/invitations/" + okJoinRequestID + "/reject", "", "researcher", http.StatusNoContent, ""},
		{"members", http.MethodGet, p + "/members", "", "researcher", http.StatusOK, `"total":1`},
		{"members, page 0", http.MethodGet, p + "/members?page=0", "", "researcher", http.StatusBadRequest, `"bad_request"`},
		{"change a role", http.MethodPatch, p + "/members/" + okUserID, `{"role":"editor"}`, "researcher", http.StatusOK, `"role":"editor"`},
		{"remove a member", http.MethodDelete, p + "/members/" + okUserID, "", "researcher", http.StatusNoContent, ""},
		{"owner leaves", http.MethodPost, p + "/leave", "", "researcher", http.StatusConflict, `"owner_must_transfer"`},
		{"transfer", http.MethodPost, p + "/transfer", `{"userId":"` + okUserID + `"}`, "researcher", http.StatusOK, `"myRole":"editor"`},
		{"my requests", http.MethodGet, "/api/v1/me/join-requests", "", "researcher", http.StatusOK, `"title":"Graph learning"`},
		{"my invitations", http.MethodGet, "/api/v1/me/invitations", "", "researcher", http.StatusOK, `"role":"viewer"`},
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
	// Requesting to join is for non-members only; transferring for the owner only.
	for role, want := range map[projectdomain.Role][2]bool{
		projectdomain.RoleNone: {true, false}, projectdomain.RoleViewer: {false, false},
		projectdomain.RoleEditor: {false, false}, projectdomain.RoleOwner: {false, true},
	} {
		if got := [2]bool{perms.Can(role, projectdomain.ActRequestToJoin), perms.Can(role, projectdomain.ActTransfer)}; got != want {
			t.Errorf("%q: request to join, transfer = %v, want %v", role, got, want)
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
