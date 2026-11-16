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
