"use client";

import { type KeyboardEvent, useId, useLayoutEffect, useRef, useState } from "react";

import { dateAndTime } from "@/lib/date-time";

import { bodyLength, counterFrom, maxBody } from "./format";
import type { SendNotice } from "./send-errors";

// maxHeight is five 20px lines with the padding and border.
const maxHeight = 118;

// RestrictedComposer takes the composer's place while the user may not send (FR-15.6).
export function RestrictedComposer({ until }: { until: string }) {
  return (
    <p role="status" className="border-t border-rs-divider px-4 py-2.5 text-rs-body text-rs-text-muted">
      You can&apos;t send messages until {dateAndTime(until)}.
    </p>
  );
}

type ComposerProps = {
  disabled?: boolean;
  notice?: SendNotice | null;
  onSend: (body: string) => void;
  // onInput gets the text after each change and onBlur tells the box lost focus; together they
  // drive the typing indicator.
  onInput?: (body: string) => void;
  onBlur?: () => void;
};

// Composer writes a message: Enter sends, Shift+Enter adds a line, and the box grows to five
// lines. A character being composed by an input method is not a send. The count appears near
// the limit; the server trims, so the count does too.
export function Composer({ disabled = false, notice = null, onSend, onInput, onBlur }: ComposerProps) {
  const [body, setBody] = useState("");
  const box = useRef<HTMLTextAreaElement>(null);
  const inputId = useId();
  const length = bodyLength(body);
  const canSend = !disabled && length > 0 && length <= maxBody;

  useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
    el.style.height = "auto";
    if (el.scrollHeight > 0) el.style.height = `${Math.min(el.scrollHeight + 2, maxHeight)}px`;
    el.style.overflowY = el.scrollHeight + 2 > maxHeight ? "auto" : "hidden";
  }, [body]);

  function send() {
    if (!canSend) return;
    onSend(body.trim());
    setBody("");
    box.current?.focus();
  }

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key !== "Enter" || event.shiftKey || event.nativeEvent.isComposing) return;
    event.preventDefault();
    send();
  }

  return (
    <div className="flex flex-col gap-2 border-t border-rs-divider px-4 py-2.5">
      {notice && (
        <p
          role="alert"
          className={`rounded-rs-control border px-2.5 py-1.5 text-rs-caption ${
            notice.tone === "warning"
              ? "border-rs-warning-line bg-rs-warning-soft text-rs-warning"
              : "border-rs-danger-line bg-rs-danger-soft text-rs-danger-strong"
          }`}
        >
          {notice.text}
        </p>
      )}
      <div className="flex items-end gap-2">
        <div className="min-w-0 flex-1">
          <label htmlFor={inputId} className="sr-only">
            Write a message
          </label>
          <textarea
            ref={box}
            id={inputId}
            rows={2}
            value={body}
            disabled={disabled}
            placeholder="Write a message…"
            onChange={(e) => {
              setBody(e.target.value);
              onInput?.(e.target.value);
            }}
            onBlur={onBlur}
            onKeyDown={onKeyDown}
            className="block w-full resize-none rounded-card border border-rs-control bg-rs-surface px-3 py-2 text-rs-body text-rs-text placeholder:text-rs-text-muted focus-visible:shadow-focus focus-visible:outline-none disabled:cursor-not-allowed disabled:border-rs-control-disabled disabled:bg-rs-subtle"
          />
        </div>
        <button
          type="button"
          onClick={send}
          disabled={!canSend}
          className="h-10 flex-none rounded-rs-pill bg-rs-accent-strong px-[18px] text-rs-body font-medium text-rs-surface transition-colors hover:bg-rs-accent-hover focus-visible:shadow-focus focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60"
        >
          Send
        </button>
      </div>
      {length >= counterFrom && (
        <p className={`text-right text-rs-caption ${length > maxBody ? "text-rs-danger" : "text-rs-text-muted"}`}>
          {length.toLocaleString("en-US")} / {maxBody.toLocaleString("en-US")}
          {length > maxBody && " · Too long to send"}
        </p>
      )}
    </div>
  );
}
