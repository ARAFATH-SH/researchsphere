"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useId, useRef, useState } from "react";

import { buttonClass } from "@/components/ui/button";
import { logout } from "@/features/auth/api";
import { setSignedOut, useSession } from "@/features/auth/session";

import { MeIcon } from "./icons";
import type { TabListVariant } from "./tab-list";

// initials returns the first letters of the first and last words of a name: "Ada
// Lovelace" gives "AL", "Ada" gives "A".
export function initials(fullName: string): string {
  const words = fullName.trim().split(/\s+/).filter(Boolean);
  if (words.length === 0) return "";
  const first = (w: string) => [...w][0].toLocaleUpperCase();
  return words.length === 1 ? first(words[0]) : first(words[0]) + first(words[words.length - 1]);
}

type MeMenuProps = { variant: TabListVariant; active: boolean; tabClass: string };

// MeMenu is the "Me" tab: the user's initials, opening a menu with their name, links to
// the profile and the account settings, and "Sign out" (FR-2.2).
export function MeMenu({ variant, active, tabClass }: MeMenuProps) {
  const session = useSession();
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [signingOut, setSigningOut] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const panelId = useId();

  // Close on a click outside the menu or on Escape.
  useEffect(() => {
    if (!open) return;
    function onPointer(event: MouseEvent) {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    }
    function onKey(event: KeyboardEvent) {
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

  async function signOut() {
    setSigningOut(true);
    // The server always answers 204; if the request does not arrive, the session check
    // on /login sends a user who is still signed in back to /home.
    await logout();
    setSignedOut();
    router.replace("/login");
  }

  const user = session.status === "signedIn" ? session.user : undefined;
  const place = variant === "top" ? "right-0 top-full mt-1" : "right-2 bottom-full mb-1";

  return (
    <div ref={root} className="relative flex w-full">
      <button
        ref={button}
        type="button"
        aria-expanded={open}
        aria-controls={panelId}
        onClick={() => setOpen(!open)}
        className={tabClass}
      >
        {user ? (
          <span
            aria-hidden="true"
            className={`flex h-6 w-6 items-center justify-center rounded-full text-[10px] font-semibold ${active ? "bg-primary text-surface" : "bg-text-muted text-surface"}`}
          >
            {initials(user.fullName)}
          </span>
        ) : (
          <MeIcon />
        )}
        <span className={variant === "bottom" ? "text-[11px]" : ""}>Me</span>
      </button>
      <div
        id={panelId}
        hidden={!open}
        className={`absolute z-30 w-64 rounded-lg border border-border bg-surface p-4 text-left shadow-sm ${place}`}
      >
        {user ? (
          <div className="mb-3 min-w-0">
            <p className="truncate font-semibold text-text">{user.fullName}</p>
            <p className="truncate text-sm text-text-muted">{user.email}</p>
          </div>
        ) : null}
        <Link
          href="/profile"
          onClick={() => setOpen(false)}
          className={buttonClass({ variant: "outline", size: "md", full: true })}
        >
          View profile
        </Link>
        <Link
          href="/settings/account"
          onClick={() => setOpen(false)}
          className={`mt-2 ${buttonClass({ variant: "text", size: "md", full: true })}`}
        >
          Settings
        </Link>
        <div className="mt-3 border-t border-border pt-3">
          <button
            type="button"
            onClick={signOut}
            disabled={signingOut}
            className={buttonClass({ variant: "text", size: "md", full: true })}
          >
            {signingOut ? "Signing out…" : "Sign out"}
          </button>
        </div>
      </div>
    </div>
  );
}
