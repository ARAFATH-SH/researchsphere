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
