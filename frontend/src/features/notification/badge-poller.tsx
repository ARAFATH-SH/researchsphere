"use client";

import { useBadgePoller } from "./badges";

// BadgePoller runs the badge poller for the signed-in shell; it renders nothing.
export function BadgePoller() {
  useBadgePoller();
  return null;
}
