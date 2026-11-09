import type { Conversation, GroupConversation, GroupMember, Message, MessagePage, SystemEvent } from "./api";

// Conversations and messages shared by the messaging tests, which sign in as Ada.

export const adaID = "0b8f4c1e-0000-4000-8000-000000000001";
export const bobID = "0b8f4c1e-0000-4000-8000-000000000002";
export const nafisaID = "0b8f4c1e-0000-4000-8000-000000000003";
export const rahimID = "0b8f4c1e-0000-4000-8000-000000000004";
export const conversationID = "4c2f7d3e-0000-4000-8000-0000000000c1";
export const groupID = "4c2f7d3e-0000-4000-8000-0000000000a1";

export function conversation(overrides: Partial<Conversation> = {}): Conversation {
  return {
    id: conversationID,
    kind: "direct",
    group: null,
    state: "active",
    otherMember: { userId: bobID, fullName: "Bob Babbage", photoUrl: null, headline: "Analyst", available: true },
    lastMessage: {
      senderId: bobID,
      senderName: "Bob Babbage",
      kind: "text",
      event: null,
      body: "See you",
      deleted: false,
      attachmentCount: 0,
      createdAt: "2026-11-05T09:00:00Z",
    },
    unreadCount: 0,
    requestedBy: null,
    lastMessageAt: "2026-11-05T09:00:00Z",
    canMessage: true,
    blockedByMe: false,
    ...overrides,
  };
}

export function member(userId: string, fullName: string, role: GroupMember["role"] = "member", joinedAt = "2026-11-01T09:00:00Z"): GroupMember {
  return { userId, fullName, photoUrl: null, role, joinedAt, available: true };
}

// group is a group of Ada (the owner unless myRole says otherwise), Nafisa and Rahim.
export function group(overrides: Partial<Conversation> = {}, myRole: GroupMember["role"] = "owner"): GroupConversation {
  const members = [
    member(adaID, "Ada Lovelace", myRole === "owner" ? "owner" : "member", "2026-11-01T09:00:00Z"),
    member(nafisaID, "Nafisa Hasan", myRole === "owner" ? "member" : "owner", "2026-11-01T09:00:00Z"),
    member(rahimID, "Rahim Uddin", "member", "2026-11-02T09:00:00Z"),
  ];
  return {
    id: groupID,
    kind: "group",
    otherMember: null,
    group: {
      name: "CVS paper team",
      memberCount: 3,
      avatars: [
        { userId: nafisaID, fullName: "Nafisa Hasan", photoUrl: null },
        { userId: rahimID, fullName: "Rahim Uddin", photoUrl: null },
      ],
      myRole,
      members,
    },
    state: "active",
    lastMessage: {
      senderId: nafisaID,
      senderName: "Nafisa Hasan",
      kind: "text",
      event: null,
      body: "I'll check the references",
      deleted: false,
      attachmentCount: 0,
      createdAt: "2026-11-05T09:00:00Z",
    },
    unreadCount: 0,
    requestedBy: null,
    lastMessageAt: "2026-11-05T09:00:00Z",
    canMessage: true,
    blockedByMe: false,
    ...overrides,
  } as GroupConversation;
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
    attachments: [],
  };
}

// systemMessage builds a group's system message number n; actorId is its sender.
export function systemMessage(n: number, actorId: string | null, event: Omit<SystemEvent, "actorId">, createdAt = "2026-11-05T09:00:00Z"): Message {
  return {
    ...message(n, actorId ?? "", "", createdAt),
    conversationId: groupID,
    senderId: actorId,
    kind: "system",
    event: { ...event, actorId },
    body: null,
  };
}

// page is a page of messages, newest first, with no more in either direction.
export function page(items: Message[], overrides: Partial<MessagePage> = {}): MessagePage {
  return { items, nextBefore: null, nextAfter: null, otherLastReadMessageId: null, readers: null, people: {}, ...overrides };
}
