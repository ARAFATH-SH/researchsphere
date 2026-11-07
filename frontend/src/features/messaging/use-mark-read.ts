import { useEffect, useRef, useSyncExternalStore } from "react";

import { markConversationRead } from "./api";

function subscribe(listener: () => void): () => void {
  window.addEventListener("focus", listener);
  window.addEventListener("blur", listener);
  document.addEventListener("visibilitychange", listener);
  return () => {
    window.removeEventListener("focus", listener);
    window.removeEventListener("blur", listener);
    document.removeEventListener("visibilitychange", listener);
  };
}

const isActive = () => document.visibilityState === "visible" && document.hasFocus();

// useMarkRead sends the read marker for messageId once, as soon as the window is focused and
// its tab visible (FR-16.3). A null messageId sends nothing. A failed send is tried again the
// next time the window comes back; the marker only moves forward, so that is safe.
export function useMarkRead(conversationId: string, messageId: string | null, onRead: () => void): void {
  const active = useSyncExternalStore(subscribe, isActive, () => false);
  const marked = useRef<string | null>(null);
  const latest = useRef(onRead);
  useEffect(() => {
    latest.current = onRead;
  });

  useEffect(() => {
    if (!active || !messageId || marked.current === messageId) return;
    marked.current = messageId;
    void markConversationRead(conversationId, messageId).then((res) => {
      if (res.ok) latest.current();
      else if (marked.current === messageId) marked.current = null;
    });
  }, [active, conversationId, messageId]);
}
