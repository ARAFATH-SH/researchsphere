import { call, type Result, unexpectedError } from "@/lib/api/call";
import { api, type components } from "@/lib/api/client";

export type PeopleRecommendations = components["schemas"]["PeopleRecommendations"];
export type PersonRecommendation = components["schemas"]["PersonRecommendation"];
export type MissingField = PeopleRecommendations["missing"][number];

// maxRecommendations is the most GET /recommendations/people returns (AI-2.1).
export const maxRecommendations = 10;

// getPeopleRecommendations reads the caller's collaborator recommendations (AI-2.1, AI-2.2,
// AI-2.4, AI-4): the whole list, which every card on a page shares.
export async function getPeopleRecommendations(): Promise<Result<PeopleRecommendations>> {
  const res = await call(() =>
    api.GET("/recommendations/people", { params: { query: { limit: maxRecommendations } } }),
  );
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}
