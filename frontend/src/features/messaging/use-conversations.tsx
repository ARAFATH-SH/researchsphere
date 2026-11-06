"use client";

import { createContext, type ReactNode, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";

import { type Box, type Conversation, listConversations } from "./api";

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
  // changes one in place; remove takes it out of both. Thread actions use these, and 14A.5
  // socket events will too.
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

// ConversationsProvider keeps both boxes for the whole messaging area, so moving between
// conversations keeps the pages already loaded and the list's scroll position.
export function ConversationsProvider({ children }: { children: ReactNode }) {
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
    setBoxes((prev) =>
      mapBoxes(prev, (items, box) => {
        const rest = items.filter((c) => c.id !== conversation.id);
        return box === target ? [conversation, ...rest] : rest;
      }),
    );
  }, []);

  const patch = useCallback((id: string, change: Partial<Conversation>) => {
    setBoxes((prev) => mapBoxes(prev, (items) => items.map((c) => (c.id === id ? { ...c, ...change } : c))));
  }, []);

  const remove = useCallback((id: string) => {
    setBoxes((prev) => mapBoxes(prev, (items) => items.filter((c) => c.id !== id)));
  }, []);

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
