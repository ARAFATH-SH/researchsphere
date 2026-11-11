"use client";

import { usePathname } from "next/navigation";
import { type KeyboardEvent, useEffect, useRef, useState } from "react";

import { useSession } from "@/features/auth/session";

import type { Box } from "./api";
import { ConversationRow } from "./conversation-row";
import { NewGroupDialog } from "./new-group-dialog";
import { MessageSearchView, SearchBox } from "./search/message-search";
import { searchable } from "./search/use-message-search";
import { useUnreadMessages } from "./unread-count";
import { type BoxState, useConversations } from "./use-conversations";

const boxes: Box[] = ["inbox", "requests"];
const emptyText: Record<Box, string> = {
  inbox: "No conversations yet. Open someone's profile to send a message.",
  requests: "No message requests.",
};

// ConversationList is the left column of design/screens/Messaging.html: the heading with New
// group (FR-16.6), the message search box (FR-16.8), and the Inbox and Requests tabs and their
// rows, paged by cursor as the list scrolls. From 2 characters in the box the search results
// take the list's place; clearing it, or Escape, brings the list back.
export function ConversationList() {
  const session = useSession();
  const me = session.status === "signedIn" ? session.user.id : "";
  const list = useConversations();
  const { open } = list;
  const counts = useUnreadMessages();
  const pathname = usePathname() ?? "";
  const [box, setBox] = useState<Box>("inbox");
  const tabRefs = useRef<Partial<Record<Box, HTMLButtonElement | null>>>({});
  const [creating, setCreating] = useState(false);
  const [query, setQuery] = useState("");
  const searching = searchable(query);

  useEffect(() => open(box), [open, box]);

  const label: Record<Box, string> = {
    inbox: counts.total > 0 ? `Inbox · ${counts.total} unread` : "Inbox",
    requests: counts.requests > 0 ? `Requests (${counts.requests})` : "Requests",
  };

  function onTabKey(event: KeyboardEvent) {
    const step = { ArrowRight: 1, ArrowLeft: -1 }[event.key];
    if (step === undefined) return;
    event.preventDefault();
    const next = boxes[(boxes.indexOf(box) + step + boxes.length) % boxes.length];
    setBox(next);
    tabRefs.current[next]?.focus();
  }

  return (
    <>
      <div className="flex flex-col gap-2 border-b border-rs-divider p-3">
        <div className="flex items-center justify-between gap-2">
          <h1 className="text-rs-h3 leading-rs-tight font-bold text-rs-text">Messaging</h1>
          <button
            id="new-group"
            type="button"
            aria-label="New group"
            onClick={() => setCreating(true)}
            className="flex h-8 flex-none items-center gap-1 rounded-rs-pill border border-rs-accent-strong bg-rs-surface px-3 text-rs-caption font-medium text-rs-accent-strong hover:bg-rs-accent-soft focus-visible:shadow-focus focus-visible:outline-none"
          >
            <span aria-hidden="true">+</span> New group
          </button>
        </div>
        <SearchBox value={query} onChange={setQuery} label="Search messages" />
        {!searching && (
        <div role="tablist" aria-label="Conversations" onKeyDown={onTabKey} className="flex flex-wrap items-center gap-1.5">
          {boxes.map((b) => (
            <button
              key={b}
              ref={(el) => {
                tabRefs.current[b] = el;
              }}
              id={`box-${b}-tab`}
              type="button"
              role="tab"
              aria-selected={box === b}
              aria-controls={`box-${b}`}
              tabIndex={box === b ? 0 : -1}
              onClick={() => setBox(b)}
              className={`h-[30px] rounded-rs-pill border px-3 text-rs-caption font-medium focus-visible:shadow-focus focus-visible:outline-none ${
                box === b ? "border-rs-accent-strong bg-rs-accent-strong text-rs-surface" : "border-rs-control bg-rs-surface text-rs-text hover:bg-rs-subtle"
              }`}
            >
              {label[b]}
            </button>
          ))}
        </div>
        )}
      </div>
      {searching ? (
        <MessageSearchView text={query} me={me} />
      ) : (
        <div id={`box-${box}`} role="tabpanel" aria-labelledby={`box-${box}-tab`} className="min-h-0 flex-1 overflow-y-auto">
          <BoxContent
            key={box}
            state={list.boxes[box]}
            empty={emptyText[box]}
            me={me}
            pathname={pathname}
            onRetry={() => list.retry(box)}
            onMore={() => list.loadMore(box)}
          />
        </div>
      )}
      {creating && <NewGroupDialog onClose={() => setCreating(false)} />}
    </>
  );
}

type BoxContentProps = {
  state: BoxState;
  empty: string;
  me: string;
  pathname: string;
  onRetry: () => void;
  onMore: () => void;
};

function BoxContent({ state, empty, me, pathname, onRetry, onMore }: BoxContentProps) {
  if (state.status === "idle" || state.status === "loading") {
    return (
      <p role="status" className="p-4 text-rs-body text-rs-text-muted">
        Loading…
      </p>
    );
  }
  if (state.status === "error") {
    return (
      <p role="alert" className="p-4 text-rs-body text-rs-text-muted">
        Couldn&apos;t load your conversations.{" "}
        <button type="button" onClick={onRetry} className="font-medium text-rs-accent hover:underline">
          Try again
        </button>
      </p>
    );
  }
  if (state.items.length === 0) return <p className="p-4 text-rs-body text-rs-text-muted">{empty}</p>;
  return (
    <>
      <ul>
        {state.items.map((c) => (
          <ConversationRow key={c.id} conversation={c} me={me} current={pathname === `/messaging/${c.id}`} />
        ))}
      </ul>
      {state.moreFailed && (
        <p role="alert" className="px-3 pt-2 text-rs-caption text-rs-danger">
          Couldn&apos;t load more. Try again.
        </p>
      )}
      {state.nextCursor && <MoreButton busy={state.loadingMore} failed={state.moreFailed} onMore={onMore} />}
    </>
  );
}

// MoreButton loads the next page when scrolled into view, and on a click for the keyboard or
// after a failure.
function MoreButton({ busy, failed, onMore }: { busy: boolean; failed: boolean; onMore: () => void }) {
  const button = useRef<HTMLButtonElement>(null);
  const latest = useRef(onMore);
  useEffect(() => {
    latest.current = onMore;
  });

  useEffect(() => {
    const el = button.current;
    if (!el || failed || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(([entry]) => {
      if (entry.isIntersecting) latest.current();
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [failed]);

  return (
    <button
      ref={button}
      type="button"
      onClick={onMore}
      disabled={busy}
      className="min-h-11 w-full text-rs-body font-medium text-rs-accent-strong hover:bg-rs-subtle focus-visible:shadow-focus focus-visible:outline-none disabled:opacity-60"
    >
      {busy ? "Loading…" : "Show more"}
    </button>
  );
}
