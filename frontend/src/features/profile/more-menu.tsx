"use client";

import { type KeyboardEvent, useEffect, useId, useRef, useState } from "react";

import { Toast, toastDuration } from "./toast";

// profileUrl is the link to a researcher's profile that anyone signed in can open.
export function profileUrl(userId: string): string {
  return `${window.location.origin}/profile/${userId}`;
}

// MoreMenu is the "…" button on the profile header and its menu: for now only "Copy
// profile link", which copies /profile/<userId> and confirms with a toast. Escape or a
// click outside closes it, and the focus goes back to the button.
export function MoreMenu({ userId }: { userId: string }) {
  const [open, setOpen] = useState(false);
  const [toast, setToast] = useState<string | null>(null);
  const root = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const firstItem = useRef<HTMLButtonElement>(null);
  const toastTimer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const menuId = useId();
  useEffect(() => () => clearTimeout(toastTimer.current), []);

  useEffect(() => {
    if (!open) return;
    firstItem.current?.focus();
    function onPointer(event: MouseEvent) {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    }
    document.addEventListener("mousedown", onPointer);
    return () => document.removeEventListener("mousedown", onPointer);
  }, [open]);

  function close() {
    setOpen(false);
    button.current?.focus();
  }

  function onButtonKey(event: KeyboardEvent) {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setOpen(true);
    }
  }

  function onMenuKey(event: KeyboardEvent) {
    if (event.key === "Escape" || event.key === "Tab") {
      if (event.key === "Escape") event.preventDefault();
      close();
    }
  }

  async function copyLink() {
    close();
    let message = "Link copied";
    try {
      await navigator.clipboard.writeText(profileUrl(userId));
    } catch {
      message = "Couldn't copy the link";
    }
    setToast(message);
    clearTimeout(toastTimer.current);
    toastTimer.current = setTimeout(() => setToast(null), toastDuration);
  }

  return (
    <div ref={root} className="relative">
      <button
        ref={button}
        type="button"
        aria-label="More"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={menuId}
        onClick={() => setOpen(!open)}
        onKeyDown={onButtonKey}
        className="shrink-0 rounded-full p-2 text-text-muted transition-colors hover:bg-background hover:text-text focus-visible:ring-2 focus-visible:ring-primary focus-visible:outline-none"
      >
        <DotsIcon />
      </button>
      <div
        id={menuId}
        role="menu"
        aria-label="More"
        hidden={!open}
        onKeyDown={onMenuKey}
        className="absolute top-full right-0 z-30 mt-1 w-56 rounded-lg border border-border bg-surface py-1 shadow-sm"
      >
        <button
          ref={firstItem}
          type="button"
          role="menuitem"
          onClick={copyLink}
          className="flex w-full items-center gap-3 px-4 py-2 text-left text-sm text-text hover:bg-background focus-visible:bg-background focus-visible:outline-none"
        >
          <LinkIcon />
          Copy profile link
        </button>
      </div>
      {toast && <Toast message={toast} />}
    </div>
  );
}

function DotsIcon() {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24" fill="currentColor" className="size-6">
      <circle cx="5" cy="12" r="1.75" />
      <circle cx="12" cy="12" r="1.75" />
      <circle cx="19" cy="12" r="1.75" />
    </svg>
  );
}

function LinkIcon() {
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      className="size-5 shrink-0 text-text-muted"
    >
      <path d="M10 14a4 4 0 0 0 5.66 0l3-3a4 4 0 0 0-5.66-5.66l-1 1" />
      <path d="M14 10a4 4 0 0 0-5.66 0l-3 3a4 4 0 0 0 5.66 5.66l1-1" />
    </svg>
  );
}
