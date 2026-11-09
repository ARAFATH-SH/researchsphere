import type { AccountWarning, MessageReport, MessageReportPerson, ModerationAction, ModerationHistory } from "./api";

// Reports and moderation rows shared by the tests, which sign in as Ada, an administrator.

export const adaID = "0b8f4c1e-0000-4000-8000-000000000001";
export const bobID = "0b8f4c1e-0000-4000-8000-000000000002";
export const reportID = "4c2f7d3e-0000-4000-8000-0000000000a1";

export const cyd: MessageReportPerson = { userId: "0b8f4c1e-0000-4000-8000-000000000003", fullName: "Cyd Charisse", photoUrl: null, headline: null, role: "researcher" };
export const bob: MessageReportPerson = { userId: bobID, fullName: "Bob Babbage", photoUrl: null, headline: null, role: "researcher" };

export function report(overrides: Partial<MessageReport> = {}): MessageReport {
  return {
    id: reportID,
    conversationId: "4c2f7d3e-0000-4000-8000-0000000000c1",
    conversationKind: "direct",
    groupName: null,
    status: "open",
    reason: "harassment",
    details: "Rude messages",
    messageId: null,
    reporter: cyd,
    reportedUser: bob,
    createdAt: "2026-11-05T09:00:00Z",
    outcome: null,
    note: null,
    resolvedAt: null,
    ...overrides,
  };
}

export function warning(overrides: Partial<AccountWarning> = {}): AccountWarning {
  return {
    id: "w1",
    reason: "Repeated spam in your messages",
    createdAt: "2026-11-02T10:00:00",
    acknowledged: false,
    acknowledgedAt: null,
    ...overrides,
  };
}

export function action(overrides: Partial<ModerationAction> = {}): ModerationAction {
  return {
    id: "a1",
    userId: bobID,
    action: "warning",
    reason: "Repeated spam in your messages",
    note: null,
    adminId: adaID,
    adminName: "Ada Lovelace",
    reportId: null,
    report: null,
    warningId: "w1",
    restrictionId: null,
    endsAt: null,
    acknowledgedAt: null,
    liftedAt: null,
    createdAt: "2026-11-02T10:00:00Z",
    ...overrides,
  };
}

export function history(items: ModerationAction[], user: Partial<ModerationHistory["user"]> = {}): ModerationHistory {
  return {
    user: {
      id: bobID,
      fullName: "Bob Babbage",
      email: "bob@example.org",
      role: "researcher",
      verified: true,
      suspended: false,
      suspensionReason: null,
      suspendedUntil: null,
      createdAt: "2026-10-01T09:00:00Z",
      ...user,
    },
    items,
    page: 1,
    pageSize: 20,
    total: items.length,
  };
}
