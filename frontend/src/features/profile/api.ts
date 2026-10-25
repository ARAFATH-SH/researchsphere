import { call, type Result, unexpectedError } from "@/lib/api/call";
import { api, type components } from "@/lib/api/client";

export type Profile = components["schemas"]["Profile"];
export type Education = components["schemas"]["Education"];
export type Tag = components["schemas"]["Tag"];
export type Experience = components["schemas"]["Experience"];
export type EmploymentType = components["schemas"]["EmploymentType"];
export type LocationType = components["schemas"]["LocationType"];
export type YearMonth = components["schemas"]["YearMonth"];

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

export type ExperienceCreate = components["schemas"]["ExperienceCreate"];
export type ExperienceUpdate = components["schemas"]["ExperienceUpdate"];

// addExperience adds a work experience entry (FR-4.8).
export async function addExperience(body: ExperienceCreate): Promise<Result<undefined>> {
  const res = await call(() => api.POST("/profiles/me/experience", { body }));
  return res.ok ? { ok: true, data: undefined } : res;
}

// updateExperience changes a work experience entry (FR-4.8). The dialog sends every field,
// with null for an optional one that is not set, so the body has ExperienceCreate's shape.
// openapi-typescript types ExperienceUpdate's dates as empty objects (they are nullable
// YearMonth refs with an x-go-type), hence the cast.
export async function updateExperience(experienceId: string, body: ExperienceCreate): Promise<Result<undefined>> {
  const params = { path: { experienceId } };
  const patch = body as unknown as ExperienceUpdate;
  const res = await call(() => api.PATCH("/profiles/me/experience/{experienceId}", { params, body: patch }));
  return res.ok ? { ok: true, data: undefined } : res;
}

// removeExperience deletes a work experience entry (FR-4.8).
export async function removeExperience(experienceId: string): Promise<Result<undefined>> {
  const params = { path: { experienceId } };
  const res = await call(() => api.DELETE("/profiles/me/experience/{experienceId}", { params }));
  return res.ok ? { ok: true, data: undefined } : res;
}

export type Contact = components["schemas"]["Contact"];
export type ContactUpdate = components["schemas"]["ContactUpdate"];
export type PublicContact = components["schemas"]["PublicContact"];
export type ProfileLink = components["schemas"]["ProfileLink"];
export type LinkKind = components["schemas"]["LinkKind"];
export type Visibility = components["schemas"]["Visibility"];

// getMyContact returns the signed-in user's contact info and who may see each item (FR-4.7).
export async function getMyContact(): Promise<Result<Contact>> {
  const res = await call(() => api.GET("/profiles/me/contact"));
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

// updateMyContact replaces the phone, birthday and visibilities; null clears an item (FR-4.7).
export async function updateMyContact(body: ContactUpdate): Promise<Result<undefined>> {
  const res = await call(() => api.PUT("/profiles/me/contact", { body }));
  return res.ok ? { ok: true, data: undefined } : res;
}

// addLink adds a link to the signed-in user's profile (FR-4.7).
export async function addLink(kind: LinkKind, url: string): Promise<Result<ProfileLink>> {
  const res = await call(() => api.POST("/profiles/me/links", { body: { kind, url } }));
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

// removeLink removes one of the signed-in user's links (FR-4.7).
export async function removeLink(linkId: string): Promise<Result<undefined>> {
  const params = { path: { linkId } };
  const res = await call(() => api.DELETE("/profiles/me/links/{linkId}", { params }));
  return res.ok ? { ok: true, data: undefined } : res;
}

// ImageKind is which profile image an upload or delete is for (FR-4.6); it is also the
// last path segment of its endpoints.
export type ImageKind = "photo" | "cover";

// ImageFailure is a refused upload or delete. status is the HTTP status, or 0 when the
// server could not be reached. The message shown depends on status, not on the body: a
// 413 from a proxy may not be in the API's error shape.
export type ImageFailure = { ok: false; status: number; code?: string; retryAfter?: number };

export type ImageResult = { ok: true } | ImageFailure;

async function imageRequest(
  request: () => Promise<{ error?: { code?: string }; response: Response }>,
): Promise<ImageResult> {
  try {
    const { error, response } = await request();
    if (response.ok) return { ok: true };
    const retry = Number.parseInt(response.headers.get("Retry-After") ?? "", 10);
    return {
      ok: false,
      status: response.status,
      code: error?.code,
      retryAfter: Number.isFinite(retry) ? retry : undefined,
    };
  } catch {
    return { ok: false, status: 0 };
  }
}

// uploadProfileImage stores a new profile photo or cover image, replacing the old one
// (FR-4.6). The body is multipart with the file in the part named "file".
export function uploadProfileImage(kind: ImageKind, file: File): Promise<ImageResult> {
  const options = {
    // openapi-typescript types a binary part as string; the serializer sends the File.
    body: { file: file as unknown as string },
    bodySerializer: () => {
      const form = new FormData();
      form.append("file", file);
      return form;
    },
  };
  return imageRequest(() =>
    kind === "photo" ? api.PUT("/profiles/me/photo", options) : api.PUT("/profiles/me/cover", options),
  );
}

// removeProfileImage deletes the profile photo or cover image; it succeeds also when none
// is set (FR-4.6).
export function removeProfileImage(kind: ImageKind): Promise<ImageResult> {
  return imageRequest(() =>
    kind === "photo" ? api.DELETE("/profiles/me/photo") : api.DELETE("/profiles/me/cover"),
  );
}
