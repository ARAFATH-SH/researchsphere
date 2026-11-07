"use client";

import { type KeyboardEvent, useEffect, useId, useRef, useState } from "react";

import { removeConnection } from "@/features/social/api";
import { BlockDialog } from "@/features/social/block-dialog";

import { Toast, toastDuration } from "./toast";

// profileUrl is the link to a researcher's profile that anyone signed in can open.
export function profileUrl(userId: string): string {
  return `${window.location.origin}/profile/${userId}`;
}

// Connected is what the menu needs to offer "Remove connection" (FR-7.5): the other
// user's name for the confirmation, and what to do once the connection is gone.
export type Connected = { name: string; onRemoved: () => void };

// Blocking is what the menu needs to offer "Block" or "Unblock" (FR-16.9).
export type Blocking = { name: string; blockedByMe: boolean; onBlocked: () => void; onUnblocked: () => void };

// MoreMenu is the "…" button on the profile header and its menu: "Remove connection" when
// connected (asked again inside the menu, then DELETE /connections/{userId}), "Block" or
// "Unblock" when blocking is given, each asked in a dialog, and "Copy profile link", which copies /profile/<userId> and confirms with a toast. Escape or a
// click outside closes it, and the focus goes back to the button. The profile card shows it
// as the design's outlined "More" pill; the sticky bar, short of room, as a "…" icon.
export function MoreMenu({
  userId,
  variant = "pill",
  connected,
  blocking,
  label = "More",
}: {
  userId: string;
  variant?: "pill" | "icon";
  connected?: Connected;
  blocking?: Blocking;
  // label names the button and the menu, such as "More actions for Ada" in a list row.
  label?: string;
}) {
  const [open, setOpenState] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [removing, setRemoving] = useState(false);
  const [failed, setFailed] = useState(false);
  const [toast, setToast] = useState<string | null>(null);
  const [asking, setAsking] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const toastTimer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const menuId = useId();
  useEffect(() => () => clearTimeout(toastTimer.current), []);

  function setOpen(next: boolean) {
    setOpenState(next);
    if (!next) {
      setConfirming(false);
      setFailed(false);
    }
  }

  useEffect(() => {
    if (!open) return;
    menu.current?.querySelector<HTMLButtonElement>("button")?.focus();
  }, [open, confirming]);

  useEffect(() => {
    if (!open) return;
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

  async function remove() {
    if (!connected) return;
    setRemoving(true);
    setFailed(false);
    const res = await removeConnection(userId);
    setRemoving(false);
    if (!res.ok) {
      setFailed(true);
      return;
    }
    close();
    connected.onRemoved();
  }

  function onButtonKey(event: KeyboardEvent) {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setOpen(true);
    }
  }

  function onMenuKey(event: KeyboardEvent) {
    // Tab moves between the confirmation's two buttons instead of closing the menu.
    if (event.key === "Escape" || (event.key === "Tab" && !confirming)) {
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
      {asking && blocking && (
        <BlockDialog
          action={blocking.blockedByMe ? "unblock" : "block"}
          userId={userId}
          name={blocking.name}
          onClose={() => setAsking(false)}
          onDone={() => {
            setAsking(false);
            if (blocking.blockedByMe) blocking.onUnblocked();
            else blocking.onBlocked();
          }}
        />
      )}
      <button
        ref={button}
        type="button"
        aria-label={label}
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
        ref={menu}
        id={menuId}
        role="menu"
        aria-label={label}
        hidden={!open}
        onKeyDown={onMenuKey}
        className="absolute top-full left-0 z-30 mt-1 w-64 rounded-rs-md border border-rs-border bg-rs-surface py-1 shadow-menu data-[variant=icon]:right-0 data-[variant=icon]:left-auto"
        data-variant={variant}
      >
        {open && connected && confirming ? (
          <RemoveConfirm
            name={connected.name}
            removing={removing}
            failed={failed}
            onRemove={remove}
            onCancel={() => setConfirming(false)}
          />
        ) : (
          <>
            {connected && (
              <button
                type="button"
                role="menuitem"
                onClick={() => setConfirming(true)}
                className={menuItem}
              >
                <RemoveIcon />
                Remove connection
              </button>
            )}
            {blocking && (
              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  close();
                  setAsking(true);
                }}
                className={menuItem}
              >
                <BlockIcon />
                {blocking.blockedByMe ? "Unblock" : "Block"}
              </button>
            )}
            <button
              type="button"
              role="menuitem"
              onClick={copyLink}
              className={menuItem}
            >
              <LinkIcon />
              Copy profile link
            </button>
          </>
        )}
      </div>
      {toast && <Toast message={toast} />}
    </div>
  );
}

const menuItem =
  "flex w-full items-center gap-3 px-4 py-2 text-left text-rs-body text-rs-text hover:bg-rs-subtle focus-visible:bg-rs-subtle focus-visible:outline-none";

// RemoveConfirm asks inside the menu before a connection is removed (never confirm()).
export function RemoveConfirm({
  name,
  removing,
  failed,
  onRemove,
  onCancel,
}: {
  name: string;
  removing: boolean;
  failed: boolean;
  onRemove: () => void;
  onCancel: () => void;
}) {
  return (
    <div className="flex flex-col gap-2 px-4 py-2">
      <p className="text-rs-body text-rs-text">
        Remove {name} from your connections?
      </p>
      {failed && (
        <p role="alert" className="text-rs-caption text-rs-danger">
          Couldn&apos;t remove the connection. Try again.
        </p>
      )}
      <div className="flex gap-2">
        <button
          type="button"
          onClick={onRemove}
          disabled={removing}
          className="h-9 rounded-rs-pill border border-rs-danger bg-rs-danger px-4 text-rs-body font-medium text-rs-surface transition-colors hover:border-rs-danger-strong hover:bg-rs-danger-strong focus-visible:shadow-focus focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60"
        >
          Remove
        </button>
        <button
          type="button"
          onClick={onCancel}
          className="h-9 rounded-rs-pill px-3 text-rs-body font-medium text-rs-text-muted transition-colors hover:bg-rs-subtle hover:text-rs-text focus-visible:shadow-focus focus-visible:outline-none"
        >
          Cancel
        </button>
      </div>
    </div>
  );
}

function RemoveIcon() {
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
      <circle cx="9" cy="8" r="3.5" />
      <path d="M2.5 20c0-3.6 2.9-6 6.5-6s6.5 2.4 6.5 6M16 11h6" />
    </svg>
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

function BlockIcon() {
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.75}
      strokeLinecap="round"
      className="size-5 shrink-0 text-rs-text-muted"
    >
      <circle cx="12" cy="12" r="9" />
      <path d="M5.6 5.6l12.8 12.8" />
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
