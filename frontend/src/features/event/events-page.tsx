"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { type KeyboardEvent, useEffect, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { FormError } from "@/components/ui/field";
import { PageNav } from "@/components/ui/page-nav";
import type { Result } from "@/lib/api/call";

import { listResearchFields, listVenues, type ResearchField, type VenueFacets, type VenuePage, type VenueSummary } from "./api";
import { locationLabel } from "./labels";
import {
  activeFilterCount,
  emptyFilters,
  type EventsState,
  type EventsTab,
  eventsHref,
  rangeError,
  readEvents,
  toQuery,
} from "./list-params";
import { dateRangeLabel, tileParts, userTimeZone } from "./time";
import { DeadlineText, deadlineTone, FieldChips, OfficialLink, TypeBadge } from "./venue-bits";
import { type FilterChange, VenueFilters } from "./venue-filters";
import { VenuePanel } from "./venue-panel";

type EventsPageProps = { now?: Date; userZone?: string };

const cardClass = "rounded-card border border-rs-border bg-rs-surface shadow-card";

// EventsPage lists venues by nearest submission deadline (FR-14.1), hides past ones unless
// asked (FR-14.2) and filters them (FR-14.3), as design/screens/Events.html shows. Every filter,
// the tab and the page live in the URL. Rolling journals have their own tab, by name. On wide
// screens the chosen venue shows in the side panel; under 641px the filters move into a sheet.
export function EventsPage({ now: fixedNow, userZone: fixedZone }: EventsPageProps) {
  const router = useRouter();
  const params = useSearchParams();
  const state = readEvents(params);
  const href = eventsHref(state);
  const badRange = rangeError(state) !== "";

  const [clock] = useState(() => fixedNow ?? new Date());
  const [userZone] = useState(() => fixedZone ?? userTimeZone());
  const [fields, setFields] = useState<ResearchField[]>([]);
  const [attempt, setAttempt] = useState(0);
  const key = `${href}#${attempt}`;
  const [result, setResult] = useState<{ key: string; res: Result<VenuePage> } | null>(null);
  const [lastFacets, setLastFacets] = useState<VenueFacets | null>(null);
  const [chosen, setChosen] = useState<string | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);

  useEffect(() => {
    let current = true;
    void listResearchFields().then((res) => {
      if (current && res.ok) setFields(res.data);
    });
    return () => {
      current = false;
    };
  }, []);

  useEffect(() => {
    const wanted = readEvents(new URL(href, "http://x").searchParams);
    if (rangeError(wanted)) return;
    let current = true;
    void listVenues(toQuery(wanted, userZone)).then((res) => {
      if (!current) return;
      setResult({ key, res });
      if (res.ok) setLastFacets(res.data.facets);
    });
    return () => {
      current = false;
    };
  }, [href, key, userZone]);

  function go(next: EventsState, push = false) {
    const url = eventsHref(next);
    if (push) router.push(url);
    else router.replace(url, { scroll: false });
  }
  // A filter change starts again at page 1.
  const changeFilters = (change: FilterChange) => go({ ...state, ...change, page: 1 });
  const changeTab = (tab: EventsTab) => {
    if (tab !== state.tab) go({ ...state, tab, page: 1 }, true);
  };

  const res = !badRange && result?.key === key ? result.res : null;
  const page = res?.ok ? res.data : null;
  const selected = page?.items.find((v) => v.id === chosen) ?? page?.items[0] ?? null;
  const journals = state.tab === "journals";
  const filterCount = activeFilterCount(state);

  let body;
  if (badRange) {
    body = <p className="px-4 py-3.5 text-rs-body text-rs-text-muted">Choose a last day on or after the first day.</p>;
  } else if (!res) {
    body = (
      <p role="status" className="px-4 py-3.5 text-rs-body text-rs-text-muted">
        Loading venues…
      </p>
    );
  } else if (!res.ok) {
    body = (
      <div className="flex flex-col items-start gap-3 px-4 py-3.5">
        <FormError look="rs">{res.code === "bad_request" ? res.message : "We couldn’t load the venues."}</FormError>
        <button type="button" onClick={() => setAttempt((n) => n + 1)} className={rsButtonClass({ variant: "secondary", size: "sm" })}>
          Try again
        </button>
      </div>
    );
  } else if (res.data.items.length === 0) {
    body = (
      <div className="flex flex-col items-start gap-3 px-4 py-3.5">
        <p className="text-rs-body text-rs-text">
          {filterCount > 0 ? "No venues match these filters." : journals ? "No open-submission journals yet." : "No upcoming deadlines yet."}
        </p>
        {filterCount > 0 && (
          <button type="button" onClick={() => changeFilters({ ...emptyFilters })} className={rsButtonClass({ variant: "secondary", size: "sm" })}>
            Clear filters
          </button>
        )}
      </div>
    );
  } else {
    body = (
      <>
        <ul aria-label={journals ? "Journals" : "Venues"}>
          {res.data.items.map((v) => (
            <VenueRow
              key={v.id}
              venue={v}
              selected={v.id === selected?.id}
              onSelect={() => setChosen(v.id)}
              userZone={userZone}
              now={clock}
            />
          ))}
        </ul>
        {res.data.total > res.data.pageSize && (
          <PageNav
            page={res.data.page}
            pageSize={res.data.pageSize}
            total={res.data.total}
            onPage={(n) => go({ ...state, page: n }, true)}
          />
        )}
      </>
    );
  }

  const filters = (layout: "bar" | "sheet") => (
    <VenueFilters state={state} facets={lastFacets} fields={fields} onChange={changeFilters} layout={layout} />
  );

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <h1 className="m-0 text-rs-h3 leading-rs-tight font-bold text-rs-text">Conferences, workshops &amp; journal calls</h1>
          <p className="mt-1 text-rs-body text-rs-text-muted">
            {journals ? "Journals that take submissions at any time, by name." : "Sorted by nearest submission deadline."}
            {page && (
              <span>
                {" "}
                {page.total} {page.total === 1 ? "venue" : "venues"}.
              </span>
            )}
          </p>
        </div>
        <ListTabs tab={state.tab} onChange={changeTab} />
      </div>

      <div className={`${cardClass} px-4 py-3.5 max-[640px]:hidden`}>{filters("bar")}</div>
      <div className="min-[641px]:hidden">
        <button
          id="venue-filters-open"
          type="button"
          onClick={() => setSheetOpen(true)}
          className={rsButtonClass({ variant: "outline", size: "md", full: true })}
        >
          Filters{filterCount > 0 ? ` (${filterCount})` : ""}
        </button>
      </div>
      {sheetOpen && (
        <Dialog title="Filters" placement="sheet" onClose={() => setSheetOpen(false)} returnFocusId="venue-filters-open">
          <div className="flex flex-col gap-4">
            {filters("sheet")}
            <button type="button" onClick={() => setSheetOpen(false)} className={rsButtonClass({ full: true })}>
              {page ? `Show ${page.total} ${page.total === 1 ? "venue" : "venues"}` : "Show venues"}
            </button>
          </div>
        </Dialog>
      )}

      <div className="flex flex-wrap items-start gap-6">
        <section aria-label="Venue list" className={`${cardClass} min-w-0 flex-[999_1_520px] overflow-hidden`}>
          {body}
        </section>
        {selected && <VenuePanel venue={selected} userZone={userZone} now={clock} />}
      </div>
    </div>
  );
}

