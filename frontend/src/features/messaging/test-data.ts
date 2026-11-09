import type { Conversation, Message } from "./api";

// Conversations and messages shared by the messaging tests, which sign in as Ada.

export const adaID = "0b8f4c1e-0000-4000-8000-000000000001";
export const bobID = "0b8f4c1e-0000-4000-8000-000000000002";
export const conversationID = "4c2f7d3e-0000-4000-8000-0000000000c1";

export function conversation(overrides: Partial<Conversation> = {}): Conversation {
  return {
    id: conversationID,
    kind: "direct",
    group: null,
    state: "active",
    otherMember: { userId: bobID, fullName: "Bob Babbage", photoUrl: null, headline: "Analyst", available: true },
    lastMessage: { senderId: bobID, kind: "text", event: null, body: "See you", deleted: false, createdAt: "2026-11-05T09:00:00Z" },
    unreadCount: 0,
    requestedBy: null,
    lastMessageAt: "2026-11-05T09:00:00Z",
    canMessage: true,
    blockedByMe: false,
    ...overrides,
  };
}

// message builds message number n; a later n has a greater ID, as UUIDv7 IDs do.
export function message(n: number, senderId: string, body = `message ${n}`, createdAt = "2026-11-05T09:00:00Z"): Message {
  return {
    id: `019a5c2e-0000-7000-8000-${String(n).padStart(12, "0")}`,
    conversationId: conversationID,
    senderId,
    kind: "text",
    event: null,
    body,
    createdAt,
    editedAt: null,
    deleted: false,
  };
}
