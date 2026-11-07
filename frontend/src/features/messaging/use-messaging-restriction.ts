"use client";

import { useCallback, useEffect, useState } from "react";

import { getCurrentUser } from "@/features/auth/api";
import { useOnConnected } from "@/features/realtime/use-realtime";

// A timer longer than this overflows and would fire at once. A restriction lasts at most 90
// days, so a page open that long reads it again on focus.
const longestTimer = 2 ** 31 - 1;

export type MessagingRestriction = {
  // until is when the restriction ends, null when the user may send.
  until: string | null;
  // refused records the end time of a send the server refused.
  refused: (until: string) => void;
};

// useMessagingRestriction says whether the signed-in user may send (FR-15.6). It reads GET
// /auth/me on load, whenever the window gets the focus and after every socket reconnect, so the
// restricted composer shows before any send and goes away after a lift. A failed read keeps the
// last answer. The restriction also ends by itself at its time, with no read needed.
export function useMessagingRestriction(): MessagingRestriction {
  const [until, setUntil] = useState<string | null>(null);

  const read = useCallback(
    () =>
      getCurrentUser().then((res) => {
        if (res.ok) setUntil(res.data.messagingRestrictedUntil);
      }),
    [],
  );

  useEffect(() => {
    void read();
    const onFocus = () => void read();
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, [read]);

  useOnConnected(() => void read());

  useEffect(() => {
    if (!until) return;
    const timer = setTimeout(() => setUntil(null), Math.min(Math.max(0, new Date(until).getTime() - Date.now()), longestTimer));
    return () => clearTimeout(timer);
  }, [until]);

  return { until, refused: setUntil };
}
