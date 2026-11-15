import { useSyncExternalStore } from "react";

// The search draft is the length of the text in the top bar's search box, shared so the
// search page can show a character counter while the user types (AI-5.1). It holds only a
// number, never the text.

let draftLength = 0;
const listeners = new Set<() => void>();

// setSearchDraftLength stores the length of the text being typed and tells every reader.
export function setSearchDraftLength(length: number) {
  if (length === draftLength) return;
  draftLength = length;
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

const read = () => draftLength;
const readOnServer = () => 0;

// useSearchDraftLength is the length of the text in the top bar's search box.
export function useSearchDraftLength(): number {
  return useSyncExternalStore(subscribe, read, readOnServer);
}
