"use client";

import { type KeyboardEvent, type ReactNode, type RefObject, useEffect, useId, useRef, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";

// longPress is how long a finger stays on a message before its menu opens.
const longPress = 500;

type MessageMenuProps = {
  // align puts the "…" and the menu on the bubble's outer side: after one of the other
  // person's messages, before one's own.
  align: "start" | "end";
  // Each action given is one item. onDelete asks first, inside the menu, and answers an error
  // to show there, or null when the message is deleted.
  onEdit?: () => void;
  onDelete?: () => Promise<string | null>;
  onReport?: () => void;
  // triggerRef gets the "…" button, so focus can go back to it after the inline editor closes.
  triggerRef?: RefObject<HTMLButtonElement | null>;
  children: ReactNode;
};

const itemClass =
  "w-full px-4 py-2 text-left text-rs-body hover:bg-rs-subtle focus-visible:bg-rs-subtle focus-visible:outline-none";

// MessageMenu wraps a message with a "…" that shows on hover and on keyboard focus, and offers
// its actions: Edit and Delete on one's own messages, Report on the other person's (FR-16.5,
// FR-16.9). On a touch screen, where nothing hovers, a long press opens it; a scroll cancels the
// press. Arrow keys move between items; Escape and Tab close it.
export function MessageMenu({ align, onEdit, onDelete, onReport, triggerRef, children }: MessageMenuProps) {
  const [open, setOpen] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const root = useRef<HTMLDivElement>(null);
  const ownButton = useRef<HTMLButtonElement>(null);
  const button = triggerRef ?? ownButton;
  const popup = useRef<HTMLDivElement>(null);
  const press = useRef<ReturnType<typeof setTimeout>>(undefined);
  const menuId = useId();
  const confirmId = useId();

  useEffect(() => () => clearTimeout(press.current), []);

  useEffect(() => {
    if (!open) return;
    popup.current?.querySelector<HTMLElement>(confirming ? "[data-cancel]" : "[role=menuitem]")?.focus();
  }, [open, confirming]);

  useEffect(() => {
    if (!open) return;
    function onPointer(event: MouseEvent) {
      if (root.current?.contains(event.target as Node)) return;
      setOpen(false);
      setConfirming(false);
      setError(null);
    }
    document.addEventListener("mousedown", onPointer);
    return () => document.removeEventListener("mousedown", onPointer);
  }, [open]);

  function close(refocus: boolean) {
    setOpen(false);
    setConfirming(false);
    setError(null);
    if (refocus) button.current?.focus();
  }

  function choose(action: () => void) {
    close(false);
    action();
  }

  async function confirmDelete() {
    if (!onDelete) return;
    setDeleting(true);
    setError(null);
    const failed = await onDelete();
    setDeleting(false);
    if (failed) setError(failed);
    else close(false);
  }

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key === "Escape" || event.key === "Tab") {
      if (event.key === "Escape") event.preventDefault();
      close(true);
      return;
    }
    if (confirming || (event.key !== "ArrowDown" && event.key !== "ArrowUp")) return;
    event.preventDefault();
    const items = [...(popup.current?.querySelectorAll<HTMLElement>("[role=menuitem]") ?? [])];
    const at = items.indexOf(document.activeElement as HTMLElement);
    const next = event.key === "ArrowDown" ? (at + 1) % items.length : (at - 1 + items.length) % items.length;
    items[next]?.focus();
  }

  const cancelPress = () => clearTimeout(press.current);
  const end = align === "end";

  return (
    <div
      ref={root}
      className={`group/message relative flex max-w-full items-center gap-1 ${end ? "flex-row-reverse" : ""}`}
      onTouchStart={() => {
        cancelPress();
        press.current = setTimeout(() => setOpen(true), longPress);
      }}
      onTouchEnd={cancelPress}
      onTouchMove={cancelPress}
      onTouchCancel={cancelPress}
    >
      {children}
      <button
        ref={button}
        type="button"
        aria-label="More actions for this message"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={menuId}
        onClick={() => (open ? close(false) : setOpen(true))}
        className={`flex-none rounded-full p-1 text-rs-text-muted hover:bg-rs-subtle hover:text-rs-text focus-visible:opacity-100 focus-visible:shadow-focus focus-visible:outline-none group-hover/message:opacity-100 ${
          open ? "opacity-100" : "opacity-0"
        }`}
      >
        <svg aria-hidden="true" viewBox="0 0 24 24" fill="currentColor" className="size-4">
          <circle cx="5" cy="12" r="1.75" />
          <circle cx="12" cy="12" r="1.75" />
          <circle cx="19" cy="12" r="1.75" />
        </svg>
      </button>
      <div
        ref={popup}
        id={menuId}
        role={confirming ? "dialog" : "menu"}
        aria-label={confirming ? "Delete message" : "Message actions"}
        aria-describedby={confirming ? confirmId : undefined}
        hidden={!open}
        onKeyDown={onKeyDown}
        className={`absolute top-full z-30 mt-1 rounded-rs-md border border-rs-border bg-rs-surface py-1 shadow-menu ${
          end ? "right-0" : "left-0"
        } ${confirming ? "w-64" : "w-44"}`}
      >
        {confirming ? (
          <div className="flex flex-col gap-2 px-4 py-2">
            <p id={confirmId} className="text-rs-body text-rs-text">
              Delete this message for everyone?
            </p>
            {error && (
              <p role="alert" className="text-rs-caption text-rs-danger">
                {error}
              </p>
            )}
            <div className="flex justify-end gap-2">
              <button type="button" data-cancel onClick={() => close(true)} className={rsButtonClass({ variant: "secondary", size: "xs" })}>
                Cancel
              </button>
              <button
                type="button"
                disabled={deleting}
                onClick={() => void confirmDelete()}
                className={rsButtonClass({ variant: "destructive", size: "xs" })}
              >
                {deleting ? "Deleting…" : "Delete"}
              </button>
            </div>
          </div>
        ) : (
          <>
            {onEdit && (
              <button type="button" role="menuitem" onClick={() => choose(onEdit)} className={`${itemClass} text-rs-text`}>
                Edit
              </button>
            )}
            {onDelete && (
              <button type="button" role="menuitem" onClick={() => setConfirming(true)} className={`${itemClass} text-rs-danger`}>
                Delete
              </button>
            )}
            {onReport && (
              <button type="button" role="menuitem" onClick={() => choose(onReport)} className={`${itemClass} text-rs-text`}>
                Report message
              </button>
            )}
          </>
        )}
      </div>
    </div>
  );
}
