import { useSyncExternalStore } from "react";

// The address a verification link was just sent to, kept for this browser tab so the
// "check your email" page can offer a resend. It stays out of the URL, where it would
// end up in history and server logs. Storage can be unavailable (private windows,
// blocked site data); every access is guarded and the pages work without it.
const KEY = "researchsphere.pendingVerificationEmail";

export function savePendingEmail(email: string): void {
  try {
    sessionStorage.setItem(KEY, email);
  } catch {
    // Not stored: the resend form starts empty.
  }
}

function readPendingEmail(): string {
  try {
    return sessionStorage.getItem(KEY) ?? "";
  } catch {
    return "";
  }
}

const subscribe = () => () => {};

// usePendingEmail returns the stored address, or "" on the server and when none is stored.
export function usePendingEmail(): string {
  return useSyncExternalStore(subscribe, readPendingEmail, () => "");
}
