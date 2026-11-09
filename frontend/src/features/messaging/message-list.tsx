"use client";

import { type ReactNode, useEffect, useLayoutEffect, useRef, useState } from "react";

import { Avatar } from "@/components/avatar";
import { dateAndTime } from "@/lib/date-time";

import { AttachmentCard } from "./attachment-card";
import { clockTime, editableUntil, groupMessages, type MessageGroup, type SeenBy, type ThreadMessage } from "./format";
import { deletedAccount, eventSentence } from "./system-events";
import { type EditOutcome, MessageEditor } from "./message-editor";
import { MessageMenu } from "./message-menu";
import { MessageText } from "./message-text";

// Near the top a scroll loads earlier messages; near the bottom new ones keep it pinned there.
const edge = 80;

// Sender is how a message names its sender: their name and photo. A person whose profile
// cannot be viewed has no photo; one whose account is gone is undefined.
export type Sender = { fullName: string; photoUrl: string | null };

type MessageListProps = {
  messages: ThreadMessage[];
  me: string;
  // personOf finds a sender, or someone a system message names.
  personOf: (userId: string) => Sender | undefined;
  // group names the sender above the first message of each run (FR-16.6).
  group?: boolean;
  // seenId is the direct conversation's "Seen"; seenBy a group's "Seen by N" (FR-16.3).
  seenId?: string | null;
  seenBy?: SeenBy | null;
  hasEarlier?: boolean;
  loadingEarlier?: boolean;
  earlierFailed?: boolean;
  onLoadEarlier?: () => void;
  // hasNewer is set while the thread was opened at a search result and does not reach the
  // newest message yet: scrolling down loads more, and Jump to latest goes to the end (FR-16.8).
  hasNewer?: boolean;
  loadingNewer?: boolean;
  newerFailed?: boolean;
  onLoadNewer?: () => void;
  onJumpToLatest?: () => void;
  // highlightId is the message the thread was opened at: it is scrolled to and marked briefly.
  highlightId?: string | null;
  onRetry: (key: string) => void;
  // onReportMessage opens the report dialog for one of the other person's messages.
  onReportMessage?: (messageId: string) => void;
  // onEdit saves a new text for one of the caller's messages; onDelete deletes one and answers
  // an error to show, or null (FR-16.5).
  onEdit?: (messageId: string, body: string) => Promise<EditOutcome>;
  onDelete?: (messageId: string) => Promise<string | null>;
};

