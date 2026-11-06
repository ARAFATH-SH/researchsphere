"use client";

import { useEffect, useState } from "react";

import { useSocketStatus } from "@/features/realtime/socket";

// noticeAfter lets the short gap of a routine reconnect pass unseen.
export const noticeAfter = 3_000;

// ReconnectingNotice says the page is not live while the socket has been down for a while. A tab
// replaced by a newer socket (4409) is not reconnecting, so it stays quiet.
export function ReconnectingNotice() {
  const status = useSocketStatus();
  const down = status === "connecting" || status === "waiting";
  const [late, setLate] = useState(false);

  useEffect(() => {
    if (!down) return;
    const timer = setTimeout(() => setLate(true), noticeAfter);
    return () => {
      clearTimeout(timer);
      setLate(false);
    };
  }, [down]);

  return (
    <div role="status">
      {late && <p className="border-b border-rs-warning-line bg-rs-warning-soft px-3 py-1 text-center text-rs-caption text-rs-warning">Reconnecting…</p>}
    </div>
  );
}
