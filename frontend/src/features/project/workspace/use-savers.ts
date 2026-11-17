"use client";

import { useCallback, useEffect, useRef, useSyncExternalStore } from "react";

import { saveContent } from "../files/api";
import { Autosaver, type SaveState } from "./autosave";

// useSavers keeps one Autosaver per text file opened in this visit, so a file left with
// unsaved text (offline, say) keeps it, and keeps retrying, while another file is open. It
// saves every file when the tab is hidden or the page goes away (best effort, keepalive) and
// retries at once when the browser is back online. While a file has unsaved changes, closing
// the tab asks first. Leaving the workspace inside the app saves what is left, then stops.
export function useSavers(projectId: string) {
  const savers = useRef(new Map<string, Autosaver>());

  const get = useCallback((fileId: string) => savers.current.get(fileId), []);

  const create = useCallback(
    (fileId: string, content: string, version: number) => {
      const saver = new Autosaver(fileId, content, version, (text, v, options) =>
        saveContent(projectId, fileId, text, v, options),
      );
      savers.current.set(fileId, saver);
      return saver;
    },
    [projectId],
  );

  // remove forgets a file that no longer exists; its unsaved text cannot be stored anywhere.
  const remove = useCallback((fileId: string) => {
    savers.current.get(fileId)?.dispose();
    savers.current.delete(fileId);
  }, []);

  useEffect(() => {
    const all = savers.current;
    const flushAll = () => all.forEach((s) => void s.flush({ keepalive: true }));
    const onVisibility = () => {
      if (document.visibilityState === "hidden") flushAll();
    };
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      if ([...all.values()].some((s) => s.dirty)) {
        event.preventDefault();
        // Older browsers show the prompt only when returnValue is set.
        event.returnValue = "";
      }
    };
    const onOnline = () => all.forEach((s) => s.retryNow());
    document.addEventListener("visibilitychange", onVisibility);
    window.addEventListener("pagehide", flushAll);
    window.addEventListener("beforeunload", onBeforeUnload);
    window.addEventListener("online", onOnline);
    return () => {
      document.removeEventListener("visibilitychange", onVisibility);
      window.removeEventListener("pagehide", flushAll);
      window.removeEventListener("beforeunload", onBeforeUnload);
      window.removeEventListener("online", onOnline);
      all.forEach((s) => void s.flush().finally(() => s.dispose()));
      all.clear();
    };
  }, []);

  return { get, create, remove };
}

const savedState: SaveState = { status: "saved" };
const noSubscription = () => () => {};
const getSaved = () => savedState;

// useSaveState follows a saver's status for rendering; no saver reads as saved.
export function useSaveState(saver: Autosaver | null): SaveState {
  return useSyncExternalStore(saver?.subscribe ?? noSubscription, saver?.getState ?? getSaved, saver?.getState ?? getSaved);
}
