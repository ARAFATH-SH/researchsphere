import { useEffect } from "react";

import { useSession } from "@/features/auth/session";
import { refreshUnreadMessages, resetUnreadMessages, setUnreadMessages } from "@/features/messaging/unread-count";
import type { RealtimeEvent } from "@/features/realtime/events";
import { onConnected, onRealtimeEvent, socketStatus } from "@/features/realtime/socket";
import { refreshPendingInvitations } from "@/features/social/pending-invitations";

import { refreshUnreadNotifications, resetUnreadNotifications, setUnreadNotifications } from "./unread-count";

// The one badge poller of the app (FR-9.4). While a user is signed in and the browser tab
// is visible it refreshes the Notifications, My Network and Messaging badges every 60
// seconds; it pauses while the tab is hidden and refreshes at once when the tab is shown again.
// While the socket is open the Notifications and Messaging counts come as events instead, so only
// My Network, which has no event, goes on being polled. After every connect all are fetched, as
// events are not stored (docs/realtime.md). Starting it twice for the same user changes nothing, so however many components mount
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
  if (!polling) return;
  if (socketStatus() === "open") void refreshPendingInvitations(polling);
  else refreshBadges(polling);
}

function onConnect(): void {
  if (polling) refreshBadges(polling);
}

function onEvent(event: RealtimeEvent): void {
  if (!polling) return;
  if (event.type === "unread.changed") setUnreadMessages(polling, event.data);
  // Every notification is followed by this count, so notification.created need not touch it.
  else if (event.type === "notifications.unread") setUnreadNotifications(polling, event.data.count);
  else if (event.type === "notification.created" && event.data.type.startsWith("connection_request")) void refreshPendingInvitations(polling);
}

let stopListening: (() => void)[] = [];

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
  stopListening = [onConnected(onConnect), onRealtimeEvent(onEvent)];
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
  stopListening.forEach((stop) => stop());
  stopListening = [];
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
