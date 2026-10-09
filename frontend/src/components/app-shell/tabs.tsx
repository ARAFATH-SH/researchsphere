import type { ComponentType } from "react";

import { EventsIcon, HomeIcon, MeIcon, NetworkIcon, NotificationsIcon, ProjectsIcon } from "./icons";

export type Tab = { href: string; label: string; Icon: ComponentType };

// The signed-in tabs, in order (docs/ui-guidelines.md, Signed-in pages).
export const tabs: Tab[] = [
  { href: "/home", label: "Home", Icon: HomeIcon },
  { href: "/connections", label: "Network", Icon: NetworkIcon },
  { href: "/projects", label: "Projects", Icon: ProjectsIcon },
  { href: "/events", label: "Events", Icon: EventsIcon },
  { href: "/notifications", label: "Notifications", Icon: NotificationsIcon },
  { href: "/profile", label: "Me", Icon: MeIcon },
];

// isActive reports whether pathname is the tab's page or one below it.
export function isActive(tab: Tab, pathname: string): boolean {
  return pathname === tab.href || pathname.startsWith(tab.href + "/");
}
