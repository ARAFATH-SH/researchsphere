"use client";

import { type KeyboardEvent, useEffect, useId, useRef, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";

import { type Publication, removePublication } from "./publications-api";

// publicationMenuId is the id of a publication's "…" button, where the focus returns when
// its edit dialog closes.
export function publicationMenuId(publicationId: string): string {
  return `publication-${publicationId}-menu`;
}

const itemClass =
  "flex w-full items-center px-4 py-2 text-left text-rs-body text-rs-text hover:bg-rs-subtle focus-visible:bg-rs-subtle focus-visible:outline-none";

// PublicationMenu is the "…" on one of the owner's publications. "Edit" is offered only for
// a record added by hand; the API still refuses one that is shared, and the dialog says so.
// "Remove" asks for confirmation inside the menu, then removes the publication from the
// profile (FR-5.7). Escape or a click outside closes the menu.
export function PublicationMenu({
  publication: p,
  onEdit,
  onRemoved,
}: {
  publication: Publication;
  onEdit: () => void;
  onRemoved: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [removing, setRemoving] = useState(false);
  const [error, setError] = useState<string>();
  const root = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const menuId = useId();

  useEffect(() => {
    if (!open) return;
    panel.current?.querySelector<HTMLElement>("button")?.focus();
  }, [open, confirming]);

  useEffect(() => {
    if (!open) return;
    function onPointer(event: MouseEvent) {
      if (root.current?.contains(event.target as Node)) return;
      setOpen(false);
      setConfirming(false);
      setError(undefined);
    }
    document.addEventListener("mousedown", onPointer);
    return () => document.removeEventListener("mousedown", onPointer);
  }, [open]);

  function dismiss() {
    setOpen(false);
    setConfirming(false);
    setError(undefined);
  }

  function close() {
    dismiss();
    button.current?.focus();
  }

  function onKey(event: KeyboardEvent) {
    if (event.key === "Escape") {
      event.preventDefault();
      close();
    }
  }

  async function remove() {
    setError(undefined);
    setRemoving(true);
    const res = await removePublication(p.id);
    setRemoving(false);
    // not_found means it is gone already, which is what the user asked for.
    if (res.ok || res.code === "not_found") {
      dismiss();
      onRemoved();
      return;
    }
    setError("Couldn't remove the publication. Please try again.");
  }

  return (
    <div ref={root} className="relative shrink-0">
      <button
        ref={button}
        id={publicationMenuId(p.id)}
        type="button"
        aria-label={`More actions for ${p.title}`}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={menuId}
        onClick={() => (open ? close() : setOpen(true))}
        className="rounded-full p-2 text-rs-text-muted transition-colors hover:bg-rs-subtle hover:text-rs-text focus-visible:shadow-focus focus-visible:outline-none"
      >
        <svg aria-hidden="true" viewBox="0 0 24 24" fill="currentColor" className="size-6">
          <circle cx="5" cy="12" r="1.75" />
          <circle cx="12" cy="12" r="1.75" />
          <circle cx="19" cy="12" r="1.75" />
        </svg>
      </button>
      <div
        ref={panel}
        id={menuId}
        role={confirming ? "group" : "menu"}
        aria-label={confirming ? "Remove publication" : `Actions for ${p.title}`}
        hidden={!open}
        onKeyDown={onKey}
        className="absolute top-full right-0 z-30 mt-1 w-64 rounded-rs-md border border-rs-border bg-rs-surface py-1 shadow-menu"
      >
        {confirming ? (
          <div className="flex flex-col gap-3 px-4 py-2">
            <p className="text-rs-body text-rs-text">
              Remove this publication from your profile?
            </p>
            {error && (
              <p role="alert" className="text-rs-caption text-rs-danger">
                {error}
              </p>
            )}
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                onClick={remove}
                disabled={removing}
                className={rsButtonClass({ variant: "destructive", size: "sm" })}
              >
                {removing ? "Removing…" : "Remove"}
              </button>
              <button
                type="button"
                onClick={() => {
                  setConfirming(false);
                  setError(undefined);
                }}
                disabled={removing}
                className={rsButtonClass({ variant: "secondary", size: "sm" })}
              >
                Cancel
              </button>
            </div>
          </div>
        ) : (
          <>
            {p.source === "manual" && (
              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  dismiss();
                  onEdit();
                }}
                className={itemClass}
              >
                Edit
              </button>
            )}
            <button
              type="button"
              role="menuitem"
              onClick={() => setConfirming(true)}
              className={`${itemClass} text-rs-danger`}
            >
              Remove
            </button>
          </>
        )}
      </div>
    </div>
  );
}
