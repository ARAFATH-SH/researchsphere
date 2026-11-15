import { call, type Result, unexpectedError } from "@/lib/api/call";
import { api, type components } from "@/lib/api/client";

export type PeopleSearchPage = components["schemas"]["PeopleSearchPage"];
export type PersonSearchResult = components["schemas"]["PersonSearchResult"];

export type PeopleSearch = {
  q: string;
  institution: string;
  interests: string[];
  page: number;
  // keywordOnly searches by keyword only (mode=keyword); otherwise by keyword and meaning.
  keywordOnly: boolean;
};

// searchPeople returns one page of researchers matching q, with the filter facets
// (FR-6.1 to FR-6.3), found by keyword and meaning unless keywordOnly (AI-5.1, AI-5.2).
// The answer's mode says how (AI-5.4). Too many searches fail with code rate_limited (429).
export async function searchPeople({
  q,
  institution,
  interests,
  page,
  keywordOnly,
}: PeopleSearch): Promise<Result<PeopleSearchPage>> {
  const res = await call(() =>
    api.GET("/search/people", {
      params: {
        query: {
          q,
          institution: institution || undefined,
          interest: interests.length > 0 ? interests : undefined,
          page,
          mode: keywordOnly ? "keyword" : undefined,
        },
      },
    }),
  );
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}
