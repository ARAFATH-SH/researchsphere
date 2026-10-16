import type { ComponentType } from "react";

import type { Role } from "@/features/auth/api";

import { AdminIcon, EventsIcon, HomeIcon, MeIcon, NetworkIcon, NotificationsIcon, ProjectsIcon } from "./icons";

// adminOnly tabs show only to an administrator. Hiding a tab is a convenience: the API
// refuses everyone else (FR-3.2).
export type Tab = { href: string; label: string; Icon: ComponentType; adminOnly?: boolean };

// The signed-in tabs, in order (docs/ui-guidelines.md, Signed-in pages).
export const tabs: Tab[] = [
  { href: "/home", label: "Home", Icon: HomeIcon },
  { href: "/connections", label: "Network", Icon: NetworkIcon },
  { href: "/projects", label: "Projects", Icon: ProjectsIcon },
  { href: "/events", label: "Events", Icon: EventsIcon },
  { href: "/notifications", label: "Notifications", Icon: NotificationsIcon },
  { href: "/admin", label: "Admin", Icon: AdminIcon, adminOnly: true },
  { href: "/profile", label: "Me", Icon: MeIcon },
];

// tabsFor returns the tabs a user with role sees; with no role (signed out or still
// loading), the admin tab stays hidden.
export function tabsFor(role: Role | undefined): Tab[] {
  return tabs.filter((tab) => !tab.adminOnly || role === "admin");
}

// isActive reports whether pathname is the tab's page or one below it.
export function isActive(tab: Tab, pathname: string): boolean {
  return pathname === tab.href || pathname.startsWith(tab.href + "/");
}
