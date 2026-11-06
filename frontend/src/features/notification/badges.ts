import { useEffect } from "react";

import { useSession } from "@/features/auth/session";
import { refreshUnreadMessages, resetUnreadMessages } from "@/features/messaging/unread-count";
import { refreshPendingInvitations } from "@/features/social/pending-invitations";

import { refreshUnreadNotifications, resetUnreadNotifications } from "./unread-count";

// The one badge poller of the app (FR-9.4). While a user is signed in and the browser tab
// is visible it refreshes the Notifications, My Network and Messaging badges every 60
// seconds; it pauses while the tab is hidden and refreshes at once when the tab is shown again.
// Starting it twice for the same user changes nothing, so however many components mount
// it there is one timer.

export const pollInterval = 60_000;

let polling: string | null = null;
let timer: ReturnType<typeof setInterval> | null = null;

// refreshBadges fetches every count again. Call it after anything that may change them:
// sign-in, a notification read, "Mark all as read", or accepting or ignoring a request.
export function refreshBadges(userId: string): void {
  void refreshUnreadNotifications(userId);
  void refreshPendingInvitations(userId);
  void refreshUnreadMessages(userId);
}

function tick(): void {
  if (polling) refreshBadges(polling);
}

function startTimer(): void {
  if (timer === null) timer = setInterval(tick, pollInterval);
}

function stopTimer(): void {
  if (timer !== null) clearInterval(timer);
  timer = null;
}

function onVisibilityChange(): void {
  if (document.visibilityState === "visible") {
    tick();
    startTimer();
  } else {
    stopTimer();
  }
}

// startBadgePolling polls for userId, refreshing at once. A different user replaces the
// current one.
export function startBadgePolling(userId: string): void {
  if (polling === userId) return;
  if (polling !== null) stopBadgePolling();
  polling = userId;
  document.addEventListener("visibilitychange", onVisibilityChange);
  if (document.visibilityState === "visible") {
    tick();
    startTimer();
  }
}

// stopBadgePolling stops the timer and forgets the unread counts.
export function stopBadgePolling(): void {
  if (polling === null) return;
  polling = null;
  stopTimer();
  document.removeEventListener("visibilitychange", onVisibilityChange);
  resetUnreadNotifications();
  resetUnreadMessages();
}

// holders counts the mounted pollers, so the poll stops only when the last one goes.
let holders = 0;

// useBadgePoller polls while someone is signed in and stops on sign-out or when the last
// poller unmounts. The app shell mounts it once (BadgePoller); public pages never do, so
// they make no call.
export function useBadgePoller(): void {
  const session = useSession();
  const userId = session.status === "signedIn" ? session.user.id : null;
  useEffect(() => {
    if (userId === null) return;
    holders++;
    startBadgePolling(userId);
    return () => {
      holders--;
      if (holders === 0) stopBadgePolling();
    };
  }, [userId]);
}
