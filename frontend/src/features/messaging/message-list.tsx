"use client";

import { useEffect, useLayoutEffect, useRef, useState } from "react";

import { Avatar } from "@/components/avatar";
import { dateAndTime } from "@/lib/date-time";

import type { ConversationMember } from "./api";
import { AttachmentCard } from "./attachment-card";
import { clockTime, editableUntil, groupMessages, type MessageGroup, type ThreadMessage } from "./format";
import { type EditOutcome, MessageEditor } from "./message-editor";
import { MessageMenu } from "./message-menu";
import { MessageText } from "./message-text";

// Near the top a scroll loads earlier messages; near the bottom new ones keep it pinned there.
const edge = 80;

type MessageListProps = {
  messages: ThreadMessage[];
  me: string;
  other: Pick<ConversationMember, "fullName" | "photoUrl">;
  seenId?: string | null;
  hasEarlier?: boolean;
  loadingEarlier?: boolean;
  earlierFailed?: boolean;
  onLoadEarlier?: () => void;
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
  other,
  seenId = null,
  hasEarlier = false,
  loadingEarlier = false,
  earlierFailed = false,
  onLoadEarlier,
  onRetry,
  onReportMessage,
  onEdit,
  onDelete,
}: MessageListProps) {
  const box = useRef<HTMLDivElement>(null);
  // heightBefore is the scroll height when earlier messages were asked for.
  const heightBefore = useRef<number | null>(null);
  const pinned = useRef(true);
  const firstKey = messages[0]?.key;
  const last = messages.at(-1);

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
    if (pinned.current || last?.senderId === me) {
      el.scrollTop = el.scrollHeight;
      pinned.current = true;
    }
  }, [firstKey, last?.key, last?.senderId, loadingEarlier, me]);

  function loadEarlier() {
    if (!box.current || !hasEarlier || loadingEarlier) return;
    heightBefore.current = box.current.scrollHeight;
    onLoadEarlier?.();
  }

  function onScroll() {
    const el = box.current;
    if (!el) return;
    pinned.current = el.scrollHeight - el.scrollTop - el.clientHeight < edge;
    if (el.scrollTop < edge && !earlierFailed) loadEarlier();
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
          {day.groups.map((group) =>
            group.senderId === me ? (
              <OwnGroup key={group.key} group={group} seenId={seenId} onRetry={onRetry} onEdit={onEdit} onDelete={onDelete} />
            ) : (
              <TheirGroup key={group.key} group={group} other={other} onReport={onReportMessage} />
            ),
          )}
        </div>
      ))}
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

type TheirGroupProps = { group: MessageGroup; other: MessageListProps["other"]; onReport?: (messageId: string) => void };

function TheirGroup({ group, other, onReport }: TheirGroupProps) {
  const lastKey = group.messages.at(-1)?.key;
  return (
    <div className="flex max-w-[80%] gap-2 self-start">
      <Avatar name={other.fullName} src={other.photoUrl} size="bubble" decorative />
      <div className="flex min-w-0 flex-col gap-1">
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
              <div key={m.key} className="flex flex-col items-start">
                <Tombstone />
                {meta}
              </div>
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
          return m.id && onReport ? (
            <MessageMenu key={m.key} align="start" onReport={() => onReport(m.id as string)}>
              {content}
            </MessageMenu>
          ) : (
            <div key={m.key}>{content}</div>
          );
        })}
      </div>
    </div>
  );
}

type OwnGroupProps = {
  group: MessageGroup;
  seenId: string | null;
  onRetry: (key: string) => void;
  onEdit?: MessageListProps["onEdit"];
  onDelete?: MessageListProps["onDelete"];
};

function OwnGroup({ group, seenId, onRetry, onEdit, onDelete }: OwnGroupProps) {
  const lastKey = group.messages.at(-1)?.key;
  return (
    <div className="flex max-w-[80%] flex-col items-end gap-1 self-end">
      {group.messages.map((m) => (
        <OwnMessage
          key={m.key}
          message={m}
          last={m.key === lastKey}
          seen={m.id !== null && m.id === seenId}
          onRetry={onRetry}
          onEdit={onEdit}
          onDelete={onDelete}
        />
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
  onRetry: (key: string) => void;
  onEdit?: MessageListProps["onEdit"];
  onDelete?: MessageListProps["onDelete"];
};

function OwnMessage({ message: m, last, seen, onRetry, onEdit, onDelete }: OwnMessageProps) {
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
      <OwnStatus message={m} last={last} seen={seen} onRetry={onRetry} />
    </div>
  );
}

function OwnStatus({ message, last, seen, onRetry }: { message: ThreadMessage; last: boolean; seen: boolean; onRetry: OwnGroupProps["onRetry"] }) {
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
  if (!last && !seen && !edited) return null;
  return (
    <span className="text-rs-tiny text-rs-text-muted">
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
    </span>
  );
}
