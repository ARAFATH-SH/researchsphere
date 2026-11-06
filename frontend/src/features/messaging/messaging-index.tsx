"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";

import { Note } from "./thread";
import { useConversations } from "./use-conversations";
import { useIsDesktop } from "./use-viewport";

// MessagingIndex is /messaging. On desktop it opens the latest conversation; under 640px the
// list is the whole screen, so nothing opens and nothing is marked as read.
export function MessagingIndex() {
  const router = useRouter();
  const desktop = useIsDesktop();
  const { inbox } = useConversations().boxes;
  const latest = inbox.status === "ok" ? inbox.items[0]?.id : undefined;

  useEffect(() => {
    if (desktop && latest) router.replace(`/messaging/${latest}`);
  }, [desktop, latest, router]);

  return <Note>Select a conversation to read it here.</Note>;
}
