import Link from "next/link";

import { Avatar } from "@/components/avatar";
import { emptyCard, homeCard } from "@/features/home/cards";

import type { PeopleSearch, PeopleSearchPage, PersonSearchResult } from "./api";
import { maxPage, searchHref } from "./search-params";

// FuzzyNotice says the results are for a name similar to the query (FR-6.2).
export function FuzzyNotice({ corrected, q }: { corrected: string; q: string }) {
  return (
    <p className={`${emptyCard} px-4 py-3.5 text-rs-body text-rs-text`}>
      Showing results for <strong className="font-bold">{corrected}</strong> · searched for “{q}”
    </p>
  );
}

// lastPage is the number of pages the API can return for total results.
function lastPage({ total, pageSize }: PeopleSearchPage): number {
  return Math.min(maxPage, Math.max(1, Math.ceil(total / pageSize)));
}

// SearchResults is the card of one page of people (FR-6.1): a count line, a row per
// person and the page links.
export function SearchResults({ search, result }: { search: PeopleSearch; result: PeopleSearchPage }) {
  const last = lastPage(result);
  const people = result.total === 1 ? "1 person" : `${result.total} people`;
  return (
    <div className={homeCard}>
      <p className="border-b border-rs-divider px-4 py-3 text-rs-body text-rs-text-muted">
        {people} · page {result.page} of {last}
      </p>
      <ul aria-label="People">
        {result.items.map((person) => (
          <PersonRow key={person.userId} person={person} />
        ))}
      </ul>
      {last > 1 && <Pages search={search} page={result.page} last={last} />}
    </div>
  );
}

function PersonRow({ person }: { person: PersonSearchResult }) {
  return (
    <li className="flex items-start gap-3 border-b border-rs-divider px-4 py-3.5">
      <Avatar name={person.fullName} src={person.photoUrl} size="result" decorative />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <Link
          href={`/profile/${person.userId}`}
          className="self-start rounded-rs-2xs text-rs-lead font-bold text-rs-text no-underline hover:text-rs-accent-hover hover:underline focus-visible:shadow-focus focus-visible:outline-none"
        >
          {person.fullName}
        </Link>
        {person.headline && <span className="text-rs-body text-rs-text">{person.headline}</span>}
        {person.institution && (
          <span className="text-rs-caption text-rs-text-muted">{person.institution}</span>
        )}
        {person.interests.length > 0 && (
          <span className="text-rs-caption text-rs-heading">Interests: {person.interests.join(", ")}</span>
        )}
      </div>
    </li>
  );
}

const pageLink =
  "flex h-10 items-center justify-center rounded-full text-rs-body font-medium no-underline focus-visible:shadow-focus focus-visible:outline-none";

// Pages links to the previous page, up to five page numbers around this one and the next.
function Pages({ search, page, last }: { search: PeopleSearch; page: number; last: number }) {
  const first = Math.max(1, Math.min(page - 2, last - 4));
  const numbers = Array.from({ length: Math.min(5, last - first + 1) }, (_, i) => first + i);
  const href = (n: number) => searchHref({ ...search, page: n });
  return (
    <nav aria-label="Pages" className="flex flex-wrap items-center justify-center gap-1.5 p-3">
      {page > 1 && (
        <Link href={href(page - 1)} className={`${pageLink} px-2.5 text-rs-accent-strong hover:text-rs-accent-hover`}>
          Previous
        </Link>
      )}
      {numbers.map((n) =>
        n === page ? (
          <Link
            key={n}
            href={href(n)}
            aria-current="page"
            className={`${pageLink} min-w-10 bg-rs-accent-strong text-rs-surface hover:text-rs-surface`}
          >
            {n}
          </Link>
        ) : (
          <Link key={n} href={href(n)} className={`${pageLink} min-w-10 text-rs-text hover:bg-rs-subtle`}>
            {n}
          </Link>
        ),
      )}
      {page < last && (
        <Link href={href(page + 1)} className={`${pageLink} px-2.5 text-rs-accent-strong hover:text-rs-accent-hover`}>
          Next
        </Link>
      )}
    </nav>
  );
}

// NoResults is the empty state: a tip, and a link that clears the filters when any are on.
export function NoResults({ search }: { search: PeopleSearch }) {
  const filtered = search.institution !== "" || search.interests.length > 0;
  return (
    <div className={`${emptyCard} flex flex-col items-start gap-1.5 px-4 py-3.5`}>
      <h2 className="text-rs-lead leading-rs-snug font-medium text-rs-text">No researchers match “{search.q}”.</h2>
      <p className="text-rs-body text-rs-text-muted">Try a different spelling, or search by institution or interest.</p>
      {filtered && (
        <Link
          href={searchHref({ q: search.q, institution: "", interests: [], page: 1 })}
          className="rounded-rs-2xs text-rs-body font-medium text-rs-accent-strong hover:text-rs-accent-hover hover:underline focus-visible:shadow-focus focus-visible:outline-none"
        >
          Reset filters
        </Link>
      )}
    </div>
  );
}

// SearchSkeleton stands in for the results card while they load. Screen readers hear only
// the status line.
export function SearchSkeleton() {
  const bar = "animate-pulse rounded-full bg-rs-neutral";
  return (
    <>
      <p role="status" className="sr-only">
        Searching…
      </p>
      <div aria-hidden="true" className={homeCard}>
        <div className="border-b border-rs-divider px-4 py-3">
          <div className={`h-4 w-48 ${bar}`} />
        </div>
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="flex items-start gap-3 border-b border-rs-divider px-4 py-3.5 last:border-b-0">
            <div className="size-14 flex-none animate-pulse rounded-full bg-rs-neutral" />
            <div className="flex flex-1 flex-col gap-2 pt-1">
              <div className={`h-4 w-1/3 ${bar}`} />
              <div className={`h-3.5 w-1/2 ${bar}`} />
              <div className={`h-3 w-1/4 ${bar}`} />
            </div>
          </div>
        ))}
      </div>
    </>
  );
}
