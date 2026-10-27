"use client";

import { type KeyboardEvent, useEffect, useId, useRef, useState } from "react";

import { Toast, toastDuration } from "./toast";

// profileUrl is the link to a researcher's profile that anyone signed in can open.
export function profileUrl(userId: string): string {
  return `${window.location.origin}/profile/${userId}`;
}

// MoreMenu is the "…" button on the profile header and its menu: for now only "Copy
// profile link", which copies /profile/<userId> and confirms with a toast. Escape or a
// click outside closes it, and the focus goes back to the button. The profile card shows it
// as the design's outlined "More" pill; the sticky bar, short of room, as a "…" icon.
export function MoreMenu({
  userId,
  variant = "pill",
}: {
  userId: string;
  variant?: "pill" | "icon";
}) {
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
        className={
          variant === "pill"
            ? "h-10 shrink-0 rounded-rs-pill border border-rs-text-muted px-4 text-rs-body font-medium text-rs-text transition-colors hover:bg-rs-subtle focus-visible:shadow-focus focus-visible:outline-none"
            : "shrink-0 rounded-full p-2 text-rs-text-muted transition-colors hover:bg-rs-subtle hover:text-rs-text focus-visible:shadow-focus focus-visible:outline-none"
        }
      >
        {variant === "pill" ? "More" : <DotsIcon />}
      </button>
      <div
        id={menuId}
        role="menu"
        aria-label="More"
        hidden={!open}
        onKeyDown={onMenuKey}
        className="absolute top-full left-0 z-30 mt-1 w-56 rounded-rs-md border border-rs-border bg-rs-surface py-1 shadow-menu data-[variant=icon]:right-0 data-[variant=icon]:left-auto"
        data-variant={variant}
      >
        <button
          ref={firstItem}
          type="button"
          role="menuitem"
          onClick={copyLink}
          className="flex w-full items-center gap-3 px-4 py-2 text-left text-rs-body text-rs-text hover:bg-rs-subtle focus-visible:bg-rs-subtle focus-visible:outline-none"
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
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      fill="currentColor"
      className="size-6"
    >
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
      className="size-5 shrink-0 text-rs-text-muted"
    >
      <path d="M10 14a4 4 0 0 0 5.66 0l3-3a4 4 0 0 0-5.66-5.66l-1 1" />
      <path d="M14 10a4 4 0 0 0-5.66 0l-3 3a4 4 0 0 0 5.66 5.66l1-1" />
    </svg>
  );
}
