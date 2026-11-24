"use client";

import Link from "next/link";
import { type ReactNode, useEffect, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { homeCard } from "@/features/home/cards";
import type { Result } from "@/lib/api/call";

import { getVenue, type Venue } from "./api";
import { downloadCalendar } from "./ics";
import { userTimeZone } from "./time";
import { FieldChips, TypeBadge } from "./venue-bits";
import { DetailList, ScopeText } from "./venue-details";

type VenuePageProps = { venueId: string; now?: Date; userZone?: string };

// VenuePage is a venue's own page (FR-14.4): every date in the venue's zone and the reader's,
// the fields, the place, the link, the full scope as text, and "Add to calendar". A venue whose
// deadline has passed says so; a removed or unknown venue is not found.
export function VenuePage({ venueId, now: fixedNow, userZone: fixedZone }: VenuePageProps) {
  const [attempt, setAttempt] = useState(0);
  const key = `${venueId}#${attempt}`;
  const [result, setResult] = useState<{ key: string; res: Result<Venue> } | null>(null);
  const [clock] = useState(() => fixedNow ?? new Date());
  const [userZone] = useState(() => fixedZone ?? userTimeZone());

  useEffect(() => {
    let current = true;
    void getVenue(venueId).then((res) => {
      if (current) setResult({ key, res });
    });
    return () => {
      current = false;
    };
  }, [venueId, key]);

  const res = result?.key === key ? result.res : null;
  if (!res) {
    return (
      <p role="status" className="text-rs-body text-rs-text-muted">
        Loading the venue…
      </p>
    );
  }
  if (!res.ok) {
    return res.code === "not_found" ? (
      <StateCard title="Venue not found" text="This venue does not exist or has been removed.">
        <Link href="/events" className={rsButtonClass({ full: true })}>
          Go to Events
        </Link>
      </StateCard>
    ) : (
      <StateCard title="Something went wrong" text="We couldn’t load this venue.">
        <button type="button" onClick={() => setAttempt((n) => n + 1)} className={rsButtonClass({ full: true })}>
          Try again
        </button>
      </StateCard>
    );
  }
  const v = res.data;
  return (
    <div className="flex flex-col gap-4">
      <Link
        href="/events"
        className="self-start text-rs-body font-medium text-rs-accent-strong hover:underline focus-visible:shadow-focus focus-visible:outline-none"
      >
        <span aria-hidden="true">← </span>All venues
      </Link>
      {v.past && (
        <p role="status" className="rounded-card border border-rs-warning-line bg-rs-warning-soft px-4 py-3 text-rs-body font-medium text-rs-warning">
          Deadline passed: submissions to this venue have closed.
        </p>
      )}
      <article aria-labelledby="venue-title" className={`${homeCard} overflow-hidden`}>
        <header className="flex flex-col gap-2 bg-rs-accent-strong p-5 text-rs-surface max-[640px]:p-4">
          <h1 id="venue-title" className="m-0 text-rs-h3 leading-rs-tight font-bold">
            {v.name}
            {v.shortName && <span className="font-medium"> ({v.shortName})</span>}
          </h1>
        </header>
        <div className="flex flex-col gap-4 p-5 max-[640px]:p-4">
          <div className="flex flex-wrap items-center gap-2">
            <TypeBadge type={v.type} />
            <FieldChips fields={v.fields} />
          </div>
          <DetailList venue={v} userZone={userZone} now={clock} full />
          {v.submissionDeadline && (
            <button
              type="button"
              onClick={() => downloadCalendar(v)}
              className={`${rsButtonClass({ variant: "outline", size: "md" })} self-start`}
            >
              Add to calendar
            </button>
          )}
          <h2 className="mt-1 text-rs-lead leading-rs-snug font-medium">Scope</h2>
          <ScopeText venue={v} />
        </div>
      </article>
    </div>
  );
}

function StateCard({ title, text, children }: { title: string; text: string; children: ReactNode }) {
  return (
    <div className={`${homeCard} mx-auto flex w-full max-w-[400px] flex-col gap-4 p-6 text-center`}>
      <h1 className="text-rs-h3 leading-rs-tight font-bold text-rs-text">{title}</h1>
      <p className="text-rs-body text-rs-text-muted">{text}</p>
      {children}
    </div>
  );
}
