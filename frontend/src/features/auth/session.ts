import { useSyncExternalStore } from "react";

import { onSessionLost } from "@/lib/api/client";

import { type CurrentUser, getCurrentUser } from "./api";

// The signed-in state of this browser tab. It asks GET /auth/me once, when the first
// component reads it; login, logout and a failed refresh then update it directly.
export type Session =
  | { status: "loading" }
  | { status: "signedIn"; user: CurrentUser }
  | { status: "signedOut" };

const loading: Session = { status: "loading" };
const signedOut: Session = { status: "signedOut" };

let session: Session = loading;
let started = false;
const listeners = new Set<() => void>();

function set(next: Session): void {
  session = next;
  listeners.forEach((listener) => listener());
}

async function load(): Promise<void> {
  const res = await getCurrentUser();
  // Only the first answer counts: login or logout may have set the state meanwhile.
  if (session.status !== "loading") return;
  // Any failure, including a network error, counts as signed out: the login page is
  // where the user can try again.
  set(res.ok ? { status: "signedIn", user: res.data } : signedOut);
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  if (!started) {
    started = true;
    void load();
  }
  return () => listeners.delete(listener);
}

// A refresh that fails ends the session (lib/api/auth-fetch.ts).
onSessionLost(() => set(signedOut));

// useSession returns the session; "loading" on the server and until /auth/me answers.
export function useSession(): Session {
  return useSyncExternalStore(
    subscribe,
    () => session,
    () => loading,
  );
}

export function setSignedIn(user: CurrentUser): void {
  started = true;
  set({ status: "signedIn", user });
}

export function setSignedOut(): void {
  started = true;
  set(signedOut);
}
