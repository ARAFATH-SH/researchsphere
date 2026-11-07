import { useSyncExternalStore } from "react";

import { type AccountWarning, listPendingWarnings } from "./api";

const none: AccountWarning[] = [];

// The warnings waiting for this tab's user, shared by the banner and the warning page.
let pending = none;
let reads = 0;
const listeners = new Set<() => void>();

function set(next: AccountWarning[]): void {
  pending = next;
  listeners.forEach((listener) => listener());
}

// refreshPendingWarnings reads the list again. A failed read keeps what is shown, and only the
// newest read counts.
export async function refreshPendingWarnings(): Promise<void> {
  const read = ++reads;
  const res = await listPendingWarnings();
  if (res.ok && read === reads) set(res.data);
}

// clearPendingWarnings forgets the list on sign-out, so the next user does not see it.
export function clearPendingWarnings(): void {
  reads++;
  if (pending !== none) set(none);
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function usePendingWarnings(): AccountWarning[] {
  return useSyncExternalStore(
    subscribe,
    () => pending,
    () => none,
  );
}
