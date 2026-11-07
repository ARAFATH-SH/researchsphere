import type { SendResult } from "./api";
import { waitText } from "./format";

export type SendNotice = { text: string; tone: "warning" | "error" };

// Answers that mean the conversation changed under the caller, so its state must be read again.
const stale = new Set(["request_pending", "conversation_closed", "cannot_message", "you_blocked_this_user", "not_found"]);

export function isStale(code: string): boolean {
  return stale.has(code);
}

// sendNotice is the line above the composer for a refused send. Moderation answers keep the
// server's own text, which says until when.
export function sendNotice(res: Extract<SendResult, { ok: false }>): SendNotice {
  switch (res.code) {
    case "rate_limited":
      return { tone: "warning", text: `You're sending messages too quickly. Try again ${waitText(res.retryAfter)}.` };
    case "request_pending":
      return { tone: "warning", text: "You can send more messages once your request is accepted." };
    case "conversation_closed":
      return { tone: "error", text: "This conversation is closed." };
    case "cannot_message":
    case "not_found":
      return { tone: "error", text: "You can't message this person." };
    case "you_blocked_this_user":
      return { tone: "error", text: "You've blocked this person, so you can't message them." };
    default:
      return { tone: "error", text: res.message };
  }
}
