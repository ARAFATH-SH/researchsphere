"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { type KeyboardEvent, useEffect, useId, useRef, useState } from "react";

import { Avatar } from "@/components/avatar";
import { logout } from "@/features/auth/api";
import { setSignedOut, useSession } from "@/features/auth/session";
import { useMyProfile } from "@/features/profile/my-profile";

import { ChevronDownIcon, MeIcon } from "./icons";
import { topLabel } from "./tabs";

// tab is the "Me" tab of the top bar (tabClass styles it, current on /profile); phone is the avatar at the left of the phone top bar.
type MeMenuProps = { placement: "tab" | "phone"; tabClass?: string };

const itemClass =
  "block w-full px-4 py-1.5 text-left text-rs-nav-muted hover:text-rs-accent hover:underline focus-visible:text-rs-accent focus-visible:underline focus-visible:outline-none";
const headingClass = "mt-2 border-t border-rs-nav-border px-4 pt-2 pb-1 font-medium text-rs-nav-text";

const panelPlace = {
  tab: "absolute right-0 top-[52px] w-rs-menu rounded-tr-none max-[640px]:fixed max-[640px]:inset-x-2 max-[640px]:top-auto max-[640px]:bottom-16 max-[640px]:w-auto max-[640px]:rounded-tr-rs-md",
  phone: "fixed inset-x-2 top-14",
};

// MeMenu is the "Me" tab: the user's photo or initials, opening a menu with their name and
// headline, the profile, account links, the admin console for administrators and "Sign out"
// (FR-2.2). Enter, Space or ArrowDown opens it on the first item; the arrow keys, Home and
// End move; Escape closes it and returns to the button; Tab or a click outside closes it.
export function MeMenu({ placement, tabClass = "" }: MeMenuProps) {
  const session = useSession();
  const router = useRouter();
  const [open, setOpen] = useState(false);
  // focusOnOpen is the item to focus once the menu shows, or null to leave the focus.
  const [focusOnOpen, setFocusOnOpen] = useState<"first" | "last" | null>(null);
  const [signingOut, setSigningOut] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const panelId = useId();

  const items = () => [...(panel.current?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? [])];

  // Close on a click outside the menu or on Escape.
  useEffect(() => {
    if (!open) return;
    function onPointer(event: MouseEvent) {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    }
    function onKey(event: globalThis.KeyboardEvent) {
      if (event.key !== "Escape") return;
      setOpen(false);
      button.current?.focus();
    }
    document.addEventListener("mousedown", onPointer);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onPointer);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  useEffect(() => {
    if (!open || !focusOnOpen) return;
    const all = items();
    (focusOnOpen === "first" ? all[0] : all[all.length - 1])?.focus();
  }, [open, focusOnOpen]);

  function show(focus: "first" | "last" | null) {
    setFocusOnOpen(focus);
    setOpen(true);
  }

  function onButtonKey(event: KeyboardEvent) {
    if (["ArrowDown", "Enter", " "].includes(event.key)) {
      event.preventDefault();
      show("first");
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      show("last");
    }
  }

  function onMenuKey(event: KeyboardEvent) {
    const all = items();
    const at = all.indexOf(document.activeElement as HTMLElement);
    const move: Record<string, number> = {
      ArrowDown: (at + 1) % all.length,
      ArrowUp: (at - 1 + all.length) % all.length,
      Home: 0,
      End: all.length - 1,
    };
    if (event.key in move) {
      event.preventDefault();
      all[move[event.key]]?.focus();
    } else if (event.key === "Tab") {
      setOpen(false);
    }
  }

  async function signOut() {
    setSigningOut(true);
    // The server always answers 204; if the request does not arrive, the session check
    // on /login sends a user who is still signed in back to /home.
    await logout();
    setSignedOut();
    router.replace("/login");
  }

  const user = session.status === "signedIn" ? session.user : undefined;
  // GET /auth/me has no photo or headline; they come from the shared own profile.
  const mine = useMyProfile();
  const profile = mine?.status === "ok" ? mine.profile : undefined;
  const photoUrl = profile?.photoUrl ?? null;
  const close = () => setOpen(false);

  const face = user ? (
    <Avatar name={user.fullName} src={photoUrl} size={placement === "tab" ? "sm" : "bar"} decorative />
  ) : (
    <MeIcon />
  );

  return (
    <div ref={root} className={placement === "tab" ? "flex w-full" : "flex flex-none"}>
      <button
        ref={button}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={panelId}
        onClick={() => (open ? close() : show(null))}
        onKeyDown={onButtonKey}
        className={
          placement === "tab"
            ? tabClass
            : "flex size-9 flex-none items-center justify-center rounded-full focus-visible:shadow-focus focus-visible:outline-none"
        }
      >
        {face}
        {placement === "tab" ? (
          <span className={topLabel}>
            Me
            <ChevronDownIcon />
          </span>
        ) : (
          <span className="sr-only">Me</span>
        )}
      </button>
      <div
        ref={panel}
        id={panelId}
        role="menu"
        aria-label="Me menu"
        hidden={!open}
        onKeyDown={onMenuKey}
        className={`z-[60] rounded-rs-md border border-rs-nav-border bg-rs-surface py-3 text-left text-rs-body text-rs-nav-text shadow-menu ${panelPlace[placement]}`}
      >
        {user ? (
          <div className="flex items-center gap-2.5 px-4 pb-2.5">
            <Avatar name={user.fullName} src={photoUrl} size="menu" decorative />
            <span className="min-w-0">
              <strong className="block truncate font-bold">{user.fullName}</strong>
              <span className="block truncate text-rs-caption text-rs-nav-muted">{profile?.headline || user.email}</span>
            </span>
          </div>
        ) : null}
        <div className="px-4">
          <Link
            href="/profile"
            role="menuitem"
            onClick={close}
            className="block rounded-rs-pill border border-rs-accent py-1 text-center font-medium text-rs-accent hover:underline focus-visible:shadow-focus focus-visible:outline-none"
          >
            View profile
          </Link>
        </div>
        <div className={headingClass}>Account</div>
        <Link href="/settings/account" role="menuitem" onClick={close} className={itemClass}>
          Settings &amp; privacy
        </Link>
        <Link href="/events" role="menuitem" onClick={close} className={itemClass}>
          Events &amp; venues
        </Link>
        <Link href="#" role="menuitem" onClick={close} className={itemClass}>
          Help
        </Link>
        <div className={headingClass}>Manage</div>
        {/* Hiding the link is a convenience: the API refuses everyone else (FR-3.2). */}
        {user?.role === "admin" ? (
          <Link href="/admin" role="menuitem" onClick={close} className={itemClass}>
            Admin console
          </Link>
        ) : null}
        <button type="button" role="menuitem" onClick={signOut} disabled={signingOut} className={itemClass}>
          {signingOut ? "Signing out…" : "Sign out"}
        </button>
      </div>
    </div>
  );
}
