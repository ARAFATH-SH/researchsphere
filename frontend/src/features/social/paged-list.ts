import { useCallback, useEffect, useRef, useState } from "react";

import type { Result } from "@/lib/api/call";

type Page<T> = { items: T[]; total: number; pageSize: number };

export type ListState<T> =
  | { status: "loading" }
  | { status: "error" }
  | { status: "notFound" }
  | { status: "ok"; items: T[]; total: number; pageSize: number; loadingMore: boolean; moreFailed: boolean };

export type PagedList<T> = {
  state: ListState<T>;
  retry: () => void;
  // loadMore appends the next page ("Show more").
  loadMore: () => void;
  // remove takes an item out after an action, and prepend puts one at the top.
  remove: (id: string) => void;
  prepend: (item: T) => void;
};

// usePagedList loads page 1 of a list and appends later pages on "Show more". Items
// removed or added here shift the server's pages, so the next page is worked out from the
// number of items on screen and items already shown are skipped. A new key loads again.
export function usePagedList<T>(
  key: string,
  fetchPage: (page: number) => Promise<Result<Page<T>>>,
  idOf: (item: T) => string,
): PagedList<T> {
  const [attempt, setAttempt] = useState(0);
  const [state, setState] = useState<ListState<T>>({ status: "loading" });
  const fetchRef = useRef(fetchPage);
  const idRef = useRef(idOf);
  useEffect(() => {
    fetchRef.current = fetchPage;
    idRef.current = idOf;
  });

  useEffect(() => {
    let current = true;
    void fetchRef.current(1).then((res) => {
      if (!current) return;
      if (res.ok) {
        const { items, total, pageSize } = res.data;
        setState({ status: "ok", items, total, pageSize, loadingMore: false, moreFailed: false });
      } else {
        setState({ status: res.code === "not_found" ? "notFound" : "error" });
      }
    });
    return () => {
      current = false;
      setState({ status: "loading" });
    };
  }, [key, attempt]);

  const loadMore = useCallback(() => {
    if (state.status !== "ok" || state.loadingMore) return;
    const next = Math.floor(state.items.length / state.pageSize) + 1;
    setState({ ...state, loadingMore: true, moreFailed: false });
    void fetchRef.current(next).then((res) =>
      setState((prev) => {
        if (prev.status !== "ok") return prev;
        if (!res.ok) return { ...prev, loadingMore: false, moreFailed: true };
        const seen = new Set(prev.items.map((item) => idRef.current(item)));
        const added = res.data.items.filter((item) => !seen.has(idRef.current(item)));
        return { ...prev, items: [...prev.items, ...added], total: res.data.total, loadingMore: false };
      }),
    );
  }, [state]);

  const remove = useCallback((id: string) => {
    setState((prev) =>
      prev.status === "ok"
        ? { ...prev, items: prev.items.filter((item) => idRef.current(item) !== id), total: Math.max(0, prev.total - 1) }
        : prev,
    );
  }, []);

  const prepend = useCallback((item: T) => {
    setState((prev) => {
      if (prev.status !== "ok") return prev;
      const id = idRef.current(item);
      const rest = prev.items.filter((other) => idRef.current(other) !== id);
      return { ...prev, items: [item, ...rest], total: prev.total + (rest.length === prev.items.length ? 1 : 0) };
    });
  }, []);

  return { state, retry: () => setAttempt((n) => n + 1), loadMore, remove, prepend };
}
