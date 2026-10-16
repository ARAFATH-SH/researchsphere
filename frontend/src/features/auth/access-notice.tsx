"use client";

import { useEffect, useState } from "react";

// A message for the next page, kept in memory for this browser tab only. A guard sets
// it before redirecting; the page it lands on shows it once.
let pending: string | null = null;

export const noAccessMessage = "You don't have access to that page.";

// setAccessNotice keeps message for the next page that shows an AccessNotice.
export function setAccessNotice(message: string): void {
  pending = message;
}

// AccessNotice shows the pending message once and forgets it, so it does not come back
// when the user returns to the page later.
export function AccessNotice() {
  const [message] = useState(() => pending);
  useEffect(() => {
    pending = null;
  }, []);
  if (!message) return null;
  return (
    <p role="status" className="mb-4 rounded-lg border border-border bg-primary-soft px-4 py-3 text-sm text-text">
      {message}
    </p>
  );
}
