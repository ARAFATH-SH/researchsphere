import { useCallback, useEffect, useRef } from "react";

import { sendEvent } from "@/features/realtime/socket";

// startEvery is the fastest typing.start goes out (the server drops the rest); idleAfter is how
// long a pause counts as having stopped.
export const startEvery = 3_000;
export const idleAfter = 4_000;

export type TypingSender = {
  // input takes the composer's text after each change.
  input: (body: string) => void;
  // stop ends typing, on a send or when the box loses focus.
  stop: () => void;
};

// useTypingSender tells the other member the user is typing (FR-16.4). allowed is the composer
// being open, so requests, declined and blocked conversations and canMessage: false send nothing.
export function useTypingSender(conversationId: string, allowed: boolean): TypingSender {
  const lastStart = useRef<number | null>(null);
  const started = useRef(false);
  const idle = useRef<ReturnType<typeof setTimeout> | null>(null);

  const quiet = useCallback(() => {
    if (idle.current !== null) clearTimeout(idle.current);
    idle.current = null;
    const wasStarted = started.current;
    started.current = false;
    return wasStarted;
  }, []);

  const stop = useCallback(() => {
    if (quiet()) sendEvent("typing.stop", conversationId);
  }, [conversationId, quiet]);

  const input = useCallback(
    (body: string) => {
      if (!allowed) return;
      if (body.trim() === "") {
        stop();
        return;
      }
      const now = Date.now();
      if ((lastStart.current === null || now - lastStart.current >= startEvery) && sendEvent("typing.start", conversationId)) {
        lastStart.current = now;
        started.current = true;
      }
      if (idle.current !== null) clearTimeout(idle.current);
      idle.current = setTimeout(stop, idleAfter);
    },
    [allowed, conversationId, stop],
  );

  // A conversation that closes takes no more frames, and the other side's indicator ends with
  // conversation.updated.
  useEffect(() => {
    if (!allowed) quiet();
  }, [allowed, quiet]);

  useEffect(() => stop, [stop]);

  return { input, stop };
}
