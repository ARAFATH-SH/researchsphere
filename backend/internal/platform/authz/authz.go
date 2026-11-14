// Package authz holds the permission tables of SRS 2.3 (FR-3.2): which platform role may
// perform which platform action, and which project role may perform which project
// action. It knows nothing about HTTP or the database. Every endpoint asks it before
// acting (docs/decisions.md); httpx.RequirePlatform is the HTTP side.
//
// Anything the tables do not list is refused: an unknown role, an unknown action, or
// the empty role of a visitor (deny by default).
package authz

import "context"

// Role is a platform role. The values match the user_role enum in the database.
// Visitors have no role: they have no access token, and httpx.RequireAuth refuses them
// before any permission check (FR-3.1).
type Role string

// Platform roles (SRS 2.2).
const (
	RoleResearcher Role = "researcher"
	RoleAdmin      Role = "admin"
)

// Action is a platform action: one row of the "Platform level" table in SRS 2.3. The
// row "View the landing page, register, log in, reset a password" has no action
// because those operations are public (security: [] in docs/openapi.yaml).
type Action string

// Platform actions.
const (
	// ViewContent is the row "View profiles, search, feed, events and recommendations".
	ViewContent Action = "view_content"
	// ActAsMember is the row "Edit own profile, connect, post, like, comment".
	ActAsMember Action = "act_as_member"
	// CreateProject is the row "Create a project".
	CreateProject Action = "create_project"
	// ManageVenues is the row "Add, edit and remove venues".
	ManageVenues Action = "manage_venues"
	// RemoveAnyPostOrComment is the row "Remove any post or comment".
	RemoveAnyPostOrComment Action = "remove_any_post_or_comment"
	// SuspendUser is the row "Suspend a user account".
	SuspendUser Action = "suspend_user"
	// ModerateUser is the row "Warn or restrict a user, and read their moderation history".
	ModerateUser Action = "moderate_user"
	// ReviewMessageReports is the row "Review reported conversations".
	ReviewMessageReports Action = "review_message_reports"
	// ManageAI is "Manage the AI matching model": read the embedding statistics and queue a
	// rebuild (AI-1.6). Administrators only.
	ManageAI Action = "manage_ai"
)

// platform is the "Platform level" table. A missing entry means No.
var platform = map[Role]map[Action]bool{
	RoleResearcher: {
		ViewContent:   true,
		ActAsMember:   true,
		CreateProject: true,
	},
	RoleAdmin: {
		ViewContent:            true,
		ActAsMember:            true,
		CreateProject:          true,
		ManageVenues:           true,
		RemoveAnyPostOrComment: true,
		SuspendUser:            true,
		ModerateUser:           true,
		ReviewMessageReports:   true,
		ManageAI:               true,
	},
}

// Can reports whether a user with role may perform action (SRS 2.3, platform level).
func Can(role Role, action Action) bool {
	return platform[role][action]
}

// ProjectRole is a user's role in one project. The member values match the
// project_role enum in the database; ProjectNonMember stands for having no member row.
type ProjectRole string

// Project roles.
const (
	ProjectNonMember ProjectRole = "non_member"
	ProjectViewer    ProjectRole = "viewer"
	ProjectEditor    ProjectRole = "editor"
	ProjectOwner     ProjectRole = "owner"
)

// ProjectAction is a project action: one row of the "Project level" table in SRS 2.3.
type ProjectAction string

// Project actions.
const (
	// ViewProjectPage is the row "View the project page (title, description, skills,
	// members)".
	ViewProjectPage ProjectAction = "view_project_page"
	// RequestToJoin is the row "Request to join".
	RequestToJoin ProjectAction = "request_to_join"
	// OpenWorkspace is the row "Open the workspace, read files, view the PDF".
	OpenWorkspace ProjectAction = "open_workspace"
	// SeeVenueRecommendations is the row "See venue recommendations for the project".
	SeeVenueRecommendations ProjectAction = "see_venue_recommendations"
	// EditFiles is the row "Edit files, upload figures".
	EditFiles ProjectAction = "edit_files"
	// CompileAndAssist is the row "Compile, use the assistant, edit the abstract".
	CompileAndAssist ProjectAction = "compile_and_assist"
	// EditProjectDetails is the row "Edit project details and status".
	EditProjectDetails ProjectAction = "edit_project_details"
	// ManageMembers is the row "Invite, decide join requests, change roles, remove
	// members".
	ManageMembers ProjectAction = "manage_members"
	// TransferOrDeleteProject is the row "Transfer ownership, delete the project".
	TransferOrDeleteProject ProjectAction = "transfer_or_delete_project"
)

// project is the "Project level" table. A missing entry means No.
var project = map[ProjectRole]map[ProjectAction]bool{
	ProjectNonMember: {
		ViewProjectPage: true,
		RequestToJoin:   true,
	},
	ProjectViewer: {
		ViewProjectPage:         true,
		OpenWorkspace:           true,
		SeeVenueRecommendations: true,
	},
	ProjectEditor: {
		ViewProjectPage:         true,
		OpenWorkspace:           true,
		SeeVenueRecommendations: true,
		EditFiles:               true,
		CompileAndAssist:        true,
	},
	ProjectOwner: {
		ViewProjectPage:         true,
		OpenWorkspace:           true,
		SeeVenueRecommendations: true,
		EditFiles:               true,
		CompileAndAssist:        true,
		EditProjectDetails:      true,
		ManageMembers:           true,
		TransferOrDeleteProject: true,
	},
}

// CanInProject reports whether a user with projectRole in a project may perform action
// there (SRS 2.3, project level). It deliberately does not take the platform role: an
// administrator has no extra rights inside a project and cannot open a workspace
// without being a member (SRS 2.3).
func CanInProject(projectRole ProjectRole, action ProjectAction) bool {
	return project[projectRole][action]
}

// ProjectRoleLookup finds a user's role in a project, ProjectNonMember when the user
// has no member row. Slice 19 (projects) implements it; nothing does yet.
type ProjectRoleLookup interface {
	ProjectRole(ctx context.Context, projectID, userID string) (ProjectRole, error)
}
