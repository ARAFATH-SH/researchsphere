"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

import { MeMenu } from "./me-menu";
import { isActive, tabs } from "./tabs";

export type TabListVariant = "top" | "bottom";

// CurrentTabList marks the tab of the current page. usePathname is only known at request
// time, so AppShell renders this inside <Suspense> with a plain TabList as the fallback.
export function CurrentTabList({ variant }: { variant: TabListVariant }) {
  return <TabList pathname={usePathname() ?? ""} variant={variant} />;
}

// TabList shows the six tabs with icons and labels; the one for pathname is current.
export function TabList({ pathname, variant }: { pathname: string; variant: TabListVariant }) {
  const list = variant === "top" ? "flex h-full items-stretch" : "grid grid-cols-6";
  return (
    <ul className={list}>
      {tabs.map((tab) => {
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
