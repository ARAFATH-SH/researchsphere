"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import type { Result } from "@/lib/api/call";

import { getVenue, type Venue, type VenueSummary } from "./api";
import { DetailList, kicker, ScopeText } from "./venue-details";

type PanelProps = { venue: VenueSummary; userZone: string; now: Date };

// VenuePanel is the design's side panel beside the list (wide screens only): the chosen
// venue's facts and its scope, loaded with GET /venues/{id}, and a link to its own page.
export function VenuePanel({ venue, userZone, now }: PanelProps) {
  const [loaded, setLoaded] = useState<{ id: string; res: Result<Venue> } | null>(null);

  useEffect(() => {
    let current = true;
    void getVenue(venue.id).then((res) => {
      if (current) setLoaded({ id: venue.id, res });
    });
    return () => {
      current = false;
    };
  }, [venue.id]);

  const res = loaded?.id === venue.id ? loaded.res : null;
  return (
    <section
      id="venue-preview"
      aria-labelledby="venue-preview-title"
      className="min-w-0 flex-[1_1_340px] overflow-hidden rounded-card border border-rs-border bg-rs-surface shadow-card max-lg:hidden lg:max-w-[400px]"
    >
      <div className="flex flex-col gap-1 bg-rs-accent-strong p-4 text-rs-surface">
        <span className="text-rs-caption font-medium">{kicker(venue)}</span>
        <h2 id="venue-preview-title" className="m-0 text-rs-lead leading-rs-snug font-medium">
          {venue.name}
        </h2>
      </div>
      <div className="flex flex-col gap-2.5 p-4">
        <DetailList venue={venue} userZone={userZone} now={now} />
        <h3 className="mt-1.5 text-rs-lead leading-rs-snug font-medium">Scope</h3>
        {!res && (
          <p role="status" className="text-rs-body text-rs-text-muted">
            Loading the scope…
          </p>
        )}
        {res && !res.ok && <p className="text-rs-body text-rs-text-muted">We couldn’t load the scope.</p>}
        {res?.ok && <ScopeText venue={res.data} />}
        <Link
          href={`/events/${venue.id}`}
          className="self-start text-rs-body font-medium text-rs-accent-strong hover:underline focus-visible:shadow-focus focus-visible:outline-none"
        >
          Open the full page
        </Link>
      </div>
    </section>
  );
}
