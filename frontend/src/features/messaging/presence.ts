import { useEffect, useSyncExternalStore } from "react";

import { getPresence, maxPresenceIds } from "@/features/realtime/api";
import { onConnected, onRealtimeEvent, onStatusChange, socketStatus } from "@/features/realtime/socket";

// Who is online among the people on screen (FR-16.4). A row or thread header watches its person
// while mounted. New people are asked for together, and everyone watched again on every connect
// and every 60 s, which covers a crashed API instance that cannot send online: false
// (docs/realtime.md).

export const presenceRefresh = 60_000;

const online = new Set<string>();
const watchers = new Map<string, number>();
const listeners = new Set<() => void>();
const waiting = new Set<string>();
let flushTimer: ReturnType<typeof setTimeout> | null = null;
let refreshTimer: ReturnType<typeof setInterval> | null = null;

function store(userId: string, isOnline: boolean): void {
  if (online.has(userId) === isOnline) return;
  if (isOnline) online.add(userId);
  else online.delete(userId);
  listeners.forEach((listener) => listener());
}

async function ask(userIds: string[]): Promise<void> {
  const res = await getPresence(userIds);
  if (res.ok) res.data.forEach((status) => store(status.userId, status.online));
}

function askInChunks(userIds: string[]): void {
  for (let i = 0; i < userIds.length; i += maxPresenceIds) void ask(userIds.slice(i, i + maxPresenceIds));
}

function refreshAll(): void {
  askInChunks([...watchers.keys()]);
}

function flush(): void {
  flushTimer = null;
  const ids = [...waiting];
  waiting.clear();
  askInChunks(ids);
}

function watch(userId: string): () => void {
  const count = watchers.get(userId) ?? 0;
  watchers.set(userId, count + 1);
  if (count === 0) {
    waiting.add(userId);
    flushTimer ??= setTimeout(flush, 0);
  }
  refreshTimer ??= setInterval(refreshAll, presenceRefresh);
  return () => {
    const left = (watchers.get(userId) ?? 1) - 1;
    if (left > 0) {
      watchers.set(userId, left);
      return;
    }
    watchers.delete(userId);
    waiting.delete(userId);
    if (watchers.size === 0 && refreshTimer !== null) {
      clearInterval(refreshTimer);
      refreshTimer = null;
    }
  };
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

onRealtimeEvent((event) => {
  if (event.type === "presence") store(event.data.userId, event.data.online);
});
onConnected(refreshAll);
// Signing out forgets who was online.
onStatusChange(() => {
  if (socketStatus() === "idle") [...online].forEach((userId) => store(userId, false));
});

// usePresence reports whether userId is online, and keeps asking while the component is mounted.
export function usePresence(userId: string): boolean {
  useEffect(() => watch(userId), [userId]);
  return useSyncExternalStore(
    subscribe,
    () => online.has(userId),
    () => false,
  );
}
