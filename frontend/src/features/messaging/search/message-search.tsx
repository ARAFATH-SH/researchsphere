"use client";

import Link from "next/link";
import { type KeyboardEvent, type RefObject, useId, useState } from "react";

import { Avatar } from "@/components/avatar";
import { rsSelectClass } from "@/components/ui/field";

import type { MessageSearchResult } from "../api";
import { rowTime, waitText } from "../format";
import { StackedAvatars } from "../stacked-avatars";
import { Highlighted } from "./highlight";
import { type DatePreset, searchable, type SearchState, useMessageSearch } from "./use-message-search";

type SearchBoxProps = {
  value: string;
  onChange: (value: string) => void;
  // label is the box's accessible name: "Search messages", or "Search in this conversation".
  label: string;
  inputRef?: RefObject<HTMLInputElement | null>;
};

// SearchBox is the design's search field above the list (design/screens/Messaging.html): a
// magnifier and the text. Escape clears it, which returns to the list.
export function SearchBox({ value, onChange, label, inputRef }: SearchBoxProps) {
  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Escape" && value !== "") {
      event.preventDefault();
      event.stopPropagation();
      onChange("");
    }
  }
  return (
    <label className="flex h-9 items-center gap-2 rounded-rs-xs border border-rs-control bg-rs-surface px-2.5 focus-within:shadow-focus">
      <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" className="size-4 flex-none text-rs-text-muted">
        <circle cx="11" cy="11" r="7" />
        <path d="m20 20-3.5-3.5" />
      </svg>
      <input
        ref={inputRef}
        type="search"
        aria-label={label}
        placeholder={label}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={onKeyDown}
        className="h-full min-w-0 flex-1 bg-transparent text-rs-body text-rs-text placeholder:text-rs-text-muted focus:outline-none"
      />
    </label>
  );
}

const presets: { value: DatePreset; label: string }[] = [
  { value: "any", label: "Any time" },
  { value: "week", label: "Past week" },
  { value: "month", label: "Past month" },
  { value: "year", label: "Past year" },
];

// SearchPerson is someone of the conversation, for the "From" filter.
export type SearchPerson = { userId: string; fullName: string };

type MessageSearchViewProps = {
  text: string;
  me: string;
  // conversationId scopes the search to one conversation; people then fill the "From" filter.
  conversationId?: string;
  people?: SearchPerson[];
  // onPick runs when a result is opened, before the link navigates.
  onPick?: () => void;
};

// MessageSearchView is the results view that replaces the list while the box has 2 characters
// or more (FR-16.8): the filters, then the results, paged with "Show more".
export function MessageSearchView({ text, me, conversationId, people, onPick }: MessageSearchViewProps) {
  const [preset, setPreset] = useState<DatePreset>("any");
  const [from, setFrom] = useState("");
  const { state, more } = useMessageSearch(text, { conversationId, from: from || undefined, preset });
  const fromId = useId();
  const whenId = useId();

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-wrap items-end gap-2 border-b border-rs-divider px-3 py-2">
        {conversationId && people && (
          <label htmlFor={fromId} className="flex flex-col gap-0.5 text-rs-caption text-rs-text-muted">
            From
            <select id={fromId} value={from} onChange={(e) => setFrom(e.target.value)} className={`${rsSelectClass} h-9 text-rs-caption`}>
              <option value="">Anyone</option>
              {people.map((p) => (
                <option key={p.userId} value={p.userId}>
                  {p.userId === me ? "You" : p.fullName}
                </option>
              ))}
            </select>
          </label>
        )}
        <label htmlFor={whenId} className="flex flex-col gap-0.5 text-rs-caption text-rs-text-muted">
          When
          <select id={whenId} value={preset} onChange={(e) => setPreset(e.target.value as DatePreset)} className={`${rsSelectClass} h-9 text-rs-caption`}>
            {presets.map((p) => (
              <option key={p.value} value={p.value}>
                {p.label}
              </option>
            ))}
          </select>
        </label>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <Results state={state} text={text} me={me} more={more} onPick={onPick} />
      </div>
    </div>
  );
}

type ResultsProps = { state: SearchState; text: string; me: string; more: () => void; onPick?: () => void };

