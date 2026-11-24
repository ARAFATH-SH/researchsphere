"use client";

import { type ReactNode, useEffect, useId, useRef, useState } from "react";

import { rsButtonClass } from "./button";

type DialogProps = {
  title: string;
  // dirty means the dialog holds changes that closing would lose; closing then asks first.
  dirty?: boolean;
  onClose: () => void;
  // returnFocusId is the element that gets the focus back when the dialog goes away,
  // usually the button that opened it.
  returnFocusId?: string;
  // placement "sheet" anchors the dialog to the bottom of the screen at full width, as the
  // phone filter sheets do; "side" is a full-height panel at the right edge, a bottom sheet
  // under 640 pixels; "center" (the default) is the usual card in the middle.
  placement?: "center" | "sheet" | "side";
  // initialFocusId is the element that gets the focus on open, in place of the first field.
  initialFocusId?: string;
  children: ReactNode;
};

// Dialog is a modal on the native <dialog> element. It is open while it is mounted: the
// parent shows it by rendering it and hides it by not rendering it. Escape, the close
// button and a click on the backdrop all ask to close; with unsaved changes that first
// asks "Discard changes?". Focus moves to the first field on open and to returnFocusId on close.
// The frame is the design's card (design/screens/ProfileEdit.html, UI.6): the header stays put
// and the body scrolls when the dialog is taller than the screen.
const placementClass = {
  center: "m-auto max-h-[calc(100dvh-2rem)] w-[calc(100%-2rem)] max-w-xl rounded-card-lg",
  sheet: "mx-0 mt-auto mb-0 max-h-[85dvh] w-full max-w-none rounded-t-card-lg",
  side: "my-0 mr-0 ml-auto h-dvh max-h-dvh w-full max-w-md rounded-l-card-lg max-[640px]:mx-0 max-[640px]:mt-auto max-[640px]:h-auto max-[640px]:max-h-[85dvh] max-[640px]:max-w-none max-[640px]:rounded-none max-[640px]:rounded-t-card-lg",
};

export function Dialog({
  title,
  dirty = false,
  onClose,
  returnFocusId,
  placement = "center",
  initialFocusId,
  children,
}: DialogProps) {
  const ref = useRef<HTMLDialogElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const keepRef = useRef<HTMLButtonElement>(null);
  const titleId = useId();
  const [confirming, setConfirming] = useState(false);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (!el.open) el.showModal();
    const first = initialFocusId ? document.getElementById(initialFocusId) : null;
    (first ?? bodyRef.current?.querySelector<HTMLElement>("input:not([disabled]), textarea, select, button"))?.focus();
    return () => {
      if (el.open) el.close();
      if (returnFocusId) document.getElementById(returnFocusId)?.focus();
    };
  }, [returnFocusId, initialFocusId]);

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
      className={`${placementClass[placement]} flex-col overflow-hidden border border-rs-border bg-rs-surface p-0 text-rs-body text-rs-text shadow-card backdrop:bg-rs-text/40 open:flex`}
    >
      <div className="flex shrink-0 items-center justify-between gap-4 border-b border-rs-divider py-3 pr-3 pl-5">
        <h2 id={titleId} className="text-rs-lead leading-rs-snug font-medium">
          {title}
        </h2>
        <button
          type="button"
          aria-label="Close"
          onClick={requestClose}
          className="rounded-rs-pill p-1.5 text-rs-text-muted hover:bg-rs-subtle hover:text-rs-text focus-visible:shadow-focus focus-visible:outline-none"
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
        <div role="alertdialog" aria-labelledby={`${titleId}-discard`} className="space-y-3 overflow-y-auto p-5">
          <p id={`${titleId}-discard`} className="text-rs-lead font-medium">
            Discard changes?
          </p>
          <p className="text-rs-text-muted">The changes you made in this dialog will be lost.</p>
          <div className="flex flex-wrap justify-end gap-2 pt-1">
            <button
              ref={keepRef}
              type="button"
              onClick={() => setConfirming(false)}
              className={rsButtonClass({ variant: "secondary", size: "form" })}
            >
              Keep editing
            </button>
            <button type="button" onClick={onClose} className={rsButtonClass({ variant: "danger", size: "form" })}>
              Discard
            </button>
          </div>
        </div>
      )}
      <div ref={bodyRef} hidden={confirming} className="min-h-0 overflow-y-auto p-5">
        {children}
      </div>
    </dialog>
  );
}
