"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

import { useSession } from "@/features/auth/session";

import { MeMenu } from "./me-menu";
import { isActive, tabsFor } from "./tabs";

export type TabListVariant = "top" | "bottom";

// CurrentTabList marks the tab of the current page. usePathname is only known at request
// time, so AppShell renders this inside <Suspense> with a plain TabList as the fallback.
export function CurrentTabList({ variant }: { variant: TabListVariant }) {
  return <TabList pathname={usePathname() ?? ""} variant={variant} />;
}

// TabList shows the tabs with icons and labels; the one for pathname is current. The
// Admin tab shows only when GET /auth/me says the user is an administrator.
export function TabList({ pathname, variant }: { pathname: string; variant: TabListVariant }) {
  const session = useSession();
  const visible = tabsFor(session.status === "signedIn" ? session.user.role : undefined);
  const columns = visible.length === 7 ? "grid-cols-7" : "grid-cols-6";
  const list = variant === "top" ? "flex h-full items-stretch" : `grid ${columns}`;
  return (
    <ul className={list}>
      {visible.map((tab) => {
        const active = isActive(tab, pathname);
        const color = active ? "text-primary" : "text-text-muted hover:text-text";
        const underline =
          variant === "top" && active
            ? "after:absolute after:inset-x-1 after:bottom-0 after:h-0.5 after:bg-primary"
            : "";
        const size = variant === "top" ? "min-w-20 px-1" : "py-2";
        const tabClass = `relative flex w-full flex-col items-center justify-center gap-0.5 text-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary ${size} ${color} ${underline}`;
        // "Me" opens the account menu instead of going straight to the profile.
        if (tab.href === "/profile") {
          return (
            <li key={tab.href} className="flex">
              <MeMenu variant={variant} active={active} tabClass={tabClass} />
            </li>
          );
        }
        return (
          <li key={tab.href} className="flex">
            <Link href={tab.href} aria-current={active ? "page" : undefined} className={tabClass}>
              <tab.Icon />
              <span className={variant === "bottom" ? "text-[11px]" : ""}>{tab.label}</span>
            </Link>
          </li>
        );
      })}
    </ul>
  );
}
