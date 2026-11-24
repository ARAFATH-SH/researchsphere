"use client";

import { type KeyboardEvent, useEffect, useId, useRef, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { CheckboxField, rsInputClass, rsSelectClass } from "@/components/ui/field";

import type { ResearchField, VenueFacets, VenueType } from "./api";
import { isVenueType, typeLabels, venueTypes } from "./labels";
import { activeFilterCount, emptyFilters, type EventsState, maxQueryLength, rangeError } from "./list-params";

export type FilterChange = Partial<Omit<EventsState, "tab" | "page">>;

type FiltersProps = {
  state: EventsState;
  facets: VenueFacets | null;
  fields: ResearchField[];
  onChange: (change: FilterChange) => void;
  // layout "bar" is the design's filter card row; "sheet" stacks the controls in the phone sheet.
  layout: "bar" | "sheet";
};

const searchDelay = 300;
const labelClass = "flex flex-col gap-1 text-rs-body font-medium text-rs-text";

// VenueFilters are the list's filters (FR-14.2, FR-14.3): search, type, field (several), the
// deadline range and "Show past deadlines". Each change goes to the URL at once; the search
// waits until typing pauses. Counts are the facets of the current list.
export function VenueFilters({ state, facets, fields, onChange, layout }: FiltersProps) {
  const id = useId();
  const journals = state.tab === "journals";
  const typeCount = (t: VenueType) => facets?.types.find((c) => c.type === t)?.count;
  const error = rangeError(state);
  const bar = layout === "bar";
  const item = bar ? "min-w-0 flex-[1_1_150px]" : "";

  return (
    <div className={bar ? "flex flex-wrap items-end gap-3" : "flex flex-col gap-4"}>
      <SearchInput id={`${id}-q`} value={state.q} onSearch={(q) => onChange({ q })} className={bar ? "min-w-0 flex-[1_1_180px]" : ""} />
      <label className={`${labelClass} ${item}`}>
        Type
        <select
          className={rsSelectClass}
          value={state.types.length === 1 ? state.types[0] : state.types.length > 1 ? "several" : ""}
          onChange={(e) => onChange({ types: isVenueType(e.target.value) ? [e.target.value] : [] })}
        >
          <option value="">All types</option>
          {state.types.length > 1 && (
            <option value="several" disabled>
              {state.types.map((t) => typeLabels[t]).join(", ")}
            </option>
          )}
          {venueTypes.map((t) => (
            <option key={t} value={t}>
              {typeLabels[t]}
              {typeCount(t) !== undefined ? ` (${typeCount(t)})` : ""}
            </option>
          ))}
        </select>
      </label>
      <FieldPicker
        id={`${id}-field`}
        selected={state.fields}
        fields={fields}
        facets={facets}
        inline={!bar}
        className={bar ? "min-w-0 flex-[1_1_180px]" : ""}
        onChange={(next) => onChange({ fields: next })}
      />
      {!journals && (
        <>
          <label className={`${labelClass} ${item}`}>
            Deadline from
            <input
              type="date"
              className={rsInputClass}
              value={state.from}
              max={state.to || undefined}
              onChange={(e) => onChange({ from: e.target.value })}
            />
          </label>
          <div className={`flex flex-col gap-1 ${item}`}>
            <label htmlFor={`${id}-to`} className="text-rs-body font-medium text-rs-text">
              Deadline to
            </label>
            <input
              id={`${id}-to`}
              type="date"
              className={rsInputClass}
              value={state.to}
              min={state.from || undefined}
              aria-invalid={error ? true : undefined}
              aria-describedby={error ? `${id}-to-error` : undefined}
              onChange={(e) => onChange({ to: e.target.value })}
            />
            {error && (
              <p id={`${id}-to-error`} className="text-rs-caption text-rs-danger">
                {error}
              </p>
            )}
          </div>
          <CheckboxField
            label="Show past deadlines"
            checked={state.past}
            onChange={(e) => onChange({ past: e.target.checked })}
            className="min-h-11"
          />
        </>
      )}
      {activeFilterCount(state) > 0 && (
        <button type="button" onClick={() => onChange({ ...emptyFilters })} className={rsButtonClass({ variant: "secondary", size: "md" })}>
          Clear filters
        </button>
      )}
    </div>
  );
}

// SearchInput searches names and short names once typing pauses, or at once on Enter.
function SearchInput({ id, value, onSearch, className }: { id: string; value: string; onSearch: (q: string) => void; className: string }) {
  const [draft, setDraft] = useState(value);
  const [seen, setSeen] = useState(value);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  // A new value from the URL (back, Clear filters) replaces the draft.
  if (value !== seen) {
    setSeen(value);
    setDraft(value);
  }
  useEffect(() => () => {
    if (timer.current) clearTimeout(timer.current);
  }, []);

  function change(next: string) {
    setDraft(next);
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => onSearch(next), searchDelay);
  }
  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key !== "Enter") return;
    e.preventDefault();
    if (timer.current) clearTimeout(timer.current);
    onSearch(draft);
  }
  return (
    <div className={`flex flex-col gap-1 ${className}`.trim()}>
      <label htmlFor={id} className="text-rs-body font-medium text-rs-text">
        Search
      </label>
      <input
        id={id}
        type="search"
        className={rsInputClass}
        placeholder="Name or short name"
        maxLength={maxQueryLength}
        value={draft}
        onChange={(e) => change(e.target.value)}
        onKeyDown={onKeyDown}
      />
    </div>
  );
}

