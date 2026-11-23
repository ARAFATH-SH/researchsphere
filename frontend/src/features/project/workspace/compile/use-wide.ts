import { useSyncExternalStore } from "react";

const wideQuery = "(min-width: 1024px)";

function subscribe(listener: () => void): () => void {
  const media = window.matchMedia?.(wideQuery);
  media?.addEventListener("change", listener);
  return () => media?.removeEventListener("change", listener);
}

// useWide is true from 1024px, where the editor and the PDF sit side by side; below it the
// workspace switches between them with tabs. Not CSS alone: a hidden PDF should not render.
export function useWide(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => window.matchMedia?.(wideQuery).matches ?? true,
    () => true,
  );
}
