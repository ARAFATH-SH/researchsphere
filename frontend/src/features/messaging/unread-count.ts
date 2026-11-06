import { useSyncExternalStore } from "react";

import { useSession } from "@/features/auth/session";

import { getMessageUnreadCount, type MessageUnreadCount } from "./api";

// The signed-in user's unread messages and pending message requests (FR-16.3), shown on the
// Messaging tab and in the list's tabs. Only the badge poller (features/notification/badges.ts)
// and the messaging pages refresh it; readers never fetch. 14A.5 moves it to socket events.

type Entry = { userId: string; counts: MessageUnreadCount };

const none: MessageUnreadCount = { total: 0, requests: 0 };

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

// refreshUnreadMessages fetches userId's counts again. A failed fetch keeps the last counts.
export async function refreshUnreadMessages(userId: string): Promise<void> {
  const mine = ++generation;
  const res = await getMessageUnreadCount();
  if (mine !== generation) return;
  if (res.ok) set({ userId, counts: res.data });
}

// useUnreadMessages returns the signed-in user's counts; zero until they are known or when
// nobody is signed in.
export function useUnreadMessages(): MessageUnreadCount {
  const session = useSession();
  const userId = session.status === "signedIn" ? session.user.id : null;
  const current = useSyncExternalStore(
    subscribe,
    () => entry,
    () => null,
  );
  return userId !== null && current?.userId === userId ? current.counts : none;
}

// resetUnreadMessages forgets the counts (on sign-out, and in tests).
export function resetUnreadMessages(): void {
  generation++;
  set(null);
}
