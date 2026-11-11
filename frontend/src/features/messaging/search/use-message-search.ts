import { useEffect, useRef, useState } from "react";

import { type MessagePeople, type MessageSearch, type MessageSearchResult, searchMessages } from "../api";

// searchDelay is how long the search waits after the last key (FR-16.8).
export const searchDelay = 300;
// minSearch is the shortest text the server searches, in characters after trimming.
export const minSearch = 2;

// DatePreset is the "When" filter: how far back to search.
export type DatePreset = "any" | "week" | "month" | "year";

const presetDays: Record<Exclude<DatePreset, "any">, number> = { week: 7, month: 30, year: 365 };

// afterFor is a preset's start as RFC 3339, counted back from now; undefined for any time.
export function afterFor(preset: DatePreset, now = Date.now()): string | undefined {
  if (preset === "any") return undefined;
  return new Date(now - presetDays[preset] * 86_400_000).toISOString();
}

// searchable reports whether a text is long enough to search: 2 characters after trimming,
// counted as code points as the server counts them.
export function searchable(text: string): boolean {
  return Array.from(text.trim()).length >= minSearch;
}

export type SearchState =
  | { status: "idle" }
  | { status: "loading" }
  | { status: "error"; message: string }
  | { status: "limited"; retryAfter?: number }
  | {
      status: "ok";
      items: MessageSearchResult[];
      people: MessagePeople;
      nextCursor: string | null;
      loadingMore: boolean;
      moreFailed: boolean;
    };

type Filters = { conversationId?: string; from?: string; preset: DatePreset };

// useMessageSearch searches the caller's messages (FR-16.8) as they type: nothing under 2
// characters, then 300 ms after the last change. An answer to an older search is dropped: each
// answer is stored with the search it answers, and anything else reads as loading. more loads
// the next page with the cursor.
export function useMessageSearch(text: string, filters: Filters): { state: SearchState; more: () => void } {
  const q = text.trim();
  const { conversationId, from, preset } = filters;
  const key = JSON.stringify([q, conversationId ?? "", from ?? "", preset]);
  const [answer, setAnswer] = useState<{ key: string; search: MessageSearch; state: SearchState } | null>(null);
  const answerRef = useRef(answer);
  useEffect(() => {
    answerRef.current = answer;
  });
  const ready = searchable(q);

  useEffect(() => {
    if (!ready) return;
    let current = true;
    const timer = setTimeout(() => {
      const search: MessageSearch = { q, conversationId, from, after: afterFor(preset) };
      void searchMessages(search).then((res) => {
        let state: SearchState;
        if (res.ok) {
          state = { status: "ok", items: res.data.items, people: res.data.people, nextCursor: res.data.nextCursor, loadingMore: false, moreFailed: false };
        } else if (res.code === "rate_limited") {
          state = { status: "limited", retryAfter: res.retryAfter };
        } else {
          state = { status: "error", message: res.message };
        }
        if (current) setAnswer({ key, search, state });
      });
    }, searchDelay);
    return () => {
      current = false;
      clearTimeout(timer);
    };
  }, [ready, key, q, conversationId, from, preset]);

  // keyRef is the search on screen, so a page that arrives after the text changed is dropped.
  const keyRef = useRef(key);
  useEffect(() => {
    keyRef.current = key;
  });

  function more() {
    const now = answerRef.current;
    if (!now || now.key !== keyRef.current || now.state.status !== "ok" || now.state.loadingMore || !now.state.nextCursor) return;
    const { key: asked, search } = now;
    const loading = { ...now, state: { ...now.state, loadingMore: true, moreFailed: false } };
    answerRef.current = loading;
    setAnswer(loading);
    void searchMessages({ ...search, cursor: now.state.nextCursor }).then((res) => {
      setAnswer((prev) => {
        if (!prev || prev.key !== asked || prev.state.status !== "ok") return prev;
        if (!res.ok) {
          const state: SearchState =
            res.code === "rate_limited" ? { status: "limited", retryAfter: res.retryAfter } : { ...prev.state, loadingMore: false, moreFailed: true };
          return { ...prev, state };
        }
        const seen = new Set(prev.state.items.map((i) => i.messageId));
        return {
          ...prev,
          state: {
            ...prev.state,
            items: [...prev.state.items, ...res.data.items.filter((i) => !seen.has(i.messageId))],
            people: { ...prev.state.people, ...res.data.people },
            nextCursor: res.data.nextCursor,
            loadingMore: false,
          },
        };
      });
    });
  }

  let state: SearchState;
  if (!ready) state = { status: "idle" };
  else if (answer?.key !== key) state = { status: "loading" };
  else state = answer.state;
  return { state, more };
}
