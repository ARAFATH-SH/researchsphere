"use client";

import { useEffect, useState } from "react";

import { Container } from "@/components/Container";

// A note, kept for this browser tab, that the account was just deleted, so the landing
// page can say so once. Storage can be unavailable (private windows, blocked site data);
// every access is guarded and the pages work without it.
const KEY = "researchsphere.accountDeleted";

export function markAccountDeleted(): void {
  try {
    sessionStorage.setItem(KEY, "1");
  } catch {
    // Not stored: the landing page shows no notice.
  }
}

// takeAccountDeleted reports whether the note is there and removes it.
function takeAccountDeleted(): boolean {
  try {
    const found = sessionStorage.getItem(KEY) !== null;
    sessionStorage.removeItem(KEY);
    return found;
  } catch {
    return false;
  }
}

// AccountDeletedNotice says "Your account was deleted" once, after a deletion (FR-2.8).
export function AccountDeletedNotice() {
  const [show, setShow] = useState(false);
  useEffect(() => {
    // Read after mount: the server has no sessionStorage, and the note is removed on read.
    // eslint-disable-next-line react-hooks/set-state-in-effect -- one read of browser storage
    if (takeAccountDeleted()) setShow(true);
  }, []);

  if (!show) return null;
  return (
    <Container size="marketing" className="pt-6">
      <p role="status" className="rounded-lg border border-primary/30 bg-primary-soft p-3 text-sm text-text">
        Your account was deleted.
      </p>
    </Container>
  );
}
