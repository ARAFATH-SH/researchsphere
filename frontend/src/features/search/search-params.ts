import { maxQueryLength, minQueryLength } from "@/components/app-shell/search-box";

import type { PeopleSearch } from "./api";

// maxPage is the last page the search API returns (FR-6.1, FR-10.3).
export const maxPage = 50;

// SearchType is the result tab: researchers (FR-6) or open projects (FR-10.3).
export type SearchType = "people" | "projects";

// SearchState is the whole search as the URL holds it.
export type SearchState = PeopleSearch & { type: SearchType };

// readSearch reads the search state from the URL: type (projects, or absent for people), q,
// institution, interest (repeated), page and mode (keyword, or absent for a search by
// meaning). q is trimmed; a page that is not 1 to 50 becomes 1.
export function readSearch(params: URLSearchParams): SearchState {
  const page = Number(params.get("page") ?? "1");
  return {
    q: (params.get("q") ?? "").trim(),
    institution: (params.get("institution") ?? "").trim(),
    interests: params.getAll("interest").filter((name) => name.trim() !== ""),
    page: Number.isInteger(page) && page >= 1 && page <= maxPage ? page : 1,
    keywordOnly: params.get("mode") === "keyword",
    type: params.get("type") === "projects" ? "projects" : "people",
  };
}

// searchHref is the URL of a search state; the people tab, empty filters, page 1 and the
// default mode (by meaning) are left out. The projects tab has no filters.
export function searchHref({
  q,
  institution,
  interests,
  page,
  keywordOnly,
  type = "people",
}: PeopleSearch & { type?: SearchType }): string {
  const params = new URLSearchParams({ q });
  if (type === "projects") {
    params.set("type", "projects");
    if (page > 1) params.set("page", String(page));
    if (keywordOnly) params.set("mode", "keyword");
    return `/search?${params}`;
  }
  if (institution) params.set("institution", institution);
  for (const name of interests) params.append("interest", name);
  if (page > 1) params.set("page", String(page));
  if (keywordOnly) params.set("mode", "keyword");
  return `/search?${params}`;
}

// queryTooShort says whether q is too short to search.
export function queryTooShort(q: string): boolean {
  return q.trim().length < minQueryLength;
}

// queryTooLong says whether q is too long for the mode: 300 characters by meaning, 100 for
// keyword only.
export function queryTooLong(q: string, keywordOnly: boolean): boolean {
  return q.trim().length > maxQueryLength(keywordOnly);
}
