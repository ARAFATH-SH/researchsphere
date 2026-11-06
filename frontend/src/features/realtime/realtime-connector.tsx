"use client";

import { useEffect } from "react";

import { useSession } from "@/features/auth/session";
import { refreshSession, reportSessionLost } from "@/lib/api/client";

import { startSocket, stopSocket } from "./socket";
import { socketUrl } from "./url";

// RealtimeConnector keeps the socket open while someone is signed in; it renders nothing. Only
// the signed-in shell mounts it, so public pages never connect. Signing out closes the socket.
export function RealtimeConnector() {
  const session = useSession();
  const userId = session.status === "signedIn" ? session.user.id : null;
  useEffect(() => {
    if (userId === null) return;
    startSocket({ url: socketUrl(), refresh: refreshSession, onSessionLost: reportSessionLost });
    return stopSocket;
  }, [userId]);
  return null;
}
