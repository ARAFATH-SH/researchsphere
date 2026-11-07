// What the socket does after a close (docs/realtime.md, Close codes): refresh first, stay away
// until the tab is shown again, or simply try again with backoff.
export type CloseAction = "refresh" | "handshake" | "replaced" | "retry";

// closeAction reads a close. A socket that never opened is a refused handshake: a browser hides
// its status and reports 1006, so it may be an expired token, or the server may be down.
export function closeAction(code: number, opened: boolean): CloseAction {
  if (!opened) return "handshake";
  if (code === 4401 || code === 4403) return "refresh";
  if (code === 4409) return "replaced";
  return "retry";
}

export const firstDelay = 1_000;
export const maxDelay = 30_000;

// backoffDelay is the wait before attempt n, from 0: it doubles up to 30 s, and jitter takes up
// to half of it, so clients dropped together do not come back together.
export function backoffDelay(attempt: number, random = Math.random()): number {
  return Math.min(maxDelay, firstDelay * 2 ** attempt) * (0.5 + random / 2);
}
