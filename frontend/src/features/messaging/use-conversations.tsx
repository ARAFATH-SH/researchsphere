"use client";

import { createContext, type ReactNode, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";

import { useSession } from "@/features/auth/session";
import type { RealtimeEvent } from "@/features/realtime/events";
import { useOnConnected, useRealtimeEvent } from "@/features/realtime/use-realtime";

import { type Box, type Conversation, type ConversationPage, getConversation, listConversations } from "./api";

export type BoxState =
  | { status: "idle" }
  | { status: "loading" }
  | { status: "error" }
  | { status: "ok"; items: Conversation[]; nextCursor: string | null; loadingMore: boolean; moreFailed: boolean };

type Boxes = Record<Box, BoxState>;

export type Conversations = {
  boxes: Boxes;
  // open loads a box's first page if it has none yet; retry loads it again.
  open: (box: Box) => void;
  retry: (box: Box) => void;
  loadMore: (box: Box) => void;
  // put places a conversation at the top of a box and takes it out of the other one; patch
  // changes one in place; remove takes it out of both. Thread actions use these; socket events
  // make the same changes from inside the provider.
  put: (box: Box, conversation: Conversation) => void;
  patch: (id: string, change: Partial<Conversation>) => void;
  remove: (id: string) => void;
};

const Context = createContext<Conversations | null>(null);

function mapBoxes(boxes: Boxes, change: (items: Conversation[], box: Box) => Conversation[]): Boxes {
  const apply = (box: Box): BoxState => {
    const state = boxes[box];
    return state.status === "ok" ? { ...state, items: change(state.items, box) } : state;
  };
  return { inbox: apply("inbox"), requests: apply("requests") };
}

// placed puts a conversation at the top of target and takes it out of the other box.
function placed(boxes: Boxes, target: Box, conversation: Conversation): Boxes {
  return mapBoxes(boxes, (items, box) => {
    const rest = items.filter((c) => c.id !== conversation.id);
    return box === target ? [conversation, ...rest] : rest;
  });
}

function findRow(boxes: Boxes, id: string): { box: Box; row: Conversation } | null {
  for (const box of ["inbox", "requests"] as const) {
    const state = boxes[box];
    const row = state.status === "ok" ? state.items.find((c) => c.id === id) : undefined;
    if (row) return { box, row };
  }
  return null;
}

// senderNameOf finds a group row's name for a sender from what the row already knows: its
// latest preview or its first members. Null when it does not know them.
function senderNameOf(boxes: Boxes, id: string, senderId: string | null): string | null {
  const row = findRow(boxes, id)?.row;
  if (!row || !senderId) return null;
  if (row.lastMessage?.senderId === senderId && row.lastMessage.senderName) return row.lastMessage.senderName;
  if (row.otherMember?.userId === senderId) return row.otherMember.fullName;
  return row.group?.avatars.find((a) => a.userId === senderId)?.fullName ?? null;
}

// merged folds a fresh first page into a box after a reconnect. Rows past that page stay, so
// pages already loaded and the scroll position survive; a row missing from the range the page
// covers has left the box.
function merged(items: Conversation[], nextCursor: string | null, page: ConversationPage) {
  if (!page.nextCursor) return { items: page.items, nextCursor: null };
  const last = Date.parse(page.items.at(-1)?.lastMessageAt ?? "");
  const seen = new Set(page.items.map((c) => c.id));
  const tail = items.filter((c) => !seen.has(c.id) && Date.parse(c.lastMessageAt) < last);
  return { items: [...page.items, ...tail], nextCursor: tail.length > 0 && nextCursor ? nextCursor : page.nextCursor };
}

// ConversationsProvider keeps both boxes for the whole messaging area, so moving between
// conversations keeps the pages already loaded and the list's scroll position. The socket keeps
// them current while the messaging area is open.
export function ConversationsProvider({ children }: { children: ReactNode }) {
  const session = useSession();
  const me = session.status === "signedIn" ? session.user.id : null;
  const [boxes, setBoxes] = useState<Boxes>({ inbox: { status: "idle" }, requests: { status: "idle" } });
  const boxesRef = useRef(boxes);
  useEffect(() => {
    boxesRef.current = boxes;
  });

  const load = useCallback((box: Box) => {
    setBoxes((prev) => ({ ...prev, [box]: { status: "loading" } }));
    void listConversations(box).then((res) =>
      setBoxes((prev) => ({
        ...prev,
        [box]: res.ok
          ? { status: "ok", items: res.data.items, nextCursor: res.data.nextCursor, loadingMore: false, moreFailed: false }
          : { status: "error" },
      })),
    );
  }, []);

  const open = useCallback(
    (box: Box) => {
      if (boxesRef.current[box].status === "idle") {
        boxesRef.current = { ...boxesRef.current, [box]: { status: "loading" } };
        load(box);
      }
    },
    [load],
  );

  const loadMore = useCallback((box: Box) => {
    const state = boxesRef.current[box];
    if (state.status !== "ok" || state.loadingMore || !state.nextCursor) return;
    const next: BoxState = { ...state, loadingMore: true, moreFailed: false };
    boxesRef.current = { ...boxesRef.current, [box]: next };
    setBoxes((prev) => ({ ...prev, [box]: next }));
    void listConversations(box, state.nextCursor).then((res) =>
      setBoxes((prev) => {
        const current = prev[box];
        if (current.status !== "ok") return prev;
        if (!res.ok) return { ...prev, [box]: { ...current, loadingMore: false, moreFailed: true } };
        const seen = new Set(current.items.map((c) => c.id));
        const added = res.data.items.filter((c) => !seen.has(c.id));
        return {
          ...prev,
          [box]: { ...current, items: [...current.items, ...added], nextCursor: res.data.nextCursor, loadingMore: false },
        };
      }),
    );
  }, []);

  const put = useCallback((target: Box, conversation: Conversation) => {
    setBoxes((prev) => placed(prev, target, conversation));
  }, []);

  const patch = useCallback((id: string, change: Partial<Conversation>) => {
    setBoxes((prev) => mapBoxes(prev, (items) => items.map((c) => (c.id === id ? { ...c, ...change } : c))));
  }, []);

  const remove = useCallback((id: string) => {
    setBoxes((prev) => mapBoxes(prev, (items) => items.filter((c) => c.id !== id)));
  }, []);

  // Delivery is at most once but an event may repeat, and a message must not count as unread twice.
  const seenMessages = useRef(new Set<string>());

  function onMessage({ message, conversationState }: Extract<RealtimeEvent, { type: "message.created" }>["data"]) {
    if (seenMessages.current.has(message.id)) return;
    seenMessages.current.add(message.id);
    const id = message.conversationId;
    const mine = message.senderId === me;
    const target: Box = conversationState === "request" && !mine ? "requests" : "inbox";
    if (!findRow(boxesRef.current, id)) {
      // A new request, or a conversation past the pages loaded.
      if (boxesRef.current[target].status !== "ok") return;
      void getConversation(id).then((res) => {
        if (res.ok) put(target, res.data);
      });
      return;
    }
    const lastMessage = {
      senderId: message.senderId,
      // The event carries no names; a group's row keeps the last name it had for this sender.
      senderName: senderNameOf(boxesRef.current, id, message.senderId),
      kind: message.kind,
      event: message.kind === "system" ? (message.event?.type ?? null) : null,
      body: message.kind === "system" ? null : [...(message.body ?? "")].slice(0, 120).join(""),
      deleted: message.deleted,
      attachmentCount: message.deleted ? 0 : message.attachments.length,
      createdAt: message.createdAt,
    };
    // System messages never count as unread (FR-16.6).
    const counts = !mine && message.kind !== "system";
    setBoxes((prev) => {
      const found = findRow(prev, id);
      if (!found) return prev;
      const unreadCount = found.row.unreadCount + (counts ? 1 : 0);
      return placed(prev, target, { ...found.row, state: conversationState, lastMessage, lastMessageAt: message.createdAt, unreadCount });
    });
    // A group row that does not know the sender's name reads it from the server.
    if (message.senderId && !mine && lastMessage.senderName === null && findRow(boxesRef.current, id)?.row.kind === "group") refresh(id);
  }

  // refresh reads one row again and replaces it in its place.
  function refresh(id: string) {
    void getConversation(id).then((res) => {
      if (!res.ok) return;
      const row = { ...res.data, group: res.data.group ? { ...res.data.group, members: null } : null };
      setBoxes((prev) => mapBoxes(prev, (items) => items.map((c) => (c.id === id ? row : c))));
    });
  }

  function onUpdated({ conversationId: id, state, canMessage, accessLost }: Extract<RealtimeEvent, { type: "conversation.updated" }>["data"]) {
    // The server lists a declined conversation in neither box, nor a group the caller left or
    // was removed from (FR-16.6).
    if (state === "declined" || accessLost) {
      remove(id);
      return;
    }
    setBoxes((prev) => {
      const found = findRow(prev, id);
      if (!found) return prev;
      const next = { ...found.row, state, canMessage };
      if (state === "active" && found.box === "requests") return placed(prev, "inbox", next);
      return mapBoxes(prev, (items) => items.map((c) => (c.id === id ? next : c)));
    });
  }

  // An edit changes a row's preview when it is the row's latest message. The preview carries no
  // message ID, so the latest is the one sent at the same moment by the same person.
  function onEdited({ message }: Extract<RealtimeEvent, { type: "message.updated" }>["data"]) {
    setBoxes((prev) => {
      const found = findRow(prev, message.conversationId);
      const last = found?.row.lastMessage;
      if (!found || !last || last.senderId !== message.senderId || Date.parse(last.createdAt) !== Date.parse(message.createdAt)) return prev;
      const lastMessage = {
        ...last,
        body: [...(message.body ?? "")].slice(0, 120).join(""),
        deleted: message.deleted,
        attachmentCount: message.deleted ? 0 : message.attachments.length,
      };
      return mapBoxes(prev, (items) => items.map((c) => (c.id === found.row.id ? { ...c, lastMessage } : c)));
    });
  }

  // A deletion may be of a row's latest message, which the event cannot tell, and a deleted
  // message no longer counts as unread: the row is read again and replaced in its place.
  function onDeleted({ conversationId: id }: Extract<RealtimeEvent, { type: "message.deleted" }>["data"]) {
    if (findRow(boxesRef.current, id)) refresh(id);
  }

  // A group's name, size and first members change with its members (FR-16.6): the name comes
  // with the event, the rest with the conversation.
  function onMembers({ conversationId: id, name }: Extract<RealtimeEvent, { type: "conversation.members" }>["data"]) {
    if (!findRow(boxesRef.current, id)) return;
    setBoxes((prev) => mapBoxes(prev, (items) => items.map((c) => (c.id === id && c.group ? { ...c, group: { ...c.group, name } } : c))));
    refresh(id);
  }

  useRealtimeEvent((event) => {
    if (event.type === "message.created") onMessage(event.data);
    else if (event.type === "message.updated") onEdited(event.data);
    else if (event.type === "message.deleted") onDeleted(event.data);
    else if (event.type === "conversation.updated") onUpdated(event.data);
    else if (event.type === "conversation.members") onMembers(event.data);
  });

  // Whatever was sent while the socket was closed is gone: read the lists again, in place.
  useOnConnected(() => {
    for (const box of ["inbox", "requests"] as const) {
      if (boxesRef.current[box].status !== "ok") continue;
      void listConversations(box).then((res) => {
        if (!res.ok) return;
        setBoxes((prev) => {
          const current = prev[box];
          if (current.status !== "ok") return prev;
          return { ...prev, [box]: { ...current, ...merged(current.items, current.nextCursor, res.data) } };
        });
      });
    }
  });

  const value = useMemo(
    () => ({ boxes, open, retry: load, loadMore, put, patch, remove }),
    [boxes, open, load, loadMore, put, patch, remove],
  );
  return <Context.Provider value={value}>{children}</Context.Provider>;
}

// useConversations reads the provider, which the messaging layout mounts.
export function useConversations(): Conversations {
  const value = useContext(Context);
  if (!value) throw new Error("useConversations needs a ConversationsProvider");
  return value;
}
