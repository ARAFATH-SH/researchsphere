import { call, type Result, unexpectedError } from "@/lib/api/call";
import { api, type components } from "@/lib/api/client";

export type Project = components["schemas"]["Project"];
export type ProjectSummary = components["schemas"]["ProjectSummary"];
export type ProjectPage = components["schemas"]["ProjectPage"];
export type ProjectStatus = components["schemas"]["ProjectStatus"];
export type ProjectRole = components["schemas"]["ProjectRole"];
export type ProjectPerson = components["schemas"]["ProjectPerson"];
export type ProjectCreate = components["schemas"]["ProjectCreate"];
export type ProjectUpdate = components["schemas"]["ProjectUpdate"];
export type ProjectSearchPage = components["schemas"]["ProjectSearchPage"];
export type ProjectSearchResult = components["schemas"]["ProjectSearchResult"];
export type ProjectRecommendations = components["schemas"]["ProjectRecommendations"];
export type ProjectRecommendation = components["schemas"]["ProjectRecommendation"];
export type PeopleRecommendations = components["schemas"]["PeopleRecommendations"];
export type ReportReason = components["schemas"]["ReportReason"];
export type MemberRole = components["schemas"]["MemberRole"];
export type ProjectMember = components["schemas"]["ProjectMember"];
export type ProjectMemberPage = components["schemas"]["ProjectMemberPage"];
export type ProjectJoinRequest = components["schemas"]["ProjectJoinRequest"];
export type ProjectJoinRequestPage = components["schemas"]["ProjectJoinRequestPage"];
export type MyJoinRequest = components["schemas"]["MyJoinRequest"];
export type MyJoinRequestPage = components["schemas"]["MyJoinRequestPage"];
export type ProjectInvitation = components["schemas"]["ProjectInvitation"];
export type ProjectInvitationPage = components["schemas"]["ProjectInvitationPage"];
export type MyInvitation = components["schemas"]["MyInvitation"];
export type MyInvitationPage = components["schemas"]["MyInvitationPage"];

// myProjectsPageSize is how many of the caller's projects one page of /projects lists.
export const myProjectsPageSize = 20;

// maxProjectRecommendations is the most GET /recommendations/projects returns (AI-3.1).
export const maxProjectRecommendations = 10;

