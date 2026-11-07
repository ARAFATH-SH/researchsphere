import type { MessageReport, ModerationAction, ReportReason } from "./api";

export const reasonLabels: Record<ReportReason, string> = {
  spam: "Spam",
  harassment: "Harassment",
  inappropriate: "Inappropriate",
  other: "Other",
};

export const outcomeLabels: Record<NonNullable<MessageReport["outcome"]>, string> = {
  dismissed: "Dismissed",
  action_taken: "Action taken",
};

export const actionLabels: Record<ModerationAction["action"], string> = {
  warning: "Warning",
  restriction: "Messaging restricted",
  restriction_lifted: "Restriction lifted",
  suspension: "Suspended",
  restoration: "Restored",
};

// isActiveRestriction says whether a history row is a restriction still in force.
export function isActiveRestriction(action: ModerationAction, now = new Date()): boolean {
  return (
    action.action === "restriction" &&
    action.restrictionId !== null &&
    action.liftedAt === null &&
    action.endsAt !== null &&
    new Date(action.endsAt) > now
  );
}