// MessageList is a thread's messages, newest at the bottom, as design/screens/Messaging.html
// draws them: runs by one sender under date separators. It is a log region, so a screen reader
// hears new messages. Scrolling up loads earlier ones and keeps the reader's place; the button
// does the same for the keyboard and for a thread too short to scroll.
export function MessageList({
  messages,
  me,
  personOf,
  group = false,
  seenId = null,
  seenBy = null,
  hasEarlier = false,
  loadingEarlier = false,
  earlierFailed = false,
  onLoadEarlier,
  hasNewer = false,
  loadingNewer = false,
  newerFailed = false,
  onLoadNewer,
  onJumpToLatest,
  highlightId = null,
  onRetry,
  onReportMessage,
  onEdit,
  onDelete,
}: MessageListProps) {
  const box = useRef<HTMLDivElement>(null);
  // heightBefore is the scroll height when earlier messages were asked for.
  const heightBefore = useRef<number | null>(null);
  // Opened at a search result, the list starts there, not at the bottom.
  const pinned = useRef(highlightId === null);
  const firstKey = messages[0]?.key;
  const last = messages.at(-1);
  const [flash, setFlash] = useState<string | null>(highlightId);

  // The message the thread was opened at is scrolled into the middle and marked for a moment.
  useLayoutEffect(() => {
    if (!highlightId) return;
    const target = box.current?.querySelector<HTMLElement>(`[data-message-id="${highlightId}"]`);
    target?.scrollIntoView?.({ block: "center" });
    pinned.current = false;
  }, [highlightId]);
  useEffect(() => {
    if (!flash) return;
    const timer = setTimeout(() => setFlash(null), highlightFor);
    return () => clearTimeout(timer);
  }, [flash]);

  useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
    if (heightBefore.current !== null) {
      if (!loadingEarlier) {
        el.scrollTop += el.scrollHeight - heightBefore.current;
        heightBefore.current = null;
      }
      return;
    }
    if (pinned.current || (last?.senderId === me && !last.event && !hasNewer)) {
      el.scrollTop = el.scrollHeight;
      pinned.current = true;
    }
  }, [firstKey, last?.key, last?.senderId, last?.event, hasNewer, loadingEarlier, me]);

  function loadEarlier() {
    if (!box.current || !hasEarlier || loadingEarlier) return;
    heightBefore.current = box.current.scrollHeight;
    onLoadEarlier?.();
  }

  function onScroll() {
    const el = box.current;
    if (!el) return;
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < edge;
    // Away from the newest messages the list never sticks to the bottom: more are still to come.
    pinned.current = nearBottom && !hasNewer;
    if (el.scrollTop < edge && !earlierFailed) loadEarlier();
    if (nearBottom && hasNewer && !loadingNewer && !newerFailed) onLoadNewer?.();
  }

  return (
    <div
      ref={box}
      role="log"
      aria-live="polite"
      aria-label="Messages"
      aria-busy={loadingEarlier}
      tabIndex={0}
      onScroll={onScroll}
      className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-4 focus-visible:shadow-focus focus-visible:outline-none"
    >
      {hasEarlier && (
        <div className="flex flex-col items-center gap-1">
          {earlierFailed && (
            <p role="alert" className="text-rs-caption text-rs-danger">
              Couldn&apos;t load earlier messages.
            </p>
          )}
          <button
            type="button"
            onClick={loadEarlier}
            disabled={loadingEarlier}
            className="rounded-rs-pill px-3 py-1 text-rs-caption font-medium text-rs-accent-strong hover:bg-rs-subtle focus-visible:shadow-focus focus-visible:outline-none disabled:opacity-60"
          >
            {loadingEarlier ? "Loading…" : "Load earlier messages"}
          </button>
        </div>
      )}
      {groupMessages(messages).map((day) => (
        <div key={day.key} className="flex flex-col gap-3">
          <p className="self-center text-rs-caption text-rs-text-muted">{day.label}</p>
          {day.groups.map((run) => {
            if (run.system) {
              const m = run.messages[0];
              return (
                <p
                  key={run.key}
                  data-message-id={m.id ?? undefined}
                  className="self-center px-4 text-center text-rs-caption text-rs-text-muted"
                >
                  {m.event && eventSentence(m.event, m.senderId || null, me, personOf)}
                </p>
              );
            }
            return run.senderId === me ? (
              <OwnGroup
                key={run.key}
                group={run}
                seenId={seenId}
                seenBy={seenBy}
                personOf={personOf}
                flash={flash}
                onRetry={onRetry}
                onEdit={onEdit}
                onDelete={onDelete}
              />
            ) : (
              <TheirGroup
                key={run.key}
                group={run}
                sender={personOf(run.senderId)}
                showName={group}
                flash={flash}
                onReport={onReportMessage}
              />
            );
          })}
        </div>
      ))}
      {hasNewer && (
        <div className="flex flex-col items-center gap-1">
          {newerFailed && (
            <p role="alert" className="text-rs-caption text-rs-danger">
              Couldn&apos;t load newer messages.
            </p>
          )}
          <div className="flex gap-2">
            <button
              type="button"
              onClick={onLoadNewer}
              disabled={loadingNewer}
              className="rounded-rs-pill px-3 py-1 text-rs-caption font-medium text-rs-accent-strong hover:bg-rs-subtle focus-visible:shadow-focus focus-visible:outline-none disabled:opacity-60"
            >
              {loadingNewer ? "Loading…" : "Load newer messages"}
            </button>
            {onJumpToLatest && (
              <button
                type="button"
                onClick={onJumpToLatest}
                className="rounded-rs-pill px-3 py-1 text-rs-caption font-medium text-rs-accent-strong hover:bg-rs-subtle focus-visible:shadow-focus focus-visible:outline-none"
              >
                Jump to latest
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

// highlightFor is how long the message a search result opened stays marked.
export const highlightFor = 2_000;

// Marked is one message's place in the list: what a search result scrolls to (data-message-id)
// and marks for a moment.
function Marked({ id, flash, align, children }: { id: string | null; flash: string | null; align: "start" | "end"; children: ReactNode }) {
  const on = id !== null && id === flash;
  return (
    <div
      data-message-id={id ?? undefined}
      data-highlighted={on || undefined}
      className={`flex w-full max-w-full flex-col rounded-[16px] transition-shadow duration-500 ${align === "end" ? "items-end" : "items-start"} ${
        on ? "shadow-[0_0_0_3px_var(--color-rs-accent-strong)]" : ""
      }`}
    >
      {children}
    </div>
  );
}

// Tombstone is a deleted message in its place (FR-16.5). It is known from the message's deleted
// flag, never from an empty body, as a message with files may have no text.
function Tombstone() {
  return (
    <div className="rounded-[14px] border border-rs-border bg-rs-surface px-3 py-2 text-rs-body text-rs-text-muted italic">
      This message was deleted
    </div>
  );
}

// Edited says a message's text was changed, with when in a tooltip (FR-16.5).
function Edited({ at }: { at: string }) {
  return (
    <time dateTime={at} title={`Edited ${dateAndTime(at)}`}>
      Edited
    </time>
  );
}

function Files({ message, align }: { message: ThreadMessage; align: "start" | "end" }) {
  if (message.attachments.length === 0) return null;
  return (
    <ul aria-label="Files" className={`flex max-w-full flex-col gap-1 ${align === "end" ? "items-end" : "items-start"}`}>
      {message.attachments.map((a) => (
        <li key={a.id} className="max-w-full">
          <AttachmentCard attachment={a} />
        </li>
      ))}
    </ul>
  );
}

type TheirGroupProps = {
  group: MessageGroup;
  sender: Sender | undefined;
  showName: boolean;
  flash: string | null;
  onReport?: (messageId: string) => void;
};

function TheirGroup({ group, sender, showName, flash, onReport }: TheirGroupProps) {
  const lastKey = group.messages.at(-1)?.key;
  const name = sender?.fullName ?? deletedAccount;
  return (
    <div className="flex max-w-[80%] gap-2 self-start">
      <Avatar name={name} src={sender?.photoUrl ?? null} size="bubble" decorative />
      <div className="flex min-w-0 flex-col gap-1">
        {showName && <p className="text-rs-caption font-medium text-rs-text-muted">{name}</p>}
        {group.messages.map((m) => {
          const last = m.key === lastKey;
          const edited = !m.deleted && m.editedAt;
          const meta =
            last || edited ? (
              <p className="mt-0.5 text-rs-tiny text-rs-text-muted">
                {last && clockTime(m.createdAt)}
                {last && edited && " · "}
                {edited && <Edited at={edited} />}
              </p>
            ) : null;
          if (m.deleted) {
            return (
              <Marked key={m.key} id={m.id} flash={flash} align="start">
                <Tombstone />
                {meta}
              </Marked>
            );
          }
          const content = (
            <div className="flex min-w-0 flex-col items-start gap-1">
              {m.body ? (
                <div className="min-w-0 rounded-[14px] rounded-tl-rs-xs bg-rs-subtle px-3 py-2 text-rs-body text-rs-text">
                  <MessageText body={m.body} />
                  {meta}
                </div>
              ) : null}
              <Files message={m} align="start" />
              {!m.body && meta}
            </div>
          );
          return (
            <Marked key={m.key} id={m.id} flash={flash} align="start">
              {m.id && onReport ? (
                <MessageMenu align="start" onReport={() => onReport(m.id as string)}>
                  {content}
                </MessageMenu>
              ) : (
                content
              )}
            </Marked>
          );
        })}
      </div>
    </div>
  );
}

type OwnGroupProps = {
  group: MessageGroup;
  seenId: string | null;
  seenBy: SeenBy | null;
  personOf: MessageListProps["personOf"];
  flash: string | null;
  onRetry: (key: string) => void;
  onEdit?: MessageListProps["onEdit"];
  onDelete?: MessageListProps["onDelete"];
};

function OwnGroup({ group, seenId, seenBy, personOf, flash, onRetry, onEdit, onDelete }: OwnGroupProps) {
  const lastKey = group.messages.at(-1)?.key;
  return (
    <div className="flex max-w-[80%] flex-col items-end gap-1 self-end">
      {group.messages.map((m) => (
        <Marked key={m.key} id={m.id} flash={flash} align="end">
          <OwnMessage
            message={m}
            last={m.key === lastKey}
            seen={m.id !== null && m.id === seenId}
            seenBy={m.id !== null && seenBy?.messageId === m.id ? seenBy.userIds.map((id) => personOf(id)?.fullName ?? deletedAccount) : null}
            onRetry={onRetry}
            onEdit={onEdit}
            onDelete={onDelete}
          />
        </Marked>
      ))}
    </div>
  );
}

// useEditable says whether a message is still inside the edit window, and changes when the
// window closes, so the Edit item goes away by itself.
function useEditable(createdAt: string, enabled: boolean): boolean {
  const until = editableUntil(createdAt);
  // now is when the message was first shown, moved on when its window closes. A message on its
  // way takes the server's createdAt once stored, which changes until.
  const [now, setNow] = useState(() => Date.now());
  const open = now < until;
  useEffect(() => {
    if (!open) return;
    const timer = setTimeout(() => setNow(Date.now()), Math.min(Math.max(0, until - Date.now()) + 1, 2 ** 31 - 1));
    return () => clearTimeout(timer);
  }, [open, now, until]);
  return enabled && open;
}

type OwnMessageProps = {
  message: ThreadMessage;
  last: boolean;
  seen: boolean;
  // seenBy names who read it, in a group (FR-16.3).
  seenBy: string[] | null;
  onRetry: (key: string) => void;
  onEdit?: MessageListProps["onEdit"];
  onDelete?: MessageListProps["onDelete"];
};

function OwnMessage({ message: m, last, seen, seenBy, onRetry, onEdit, onDelete }: OwnMessageProps) {
  const [editingState, setEditing] = useState(false);
  // A deletion from another tab ends an edit.
  const editing = editingState && !m.deleted;
  const [editClosed, setEditClosed] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const refocus = useRef(false);
  const id = m.id;
  const stored = id !== null && m.delivery === "sent" && !m.deleted;
  const editable = useEditable(m.createdAt, stored && m.body !== "" && !!onEdit && !editClosed);

  useEffect(() => {
    if (editing || !refocus.current) return;
    refocus.current = false;
    trigger.current?.focus();
  }, [editing]);

  function done(closed: boolean) {
    refocus.current = true;
    setEditing(false);
    if (closed) setEditClosed(true);
  }

  let content;
  if (m.deleted) {
    content = <Tombstone />;
  } else {
    content = (
      <div className="flex max-w-full min-w-0 flex-col items-end gap-1">
        {editing && id && onEdit ? (
          <MessageEditor body={m.body} onSave={(body) => onEdit(id, body)} onDone={done} />
        ) : (
          m.body && (
            <div
              className={`rounded-[14px] rounded-tr-rs-xs bg-rs-accent-strong px-3 py-2 text-rs-body text-rs-surface ${
                m.delivery === "sent" ? "" : "opacity-80"
              }`}
            >
              <MessageText body={m.body} />
            </div>
          )
        )}
        <Files message={m} align="end" />
      </div>
    );
  }

  return (
    <div className={`flex max-w-full flex-col items-end gap-0.5 ${editing ? "w-full" : ""}`}>
      {stored && id && onDelete && !editing ? (
        <MessageMenu
          align="end"
          triggerRef={trigger}
          onEdit={editable ? () => setEditing(true) : undefined}
          onDelete={() => onDelete(id)}
        >
          {content}
        </MessageMenu>
      ) : (
        content
      )}
      {editClosed && (
        <p role="alert" className="text-rs-tiny text-rs-danger">
          This message can no longer be edited.
        </p>
      )}
      <OwnStatus message={m} last={last} seen={seen} seenBy={seenBy} onRetry={onRetry} />
    </div>
  );
}

type OwnStatusProps = {
  message: ThreadMessage;
  last: boolean;
  seen: boolean;
  seenBy: string[] | null;
  onRetry: OwnGroupProps["onRetry"];
};

function OwnStatus({ message, last, seen, seenBy, onRetry }: OwnStatusProps) {
  if (message.delivery === "sending") return <span className="text-rs-tiny text-rs-text-muted">Sending…</span>;
  if (message.delivery === "failed") {
    return (
      <span className="text-rs-tiny text-rs-danger">
        Not sent.{" "}
        <button
          type="button"
          onClick={() => onRetry(message.key)}
          className="rounded-rs-2xs font-medium underline focus-visible:shadow-focus focus-visible:outline-none"
        >
          Retry
        </button>
      </span>
    );
  }
  const edited = !message.deleted && message.editedAt;
  if (!last && !seen && !seenBy && !edited) return null;
  return (
    <span className="flex flex-col items-end text-rs-tiny text-rs-text-muted">
      <span>
        {clockTime(message.createdAt)}
        {edited && (
          <>
            {" · "}
            <Edited at={edited} />
          </>
        )}
        {seen && (
          <>
            {" · "}
            <strong className="text-rs-heading">Seen</strong>
          </>
        )}
        {seenBy && (
          <>
            {" · "}
            <SeenByButton names={seenBy} />
          </>
        )}
      </span>
    </span>
  );
}

// SeenByButton is "Seen by N" in a group (FR-16.3). Hovering shows the names as a tooltip; a
// click or a tap lists them.
function SeenByButton({ names }: { names: string[] }) {
  const [open, setOpen] = useState(false);
  return (
    <span className="relative inline-flex flex-col items-end">
      <button
        type="button"
        aria-expanded={open}
        title={names.join(", ")}
        onClick={() => setOpen((o) => !o)}
        className="rounded-rs-2xs font-bold text-rs-heading hover:underline focus-visible:shadow-focus focus-visible:outline-none"
      >
        Seen by {names.length}
      </button>
      {open && (
        <ul
          aria-label="Seen by"
          className="absolute top-full right-0 z-10 mt-1 max-h-48 min-w-40 overflow-y-auto rounded-card border border-rs-border bg-rs-surface p-2 text-left text-rs-caption text-rs-text shadow-card"
        >
          {names.map((name, i) => (
            <li key={`${name}-${i}`} className="truncate py-0.5">
              {name}
            </li>
          ))}
        </ul>
      )}
    </span>
  );
}
