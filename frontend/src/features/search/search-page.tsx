"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { type KeyboardEvent, useEffect, useState } from "react";

import { maxQueryLength } from "@/components/app-shell/search-box";
import { useSearchDraftLength } from "@/components/app-shell/search-draft";

import { rsButtonClass } from "@/components/ui/button";
import { FormError } from "@/components/ui/field";
import { emptyCard } from "@/features/home/cards";
import { type ProjectSearchPage, searchProjects } from "@/features/project/api";
import type { Result } from "@/lib/api/call";

import { type PeopleSearch, type PeopleSearchPage, searchPeople } from "./api";
import { ProjectNoResults, ProjectResults } from "./project-results";
import { SearchFilters } from "./search-filters";
import { queryTooLong, queryTooShort, readSearch, searchHref, type SearchType } from "./search-params";
import {
  FallbackBanner,
  FuzzyNotice,
  HybridNote,
  NoResults,
  SearchResults,
  SearchSkeleton,
} from "./search-results";

type Found =
  | { type: "people"; res: Result<PeopleSearchPage> }
  | { type: "projects"; res: Result<ProjectSearchPage> };

// SearchPage is the search page (FR-6.1 to FR-6.3, FR-10.3, AI-5.1 to AI-5.4). Its state
// lives in the URL (type=projects, q, institution, interest repeated, page, mode=keyword), so
// back, forward and shared links work. It searches when the URL changes: on Enter in the top
// bar, a tab, mode or filter change, or a page link. The People tab searches researchers with
// filters; the Projects tab searches open projects, with the same modes and notices.
export function SearchPage() {
  const router = useRouter();
  const params = useSearchParams();
  const search = readSearch(params);
  const href = searchHref(search);
  const tooShort = queryTooShort(search.q);
  const tooLong = queryTooLong(search.q, search.keywordOnly);
  const idle = tooShort || tooLong;

  const [attempt, setAttempt] = useState(0);
  const key = `${href}#${attempt}`;
  const [result, setResult] = useState<{ key: string; value: Found } | null>(null);
  // lastFacets keeps the filters listed while the next results load.
  const [lastFacets, setLastFacets] = useState<PeopleSearchPage["facets"] | null>(null);

  useEffect(() => {
    const wanted = readSearch(new URL(href, "http://x").searchParams);
    if (queryTooShort(wanted.q) || queryTooLong(wanted.q, wanted.keywordOnly)) return;
    let current = true;
    const { type, ...people } = wanted;
    if (type === "projects") {
      void searchProjects({ q: people.q, page: people.page, keywordOnly: people.keywordOnly }).then((res) => {
        if (current) setResult({ key, value: { type: "projects", res } });
      });
    } else {
      void searchPeople(people).then((res) => {
        if (!current) return;
        setResult({ key, value: { type: "people", res } });
        if (res.ok) setLastFacets(res.data.facets);
      });
    }
    return () => {
      current = false;
    };
  }, [href, key]);

  // A filter change starts again at page 1.
  function changeFilters(filters: Pick<PeopleSearch, "institution" | "interests">) {
    router.push(searchHref({ ...search, ...filters, page: 1 }));
  }

  // A tab change keeps the text and the mode and starts again at page 1.
  function changeType(type: SearchType) {
    if (type !== search.type) router.push(searchHref({ ...search, type, page: 1 }));
  }

  // A mode change keeps the filters and starts again at page 1.
  function changeMode(keywordOnly: boolean) {
    if (keywordOnly !== search.keywordOnly) router.push(searchHref({ ...search, keywordOnly, page: 1 }));
  }

  const found = !idle && result?.key === key ? result.value : null;
  const value = found?.res ?? null;
  let body;
  if (tooShort) {
    body = (
      <p className={`${emptyCard} px-4 py-3.5 text-rs-body text-rs-text`}>
        Type at least 2 characters to search.
      </p>
    );
  } else if (tooLong) {
    body = (
      <p className={`${emptyCard} px-4 py-3.5 text-rs-body text-rs-text`}>
        {search.keywordOnly
          ? "Keyword search takes up to 100 characters. Shorten your search, or search by meaning."
          : "Searches take up to 300 characters. Shorten your search."}
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
  } else if (found?.type === "projects" && found.res.ok) {
    const page = found.res.data;
    body = (
      <>
        {page.mode === "keyword_fallback" && <FallbackBanner />}
        {page.fuzzy && page.correctedQuery && <FuzzyNotice corrected={page.correctedQuery} q={search.q} />}
        {page.total === 0 ? (
          <ProjectNoResults q={search.q} mode={page.mode} />
        ) : (
          <>
            {page.mode === "hybrid" && <HybridNote what="projects whose work matches" />}
            <ProjectResults search={search} result={page} />
          </>
        )}
      </>
    );
  } else if (found?.type === "people" && found.res.ok) {
    const page = found.res.data;
    body = (
      <>
        {page.mode === "keyword_fallback" && <FallbackBanner />}
        {page.fuzzy && page.correctedQuery && <FuzzyNotice corrected={page.correctedQuery} q={search.q} />}
        {page.total === 0 ? (
          <NoResults search={search} mode={page.mode} />
        ) : (
          <>
            {page.mode === "hybrid" && <HybridNote />}
            <SearchResults search={search} result={page} />
          </>
        )}
      </>
    );
  }

  const facets = found?.type === "people" && found.res.ok ? found.res.data.facets : idle ? null : lastFacets;
  return (
    <div className="flex flex-col gap-5 max-[640px]:gap-4">
      <div className="-mt-6 flex flex-wrap items-center gap-2 border-b border-rs-border py-2.5 max-[640px]:-mt-4">
        <ResultTabs type={search.type} onChange={changeType} />
        <span aria-hidden="true" className="mx-1 h-7 w-px bg-rs-border" />
        <ModeSwitch keywordOnly={search.keywordOnly} onChange={changeMode} />
        <QueryCounter keywordOnly={search.keywordOnly} />
      </div>
      <h1 className="sr-only">Search{search.q && ` results for “${search.q}”`}</h1>
      <div className="flex flex-wrap items-start gap-6 max-[640px]:gap-4">
        {search.type === "people" && <SearchFilters search={search} facets={facets} onChange={changeFilters} />}
        <section aria-label="Results" className="flex min-w-0 flex-[999_1_560px] flex-col gap-3">
          {body}
        </section>
      </div>
    </div>
  );
}

const tabs: { type: SearchType; label: string }[] = [
  { type: "people", label: "People" },
  { type: "projects", label: "Projects" },
];

// ResultTabs picks what to search: People or Projects. Posts join with their slice. The
// arrow keys move between the tabs.
function ResultTabs({ type, onChange }: { type: SearchType; onChange: (type: SearchType) => void }) {
  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
    event.preventDefault();
    const i = tabs.findIndex((t) => t.type === type);
    const next = (i + (event.key === "ArrowRight" ? 1 : tabs.length - 1)) % tabs.length;
    onChange(tabs[next].type);
    event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus();
  }
  return (
    <div role="tablist" aria-label="Result type" onKeyDown={onKeyDown} className="flex flex-wrap gap-1.5">
      {tabs.map((tab) => {
        const selected = tab.type === type;
        return (
          <button
            key={tab.type}
            type="button"
            role="tab"
            aria-selected={selected}
            tabIndex={selected ? 0 : -1}
            onClick={() => onChange(tab.type)}
            className={rsButtonClass({ variant: selected ? "primary" : "secondary", size: "sm" })}
          >
            {tab.label}
          </button>
        );
      })}
    </div>
  );
}

