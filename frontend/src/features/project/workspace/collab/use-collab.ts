"use client";

import { type RefObject, useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";

import { refreshSession, reportSessionLost } from "@/lib/api/client";

import { createCollabTicket, getFile, saveContent } from "../../files/api";
import type { CollabSession } from "./session";
import { type CollabState, initialState, type PresenceFiles } from "./types";

// useCollab keeps one CollabSession per text file opened in this visit (docs/sync.md). Leaving
// a file releases its session, which closes once the server has everything, so the member's
// editor slot frees (FR-12.8); a session with unsent edits stays until they are sent. While any
// file holds unsent edits, closing the tab asks first. Hidden tabs and the page going away
// flush REST saves (fallback); the browser coming back online retries at once. Yjs and the
// provider load only when the first text file opens.
// onGone runs when an open file or its project was deleted (file_gone).
export function useCollab(projectId: string, onGone: RefObject<() => void>) {
  const sessions = useRef(new Map<string, CollabSession>());
  const teardown = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const [presence, setPresence] = useState<PresenceFiles | null>(null);

  const latestPresence = useCallback(() => {
    let last: PresenceFiles | null = null;
    for (const s of sessions.current.values()) {
      const p = s.getState().presence;
      if (p) last = p;
    }
    setPresence(last);
  }, []);

  const open = useCallback(
    async (fileId: string): Promise<CollabSession> => {
      const kept = sessions.current.get(fileId);
      if (kept) {
        kept.reopen();
        return kept;
      }
      const [{ CollabSession }, { hocuspocusLink }] = await Promise.all([import("./session"), import("./link")]);
      const again = sessions.current.get(fileId);
      if (again) {
        again.reopen();
        return again;
      }
      const session = new CollabSession({
        projectId,
        fileId,
        link: hocuspocusLink,
        ticket: createCollabTicket,
        getFile,
        save: (text, version, options) => saveContent(projectId, fileId, text, version, options),
        refresh: refreshSession,
        sessionLost: reportSessionLost,
      });
      sessions.current.set(fileId, session);
      const unsubscribe = session.subscribe(latestPresence);
      session.onDisposed = () => {
        unsubscribe();
        if (sessions.current.get(fileId) === session) sessions.current.delete(fileId);
        latestPresence();
      };
      session.onGone = () => onGone.current();
      session.start();
      return session;
    },
    [projectId, latestPresence, onGone],
  );

  // leave releases a file the member no longer shows.
  const leave = useCallback((fileId: string) => sessions.current.get(fileId)?.release(), []);

  // remove forgets a file that no longer exists; nothing it holds can be stored anywhere.
  const remove = useCallback((fileId: string) => sessions.current.get(fileId)?.dispose(), []);

  useEffect(() => {
    // A development remount (React Strict Mode) keeps the sessions.
    clearTimeout(teardown.current);
    const all = sessions.current;
    const onVisibility = () => {
      if (document.visibilityState === "hidden") all.forEach((s) => void s.flush({ keepalive: true }));
    };
    const onPageHide = () => all.forEach((s) => void s.flush({ keepalive: true }));
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      if ([...all.values()].some((s) => s.hasUnsent())) {
        event.preventDefault();
        // Older browsers show the prompt only when returnValue is set.
        event.returnValue = "";
      }
    };
    const onOnline = () => all.forEach((s) => s.retryNow());
    document.addEventListener("visibilitychange", onVisibility);
    window.addEventListener("pagehide", onPageHide);
    window.addEventListener("beforeunload", onBeforeUnload);
    window.addEventListener("online", onOnline);
    return () => {
      document.removeEventListener("visibilitychange", onVisibility);
      window.removeEventListener("pagehide", onPageHide);
      window.removeEventListener("beforeunload", onBeforeUnload);
      window.removeEventListener("online", onOnline);
      // Leaving the workspace: each file closes once what it holds is sent.
      teardown.current = setTimeout(() => all.forEach((s) => s.release()), 0);
    };
  }, []);

  return { open, leave, remove, presence };
}

const noSubscription = () => () => {};
const getInitial = () => initialState;

// useCollabState follows a session's state for rendering; no session reads as the initial state.
export function useCollabState(session: CollabSession | null): CollabState {
  return useSyncExternalStore(session?.subscribe ?? noSubscription, session?.getState ?? getInitial, session?.getState ?? getInitial);
}
