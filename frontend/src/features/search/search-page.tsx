"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { FormError } from "@/components/ui/field";
import { emptyCard } from "@/features/home/cards";
import type { Result } from "@/lib/api/call";

import { type PeopleSearch, type PeopleSearchPage, searchPeople } from "./api";
import { SearchFilters } from "./search-filters";
import { queryTooShort, readSearch, searchHref } from "./search-params";
import { FuzzyNotice, NoResults, SearchResults, SearchSkeleton } from "./search-results";

// SearchPage is the search page (FR-6.1 to FR-6.3). Its state lives in the URL (q,
// institution, interest repeated, page), so back, forward and shared links work. It
// searches when the URL changes: on Enter in the top bar, a filter change or a page link.
export function SearchPage() {
  const router = useRouter();
  const params = useSearchParams();
  const search = readSearch(params);
  const href = searchHref(search);
  const tooShort = queryTooShort(search.q);

  const [attempt, setAttempt] = useState(0);
  const key = `${href}#${attempt}`;
  const [result, setResult] = useState<{ key: string; value: Result<PeopleSearchPage> } | null>(null);
  // lastFacets keeps the filters listed while the next results load.
  const [lastFacets, setLastFacets] = useState<PeopleSearchPage["facets"] | null>(null);

  useEffect(() => {
    const wanted = readSearch(new URL(href, "http://x").searchParams);
    if (queryTooShort(wanted.q)) return;
    let current = true;
    void searchPeople(wanted).then((value) => {
      if (!current) return;
      setResult({ key, value });
      if (value.ok) setLastFacets(value.data.facets);
    });
    return () => {
      current = false;
    };
  }, [href, key]);

  // A filter change starts again at page 1.
  function changeFilters(filters: Pick<PeopleSearch, "institution" | "interests">) {
    router.push(searchHref({ ...search, ...filters, page: 1 }));
  }

  const value = !tooShort && result?.key === key ? result.value : null;
  let body;
  if (tooShort) {
    body = (
      <p className={`${emptyCard} px-4 py-3.5 text-rs-body text-rs-text`}>
        Type at least 2 characters to search.
      </p>
    );
  } else if (!value) {
    body = <SearchSkeleton />;
  } else if (!value.ok) {
    body =
      value.code === "rate_limited" ? (
        <FormError look="rs">You&apos;re searching too fast. Try again in a moment.</FormError>
      ) : (
        <div className="flex flex-col items-start gap-3">
          <FormError look="rs">We couldn&apos;t load the results.</FormError>
          <button
            type="button"
            onClick={() => setAttempt((n) => n + 1)}
            className={rsButtonClass({ variant: "secondary", size: "sm" })}
          >
            Try again
          </button>
        </div>
      );
  } else {
    const page = value.data;
    body = (
      <>
        {page.fuzzy && page.correctedQuery && <FuzzyNotice corrected={page.correctedQuery} q={search.q} />}
        {page.total === 0 ? <NoResults search={search} /> : <SearchResults search={search} result={page} />}
      </>
    );
  }

  const facets = value?.ok ? value.data.facets : tooShort ? null : lastFacets;
  return (
    <div className="flex flex-col gap-5 max-[640px]:gap-4">
      <div className="-mt-6 border-b border-rs-border py-2.5 max-[640px]:-mt-4">
        <div role="tablist" aria-label="Result type" className="flex flex-wrap gap-1.5">
          <button
            type="button"
            role="tab"
            aria-selected="true"
            className={rsButtonClass({ variant: "primary", size: "sm" })}
          >
            People
          </button>
        </div>
      </div>
      <h1 className="sr-only">Search{search.q && ` results for “${search.q}”`}</h1>
      <div className="flex flex-wrap items-start gap-6 max-[640px]:gap-4">
        <SearchFilters search={search} facets={facets} onChange={changeFilters} />
        <section aria-label="Results" className="flex min-w-0 flex-[999_1_560px] flex-col gap-3">
          {body}
        </section>
      </div>
    </div>
  );
}
