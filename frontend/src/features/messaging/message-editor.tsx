"use client";

import { type KeyboardEvent, useEffect, useId, useLayoutEffect, useRef, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";

import { bodyLength, maxBody } from "./format";

// maxHeight is eight 20px lines with the padding and border.
const maxHeight = 178;

// EditOutcome is what saving an edit came to: saved, refused for good because the edit window
// closed, or an error the editor shows and the writer may try again after.
export type EditOutcome = { ok: true } | { ok: false; closed: true } | { ok: false; closed: false; error: string };

type MessageEditorProps = {
  body: string;
  onSave: (body: string) => Promise<EditOutcome>;
  // onDone closes the editor; closed is true when the edit window had passed.
  onDone: (closed: boolean) => void;
};

// MessageEditor edits a message in place (FR-16.5): Enter saves, Shift+Enter adds a line and
// Escape cancels. Text that is the same after trimming closes it with no request.
export function MessageEditor({ body, onSave, onDone }: MessageEditorProps) {
  const [text, setText] = useState(body);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const box = useRef<HTMLTextAreaElement>(null);
  const errorId = useId();
  const length = bodyLength(text);
  const canSave = !saving && length > 0 && length <= maxBody;

  useEffect(() => {
    const el = box.current;
    el?.focus();
    el?.setSelectionRange(el.value.length, el.value.length);
  }, []);

  useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
    el.style.height = "auto";
    if (el.scrollHeight > 0) el.style.height = `${Math.min(el.scrollHeight + 2, maxHeight)}px`;
  }, [text]);

  async function save() {
    if (!canSave) return;
    const next = text.trim();
    if (next === body.trim()) {
      onDone(false);
      return;
    }
    setSaving(true);
    setError(null);
    const outcome = await onSave(next);
    if (outcome.ok) {
      onDone(false);
      return;
    }
    if (outcome.closed) {
      onDone(true);
      return;
    }
    setSaving(false);
    setError(outcome.error);
  }

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      onDone(false);
      return;
    }
    if (event.key !== "Enter" || event.shiftKey || event.nativeEvent.isComposing) return;
    event.preventDefault();
    void save();
  }

  return (
    <div className="flex w-full min-w-[min(20rem,100%)] flex-col gap-2">
      <textarea
        ref={box}
        aria-label="Edit message"
        aria-describedby={error ? errorId : undefined}
        value={text}
        disabled={saving}
        rows={2}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={onKeyDown}
        className="block w-full resize-none rounded-card border border-rs-control bg-rs-surface px-3 py-2 text-rs-body text-rs-text focus-visible:shadow-focus focus-visible:outline-none disabled:bg-rs-subtle"
      />
      {length > maxBody && (
        <p className="text-right text-rs-caption text-rs-danger">
          {length.toLocaleString("en-US")} / {maxBody.toLocaleString("en-US")} · Too long to save
        </p>
      )}
      {error && (
        <p id={errorId} role="alert" className="text-rs-caption text-rs-danger">
          {error}
        </p>
      )}
      <div className="flex justify-end gap-2">
        <button type="button" onClick={() => onDone(false)} className={rsButtonClass({ variant: "secondary", size: "xs" })}>
          Cancel
        </button>
        <button type="button" onClick={() => void save()} disabled={!canSave} className={rsButtonClass({ variant: "primary", size: "xs" })}>
          {saving ? "Saving…" : "Save"}
        </button>
      </div>
    </div>
  );
}
