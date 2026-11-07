import { useSyncExternalStore } from "react";

const desktopQuery = "(min-width: 641px)";

function subscribe(listener: () => void): () => void {
  const media = window.matchMedia?.(desktopQuery);
  media?.addEventListener("change", listener);
  return () => media?.removeEventListener("change", listener);
}

// useIsDesktop is true from 641px, where the list and the thread sit side by side. CSS alone
// is not enough for this: a thread hidden by CSS would still mark messages as read.
export function useIsDesktop(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => window.matchMedia?.(desktopQuery).matches ?? false,
    () => false,
  );
}
