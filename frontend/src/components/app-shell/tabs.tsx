import type { ComponentType } from "react";

import { HomeIcon, MeIcon, MessagingIcon, NetworkIcon, NotificationsIcon, PostIcon, ProjectsIcon } from "./icons";

// action tabs start something rather than open a page, so they are never the current tab.
export type Tab = { href: string; label: string; Icon: ComponentType; action?: boolean };

// The signed-in top bar tabs, in order (docs/ui-guidelines.md, Signed-in pages). Pages
// that later slices build (Messaging) may 404 until then. Events and the admin console are
// in the Me menu.
export const tabs: Tab[] = [
  { href: "/home", label: "Home", Icon: HomeIcon },
  { href: "/connections", label: "My Network", Icon: NetworkIcon },
  { href: "/projects", label: "Projects", Icon: ProjectsIcon },
  { href: "/messaging", label: "Messaging", Icon: MessagingIcon },
  { href: "/notifications", label: "Notifications", Icon: NotificationsIcon },
  { href: "/profile", label: "Me", Icon: MeIcon },
];

// The phone bottom bar (480px and narrower). Post goes to the home feed, where posts are
// written.
export const phoneTabs: Tab[] = [
  { href: "/home", label: "Home", Icon: HomeIcon },
  { href: "/connections", label: "Network", Icon: NetworkIcon },
  { href: "/home", label: "Post", Icon: PostIcon, action: true },
  { href: "/notifications", label: "Alerts", Icon: NotificationsIcon },
  { href: "/projects", label: "Projects", Icon: ProjectsIcon },
];

// isActive reports whether pathname is the tab's page or one below it. The Me tab is
// current only on the signed-in user's own profile (userId) and its sub-pages, not on
// another user's /profile/<id>.
export function isActive(tab: Tab, pathname: string, userId?: string): boolean {
  if (tab.action) return false;
  if (tab.href === "/profile") {
    return pathname === "/profile" || isAt(pathname, "/profile/edit") || (!!userId && isAt(pathname, `/profile/${userId}`));
  }
  return isAt(pathname, tab.href);
}

function isAt(pathname: string, href: string): boolean {
  return pathname === href || pathname.startsWith(href + "/");
}

// topLabel hides a top-bar tab's label under 760px (it stays for screen readers) and shows
// it again, smaller, in the 481–640px bottom bar.
export const topLabel =
  "flex items-center gap-px max-[760px]:sr-only max-[640px]:not-sr-only max-[640px]:text-rs-micro";
