import { useCallback, useEffect, useState } from "react";

import { useRealtimeEvent } from "@/features/realtime/use-realtime";

import { listNotifications, type Notification } from "./api";

export type NotificationListState =
  | { status: "loading" }
  | { status: "error" }
  | { status: "ok"; items: Notification[]; nextCursor: string | null; loadingMore: boolean; moreFailed: boolean };

export type NotificationList = {
  state: NotificationListState;
  retry: () => void;
  // loadMore appends the next page ("Show more").
  loadMore: () => void;
  // update changes one item in place, updateAll every item, and remove takes one out.
  update: (id: string, patch: Partial<Notification>) => void;
  updateAll: (patch: Partial<Notification>) => void;
  remove: (id: string) => void;
};

// useNotificationList loads the first page of the caller's notifications and appends the
// next one, by the server's cursor, on "Show more". The cursor is a position in the
// newest-first order, so removing an item here never skips one; items already shown are
// not added twice. A new key (another user) loads again. A notification that arrives on the
// socket goes on top while the page is open.
export function useNotificationList(key: string): NotificationList {
  const [attempt, setAttempt] = useState(0);
  const [state, setState] = useState<NotificationListState>({ status: "loading" });

  useEffect(() => {
    let current = true;
    void listNotifications().then((res) => {
      if (!current) return;
      setState(
        res.ok
          ? { status: "ok", items: res.data.items, nextCursor: res.data.nextCursor, loadingMore: false, moreFailed: false }
          : { status: "error" },
      );
    });
    return () => {
      current = false;
      setState({ status: "loading" });
    };
  }, [key, attempt]);

  useRealtimeEvent((event) => {
    if (event.type !== "notification.created") return;
    setState((prev) => {
      if (prev.status !== "ok" || prev.items.some((n) => n.id === event.data.id)) return prev;
      return { ...prev, items: [event.data, ...prev.items] };
    });
  });

  const loadMore = useCallback(() => {
    if (state.status !== "ok" || state.loadingMore || !state.nextCursor) return;
    setState({ ...state, loadingMore: true, moreFailed: false });
    void listNotifications(state.nextCursor).then((res) =>
      setState((prev) => {
        if (prev.status !== "ok") return prev;
        if (!res.ok) return { ...prev, loadingMore: false, moreFailed: true };
        const seen = new Set(prev.items.map((n) => n.id));
        const added = res.data.items.filter((n) => !seen.has(n.id));
        return { ...prev, items: [...prev.items, ...added], nextCursor: res.data.nextCursor, loadingMore: false };
      }),
    );
  }, [state]);

  const update = useCallback((id: string, patch: Partial<Notification>) => {
    setState((prev) =>
      prev.status === "ok" ? { ...prev, items: prev.items.map((n) => (n.id === id ? { ...n, ...patch } : n)) } : prev,
    );
  }, []);

  const updateAll = useCallback((patch: Partial<Notification>) => {
    setState((prev) => (prev.status === "ok" ? { ...prev, items: prev.items.map((n) => ({ ...n, ...patch })) } : prev));
  }, []);

  const remove = useCallback((id: string) => {
    setState((prev) => (prev.status === "ok" ? { ...prev, items: prev.items.filter((n) => n.id !== id) } : prev));
  }, []);

  return { state, retry: () => setAttempt((n) => n + 1), loadMore, update, updateAll, remove };
}
