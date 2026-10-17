import { call, type Result, unexpectedError } from "@/lib/api/call";
import { api, type components } from "@/lib/api/client";

export type Profile = components["schemas"]["Profile"];
export type Education = components["schemas"]["Education"];
export type Tag = components["schemas"]["Tag"];

// ProfileResult is what the profile page needs to know about a request: the profile, or
// that there is none, or that something else went wrong. The server's error text is
// never passed on; the page shows its own words.
export type ProfileResult = { status: "ok"; profile: Profile } | { status: "notFound" } | { status: "error" };

async function load(
  request: () => Promise<{ data?: Profile; response: Response }>,
): Promise<ProfileResult> {
  try {
    const { data, response } = await request();
    if (response.ok && data) return { status: "ok", profile: data };
    if (response.status === 404) return { status: "notFound" };
    return { status: "error" };
  } catch {
    return { status: "error" };
  }
}

// getMyProfile returns the signed-in user's own profile (FR-4.1).
export function getMyProfile(): Promise<ProfileResult> {
  return load(() => api.GET("/profiles/me"));
}

// getProfile returns any user's profile (FR-4.5). An unknown, suspended or unverified
// account is notFound.
export function getProfile(userId: string): Promise<ProfileResult> {
  return load(() => api.GET("/profiles/{userId}", { params: { path: { userId } } }));
}

export type ProfileUpdate = components["schemas"]["ProfileUpdate"];
export type EducationCreate = components["schemas"]["EducationCreate"];
export type EducationUpdate = components["schemas"]["EducationUpdate"];

// TagKind says which list a tag belongs to; it is also the path segment of its endpoints.
export type TagKind = "interests" | "skills";

// updateMyProfile replaces the signed-in user's profile fields (FR-4.1).
export async function updateMyProfile(body: ProfileUpdate): Promise<Result<undefined>> {
  const res = await call(() => api.PUT("/profiles/me", { body }));
  return res.ok ? { ok: true, data: undefined } : res;
}

// addTag adds a research interest or skill by name (FR-4.2). The server matches names
// ignoring letter case and answers with the tag it stored or already had.
export async function addTag(kind: TagKind, name: string): Promise<Result<Tag>> {
  const res = await call(() =>
    kind === "interests"
      ? api.POST("/profiles/me/interests", { body: { name } })
      : api.POST("/profiles/me/skills", { body: { name } }),
  );
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

// removeTag removes a research interest or skill from the user's profile (FR-4.2).
export async function removeTag(kind: TagKind, tagId: string): Promise<Result<undefined>> {
  const params = { path: { tagId } };
  const res = await call(() =>
    kind === "interests"
      ? api.DELETE("/profiles/me/interests/{tagId}", { params })
      : api.DELETE("/profiles/me/skills/{tagId}", { params }),
  );
  return res.ok ? { ok: true, data: undefined } : res;
}

// suggestTags returns existing tags that match the typed text, best first (FR-4.3).
export async function suggestTags(kind: TagKind, q: string): Promise<Result<Tag[]>> {
  const params = { query: { q, limit: 8 } };
  const res = await call(() =>
    kind === "interests" ? api.GET("/tags/interests", { params }) : api.GET("/tags/skills", { params }),
  );
  if (!res.ok) return res;
  return { ok: true, data: res.data?.items ?? [] };
}

// addEducation adds an education entry (FR-4.4).
export async function addEducation(body: EducationCreate): Promise<Result<undefined>> {
  const res = await call(() => api.POST("/profiles/me/education", { body }));
  return res.ok ? { ok: true, data: undefined } : res;
}

// updateEducation changes an education entry; null clears an optional field (FR-4.4).
export async function updateEducation(educationId: string, body: EducationUpdate): Promise<Result<undefined>> {
  const params = { path: { educationId } };
  const res = await call(() => api.PATCH("/profiles/me/education/{educationId}", { params, body }));
  return res.ok ? { ok: true, data: undefined } : res;
}

// removeEducation deletes an education entry (FR-4.4).
export async function removeEducation(educationId: string): Promise<Result<undefined>> {
  const params = { path: { educationId } };
  const res = await call(() => api.DELETE("/profiles/me/education/{educationId}", { params }));
  return res.ok ? { ok: true, data: undefined } : res;
}
