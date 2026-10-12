"use client";

import { useRouter } from "next/navigation";
import { type ReactNode, useEffect } from "react";

import { useSession } from "./session";

// RequireSession shows signed-in pages only to a signed-in user and sends everyone else
// to /login. It guards the page, not the data: the API refuses requests without a
// valid access token (FR-3.1).
export function RequireSession({ children }: { children: ReactNode }) {
  const session = useSession();
  const router = useRouter();
  useEffect(() => {
    if (session.status === "signedOut") router.replace("/login");
  }, [session.status, router]);

  if (session.status !== "signedIn") {
    return (
      <p role="status" className="sr-only">
        Loading…
      </p>
    );
  }
  return children;
}
