import type { components } from "@/lib/api/client";

type Schemas = components["schemas"];

// RealtimeEvent is one server frame (docs/realtime.md).
export type RealtimeEvent =
  | { type: "message.created"; data: Schemas["RealtimeMessageCreated"] }
  | { type: "conversation.read"; data: Schemas["RealtimeConversationRead"] }
  | { type: "conversation.updated"; data: Schemas["RealtimeConversationUpdated"] }
  | { type: "unread.changed"; data: Schemas["MessageUnreadCount"] }
  | { type: "typing"; data: Schemas["RealtimeTyping"] }
  | { type: "presence"; data: Schemas["PresenceStatus"] }
  | { type: "notification.created"; data: Schemas["Notification"] }
  | { type: "notifications.unread"; data: Schemas["UnreadNotificationCount"] };

const types: ReadonlySet<string> = new Set<RealtimeEvent["type"]>([
  "message.created",
  "conversation.read",
  "conversation.updated",
  "unread.changed",
  "typing",
  "presence",
  "notification.created",
  "notifications.unread",
]);

// parseEvent reads a frame, or returns null for one that is not JSON or not a known event, so
// a newer server cannot break an older client.
export function parseEvent(frame: unknown): RealtimeEvent | null {
  if (typeof frame !== "string") return null;
  try {
    const event = JSON.parse(frame) as { type?: unknown; data?: unknown } | null;
    if (typeof event?.type !== "string" || !types.has(event.type) || typeof event.data !== "object" || !event.data) return null;
    return event as RealtimeEvent;
  } catch {
    return null;
  }
}
