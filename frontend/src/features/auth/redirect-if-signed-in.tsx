"use client";

import { useRouter } from "next/navigation";
import { type ReactNode, useEffect } from "react";

import { useSession } from "./session";

// RedirectIfSignedIn sends a signed-in user from the sign-in and join pages to /home.
// The page shows at once, so a signed-out visitor never waits for the check.
export function RedirectIfSignedIn({ children }: { children: ReactNode }) {
  const { status } = useSession();
  const router = useRouter();
  useEffect(() => {
    if (status === "signedIn") router.replace("/home");
  }, [status, router]);
  return children;
}
