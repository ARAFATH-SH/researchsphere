import { useSyncExternalStore } from "react";

import { onRealtimeEvent, onStatusChange, socketStatus } from "@/features/realtime/socket";

// Who is typing, by conversation (FR-16.4), shared by the list and the thread. In a group
// several people may type at once. An indicator ends on isTyping: false, on that person's
// message, on conversation.updated, or after typingTimeout if the server's stop is lost; each
// start restarts that person's wait.

export const typingTimeout = 6_000;

// typing holds each conversation's typers, in the order they started, with their timers.
const typing = new Map<string, Map<string, ReturnType<typeof setTimeout>>>();
// snapshots keeps each conversation's list of typers stable between changes, as
// useSyncExternalStore needs.
const snapshots = new Map<string, string[]>();
const none: string[] = [];
const listeners = new Set<() => void>();

function changed(conversationId: string): void {
  const users = typing.get(conversationId);
  if (users && users.size > 0) snapshots.set(conversationId, [...users.keys()]);
  else {
    typing.delete(conversationId);
    snapshots.delete(conversationId);
  }
  listeners.forEach((listener) => listener());
}

function stop(conversationId: string, userId: string): void {
  const users = typing.get(conversationId);
  const timer = users?.get(userId);
  if (!users || timer === undefined) return;
  clearTimeout(timer);
  users.delete(userId);
  changed(conversationId);
}

function clear(conversationId: string): void {
  const users = typing.get(conversationId);
  if (!users) return;
  users.forEach((timer) => clearTimeout(timer));
  users.clear();
  changed(conversationId);
}

function start(conversationId: string, userId: string): void {
  let users = typing.get(conversationId);
  if (!users) {
    users = new Map();
    typing.set(conversationId, users);
  }
  const timer = users.get(userId);
  if (timer !== undefined) clearTimeout(timer);
  users.set(userId, setTimeout(() => stop(conversationId, userId), typingTimeout));
  if (timer === undefined) changed(conversationId);
}

onRealtimeEvent((event) => {
  if (event.type === "typing") {
    if (event.data.isTyping) start(event.data.conversationId, event.data.userId);
    else stop(event.data.conversationId, event.data.userId);
  } else if (event.type === "message.created") {
    const { conversationId, senderId } = event.data.message;
    if (senderId) stop(conversationId, senderId);
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

// useTypers lists who is typing in the conversation, in the order they started.
export function useTypers(conversationId: string): string[] {
  return useSyncExternalStore(
    subscribe,
    () => snapshots.get(conversationId) ?? none,
    () => none,
  );
}

// useIsTyping reports whether anyone else is typing in the conversation.
export function useIsTyping(conversationId: string): boolean {
  return useTypers(conversationId).length > 0;
}

// typingText says who is typing: "Nafisa is typing…", "Nafisa and Rahim are typing…", and from
// three people on "Several people are typing…".
export function typingText(names: string[]): string {
  if (names.length === 0) return "";
  if (names.length === 1) return `${names[0]} is typing…`;
  if (names.length === 2) return `${names[0]} and ${names[1]} are typing…`;
  return "Several people are typing…";
}
