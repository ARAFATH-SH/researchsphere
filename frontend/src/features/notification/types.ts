import type { Notification } from "./api";

// The type registry: the sentence each notification type reads as. A type the server
// sends that is not here (one a later slice adds) is skipped rather than shown broken.
const sentences: Partial<Record<string, (name: string) => string>> = {
  connection_request_received: (name) => `${name} sent you a connection request.`,
  connection_request_accepted: (name) => `${name} accepted your connection request.`,
  account_warning: () => "You received a warning from ResearchSphere. Tap to read it.",
  attachment_blocked: () => "A file you sent was removed because it failed a security check.",
};

// senderName is who a notification is from: its actor, or ResearchSphere for a system
// notification, which has none.
export function senderName(n: Notification): string {
  return n.actor?.fullName ?? "ResearchSphere";
}

// sentenceFor returns the notification's sentence, or null for an unknown type.
export function sentenceFor(n: Notification): string | null {
  const sentence = sentences[n.type as string];
  return sentence ? sentence(senderName(n)) : null;
}

// hasPendingRequest reports whether the notification is a received connection request
// that can still be accepted or ignored.
export function hasPendingRequest(n: Notification): n is Notification & { requestId: string } {
  return n.type === "connection_request_received" && n.requestPending === true && !!n.requestId;
}
