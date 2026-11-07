import Link from "next/link";

import type { Profile } from "@/features/profile/api";
import { pillClass, type PillKind } from "@/features/social/connect-button";

type MessageAction = { label: string; href: string; kind: Extract<PillKind, "filled" | "outline"> };

// messageAction is the profile's message button for me, or null when it has none: on my own
// profile, for someone I blocked, and in a conversation I can no longer use (FR-16.1,
// FR-16.2, FR-16.9). People who are suspended or have blocked me have no profile to show.
export function messageAction(profile: Profile, connected: boolean, me: string | null): MessageAction | null {
  if (profile.blockedByMe || profile.userId === me) return null;
  const { conversation } = profile;
  if (!conversation) {
    return {
      label: connected ? "Message" : "Message request",
      href: `/messaging/new?to=${profile.userId}`,
      kind: connected ? "filled" : "outline",
    };
  }
  const href = `/messaging/${conversation.id}`;
  if (conversation.state === "request") {
    const mine = conversation.requestedBy === me;
    return { label: mine ? "Message request sent" : "Reply to request", href, kind: "outline" };
  }
  if (conversation.state === "active" && conversation.canMessage) return { label: "Message", href, kind: "filled" };
  return null;
}

// MessageButton is the 40px pill of the profile's action row.
export function MessageButton({ action }: { action: MessageAction }) {
  return (
    <Link href={action.href} className={pillClass(action.kind, "profile")}>
      {action.label}
    </Link>
  );
}
