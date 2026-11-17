import type { Notification } from "./api";

// The type registry: the sentence each notification type reads as. A type the server
// sends that is not here (one a later slice adds) is skipped rather than shown broken.
// group_added names the group by its current name, the notification's subject, or says "a
// group" when the subject is gone or hidden (FR-16.6); the project types name the project the
// same way.
const sentences: Partial<Record<string, (name: string, n: Notification) => string>> = {
  connection_request_received: (name) => `${name} sent you a connection request.`,
  connection_request_accepted: (name) => `${name} accepted your connection request.`,
  account_warning: () => "You received a warning from ResearchSphere. Tap to read it.",
  attachment_blocked: () => "A file you sent was removed because it failed a security check.",
  group_added: (name, n) => (n.subject ? `${name} added you to ${n.subject.title}.` : `${name} added you to a group.`),
  project_join_request: (name, n) => `${name} asked to join ${projectName(n)}.`,
  project_join_request_accepted: (name, n) => `${name} accepted your request to join ${projectName(n)}.`,
  project_join_request_rejected: (name, n) => `${name} declined your request to join ${projectName(n)}.`,
  project_invitation: (name, n) => `${name} invited you to join ${projectName(n)}.`,
  project_invitation_accepted: (name, n) => `${name} accepted your invitation to ${projectName(n)}.`,
  project_invitation_rejected: (name, n) => `${name} declined your invitation to ${projectName(n)}.`,
  project_member_role_changed: (name, n) => `${name} changed your role in ${projectName(n)}.`,
  project_member_removed: (name, n) => `${name} removed you from ${projectName(n)}.`,
  project_ownership_transferred: (name, n) => `${name} made you the owner of ${projectName(n)}.`,
};

// projectName names a project notification's subject by its current title, or says "a
// project" when the subject is gone or hidden (FR-9.2).
function projectName(n: Notification): string {
  return n.subject ? n.subject.title : "a project";
}

// senderName is who a notification is from: its actor, or ResearchSphere for a system
// notification, which has none.
export function senderName(n: Notification): string {
  return n.actor?.fullName ?? "ResearchSphere";
}

// sentenceFor returns the notification's sentence, or null for an unknown type.
export function sentenceFor(n: Notification): string | null {
  const sentence = sentences[n.type as string];
  return sentence ? sentence(senderName(n), n) : null;
}

// hasPendingRequest reports whether the notification is a received connection request
// that can still be accepted or ignored.
export function hasPendingRequest(n: Notification): n is Notification & { requestId: string } {
  return n.type === "connection_request_received" && n.requestPending === true && !!n.requestId;
}

// joinRequestsAnchor is the id of the owner's Join requests card on a project page.
export const joinRequestsAnchor = "join-requests";

// linkFor is where a notification leads: the server's link, except that a join request takes
// the owner straight to the project's Join requests card (FR-11.2).
export function linkFor(n: Notification): string {
  return n.type === "project_join_request" ? `${n.link}#${joinRequestsAnchor}` : n.link;
}

// projectIdOf is the project a project notification is about: its subject, or the project in
// its link when the subject is hidden; null for other types.
export function projectIdOf(n: Notification): string | null {
  if (!(n.type as string).startsWith("project_")) return null;
  if (n.subject?.type === "project") return n.subject.id;
  const m = /^\/projects\/([^/#?]+)/.exec(n.link);
  return m ? m[1] : null;
}
