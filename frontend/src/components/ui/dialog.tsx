"use client";

import { type ReactNode, useEffect, useId, useRef, useState } from "react";

import { buttonClass } from "./button";

type DialogProps = {
  title: string;
  // dirty means the dialog holds changes that closing would lose; closing then asks first.
  dirty?: boolean;
  onClose: () => void;
  // returnFocusId is the element that gets the focus back when the dialog goes away,
  // usually the button that opened it.
  returnFocusId?: string;
  children: ReactNode;
};

// Dialog is a modal on the native <dialog> element. It is open while it is mounted: the
// parent shows it by rendering it and hides it by not rendering it. Escape, the close
// button and a click on the backdrop all ask to close; with unsaved changes that first
// asks "Discard changes?". Focus moves to the first field on open and to returnFocusId on close.
export function Dialog({ title, dirty = false, onClose, returnFocusId, children }: DialogProps) {
  const ref = useRef<HTMLDialogElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const keepRef = useRef<HTMLButtonElement>(null);
  const titleId = useId();
  const [confirming, setConfirming] = useState(false);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (!el.open) el.showModal();
    bodyRef.current?.querySelector<HTMLElement>("input:not([disabled]), textarea, select, button")?.focus();
    return () => {
      if (el.open) el.close();
      if (returnFocusId) document.getElementById(returnFocusId)?.focus();
    };
  }, [returnFocusId]);

  useEffect(() => {
    if (confirming) keepRef.current?.focus();
  }, [confirming]);

  function requestClose() {
    if (confirming) setConfirming(false);
    else if (dirty) setConfirming(true);
    else onClose();
  }

  return (
    <dialog
      ref={ref}
      aria-labelledby={titleId}
      onCancel={(e) => {
        e.preventDefault();
        requestClose();
      }}
      onClick={(e) => {
        if (e.target === ref.current) requestClose();
      }}
      className="m-auto max-h-[calc(100dvh-2rem)] w-[calc(100%-2rem)] max-w-xl overflow-y-auto rounded-lg border border-border bg-surface p-0 text-text shadow-sm backdrop:bg-text/40"
    >
      <div className="flex items-center justify-between gap-4 border-b border-border px-4 py-3 md:px-6">
        <h2 id={titleId} className="text-lg font-semibold">
          {title}
        </h2>
        <button
          type="button"
          aria-label="Close"
          onClick={requestClose}
          className="rounded-full p-1.5 text-text-muted hover:bg-background hover:text-text focus-visible:ring-2 focus-visible:ring-primary focus-visible:outline-none"
        >
          <svg
            aria-hidden="true"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth={1.75}
            strokeLinecap="round"
            className="size-6"
          >
            <path d="M6 6l12 12M18 6 6 18" />
          </svg>
        </button>
      </div>
      {confirming && (
        <div role="alertdialog" aria-labelledby={`${titleId}-discard`} className="space-y-4 px-4 py-6 md:px-6">
          <p id={`${titleId}-discard`} className="font-semibold">
            Discard changes?
          </p>
          <p className="text-sm text-text-muted">The changes you made in this dialog will be lost.</p>
          <div className="flex flex-wrap justify-end gap-2">
            <button
              ref={keepRef}
              type="button"
              onClick={() => setConfirming(false)}
              className={buttonClass({ variant: "outline", size: "md" })}
            >
              Keep editing
            </button>
            <button type="button" onClick={onClose} className={buttonClass({ variant: "primary", size: "md" })}>
              Discard
            </button>
          </div>
        </div>
      )}
      <div ref={bodyRef} hidden={confirming} className="px-4 py-4 md:px-6 md:py-6">
        {children}
      </div>
    </dialog>
  );
}
