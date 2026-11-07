"use client";

import { useLayoutEffect, useRef } from "react";

import { Avatar } from "@/components/avatar";

import type { ConversationMember } from "./api";
import { clockTime, groupMessages, type MessageGroup, type ThreadMessage } from "./format";
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
              <OwnGroup key={group.key} group={group} seenId={seenId} onRetry={onRetry} />
            ) : (
              <TheirGroup key={group.key} group={group} other={other} onReport={onReportMessage} />
            ),
          )}
        </div>
      ))}
    </div>
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
          const bubble = (
            <div className="min-w-0 rounded-[14px] rounded-tl-rs-xs bg-rs-subtle px-3 py-2 text-rs-body text-rs-text">
              <MessageText body={m.body} />
              {m.key === lastKey && <p className="mt-0.5 text-rs-tiny text-rs-text-muted">{clockTime(m.createdAt)}</p>}
            </div>
          );
          return m.id && onReport ? (
            <MessageMenu key={m.key} messageId={m.id} onReport={onReport}>
              {bubble}
            </MessageMenu>
          ) : (
            <div key={m.key}>{bubble}</div>
          );
        })}
      </div>
    </div>
  );
}

type OwnGroupProps = { group: MessageGroup; seenId: string | null; onRetry: (key: string) => void };

function OwnGroup({ group, seenId, onRetry }: OwnGroupProps) {
  const lastKey = group.messages.at(-1)?.key;
  return (
    <div className="flex max-w-[80%] flex-col items-end gap-1 self-end">
      {group.messages.map((m) => (
        <div key={m.key} className="flex max-w-full flex-col items-end gap-0.5">
          <div
            className={`rounded-[14px] rounded-tr-rs-xs bg-rs-accent-strong px-3 py-2 text-rs-body text-rs-surface ${
              m.delivery === "sent" ? "" : "opacity-80"
            }`}
          >
            <MessageText body={m.body} />
          </div>
          <OwnStatus message={m} last={m.key === lastKey} seen={m.id !== null && m.id === seenId} onRetry={onRetry} />
        </div>
      ))}
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
  if (!last && !seen) return null;
  return (
    <span className="text-rs-tiny text-rs-text-muted">
      {clockTime(message.createdAt)}
      {seen && (
        <>
          {" · "}
          <strong className="text-rs-heading">Seen</strong>
        </>
      )}
    </span>
  );
}
