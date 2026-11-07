"use client";

import Link from "next/link";
import { useEffect } from "react";

import { Container } from "@/components/Container";
import { useSession } from "@/features/auth/session";
import { useOnConnected, useRealtimeEvent } from "@/features/realtime/use-realtime";

import { clearPendingWarnings, refreshPendingWarnings, usePendingWarnings } from "./pending-warnings";

// WarningBanner shows under the top bar on every signed-in page while the user has a warning
// they have not acknowledged (FR-15.5). It links to the newest; the next one takes its place.
export function WarningBanner() {
  const session = useSession();
  const userId = session.status === "signedIn" ? session.user.id : null;
  const warnings = usePendingWarnings();

  useEffect(() => {
    if (!userId) {
      clearPendingWarnings();
      return;
    }
    void refreshPendingWarnings();
    const onFocus = () => void refreshPendingWarnings();
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, [userId]);

  useOnConnected(() => {
    if (userId) void refreshPendingWarnings();
  });
  useRealtimeEvent((event) => {
    if (userId && event.type === "notification.created" && event.data.type === "account_warning") {
      void refreshPendingWarnings();
    }
  });

  const newest = warnings[0];
  if (!userId || !newest) return null;
  return (
    <div role="status" className="border-b border-rs-warning-line bg-rs-warning-soft text-rs-warning">
      <Container className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 py-2.5">
        <p className="text-rs-body">You received a warning from ResearchSphere.</p>
        <Link href={`/warnings/${newest.id}`} className="text-rs-body font-medium underline hover:no-underline focus-visible:shadow-focus focus-visible:outline-none">
          Read it
        </Link>
      </Container>
    </div>
  );
}
