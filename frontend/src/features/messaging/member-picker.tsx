"use client";

import { useEffect, useId, useState } from "react";

import { Avatar } from "@/components/avatar";
import { rsInputClass } from "@/components/ui/field";
import { type Connection, listMyConnections, searchMyConnections } from "@/features/social/api";

// Picked is someone chosen for a group: what a chip and a row show.
export type Picked = { userId: string; fullName: string; headline: string | null; photoUrl: string | null };

// pickDelay is how long the picker waits after the last key before it searches.
export const pickDelay = 300;
const minQuery = 2;

type MemberPickerProps = {
  selected: Picked[];
  onChange: (selected: Picked[]) => void;
  // max is how many may be picked: 49 others for a new group, fewer when adding.
  max: number;
  // exclude are people already in the group, left out of the results.
  exclude?: ReadonlySet<string>;
  // ineligible are picked people the server refused (FR-16.6); their chips say so.
  ineligible?: ReadonlySet<string>;
  disabled?: boolean;
};

type Results = { status: "loading" } | { status: "error" } | { status: "ok"; items: Connection[] };

// MemberPicker chooses people for a group from the caller's own connections only (FR-16.6): with
// an empty box it lists the most recent ones, and from 2 characters it searches them by name,
// headline and institution (GET /connections?q). The chosen ones show as removable chips.
export function MemberPicker({ selected, onChange, max, exclude, ineligible, disabled = false }: MemberPickerProps) {
  const inputId = useId();
  const [query, setQuery] = useState("");
  // answer is the last list or search result, with the text it answers: anything else is loading.
  const [answer, setAnswer] = useState<{ text: string; results: Results } | null>(null);
  const text = query.trim();
  const full = selected.length >= max;

  useEffect(() => {
    let current = true;
    const run = () => {
      const res = [...text].length >= minQuery ? searchMyConnections(text) : listMyConnections(1);
      void res.then((page) => {
        if (current) setAnswer({ text, results: page.ok ? { status: "ok", items: page.data.items } : { status: "error" } });
      });
    };
    // The first list needs no wait; typing does.
    const timer = setTimeout(run, text === "" ? 0 : pickDelay);
    return () => {
      current = false;
      clearTimeout(timer);
    };
  }, [text]);

  const results: Results = answer?.text === text ? answer.results : { status: "loading" };

  const chosen = new Set(selected.map((p) => p.userId));

  function toggle(c: Connection) {
    if (chosen.has(c.userId)) {
      onChange(selected.filter((p) => p.userId !== c.userId));
      return;
    }
    if (full) return;
    onChange([...selected, { userId: c.userId, fullName: c.fullName, headline: c.headline, photoUrl: c.photoUrl }]);
  }

  const shown = results.status === "ok" ? results.items.filter((c) => !exclude?.has(c.userId)) : [];

  return (
    <div className="flex flex-col gap-2">
      {selected.length > 0 && (
        <ul aria-label="Chosen people" className="flex flex-wrap gap-1.5">
          {selected.map((p) => {
            const refused = ineligible?.has(p.userId) ?? false;
            return (
              <li
                key={p.userId}
                className={`flex items-center gap-1.5 rounded-rs-pill border py-1 pr-1 pl-2.5 text-rs-caption ${
                  refused ? "border-rs-danger-line bg-rs-danger-soft text-rs-danger-strong" : "border-rs-control bg-rs-subtle text-rs-text"
                }`}
              >
                <span className="max-w-[12rem] truncate">{p.fullName}</span>
                {refused && <span className="font-medium">· Can&apos;t be added</span>}
                <button
                  type="button"
                  aria-label={`Remove ${p.fullName}`}
                  disabled={disabled}
                  onClick={() => onChange(selected.filter((s) => s.userId !== p.userId))}
                  className="flex size-6 items-center justify-center rounded-full hover:bg-rs-surface focus-visible:shadow-focus focus-visible:outline-none"
                >
                  <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" className="size-3.5">
                    <path d="M6 6l12 12M18 6 6 18" />
                  </svg>
                </button>
              </li>
            );
          })}
        </ul>
      )}
      <label htmlFor={inputId} className="text-rs-body font-medium text-rs-text">
        Add people
      </label>
      <input
        id={inputId}
        type="search"
        value={query}
        disabled={disabled}
        onChange={(e) => setQuery(e.target.value)}
        placeholder="Search your connections"
        aria-describedby={`${inputId}-count`}
        className={rsInputClass}
      />
      <p id={`${inputId}-count`} aria-live="polite" className="text-rs-caption text-rs-text-muted">
        {full ? `You can add up to ${max} people.` : `${selected.length} chosen · up to ${max}`}
      </p>
      <div className="max-h-64 overflow-y-auto rounded-rs-md border border-rs-border">
        {results.status === "loading" && (
          <p role="status" className="p-3 text-rs-body text-rs-text-muted">
            Loading…
          </p>
        )}
        {results.status === "error" && (
          <p role="alert" className="p-3 text-rs-body text-rs-danger">
            Couldn&apos;t load your connections.
          </p>
        )}
        {results.status === "ok" && shown.length === 0 && (
          <p className="p-3 text-rs-body text-rs-text-muted">
            {query.trim() ? "None of your connections match." : "You have no connections to add yet."}
          </p>
        )}
        {shown.length > 0 && (
          <ul aria-label="Your connections">
            {shown.map((c) => {
              const on = chosen.has(c.userId);
              return (
                <li key={c.userId} className="border-b border-rs-divider last:border-b-0">
                  <button
                    type="button"
                    aria-pressed={on}
                    disabled={disabled || (!on && full)}
                    onClick={() => toggle(c)}
                    className="flex w-full items-center gap-2.5 px-3 py-2 text-left hover:bg-rs-subtle focus-visible:shadow-focus focus-visible:outline-none disabled:opacity-60"
                  >
                    <Avatar name={c.fullName} src={c.photoUrl} size="bubble" decorative />
                    <span className="flex min-w-0 flex-1 flex-col">
                      <span className="truncate text-rs-body font-medium text-rs-text">{c.fullName}</span>
                      {c.headline && <span className="truncate text-rs-caption text-rs-text-muted">{c.headline}</span>}
                    </span>
                    <span className={`flex-none text-rs-caption font-medium ${on ? "text-rs-accent-strong" : "text-rs-text-muted"}`}>
                      {on ? "Added" : "Add"}
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </div>
  );
}
