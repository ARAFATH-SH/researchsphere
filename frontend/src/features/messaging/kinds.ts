import type { Conversation, DirectConversation, GroupConversation } from "./api";

// The two kinds of conversation (FR-16.6). They live apart from api.ts, which tests replace with
// mocks, so the checks stay real.

export function isDirect(c: Conversation): c is DirectConversation {
  return c.kind === "direct" && c.otherMember !== null;
}

export function isGroup(c: Conversation): c is GroupConversation {
  return c.kind === "group" && c.group !== null;
}