const modeButton =
  "flex min-h-[34px] items-center gap-1.5 px-3.5 text-rs-caption font-medium focus-visible:shadow-focus focus-visible:outline-none";

// ModeSwitch is the design's search mode control: Keyword, or By meaning (AI), the
// default (AI-5.1). It is a radio group: the arrow keys move the choice.
function ModeSwitch({ keywordOnly, onChange }: { keywordOnly: boolean; onChange: (keywordOnly: boolean) => void }) {
  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (!["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(event.key)) return;
    event.preventDefault();
    const next = !keywordOnly;
    onChange(next);
    event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="radio"]')[next ? 0 : 1]?.focus();
  }
  const option = (checked: boolean) =>
    `${modeButton} ${checked ? "bg-rs-accent-soft text-rs-heading" : "bg-rs-surface text-rs-accent-strong hover:bg-rs-subtle"}`;
  return (
    <div
      role="radiogroup"
      aria-label="Search mode"
      onKeyDown={onKeyDown}
      className="flex overflow-hidden rounded-full border border-rs-accent-strong"
    >
      <button
        type="button"
        role="radio"
        aria-checked={keywordOnly}
        tabIndex={keywordOnly ? 0 : -1}
        onClick={() => onChange(true)}
        className={option(keywordOnly)}
      >
        Keyword
      </button>
      <button
        type="button"
        role="radio"
        aria-checked={!keywordOnly}
        tabIndex={keywordOnly ? -1 : 0}
        onClick={() => onChange(false)}
        className={option(!keywordOnly)}
      >
        <SparkleIcon />
        By meaning (AI)
      </button>
    </div>
  );
}

function SparkleIcon() {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M12 3l1.8 4.7L18.5 9.5l-4.7 1.8L12 16l-1.8-4.7L5.5 9.5l4.7-1.8z" />
    </svg>
  );
}

// QueryCounter shows how many characters the top bar's search box holds while the user
// types, once they near the limit: from 250 of 300 by meaning, from 80 of 100 for keyword.
function QueryCounter({ keywordOnly }: { keywordOnly: boolean }) {
  const length = useSearchDraftLength();
  const max = maxQueryLength(keywordOnly);
  if (length < (keywordOnly ? 80 : 250)) return null;
  return (
    <span className={`ml-auto text-rs-caption ${length > max ? "text-rs-danger" : "text-rs-text-muted"}`}>
      {length} / {max} characters
    </span>
  );
}
