"use client";

import { type KeyboardEvent, useEffect, useId, useState } from "react";

import { FormError, rsInputClass } from "@/components/ui/field";

import { suggestTags, type Tag, type TagKind } from "./api";
import { normalizeTagName, sameTag, validateTagName } from "./validation";

// suggestDelay is how long typing must pause before suggestions are fetched, in milliseconds.
export const suggestDelay = 250;

export type ChipInputProps = {
  // kind picks the suggestions (FR-4.3): research interests or skills.
  kind: TagKind;
  // label names the list of chips ("Skills").
  label: string;
  // inputLabel names the text box ("Add a skill").
  inputLabel: string;
  // hideInputLabel keeps inputLabel for screen readers only and shows it as the placeholder.
  hideInputLabel?: boolean;
  limit: number;
  // chip is the class of one chip.
  chip: string;
  names: string[];
  // onAdd adds a name that is already normalized, valid, new and within the limit. It
  // returns an error message to show, or null when the name was added.
  onAdd: (name: string) => Promise<string | null> | string | null;
  // onRemove removes a name; it returns an error message, or null when it is gone.
  onRemove: (name: string) => Promise<string | null> | string | null;
  // onDraftChange says whether the input holds text that has not been added yet.
  onDraftChange?: (hasDraft: boolean) => void;
  // emptyText is shown without chips; null shows nothing.
  emptyText?: string | null;
  // error is a message from outside, such as the server's answer to a whole form.
  error?: string;
  className?: string;
};

// ChipInput is the chip list with a remove button on each chip and a combobox that
// suggests existing tags while the user types (FR-4.2, FR-4.3). It keeps no list of its
// own: the parent holds the names and decides what adding and removing do, so the profile
// sends each change at once and a project form keeps them until it is saved.
export function ChipInput({
  kind,
  label,
  inputLabel,
  hideInputLabel = false,
  limit,
  chip,
  names,
  onAdd,
  onRemove,
  onDraftChange,
  emptyText = `No ${label.toLowerCase()} yet.`,
  error: outsideError,
  className = "space-y-4",
}: ChipInputProps) {
  const id = useId();
  const listId = `${id}-list`;
  const [query, setQuery] = useState("");
  // found remembers which text the suggestions answer, so a stale reply is never shown.
  const [found, setFound] = useState<{ q: string; items: Tag[] }>({ q: "", items: [] });
  const [listOpen, setListOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);

  const q = normalizeTagName(query);
  const full = names.length >= limit;
  const suggestions =
    found.q === q && q !== "" ? found.items.filter((s) => !names.some((n) => sameTag(n, s.name))) : [];
  const showList = listOpen && suggestions.length > 0;

  useEffect(() => {
    if (q === "") return;
    let current = true;
    const timer = setTimeout(() => {
      void suggestTags(kind, q).then((res) => {
        if (current) setFound({ q, items: res.ok ? res.data : [] });
      });
    }, suggestDelay);
    return () => {
      current = false;
      clearTimeout(timer);
    };
  }, [kind, q]);

  function type(value: string) {
    setQuery(value);
    setListOpen(true);
    setActive(-1);
    setError(undefined);
    onDraftChange?.(value.trim() !== "");
  }

  function clear() {
    setQuery("");
    setListOpen(false);
    setActive(-1);
    onDraftChange?.(false);
  }

  async function add(name: string) {
    const n = normalizeTagName(name);
    if (n === "" || busy) return;
    // The same tag in another letter case is already there: nothing to do (FR-4.2).
    if (names.some((t) => sameTag(t, n))) return clear();
    if (full) return;
    const invalid = validateTagName(n);
    if (invalid) return setError(invalid);

    setBusy(true);
    const failed = await onAdd(n);
    setBusy(false);
    if (failed) setError(failed);
    else clear();
  }

  async function remove(name: string) {
    setError(undefined);
    const failed = await onRemove(name);
    if (failed) setError(failed);
  }

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    switch (e.key) {
      case "ArrowDown":
        e.preventDefault();
        setListOpen(true);
        setActive((i) => Math.min(i + 1, suggestions.length - 1));
        break;
      case "ArrowUp":
        e.preventDefault();
        setActive((i) => Math.max(i - 1, -1));
        break;
      case "Enter":
        e.preventDefault();
        void add(showList && active >= 0 ? suggestions[active].name : query);
        break;
      case "Escape":
        // The first Escape closes the list; the dialog sees only the next one.
        if (showList) {
          e.preventDefault();
          e.stopPropagation();
          setListOpen(false);
          setActive(-1);
        }
        break;
    }
  }

  const hintId = `${id}-hint`;
  const shownError = error ?? outsideError;
  return (
    <div className={className}>
      {shownError ? <FormError look="rs">{shownError}</FormError> : null}
      {names.length > 0 ? (
        <ul aria-label={label} className="flex flex-wrap gap-1.5">
          {names.map((name) => (
            <li
              key={name}
              className={`inline-flex items-center gap-1.5 rounded-rs-pill py-1 pr-1.5 pl-3 text-rs-body ${chip}`}
            >
              {name}
              <button
                type="button"
                aria-label={`Remove ${name}`}
                onClick={() => void remove(name)}
                className="flex size-6 items-center justify-center rounded-rs-pill hover:bg-rs-surface focus-visible:shadow-focus focus-visible:outline-none"
              >
                <svg
                  aria-hidden="true"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth={1.75}
                  strokeLinecap="round"
                  className="size-4"
                >
                  <path d="M6 6l12 12M18 6 6 18" />
                </svg>
              </button>
            </li>
          ))}
        </ul>
      ) : emptyText ? (
        <p className="text-rs-text-muted">{emptyText}</p>
      ) : null}
      <div className="relative">
        <label
          htmlFor={`${id}-input`}
          className={
            hideInputLabel
              ? "sr-only"
              : `mb-1 block text-rs-body font-medium ${full ? "text-rs-text-muted" : "text-rs-text"}`
          }
        >
          {inputLabel}
        </label>
        <input
          id={`${id}-input`}
          role="combobox"
          aria-autocomplete="list"
          aria-expanded={showList}
          aria-controls={listId}
          aria-activedescendant={showList && active >= 0 ? `${listId}-${active}` : undefined}
          aria-describedby={hintId}
          autoComplete="off"
          disabled={full}
          placeholder={hideInputLabel ? inputLabel : undefined}
          value={query}
          onChange={(e) => type(e.target.value)}
          onKeyDown={onKeyDown}
          onBlur={() => setListOpen(false)}
          className={rsInputClass}
        />
        <ul
          id={listId}
          role="listbox"
          aria-label={`Suggested ${label.toLowerCase()}`}
          hidden={!showList}
          className="absolute inset-x-0 top-full z-10 mt-1 max-h-60 overflow-y-auto rounded-rs-md border border-rs-border bg-rs-surface shadow-popover"
        >
          {suggestions.map((s, i) => (
            <li
              key={s.id}
              id={`${listId}-${i}`}
              role="option"
              aria-selected={i === active}
              // Keep the focus in the input while the pointer picks a suggestion.
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => void add(s.name)}
              className={`cursor-pointer px-3 py-2.5 text-rs-body text-rs-text ${i === active ? "bg-rs-accent-soft" : "hover:bg-rs-subtle"}`}
            >
              {s.name}
            </li>
          ))}
        </ul>
        <p id={hintId} className="mt-1.5 text-rs-caption text-rs-text-muted">
          <span>
            {names.length} / {limit}
          </span>
          {full ? ` · You have reached the limit of ${limit}. Remove one to add another.` : " · Press Enter to add."}
        </p>
      </div>
    </div>
  );
}