function withData<T>(res: Result<T | undefined>): Result<T> {
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

// listMyProjects returns one page of the caller's projects with the caller's role in each
// (FR-10, FR-11.6), most recently changed first.
export async function listMyProjects(page: number): Promise<Result<ProjectPage>> {
  return withData(
    await call(() => api.GET("/me/projects", { params: { query: { page, pageSize: myProjectsPageSize } } })),
  );
}

// createProject creates a project owned by the caller (FR-10.1).
export async function createProject(body: ProjectCreate): Promise<Result<Project>> {
  return withData(await call(() => api.POST("/projects", { body })));
}

// getProject reads a project's page. A project the caller may not see is not_found (404),
// like one that does not exist.
export async function getProject(projectId: string): Promise<Result<Project>> {
  return withData(await call(() => api.GET("/projects/{projectId}", { params: { path: { projectId } } })));
}

// updateProject changes the named fields of a project (FR-10.2); null clears the abstract.
export async function updateProject(projectId: string, body: ProjectUpdate): Promise<Result<Project>> {
  return withData(
    await call(() => api.PATCH("/projects/{projectId}", { params: { path: { projectId } }, body })),
  );
}

// deleteProject deletes a project and everything in it once confirm is its title (FR-10.6).
export async function deleteProject(projectId: string, confirm: string): Promise<Result<undefined>> {
  const res = await call(() =>
    api.DELETE("/projects/{projectId}", { params: { path: { projectId }, query: { confirm } } }),
  );
  return res.ok ? { ok: true, data: undefined } : res;
}

// reportProject reports a project to the administrators (FR-15.11).
export async function reportProject(
  projectId: string,
  body: { reason: ReportReason; details?: string },
): Promise<Result<undefined>> {
  const res = await call(() =>
    api.POST("/projects/{projectId}/reports", { params: { path: { projectId } }, body }),
  );
  return res.ok ? { ok: true, data: undefined } : res;
}

export type ProjectSearch = {
  q: string;
  page: number;
  // keywordOnly searches by keyword only (mode=keyword); otherwise by keyword and meaning.
  keywordOnly: boolean;
};

// searchProjects returns one page of open projects matching q (FR-10.3), found by keyword
// and meaning unless keywordOnly (AI-5.1). The answer's mode says how (AI-5.4).
export async function searchProjects({ q, page, keywordOnly }: ProjectSearch): Promise<Result<ProjectSearchPage>> {
  return withData(
    await call(() =>
      api.GET("/search/projects", {
        params: { query: { q, page, mode: keywordOnly ? "keyword" : undefined } },
      }),
    ),
  );
}

// getProjectRecommendations reads the open projects most similar to the caller's profile
// (AI-3.1): the whole list, which every card on a page shares.
export async function getProjectRecommendations(): Promise<Result<ProjectRecommendations>> {
  return withData(
    await call(() =>
      api.GET("/recommendations/projects", { params: { query: { limit: maxProjectRecommendations } } }),
    ),
  );
}

// getRecommendedResearchers reads the researchers most similar to a project, for its owner
// (AI-2.3).
export async function getRecommendedResearchers(projectId: string): Promise<Result<PeopleRecommendations>> {
  return withData(
    await call(() =>
      api.GET("/projects/{projectId}/recommended-researchers", { params: { path: { projectId } } }),
    ),
  );
}

// membershipPageSize is how many members, requests or invitations one membership list reads:
// the server's cap (FR-11).
export const membershipPageSize = 50;

// maxJoinMessageLength is the most a join request's message may hold (FR-11.1).
export const maxJoinMessageLength = 500;

// myRequestPages is how many pages of the caller's own join requests findMyPendingRequest
// reads at most: 250 requests, more than a year of the 20-a-day limit is unlikely to reach.
export const myRequestPages = 5;

function done(res: Result<unknown>): Result<undefined> {
  return res.ok ? { ok: true, data: undefined } : res;
}

const pageQuery = (page: number) => ({ page, pageSize: membershipPageSize });

// createJoinRequest asks to join an open project; an empty message means none (FR-11.1).
export async function createJoinRequest(projectId: string, message: string): Promise<Result<MyJoinRequest>> {
  const body = { message: message.trim() === "" ? null : message.trim() };
  return withData(
    await call(() => api.POST("/projects/{projectId}/join-requests", { params: { path: { projectId } }, body })),
  );
}

// cancelJoinRequest withdraws the caller's own pending request (FR-11.1).
export async function cancelJoinRequest(projectId: string, requestId: string): Promise<Result<undefined>> {
  return done(
    await call(() =>
      api.DELETE("/projects/{projectId}/join-requests/{requestId}", { params: { path: { projectId, requestId } } }),
    ),
  );
}

// listProjectJoinRequests reads the project's join requests, pending first, for its owner
// (FR-11.2).
export async function listProjectJoinRequests(projectId: string, page = 1): Promise<Result<ProjectJoinRequestPage>> {
  return withData(
    await call(() =>
      api.GET("/projects/{projectId}/join-requests", { params: { path: { projectId }, query: pageQuery(page) } }),
    ),
  );
}

// acceptJoinRequest makes the request's sender a member with role (FR-11.2).
export async function acceptJoinRequest(
  projectId: string,
  requestId: string,
  role: MemberRole,
): Promise<Result<ProjectMember>> {
  return withData(
    await call(() =>
      api.POST("/projects/{projectId}/join-requests/{requestId}/accept", {
        params: { path: { projectId, requestId } },
        body: { role },
      }),
    ),
  );
}

// rejectJoinRequest turns the request down (FR-11.2).
export async function rejectJoinRequest(projectId: string, requestId: string): Promise<Result<undefined>> {
  return done(
    await call(() =>
      api.POST("/projects/{projectId}/join-requests/{requestId}/reject", { params: { path: { projectId, requestId } } }),
    ),
  );
}

// createInvitation invites a user to the project as editor or viewer (FR-11.3).
export async function createInvitation(
  projectId: string,
  userId: string,
  role: MemberRole,
): Promise<Result<ProjectInvitation>> {
  return withData(
    await call(() =>
      api.POST("/projects/{projectId}/invitations", { params: { path: { projectId } }, body: { userId, role } }),
    ),
  );
}

// listProjectInvitations reads the project's invitations, pending first, for its owner
// (FR-11.3).
export async function listProjectInvitations(projectId: string, page = 1): Promise<Result<ProjectInvitationPage>> {
  return withData(
    await call(() =>
      api.GET("/projects/{projectId}/invitations", { params: { path: { projectId }, query: pageQuery(page) } }),
    ),
  );
}

// cancelInvitation withdraws a pending invitation (FR-11.3).
export async function cancelInvitation(projectId: string, invitationId: string): Promise<Result<undefined>> {
  return done(
    await call(() =>
      api.DELETE("/projects/{projectId}/invitations/{invitationId}", {
        params: { path: { projectId, invitationId } },
      }),
    ),
  );
}

// acceptInvitation joins the project with the invitation's role (FR-11.3).
export async function acceptInvitation(projectId: string, invitationId: string): Promise<Result<ProjectMember>> {
  return withData(
    await call(() =>
      api.POST("/projects/{projectId}/invitations/{invitationId}/accept", {
        params: { path: { projectId, invitationId } },
      }),
    ),
  );
}

// rejectInvitation declines an invitation (FR-11.3).
export async function rejectInvitation(projectId: string, invitationId: string): Promise<Result<undefined>> {
  return done(
    await call(() =>
      api.POST("/projects/{projectId}/invitations/{invitationId}/reject", {
        params: { path: { projectId, invitationId } },
      }),
    ),
  );
}

// listProjectMembers reads the members, the owner first (SRS 2.3).
export async function listProjectMembers(projectId: string, page = 1): Promise<Result<ProjectMemberPage>> {
  return withData(
    await call(() =>
      api.GET("/projects/{projectId}/members", { params: { path: { projectId }, query: pageQuery(page) } }),
    ),
  );
}

// updateProjectMember changes a member's role (FR-11.4).
export async function updateProjectMember(
  projectId: string,
  userId: string,
  role: MemberRole,
): Promise<Result<ProjectMember>> {
  return withData(
    await call(() =>
      api.PATCH("/projects/{projectId}/members/{userId}", { params: { path: { projectId, userId } }, body: { role } }),
    ),
  );
}

// removeProjectMember removes a member (FR-11.4).
export async function removeProjectMember(projectId: string, userId: string): Promise<Result<undefined>> {
  return done(
    await call(() => api.DELETE("/projects/{projectId}/members/{userId}", { params: { path: { projectId, userId } } })),
  );
}

// leaveProject takes the caller, a member other than the owner, out of the project (FR-11.5).
export async function leaveProject(projectId: string): Promise<Result<undefined>> {
  return done(await call(() => api.POST("/projects/{projectId}/leave", { params: { path: { projectId } } })));
}

// transferProject makes another member the owner; the caller becomes an editor (FR-10.5). It
// answers with the project as the caller now sees it.
export async function transferProject(projectId: string, userId: string): Promise<Result<Project>> {
  return withData(
    await call(() => api.POST("/projects/{projectId}/transfer", { params: { path: { projectId } }, body: { userId } })),
  );
}

// listMyJoinRequests reads the caller's own join requests, newest first (FR-11.1).
export async function listMyJoinRequests(page = 1): Promise<Result<MyJoinRequestPage>> {
  return withData(await call(() => api.GET("/me/join-requests", { params: { query: pageQuery(page) } })));
}

// listMyInvitations reads the caller's pending invitations, newest first (FR-11.3).
export async function listMyInvitations(page = 1): Promise<Result<MyInvitationPage>> {
  return withData(await call(() => api.GET("/me/invitations", { params: { query: pageQuery(page) } })));
}

// findMyPendingRequest finds the caller's pending join request to the project, or null. The
// project page has no field for it, so it reads the caller's requests, newest first, at most
// myRequestPages pages.
export async function findMyPendingRequest(projectId: string): Promise<Result<MyJoinRequest | null>> {
  for (let page = 1; page <= myRequestPages; page++) {
    const res = await listMyJoinRequests(page);
    if (!res.ok) return res;
    const found = res.data.items.find((r) => r.project.id === projectId && r.status === "pending");
    if (found) return { ok: true, data: found };
    if (page * res.data.pageSize >= res.data.total) break;
  }
  return { ok: true, data: null };
}

// findMyInvitation finds the caller's pending invitation to the project, or null.
export async function findMyInvitation(projectId: string): Promise<Result<MyInvitation | null>> {
  for (let page = 1; page <= myRequestPages; page++) {
    const res = await listMyInvitations(page);
    if (!res.ok) return res;
    const found = res.data.items.find((i) => i.project.id === projectId);
    if (found) return { ok: true, data: found };
    if (page * res.data.pageSize >= res.data.total) break;
  }
  return { ok: true, data: null };
}

const membershipErrors: Record<string, string> = {
  already_member: "They are already a member of this project.",
  project_not_open: "This project is not accepting members.",
  request_pending: "There is already a pending join request.",
  invitation_pending: "You have an invitation to this project. Accept it instead.",
  already_invited: "They are already invited.",
  request_not_pending: "This request was already decided.",
  invitation_not_pending: "This invitation was already answered.",
  use_transfer: "Use Transfer ownership to change the owner.",
  cannot_remove_owner: "The owner cannot be removed.",
  owner_must_transfer: "Transfer ownership before you leave, or delete the project.",
  not_a_member: "They are no longer a member.",
  already_owner: "They already own this project.",
  member_unavailable: "This member cannot take over the project right now.",
  user_not_found: "This researcher cannot be invited.",
  rate_limited: "You have reached today’s limit of 20. Try again tomorrow.",
  account_suspended: "Your account is suspended, so you can’t do this now.",
  network_error: "Could not reach the server. Check your connection and try again.",
};

// membershipErrorText is the sentence for a failed membership action, or fallback.
export function membershipErrorText(code: string, fallback = "Something went wrong. Try again."): string {
  return membershipErrors[code] ?? fallback;
}
