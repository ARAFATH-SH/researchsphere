"use client";

import { useRouter } from "next/navigation";
import { type ReactNode, useEffect } from "react";

import { noAccessMessage, setAccessNotice } from "./access-notice";
import { useSession } from "./session";

// RequireAdmin shows its children only to an administrator. Anyone else goes to /home
// with a short notice, and the children never render, so they never ask the API for
// admin data. It guards the page, not the data: the API refuses everyone but an
// administrator (FR-3.2, FR-3.3).
export function RequireAdmin({ children }: { children: ReactNode }) {
  const session = useSession();
  const router = useRouter();
  const isAdmin = session.status === "signedIn" && session.user.role === "admin";
  const refused = session.status === "signedIn" && !isAdmin;

  useEffect(() => {
    if (!refused) return;
    setAccessNotice(noAccessMessage);
    router.replace("/home");
  }, [refused, router]);

  if (!isAdmin) {
    return (
      <p role="status" className="sr-only">
        Loading…
      </p>
    );
  }
  return children;
}
