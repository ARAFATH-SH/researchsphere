import type { PeopleRecommendations, PersonRecommendation } from "./api";

// Test data for the recommendation lists.

export function recommended(n: number, extra: Partial<PersonRecommendation> = {}): PersonRecommendation {
  return {
    userId: `5c3e9a10-0000-4000-8000-0000000003${String(n).padStart(2, "0")}`,
    fullName: `Researcher ${n}`,
    headline: `Headline ${n}`,
    institution: null,
    photoUrl: null,
    connectionStatus: "none",
    score: 100 - n,
    shared: [],
    ...extra,
  };
}

export function people(...ns: number[]): PersonRecommendation[] {
  return ns.map((n) => recommended(n));
}

export function ready(items: PersonRecommendation[]): { ok: true; data: PeopleRecommendations } {
  return { ok: true, data: { state: "ready", missing: [], items } };
}

export const needsProfile: { ok: true; data: PeopleRecommendations } = {
  ok: true,
  data: { state: "needs_profile", missing: ["interests", "skills", "bio"], items: [] },
};

export const processing: { ok: true; data: PeopleRecommendations } = {
  ok: true,
  data: { state: "processing", missing: [], items: [] },
};

export const failed = { ok: false as const, code: "internal_error", message: "Something went wrong on our side." };
