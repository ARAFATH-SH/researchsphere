import { shortDate } from "@/features/social/format";

import type { Attachment, Conversation, MessageReader, SystemEvent } from "./api";
import { eventPreview, firstName } from "./system-events";

// maxBody is the longest message, in characters after trimming; counterFrom is where the
// composer starts showing the count.
export const maxBody = 5000;
export const counterFrom = 4500;

// editWindow is how long after sending a message can be edited: the server's default
// MESSAGE_EDIT_WINDOW (FR-16.5). The server decides; a 409 covers a different setting or clock.
export const editWindow = 15 * 60_000;

// editableUntil is when a message sent at createdAt stops being editable, in milliseconds.
export function editableUntil(createdAt: string): number {
  return Date.parse(createdAt) + editWindow;
}

const weekdays = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

// bodyLength counts code points, as the server does, so an emoji counts once.
export function bodyLength(body: string): number {
  return [...body.trim()].length;
}

function daysAgo(then: Date, now: Date): number {
  const day = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  return Math.round((day(now) - day(then)) / 86_400_000);
}

// clockTime is a time of day as the design writes it: "6:02 PM".
export function clockTime(iso: string): string {
  return new Date(iso).toLocaleTimeString("en-US", { hour: "numeric", minute: "2-digit" });
}

// rowTime is a list row's time, as the design writes it: "6:11 PM" today, "Mon" within the
// week, then "3 Oct", with the year when it is another year.
export function rowTime(iso: string, now = new Date()): string {
  const then = new Date(iso);
  const days = daysAgo(then, now);
  if (days <= 0) return clockTime(iso);
  if (days < 7) return weekdays[then.getDay()];
  const date = shortDate(then);
  return then.getFullYear() === now.getFullYear() ? date.slice(0, date.lastIndexOf(" ")) : date;
}

// dayLabel is a date separator: "Today", "Yesterday", then "3 Oct 2026".
export function dayLabel(iso: string, now = new Date()): string {
  const then = new Date(iso);
  const days = daysAgo(then, now);
  if (days <= 0) return "Today";
  if (days === 1) return "Yesterday";
  return shortDate(then);
}

// waitText says how long a rate limit lasts: "in 30 seconds", "in a minute", "in 5 minutes".
export function waitText(seconds?: number): string {
  if (!seconds || seconds <= 0) return "in a minute";
  if (seconds < 60) return `in ${seconds} ${seconds === 1 ? "second" : "seconds"}`;
  const minutes = Math.ceil(seconds / 60);
  if (minutes === 1) return "in a minute";
  if (minutes < 120) return `in ${minutes} minutes`;
  return `in ${Math.ceil(minutes / 60)} hours`;
}

// rowPreview is the line under a row's name: who is waiting for a request, otherwise the
// last message, with "You: " before the caller's own.
export function rowPreview(conversation: Conversation, me: string): string {
  if (conversation.state === "request") {
    return conversation.requestedBy === me ? "Request sent" : "Wants to message you";
  }
  const last = conversation.lastMessage;
  if (!last) return "";
  // Only deleted marks a deletion: a message with files may have no text (FR-16.5, FR-16.7).
  if (last.deleted) return "Message deleted";
  if (last.kind === "system" && last.event) return eventPreview(last.event, last.senderId, last.senderName, me);
  const mine = last.senderId === me;
  // In a group a row names who wrote (FR-16.6): "Nafisa: …".
  const who = conversation.kind === "group" && !mine && last.senderName ? firstName(last.senderName) : null;
  const text = last.body ?? "";
  if (!text && last.attachmentCount > 0) {
    const files = last.attachmentCount === 1 ? "a file" : `${last.attachmentCount} files`;
    if (mine) return `You sent ${files}`;
    return who ? `${who} sent ${files}` : `Sent ${files}`;
  }
  if (mine) return `You: ${text}`;
  return who ? `${who}: ${text}` : text;
}

// ThreadStatus is what the caller may do in a conversation; open is the only one that sends.
export type ThreadStatus = "open" | "waiting" | "received" | "closed" | "readOnly" | "blocked";

export function threadStatus(conversation: Conversation, me: string): ThreadStatus {
  // A block comes first, as the caller can do something about it. A block by the other person
  // is never told: it reads as readOnly.
  if (conversation.blockedByMe) return "blocked";
  if (conversation.state === "declined") return "closed";
  if (conversation.state === "request") return conversation.requestedBy === me ? "waiting" : "received";
  return conversation.canMessage ? "open" : "readOnly";
}

// ThreadMessage is a stored message or one on its way. key stays the same when a message
// goes from sending to sent, so its row is not replaced; id is null until it is stored.
export type ThreadMessage = {
  key: string;
  id: string | null;
  // senderId is empty for a system message with no actor (an ownership hand-over).
  senderId: string;
  // event is set for a group's system message, which is shown as a line of its own.
  event: SystemEvent | null;
  body: string;
  createdAt: string;
  editedAt: string | null;
  // deleted comes from the message's flag, never from an empty body.
  deleted: boolean;
  attachments: Attachment[];
  delivery: "sent" | "sending" | "failed";
};

// MessageGroup is a run of messages by one sender, or one system message (system).
export type MessageGroup = { key: string; senderId: string; system: boolean; messages: ThreadMessage[] };
export type MessageDay = { key: string; label: string; groups: MessageGroup[] };

// groupMessages arranges messages, oldest first, by day and, within a day, into runs by the
// same sender. A system message stands alone and ends a run, so the sender is named again after
// it.
export function groupMessages(messages: ThreadMessage[], now = new Date()): MessageDay[] {
  const days: MessageDay[] = [];
  for (const message of messages) {
    const label = dayLabel(message.createdAt, now);
    let day = days.at(-1);
    if (!day || day.label !== label) {
      day = { key: message.key, label, groups: [] };
      days.push(day);
    }
    const group = day.groups.at(-1);
    const system = message.event !== null;
    if (group && !group.system && !system && group.senderId === message.senderId) group.messages.push(message);
    else day.groups.push({ key: message.key, senderId: message.senderId, system, messages: [message] });
  }
  return days;
}

// SeenBy is "Seen by N" under the caller's latest message others have read in a group
// (FR-16.3): the message and who read it.
export type SeenBy = { messageId: string; userIds: string[] };

// seenBy finds the caller's newest stored message that at least one other member has read,
// with every member whose marker is at or after it. Message IDs are UUIDv7, so a later message
// has a greater ID.
export function seenBy(messages: ThreadMessage[], me: string, readers: MessageReader[] | null): SeenBy | null {
  if (!readers || readers.length === 0) return null;
  for (let i = messages.length - 1; i >= 0; i--) {
    const { id, senderId, event } = messages[i];
    if (!id || senderId !== me || event) continue;
    const userIds = readers.filter((r) => r.lastReadMessageId !== null && r.lastReadMessageId >= id).map((r) => r.userId);
    if (userIds.length > 0) return { messageId: id, userIds };
  }
  return null;
}

// seenMessageId is the caller's latest stored message the other member has read: message
// IDs are UUIDv7, so a later message has a greater ID. Only an active conversation has Seen:
// a request is not read until it is accepted (FR-16.2), and a declined one never is.
export function seenMessageId(
  messages: ThreadMessage[],
  me: string,
  otherRead: string | null,
  conversation: Conversation,
): string | null {
  if (conversation.state !== "active" || !otherRead) return null;
  for (let i = messages.length - 1; i >= 0; i--) {
    const { id, senderId } = messages[i];
    if (id && senderId === me && id <= otherRead) return id;
  }
  return null;
}
