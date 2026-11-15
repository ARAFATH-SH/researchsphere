import { maxQueryLength, minQueryLength } from "@/components/app-shell/search-box";

import type { PeopleSearch } from "./api";

// maxPage is the last page the search API returns (FR-6.1).
export const maxPage = 50;

// readSearch reads the search state from the URL: q, institution, interest (repeated),
// page and mode (keyword, or absent for a search by meaning). q is trimmed; a page that is
// not 1 to 50 becomes 1.
export function readSearch(params: URLSearchParams): PeopleSearch {
  const page = Number(params.get("page") ?? "1");
  return {
    q: (params.get("q") ?? "").trim(),
    institution: (params.get("institution") ?? "").trim(),
    interests: params.getAll("interest").filter((name) => name.trim() !== ""),
    page: Number.isInteger(page) && page >= 1 && page <= maxPage ? page : 1,
    keywordOnly: params.get("mode") === "keyword",
  };
}

// searchHref is the URL of a search state; empty filters, page 1 and the default mode (by
// meaning) are left out.
export function searchHref({ q, institution, interests, page, keywordOnly }: PeopleSearch): string {
  const params = new URLSearchParams({ q });
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
