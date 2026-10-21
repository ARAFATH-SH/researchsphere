"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useId, useRef, useState } from "react";

import { Avatar } from "@/components/avatar";
import { buttonClass } from "@/components/ui/button";
import { logout } from "@/features/auth/api";
import { setSignedOut, useSession } from "@/features/auth/session";
import { useMyProfile } from "@/features/profile/my-profile";

import { MeIcon } from "./icons";
import type { TabListVariant } from "./tab-list";

type MeMenuProps = { variant: TabListVariant; active: boolean; tabClass: string };

// MeMenu is the "Me" tab: the user's photo or initials, opening a menu with their name, links to
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
  // GET /auth/me has no photo; it comes from the shared own profile.
  const mine = useMyProfile();
  const photoUrl = mine?.status === "ok" ? mine.profile.photoUrl : null;
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
          <Avatar
            name={user.fullName}
            src={photoUrl}
            size="sm"
            decorative
            className={active ? "ring-2 ring-primary ring-offset-1 ring-offset-surface" : ""}
          />
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
