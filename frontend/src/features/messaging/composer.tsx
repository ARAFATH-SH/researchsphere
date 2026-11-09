"use client";

import { type ClipboardEvent, type KeyboardEvent, useId, useLayoutEffect, useRef, useState } from "react";

import { dateAndTime } from "@/lib/date-time";

import type { Attachment } from "./api";
import { AttachmentChip } from "./attachment-card";
import { accept, isImage } from "./attachments";
import { bodyLength, counterFrom, maxBody } from "./format";
import type { SendNotice } from "./send-errors";
import type { Attachments } from "./use-attachments";

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
  onSend: (body: string, attachments: Attachment[]) => void;
  // files are the attachments of the next message (FR-16.7); without them there is no attach
  // button.
  files?: Attachments;
  // onInput gets the text after each change and onBlur tells the box lost focus; together they
  // drive the typing indicator.
  onInput?: (body: string) => void;
  onBlur?: () => void;
};

// Composer writes a message: Enter sends, Shift+Enter adds a line, and the box grows to five
// lines. A character being composed by an input method is not a send. The count appears near
// the limit; the server trims, so the count does too. Files come from the attach button, a
// paste of images or a drop on the thread; with at least one, the text is optional. Send waits
// while a file uploads, and while a file that cannot be sent is listed.
export function Composer({ disabled = false, notice = null, onSend, onInput, onBlur, files }: ComposerProps) {
  const [body, setBody] = useState("");
  const box = useRef<HTMLTextAreaElement>(null);
  const picker = useRef<HTMLInputElement>(null);
  const inputId = useId();
  const statusId = useId();
  const length = bodyLength(body);
  const ready = files?.ready ?? [];
  const waiting = !!files?.uploading;
  const blocked = !!files?.blocked;
  const canSend = !disabled && !waiting && !blocked && length <= maxBody && (length > 0 || ready.length > 0);

  useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
    el.style.height = "auto";
    if (el.scrollHeight > 0) el.style.height = `${Math.min(el.scrollHeight + 2, maxHeight)}px`;
    el.style.overflowY = el.scrollHeight + 2 > maxHeight ? "auto" : "hidden";
  }, [body]);

  function send() {
    if (!canSend) return;
    onSend(body.trim(), ready);
    setBody("");
    files?.clear();
    box.current?.focus();
  }

  // A paste of images attaches them; text pasted with them still goes into the box.
  function onPaste(event: ClipboardEvent<HTMLTextAreaElement>) {
    if (!files) return;
    const images = [...event.clipboardData.files].filter(isImage);
    if (images.length === 0) return;
    if (!event.clipboardData.getData("text/plain")) event.preventDefault();
    files.add(images);
  }

  let status: string | null = null;
  if (waiting) status = "Waiting for files to finish uploading…";
  else if (blocked) status = "Remove the files that can't be sent.";

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
      {files && files.files.length > 0 && (
        <ul aria-label="Files to send" className="flex flex-wrap gap-2">
          {files.files.map((f) => (
            <AttachmentChip key={f.key} file={f} onRemove={() => files.remove(f.key)} />
          ))}
        </ul>
      )}
      {files?.notice && (
        <p role="alert" className="text-rs-caption text-rs-danger">
          {files.notice}
        </p>
      )}
      <div className="flex items-end gap-2">
        {files && (
          <>
            <button
              type="button"
              disabled={disabled}
              onClick={() => picker.current?.click()}
              aria-label="Attach file (PDF, image, office document up to 25 MB)"
              className="flex size-10 flex-none items-center justify-center rounded-full border border-rs-control bg-rs-surface text-rs-text hover:bg-rs-subtle focus-visible:shadow-focus focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60"
            >
              <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" className="size-[18px]">
                <path d="M21 11l-9 9a5 5 0 0 1-7-7l9-9a3.5 3.5 0 0 1 5 5l-9 9a2 2 0 0 1-3-3l8-8" />
              </svg>
            </button>
            <input
              ref={picker}
              type="file"
              multiple
              accept={accept}
              hidden
              data-testid="attach-input"
              onChange={(e) => {
                const picked = [...(e.target.files ?? [])];
                e.target.value = "";
                if (picked.length > 0) files.add(picked);
              }}
            />
          </>
        )}
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
            onPaste={onPaste}
            className="block w-full resize-none rounded-card border border-rs-control bg-rs-surface px-3 py-2 text-rs-body text-rs-text placeholder:text-rs-text-muted focus-visible:shadow-focus focus-visible:outline-none disabled:cursor-not-allowed disabled:border-rs-control-disabled disabled:bg-rs-subtle"
          />
        </div>
        <button
          type="button"
          onClick={send}
          disabled={!canSend}
          aria-describedby={status ? statusId : undefined}
          className="h-10 flex-none rounded-rs-pill bg-rs-accent-strong px-[18px] text-rs-body font-medium text-rs-surface transition-colors hover:bg-rs-accent-hover focus-visible:shadow-focus focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60"
        >
          Send
        </button>
      </div>
      {status && (
        <p id={statusId} role="status" className="text-rs-caption text-rs-text-muted">
          {status}
        </p>
      )}
      {length >= counterFrom && (
        <p className={`text-right text-rs-caption ${length > maxBody ? "text-rs-danger" : "text-rs-text-muted"}`}>
          {length.toLocaleString("en-US")} / {maxBody.toLocaleString("en-US")}
          {length > maxBody && " · Too long to send"}
        </p>
      )}
    </div>
  );
}