type FieldPickerProps = {
  id: string;
  selected: string[];
  fields: ResearchField[];
  facets: VenueFacets | null;
  inline: boolean;
  className: string;
  onChange: (fields: string[]) => void;
};

// FieldPicker chooses any number of research fields. In the filter bar it is a button that
// opens a list of checkboxes (Escape or a click outside closes it); in the sheet the
// checkboxes show at once.
function FieldPicker({ id, selected, fields, facets, inline, className, onChange }: FieldPickerProps) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const count = (key: string) => facets?.fields.find((f) => f.key === key)?.count;
  const labelOf = (key: string) => fields.find((f) => f.key === key)?.label ?? key;
  const summary =
    selected.length === 0 ? "All fields" : selected.length === 1 ? labelOf(selected[0]) : `${selected.length} fields`;

  useEffect(() => {
    if (!open) return;
    function onPointer(e: MouseEvent) {
      if (!root.current?.contains(e.target as Node)) setOpen(false);
    }
    function onKey(e: globalThis.KeyboardEvent) {
      if (e.key !== "Escape") return;
      setOpen(false);
      button.current?.focus();
    }
    document.addEventListener("mousedown", onPointer);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onPointer);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  function toggle(key: string, on: boolean) {
    const next = on ? [...selected, key] : selected.filter((k) => k !== key);
    // Keep the order of the fixed list.
    onChange(fields.map((f) => f.key).filter((k) => next.includes(k)));
  }

  const list = (
    <ul className="flex flex-col">
      {fields.map((f) => (
        <li key={f.key}>
          <CheckboxField
            label={`${f.label}${count(f.key) !== undefined ? ` (${count(f.key)})` : ""}`}
            checked={selected.includes(f.key)}
            onChange={(e) => toggle(f.key, e.target.checked)}
          />
        </li>
      ))}
    </ul>
  );

  if (inline) {
    return (
      <fieldset className={className}>
        <legend className="mb-1 text-rs-body font-medium text-rs-text">Field</legend>
        {list}
      </fieldset>
    );
  }
  return (
    <div ref={root} className={`relative flex flex-col gap-1 ${className}`.trim()}>
      <span id={`${id}-label`} className="text-rs-body font-medium text-rs-text">
        Field
      </span>
      <button
        ref={button}
        type="button"
        aria-expanded={open}
        aria-controls={`${id}-list`}
        aria-labelledby={`${id}-label ${id}-value`}
        onClick={() => setOpen((o) => !o)}
        className={`${rsSelectClass} flex items-center justify-between gap-2 text-left`}
      >
        <span id={`${id}-value`} className="truncate">
          {summary}
        </span>
        <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} className="size-4 shrink-0">
          <path d="M6 9l6 6 6-6" />
        </svg>
      </button>
      <div
        id={`${id}-list`}
        role="group"
        aria-label="Research fields"
        hidden={!open}
        className="absolute top-full left-0 z-30 mt-1 max-h-80 w-max max-w-[min(360px,calc(100vw-2rem))] overflow-y-auto rounded-card border border-rs-border bg-rs-surface px-3 py-2 shadow-popover"
      >
        {list}
      </div>
    </div>
  );
}
