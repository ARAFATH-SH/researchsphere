package authz

import "testing"

// TestCan checks every cell of the "Platform level" table in SRS 2.3. The visitor
// column is the empty role: in practice httpx.RequireAuth answers 401 before any
// permission check, but the table must refuse a visitor on its own as well. The row
// "View the landing page, register, log in, reset a password" is public and has no
// action.
func TestCan(t *testing.T) {
	const visitor Role = ""
	rows := []struct {
		action                     Action
		visitor, researcher, admin bool
	}{
		{ViewContent, false, true, true},
		{ActAsMember, false, true, true},
		{CreateProject, false, true, true},
		{ManageVenues, false, false, true},
		{RemoveAnyPostOrComment, false, false, true},
		{SuspendUser, false, false, true},
		{ReviewMessageReports, false, false, true},
		{ManageAI, false, false, true},
	}
	for _, row := range rows {
		for _, cell := range []struct {
			role Role
			want bool
		}{
			{visitor, row.visitor},
			{RoleResearcher, row.researcher},
			{RoleAdmin, row.admin},
		} {
			t.Run(string(row.action)+"/"+string(cell.role), func(t *testing.T) {
				if got := Can(cell.role, row.action); got != cell.want {
					t.Errorf("Can(%q, %q) = %v, want %v", cell.role, row.action, got, cell.want)
				}
			})
		}
	}
}

// TestCanInProject checks every cell of the "Project level" table in SRS 2.3.
func TestCanInProject(t *testing.T) {
	rows := []struct {
		action                           ProjectAction
		nonMember, viewer, editor, owner bool
	}{
		{ViewProjectPage, true, true, true, true},
		{RequestToJoin, true, false, false, false},
		{OpenWorkspace, false, true, true, true},
		{SeeVenueRecommendations, false, true, true, true},
		{EditFiles, false, false, true, true},
		{CompileAndAssist, false, false, true, true},
		{EditProjectDetails, false, false, false, true},
		{ManageMembers, false, false, false, true},
		{TransferOrDeleteProject, false, false, false, true},
	}
	for _, row := range rows {
		for _, cell := range []struct {
			role ProjectRole
			want bool
		}{
			{ProjectNonMember, row.nonMember},
			{ProjectViewer, row.viewer},
			{ProjectEditor, row.editor},
			{ProjectOwner, row.owner},
		} {
			t.Run(string(row.action)+"/"+string(cell.role), func(t *testing.T) {
				if got := CanInProject(cell.role, row.action); got != cell.want {
					t.Errorf("CanInProject(%q, %q) = %v, want %v", cell.role, row.action, got, cell.want)
				}
			})
		}
	}
}

// An administrator who is not a member of a project has no extra rights there (SRS 2.3).
// Inside a project only the project role counts, so the admin is a non-member.
func TestAdminOutsideProjectCannotOpenWorkspace(t *testing.T) {
	if !Can(RoleAdmin, SuspendUser) {
		t.Fatal("admin should hold platform rights")
	}
	if CanInProject(ProjectNonMember, OpenWorkspace) {
		t.Error("admin who is a non-member may open the workspace, want refused")
	}
}

func TestUnknownIsRefused(t *testing.T) {
	tests := []struct {
		name string
		got  bool
	}{
		{"unknown role", Can("superuser", ViewContent)},
		{"role in other case", Can("Admin", SuspendUser)},
		{"project role used as platform role", Can(Role(ProjectOwner), ViewContent)},
		{"unknown action, admin", Can(RoleAdmin, "delete_everything")},
		{"empty action, admin", Can(RoleAdmin, "")},
		{"unknown project role", CanInProject("admin", ViewProjectPage)},
		{"empty project role", CanInProject("", ViewProjectPage)},
		{"unknown project action, owner", CanInProject(ProjectOwner, "delete_everything")},
		{"empty project action, owner", CanInProject(ProjectOwner, "")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got {
				t.Error("allowed, want refused")
			}
		})
	}
}
