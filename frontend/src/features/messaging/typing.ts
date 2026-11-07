import { useSyncExternalStore } from "react";

import { onRealtimeEvent, onStatusChange, socketStatus } from "@/features/realtime/socket";

// Who is typing, by conversation (FR-16.4), shared by the list and the thread. An indicator ends
// on isTyping: false, on that person's message, on conversation.updated, or after typingTimeout
// if the server's stop is lost; each start restarts the wait.

export const typingTimeout = 6_000;

type Entry = { userId: string; timer: ReturnType<typeof setTimeout> };

const typing = new Map<string, Entry>();
const listeners = new Set<() => void>();

function clear(conversationId: string): void {
  const entry = typing.get(conversationId);
  if (!entry) return;
  clearTimeout(entry.timer);
  typing.delete(conversationId);
  listeners.forEach((listener) => listener());
}

function start(conversationId: string, userId: string): void {
  const entry = typing.get(conversationId);
  if (entry) clearTimeout(entry.timer);
  typing.set(conversationId, { userId, timer: setTimeout(() => clear(conversationId), typingTimeout) });
  if (!entry) listeners.forEach((listener) => listener());
}

onRealtimeEvent((event) => {
  if (event.type === "typing") {
    if (event.data.isTyping) start(event.data.conversationId, event.data.userId);
    else clear(event.data.conversationId);
  } else if (event.type === "message.created") {
    const { conversationId, senderId } = event.data.message;
    if (typing.get(conversationId)?.userId === senderId) clear(conversationId);
  } else if (event.type === "conversation.updated") {
    clear(event.data.conversationId);
  }
});
onStatusChange(() => {
  if (socketStatus() === "idle") [...typing.keys()].forEach(clear);
});

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

// useIsTyping reports whether the other member of the conversation is typing.
export function useIsTyping(conversationId: string): boolean {
  return useSyncExternalStore(
    subscribe,
    () => typing.has(conversationId),
    () => false,
  );
}
