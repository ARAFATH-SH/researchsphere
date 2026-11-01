import { useEffect, useSyncExternalStore } from "react";

import { useSession } from "@/features/auth/session";

import { listReceivedRequests } from "./api";

// The number of pending connection requests sent to the signed-in user: the total of the
// received list (FR-7.4), shown as the badge on the My Network tab. The first reader
// fetches it once per page load; accepting or ignoring a request refreshes it. Real-time
// updates come with notifications (slice 14).

type Entry = { userId: string; count: number };

let entry: Entry | null = null;
let loadingFor: string | null = null;
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

// refreshPendingInvitations fetches userId's count again. A failed fetch keeps the last
// count.
export async function refreshPendingInvitations(userId: string): Promise<void> {
  const mine = ++generation;
  loadingFor = userId;
  const res = await listReceivedRequests(1);
  if (mine !== generation) return;
  loadingFor = null;
  if (res.ok) set({ userId, count: res.data.total });
  else if (entry?.userId !== userId) set({ userId, count: 0 });
}

// setPendingInvitations stores a count the caller already has, such as the total of the
// received list it just loaded, so the badge agrees with it without another request.
export function setPendingInvitations(userId: string, count: number): void {
  generation++;
  loadingFor = null;
  set({ userId, count });
}

// usePendingInvitations returns the signed-in user's count; 0 while it loads or when
// nobody is signed in.
export function usePendingInvitations(): number {
  const session = useSession();
  const userId = session.status === "signedIn" ? session.user.id : null;
  const current = useSyncExternalStore(
    subscribe,
    () => entry,
    () => null,
  );
  const stale = userId !== null && current?.userId !== userId;
  useEffect(() => {
    if (userId !== null && stale && loadingFor !== userId) void refreshPendingInvitations(userId);
  }, [stale, userId]);
  return userId !== null && current?.userId === userId ? current.count : 0;
}

// resetPendingInvitations forgets the count; for tests.
export function resetPendingInvitations(): void {
  generation++;
  loadingFor = null;
  entry = null;
}
