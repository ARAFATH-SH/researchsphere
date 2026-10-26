"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

import { useSession } from "@/features/auth/session";

import { MeMenu } from "./me-menu";
import { isActive, phoneTabs, type Tab, tabs, topLabel } from "./tabs";

// top is the tab row of the top bar (a bottom bar from 481 to 640px); phone is the bottom bar
// at 480px and narrower (docs/ui-guidelines.md, Signed-in pages).
export type TabListVariant = "top" | "phone";

// Counts are unread numbers by tab href, shown as a badge. Nothing supplies them until the
// messaging and notification slices.
export type TabCounts = Partial<Record<string, number>>;

type TabListProps = { pathname: string; variant: TabListVariant; counts?: TabCounts };

// CurrentTabList marks the tab of the current page. usePathname is only known at request
// time, so AppShell renders this inside <Suspense> with a plain TabList as the fallback.
export function CurrentTabList({ variant, counts }: { variant: TabListVariant; counts?: TabCounts }) {
  return <TabList pathname={usePathname() ?? ""} variant={variant} counts={counts} />;
}

// TabList shows the tabs with icons and labels; the one for pathname is current.
export function TabList({ pathname, variant, counts = {} }: TabListProps) {
  return variant === "top" ? (
    <TopTabs pathname={pathname} counts={counts} />
  ) : (
    <PhoneTabs pathname={pathname} counts={counts} />
  );
}

// From 641px the tabs sit in the 52px bar; under 760px only the icons show. From 481 to
// 640px the same row is fixed to the bottom of the screen, 56px high, with labels again.
function TopTabs({ pathname, counts }: { pathname: string; counts: TabCounts }) {
  const session = useSession();
  const userId = session.status === "signedIn" ? session.user.id : undefined;
  return (
    <ul className="flex h-[52px] max-[640px]:fixed max-[640px]:inset-x-0 max-[640px]:bottom-0 max-[640px]:z-50 max-[640px]:h-14 max-[640px]:justify-around max-[640px]:border-t max-[640px]:border-rs-nav-border max-[640px]:bg-rs-surface">
      {tabs.map((tab) => {
        const active = isActive(tab, pathname, userId);
        const tabClass = topTabClass(active);
        return (
          <li key={tab.href} className="relative flex w-20 max-[760px]:w-[52px] max-[640px]:w-auto max-[640px]:flex-1">
            {/* "Me" opens the account menu instead of going straight to the profile. */}
            {tab.href === "/profile" ? (
              <MeMenu placement="tab" tabClass={tabClass} />
            ) : (
              <Link href={tab.href} aria-current={active ? "page" : undefined} className={tabClass}>
                <tab.Icon />
                <span className={topLabel}>{tab.label}</span>
                <Badge tab={tab} counts={counts} place="left-[calc(50%+4px)]" />
              </Link>
            )}
          </li>
        );
      })}
    </ul>
  );
}

function topTabClass(active: boolean): string {
  const color = active ? "text-rs-nav-text" : "text-rs-nav-muted hover:text-rs-accent focus-visible:text-rs-accent";
  const underline = active
    ? "after:absolute after:inset-x-0 after:bottom-0 after:h-0.5 after:bg-rs-accent max-[640px]:after:top-0 max-[640px]:after:bottom-auto"
    : "";
  return `relative flex h-[52px] w-full flex-col items-center justify-center gap-0.5 text-rs-tiny leading-rs-tight tracking-[.01em] focus-visible:shadow-focus focus-visible:outline-none max-[640px]:h-14 ${color} ${underline}`;
}

// PhoneTabs is the 64px bottom bar of the phone layout, five items in a grid.
function PhoneTabs({ pathname, counts }: { pathname: string; counts: TabCounts }) {
  return (
    <ul className="grid h-16 grid-cols-5">
      {phoneTabs.map((tab) => {
        const active = isActive(tab, pathname);
        const state = active
          ? "font-semibold text-rs-nav-text before:absolute before:inset-x-1/4 before:top-0 before:h-0.5 before:rounded-b-[2px] before:bg-rs-accent [&>svg]:text-rs-accent"
          : "text-rs-nav-muted";
        return (
          <li key={tab.label} className="flex">
            <Link
              href={tab.href}
              aria-current={active ? "page" : undefined}
              className={`relative flex min-h-16 w-full flex-col items-center justify-center gap-[3px] text-rs-tiny leading-rs-tight focus-visible:shadow-focus focus-visible:outline-none ${state}`}
            >
              <tab.Icon />
              {tab.label}
              <Badge tab={tab} counts={counts} place="left-[calc(50%+6px)]" />
            </Link>
          </li>
        );
      })}
    </ul>
  );
}

// Badge is the red unread count on a tab. The number is hidden from screen readers, which
// hear ", N unread" after the label instead.
export function Badge({ tab, counts, place }: { tab: Tab; counts: TabCounts; place: string }) {
  const count = counts[tab.href];
  if (!count || tab.action) return null;
  return (
    <>
      <span
        aria-hidden="true"
        className={`absolute top-1 flex h-4 min-w-4 items-center justify-center rounded-rs-pill bg-rs-danger px-1 text-rs-micro font-bold text-rs-surface ${place}`}
      >
        {count}
      </span>
      <span className="sr-only">, {count} unread</span>
    </>
  );
}
