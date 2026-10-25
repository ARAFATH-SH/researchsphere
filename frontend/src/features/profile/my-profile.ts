import { useEffect, useSyncExternalStore } from "react";

import { useSession } from "@/features/auth/session";

import { getMyProfile, type ProfileResult } from "./api";

// The signed-in user's own profile, shared by every place that shows it: the Me menu in
// the top bar, the home profile card and the own profile page. GET /auth/me carries no
// photo, so the top bar reads it from here. After an edit, reloadMyProfile fetches it
// again and every reader updates without a page reload.

// entry is the last answer and whose profile it is: signing out and in as someone else
// in the same tab must not show the previous user's profile.
type Entry = { userId: string; result: ProfileResult };

let entry: Entry | null = null;
// loadingFor is the user whose profile is being fetched, so readers do not ask twice.
let loadingFor: string | null = null;
// generation grows with every fetch; only the latest one may write its answer.
let generation = 0;
const listeners = new Set<() => void>();

function set(next: Entry | null): void {
  entry = next;
  listeners.forEach((listener) => listener());
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

// reloadMyProfile fetches userId's own profile again and shares the answer.
export async function reloadMyProfile(userId: string): Promise<ProfileResult> {
  const mine = ++generation;
  loadingFor = userId;
  const result = await getMyProfile();
  if (mine === generation) {
    loadingFor = null;
    set({ userId, result });
  }
  return result;
}

// useMyProfile returns the signed-in user's own profile; null while it loads or when
// nobody is signed in. The first reader starts the request.
export function useMyProfile(): ProfileResult | null {
  const session = useSession();
  const userId = session.status === "signedIn" ? session.user.id : null;
  const current = useSyncExternalStore(
    subscribe,
    () => entry,
    () => null,
  );
  const stale = userId !== null && current?.userId !== userId;
  useEffect(() => {
    if (userId !== null && stale && loadingFor !== userId)
      void reloadMyProfile(userId);
  }, [stale, userId]);
  return userId !== null && current?.userId === userId ? current.result : null;
}

// clearMyProfile forgets the shared profile. Tests call it between cases.
export function clearMyProfile(): void {
  generation++;
  loadingFor = null;
  set(null);
}
