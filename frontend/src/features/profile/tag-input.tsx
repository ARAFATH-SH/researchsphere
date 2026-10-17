"use client";

import { type KeyboardEvent, useEffect, useId, useState } from "react";

import { FormError, inputClass } from "@/components/ui/field";

import { addTag, removeTag, suggestTags, type Tag, type TagKind } from "./api";
import { genericError, MAX_INTERESTS, MAX_SKILLS, normalizeTagName, sameTag, validateTagName } from "./validation";

// suggestDelay is how long typing must pause before suggestions are fetched, in milliseconds.
export const suggestDelay = 250;

// The chips look as they do on the profile page (profile-sections.tsx).
const words: Record<TagKind, { label: string; one: string; limit: number; chip: string }> = {
  interests: { label: "Research interests", one: "research interest", limit: MAX_INTERESTS, chip: "bg-primary-soft text-primary" },
  skills: { label: "Skills", one: "skill", limit: MAX_SKILLS, chip: "border border-border bg-background text-text" },
};

const byName = (a: Tag, b: Tag) => a.name.localeCompare(b.name, undefined, { sensitivity: "base" });

type TagEditorProps = {
  kind: TagKind;
  initial: Tag[];
  // onChange is called after each tag the server added or removed.
  onChange: () => void;
  // onDraftChange says whether the input holds text that has not been added yet.
  onDraftChange?: (hasDraft: boolean) => void;
};

// TagEditor edits the user's research interests or skills (FR-4.2): the chips, each with
// a remove button, and a combobox that suggests existing tags while the user types
// (FR-4.3). Every change is sent at once; there is nothing to save.
export function TagEditor({ kind, initial, onChange, onDraftChange }: TagEditorProps) {
  const { label, one, limit, chip } = words[kind];
  const id = useId();
  const listId = `${id}-list`;
  const [tags, setTags] = useState(initial);
  const [query, setQuery] = useState("");
  // found remembers which text the suggestions answer, so a stale reply is never shown.
  const [found, setFound] = useState<{ q: string; items: Tag[] }>({ q: "", items: [] });
  const [listOpen, setListOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);

  const q = normalizeTagName(query);
  const full = tags.length >= limit;
  const suggestions =
    found.q === q && q !== "" ? found.items.filter((s) => !tags.some((t) => sameTag(t.name, s.name))) : [];
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
    if (tags.some((t) => sameTag(t.name, n))) return clear();
    if (full) return;
    const invalid = validateTagName(n);
    if (invalid) return setError(invalid);

    setBusy(true);
    const res = await addTag(kind, n);
    setBusy(false);
    if (res.ok) {
      const tag = res.data;
      setTags((ts) => (ts.some((t) => t.id === tag.id) ? ts : [...ts, tag].sort(byName)));
      clear();
      onChange();
      return;
    }
    setError(res.code === "tag_limit_reached" || res.code === "invalid_tag_name" ? res.message : genericError);
  }

  async function remove(tag: Tag) {
    setError(undefined);
    const res = await removeTag(kind, tag.id);
    // not_found means it is already gone, which is what the user asked for.
    if (res.ok || res.code === "not_found") {
      setTags((ts) => ts.filter((t) => t.id !== tag.id));
      onChange();
    } else setError(genericError);
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
  return (
    <div className="space-y-4">
      {error ? <FormError>{error}</FormError> : null}
      {tags.length > 0 ? (
        <ul aria-label={label} className="flex flex-wrap gap-2">
          {tags.map((t) => (
            <li
              key={t.id}
              className={`inline-flex items-center gap-1 rounded-full py-1 pr-1 pl-3 text-sm font-medium ${chip}`}
            >
              {t.name}
              <button
                type="button"
                aria-label={`Remove ${t.name}`}
                onClick={() => void remove(t)}
                className="rounded-full p-0.5 hover:bg-surface focus-visible:ring-2 focus-visible:ring-primary focus-visible:outline-none"
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
      ) : (
        <p className="text-sm text-text-muted">No {label.toLowerCase()} yet.</p>
      )}
      <div className="relative">
        <label htmlFor={`${id}-input`} className="block text-sm font-semibold">
          Add a {one}
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
          value={query}
          onChange={(e) => type(e.target.value)}
          onKeyDown={onKeyDown}
          onBlur={() => setListOpen(false)}
          className={`${inputClass} disabled:cursor-not-allowed disabled:bg-background`}
        />
        <ul
          id={listId}
          role="listbox"
          aria-label={`Suggested ${label.toLowerCase()}`}
          hidden={!showList}
          className="absolute inset-x-0 top-full z-10 mt-1 max-h-60 overflow-y-auto rounded-lg border border-border bg-surface py-1 shadow-sm"
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
              className={`cursor-pointer px-3 py-2 text-sm text-text ${i === active ? "bg-primary-soft" : "hover:bg-background"}`}
            >
              {s.name}
            </li>
          ))}
        </ul>
        <p id={hintId} className="mt-1 text-sm text-text-muted">
          <span>
            {tags.length} / {limit}
          </span>
          {full
            ? ` · You have reached the limit of ${limit}. Remove one to add another.`
            : " · Press Enter to add."}
        </p>
      </div>
    </div>
  );
}