const tabs: { tab: EventsTab; label: string }[] = [
  { tab: "deadlines", label: "Upcoming deadlines" },
  { tab: "journals", label: "Journals (open submission)" },
];

// ListTabs switches between venues with a deadline and rolling journals. The arrow keys move.
function ListTabs({ tab, onChange }: { tab: EventsTab; onChange: (tab: EventsTab) => void }) {
  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
    event.preventDefault();
    const i = tabs.findIndex((t) => t.tab === tab);
    const next = (i + 1) % tabs.length;
    onChange(tabs[next].tab);
    event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus();
  }
  return (
    <div role="tablist" aria-label="Venue lists" onKeyDown={onKeyDown} className="flex flex-wrap gap-1.5">
      {tabs.map((t) => {
        const selected = t.tab === tab;
        return (
          <button
            key={t.tab}
            type="button"
            role="tab"
            aria-selected={selected}
            tabIndex={selected ? 0 : -1}
            onClick={() => onChange(t.tab)}
            className={rsButtonClass({ variant: selected ? "primary" : "secondary", size: "sm" })}
          >
            {t.label}
          </button>
        );
      })}
    </div>
  );
}

type RowProps = { venue: VenueSummary; selected: boolean; onSelect: () => void; userZone: string; now: Date };

// VenueRow is one venue of the list as the design draws it: the deadline's date tile, the name,
// type and fields, place and dates, the deadline with its countdown, and Details.
function VenueRow({ venue: v, selected, onSelect, userZone, now }: RowProps) {
  const tile = v.submissionDeadline ? tileParts(v.submissionDeadline) : null;
  const where = [locationLabel(v), dateRangeLabel(v.startsOn, v.endsOn)].filter(Boolean).join(" · ");
  const pill = rsButtonClass({ variant: "outline", size: "xs" });
  return (
    <li
      className={`flex flex-wrap items-start gap-3.5 border-b border-rs-divider px-4 py-3.5 last:border-b-0 ${
        selected ? "lg:bg-rs-accent-soft/60" : ""
      } ${v.past ? "opacity-80" : ""}`}
    >
      <div aria-hidden="true" className="w-16 flex-none overflow-hidden rounded-rs-md border border-rs-border bg-rs-surface text-center">
        <div className="bg-rs-accent-strong py-0.5 text-rs-micro font-bold text-rs-surface">{tile ? tile.month : "OPEN"}</div>
        <div className="py-0.5 text-[22px] leading-tight font-bold text-rs-text">{tile ? tile.day : "∞"}</div>
      </div>
      <div className="flex min-w-[220px] flex-1 flex-col gap-1">
        <Link
          href={`/events/${v.id}`}
          className="text-rs-lead font-medium text-rs-text no-underline hover:text-rs-accent-hover hover:underline focus-visible:shadow-focus focus-visible:outline-none"
        >
          {v.name}
          {v.shortName && <span className="font-normal text-rs-text-muted"> · {v.shortName}</span>}
        </Link>
        <div className="flex flex-wrap items-center gap-1.5">
          <TypeBadge type={v.type} />
          <FieldChips fields={v.fields} />
        </div>
        {where && <span className="text-rs-caption text-rs-text-muted">{where}</span>}
        <span className={`text-rs-caption font-medium ${deadlineTone(v, now)}`}>
          {v.submissionDeadline ? (
            <>
              Submission deadline{" "}
              <DeadlineText time={v.submissionDeadline} zone={v.timeZone ?? "UTC"} userZone={userZone} now={now} />
            </>
          ) : (
            "Open submission: no deadline"
          )}
        </span>
        <OfficialLink venue={v} className="self-start text-rs-caption" />
      </div>
      {/* Wide screens show the venue in the side panel; narrower ones open its page. */}
      <button
        type="button"
        aria-controls="venue-preview"
        aria-pressed={selected}
        aria-label={`Details of ${v.name}`}
        onClick={onSelect}
        className={`${pill} max-lg:hidden`}
      >
        Details
      </button>
      <Link href={`/events/${v.id}`} aria-label={`Details of ${v.name}`} className={`${pill} lg:hidden`}>
        Details
      </Link>
    </li>
  );
}
