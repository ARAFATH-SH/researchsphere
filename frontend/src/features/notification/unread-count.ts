import { useSyncExternalStore } from "react";

import { useSession } from "@/features/auth/session";

import { getUnreadNotificationCount } from "./api";

// The signed-in user's unread notifications (FR-9.4), shown as the badge on the
// Notifications tab. Only the badge poller (badges.ts) and the notifications page write it;
// readers never fetch.

type Entry = { userId: string; count: number };

let entry: Entry | null = null;
let generation = 0;
const listeners = new Set<() => void>();

function set(next: Entry | null): void {
  entry = next;
  listeners.forEach((listener) => listener());
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

// refreshUnreadNotifications fetches userId's count again. A failed fetch keeps the last
// count.
export async function refreshUnreadNotifications(userId: string): Promise<void> {
  const mine = ++generation;
  const res = await getUnreadNotificationCount();
  if (mine !== generation) return;
  if (res.ok) set({ userId, count: res.data });
}

// setUnreadNotifications stores a count the caller already knows, such as 0 after "Mark
// all as read".
export function setUnreadNotifications(userId: string, count: number): void {
  generation++;
  set({ userId, count });
}

// useUnreadNotifications returns the signed-in user's count; 0 until it is known or when
// nobody is signed in.
export function useUnreadNotifications(): number {
  const session = useSession();
  const userId = session.status === "signedIn" ? session.user.id : null;
  const current = useSyncExternalStore(
    subscribe,
    () => entry,
    () => null,
  );
  return userId !== null && current?.userId === userId ? current.count : 0;
}

// resetUnreadNotifications forgets the count (on sign-out, and in tests).
export function resetUnreadNotifications(): void {
  generation++;
  set(null);
}