function Results({ state, text, me, more, onPick }: ResultsProps) {
  if (state.status === "idle") {
    return <p className="p-4 text-rs-body text-rs-text-muted">{searchable(text) ? "" : "Type at least 2 characters to search."}</p>;
  }
  if (state.status === "loading") {
    return (
      <p role="status" className="p-4 text-rs-body text-rs-text-muted">
        Searching…
      </p>
    );
  }
  if (state.status === "limited") {
    return (
      <p role="alert" className="p-4 text-rs-body text-rs-text-muted">
        You&apos;re searching too quickly. Try again {waitText(state.retryAfter)}.
      </p>
    );
  }
  if (state.status === "error") {
    return (
      <p role="alert" className="p-4 text-rs-body text-rs-danger">
        {state.message}
      </p>
    );
  }
  if (state.items.length === 0) {
    return (
      <p role="status" className="p-4 text-rs-body text-rs-text-muted">
        No messages match &ldquo;{text.trim()}&rdquo;.
      </p>
    );
  }
  return (
    <>
      <p role="status" className="sr-only">
        {state.items.length} {state.items.length === 1 ? "result" : "results"}
      </p>
      <ul aria-label="Search results">
        {state.items.map((r) => (
          <ResultRow key={r.messageId} result={r} me={me} photo={state.people[r.sender.userId]?.photoUrl ?? null} onPick={onPick} />
        ))}
      </ul>
      {state.moreFailed && (
        <p role="alert" className="px-3 pt-2 text-rs-caption text-rs-danger">
          Couldn&apos;t load more. Try again.
        </p>
      )}
      {state.nextCursor && (
        <button
          type="button"
          onClick={more}
          disabled={state.loadingMore}
          className="min-h-11 w-full text-rs-body font-medium text-rs-accent-strong hover:bg-rs-subtle focus-visible:shadow-focus focus-visible:outline-none disabled:opacity-60"
        >
          {state.loadingMore ? "Loading…" : "Show more"}
        </button>
      )}
    </>
  );
}

// ResultRow is one result: the conversation's picture and name, who wrote it and when, and the
// snippet with its matches marked. It opens the conversation at the message.
function ResultRow({ result: r, me, photo, onPick }: { result: MessageSearchResult; me: string; photo: string | null; onPick?: () => void }) {
  const c = r.conversation;
  const name = c.name ?? c.otherMember?.fullName ?? "";
  const sender = r.sender.userId === me ? "You" : r.sender.fullName;
  return (
    <li>
      <Link
        href={`/messaging/${r.conversationId}?around=${r.messageId}`}
        onClick={onPick}
        className="flex items-start gap-2.5 border-b border-rs-divider px-3 py-2.5 hover:bg-rs-subtle focus-visible:shadow-focus focus-visible:outline-none"
      >
        {c.kind === "group" ? (
          <StackedAvatars avatars={c.avatars} name={name} size="row" />
        ) : (
          <Avatar name={name} src={c.otherMember?.photoUrl ?? photo} size="post" decorative />
        )}
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="flex justify-between gap-1.5">
            <strong className="truncate text-rs-body text-rs-text">{name}</strong>
            <time dateTime={r.createdAt} className="flex-none text-rs-caption text-rs-text-muted">
              {rowTime(r.createdAt)}
            </time>
          </span>
          <span className="truncate text-rs-caption text-rs-text-muted">{sender}</span>
          <span className="flex min-w-0 items-start gap-1 text-rs-caption text-rs-text">
            {r.matchedOn === "attachment" && (
              <svg role="img" aria-label="In a file name" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" className="mt-0.5 size-3.5 flex-none text-rs-text-muted">
                <path d="m21 11-8.6 8.6a5 5 0 0 1-7-7L14 4a3.3 3.3 0 0 1 4.7 4.7l-8.6 8.6a1.7 1.7 0 0 1-2.4-2.4L16 6.6" />
              </svg>
            )}
            <span className="line-clamp-2 min-w-0 break-words">
              <Highlighted snippet={r.snippet} ranges={r.highlights} />
            </span>
          </span>
        </span>
      </Link>
    </li>
  );
}
