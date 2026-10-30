"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { homeCard } from "@/features/home/cards";

import { PublicationItem } from "./publication-item";
import { listPublications, type PublicationPage } from "./publications-api";

// publicationsPageSize is how many publications one page shows.
export const publicationsPageSize = 10;

type Loaded =
  | { status: "ok"; page: PublicationPage }
  | { status: "notFound" }
  | { status: "error" };

// PublicationListPage lists all of a user's confirmed publications (FR-5.5), ten a page,
// with Previous and Next from the API's total. It is read-only, also for the owner, who
// adds and removes publications on the profile.
export function PublicationListPage({ userId }: { userId: string }) {
  const [page, setPage] = useState(1);
  const [attempt, setAttempt] = useState(0);
  const key = `${userId}:${page}:${attempt}`;
  const [result, setResult] = useState<{ key: string; value: Loaded } | null>(
    null,
  );

  useEffect(() => {
    let current = true;
    void listPublications(userId, page, publicationsPageSize).then((res) => {
      if (!current) return;
      let value: Loaded;
      if (res.ok) value = { status: "ok", page: res.data };
      else if (res.status === 404) value = { status: "notFound" };
      else value = { status: "error" };
      setResult({ key, value });
    });
    return () => {
      current = false;
    };
  }, [userId, page, key]);

  const loading = !result || result.key !== key;
  const value = result?.value;
  let body;
  if (!value) {
    body = <p className="text-rs-body text-rs-text-muted">Loading publications…</p>;
  } else if (value.status === "notFound") {
    body = (
      <p className="text-rs-body text-rs-text-muted">
        This profile does not exist or is not available.
      </p>
    );
  } else if (value.status === "error") {
    body = (
      <div className="flex flex-col items-start gap-3">
        <p className="text-rs-body text-rs-text-muted">
          We couldn&apos;t load the publications.
        </p>
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
    const { items, total } = value.page;
    const pages = Math.max(1, Math.ceil(total / publicationsPageSize));
    body =
      total === 0 ? (
        <p className="text-rs-body text-rs-text-muted">No publications yet.</p>
      ) : (
        <>
          <ul aria-label="Publications" className="flex flex-col gap-3">
            {items.map((p) => (
              <PublicationItem key={p.id} publication={p} />
            ))}
          </ul>
          {pages > 1 && (
            <nav
              aria-label="Pages"
              className="flex items-center justify-between gap-2 border-t border-rs-divider pt-3"
            >
              <button
                type="button"
                onClick={() => setPage(page - 1)}
                disabled={page <= 1 || loading}
                className={rsButtonClass({ variant: "secondary", size: "sm" })}
              >
                Previous
              </button>
              <p className="text-rs-body text-rs-text-muted" aria-live="polite">
                Page {page} of {pages}
              </p>
              <button
                type="button"
                onClick={() => setPage(page + 1)}
                disabled={page >= pages || loading}
                className={rsButtonClass({ variant: "secondary", size: "sm" })}
              >
                Next
              </button>
            </nav>
          )}
        </>
      );
  }

  const total = value?.status === "ok" ? value.page.total : undefined;
  return (
    <section
      aria-labelledby="all-publications-heading"
      className={`${homeCard} flex flex-col gap-3 p-5`}
    >
      <Link
        href={`/profile/${userId}`}
        className="self-start rounded-rs-2xs text-rs-body font-medium text-rs-accent-strong hover:underline focus-visible:shadow-focus focus-visible:outline-none"
      >
        <span aria-hidden="true">←</span> Back to profile
      </Link>
      <h1
        id="all-publications-heading"
        className="text-rs-title leading-rs-snug font-medium text-rs-text"
      >
        Publications{total !== undefined && ` (${total})`}
      </h1>
      {body}
    </section>
  );
}
