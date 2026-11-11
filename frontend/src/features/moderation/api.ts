import { call, type Result, unexpectedError } from "@/lib/api/call";
import { api, type components } from "@/lib/api/client";

export type AccountWarning = components["schemas"]["AccountWarning"];
export type MessageReport = components["schemas"]["MessageReport"];
export type MessageReportDetail = components["schemas"]["MessageReportDetail"];
export type MessageReportPage = components["schemas"]["MessageReportPage"];
export type MessageReportPerson = components["schemas"]["MessageReportPerson"];
export type ReportedMessage = components["schemas"]["ReportedMessage"];
export type ModerationAction = components["schemas"]["ModerationAction"];
export type ModerationHistory = components["schemas"]["ModerationHistory"];
export type ReportReason = components["schemas"]["ReportReason"];

export type ReportStatus = "open" | "resolved";

// ActionInput is what every action takes: the reason the user sees and, for administrators
// only, a note. reportId resolves that report with the action.
export type ActionInput = { reason: string; note?: string; reportId?: string };

function withData<T>(res: Result<T | undefined>): Result<T> {
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

function done<T>(res: Result<T | undefined>): Result<undefined> {
  return res.ok ? { ok: true, data: undefined } : res;
}

// listPendingWarnings returns the warnings the caller has not acknowledged (FR-15.5).
export async function listPendingWarnings(): Promise<Result<AccountWarning[]>> {
  const res = withData(await call(() => api.GET("/me/warnings/pending")));
  return res.ok ? { ok: true, data: res.data.items } : res;
}

// getMyWarning returns one of the caller's warnings; any other ID is not_found.
export async function getMyWarning(warningId: string): Promise<Result<AccountWarning>> {
  return withData(await call(() => api.GET("/me/warnings/{warningId}", { params: { path: { warningId } } })));
}

// acknowledgeWarning clears a warning from the banner. Doing it twice keeps the first time.
export async function acknowledgeWarning(warningId: string): Promise<Result<undefined>> {
  return done(await call(() => api.POST("/me/warnings/{warningId}/acknowledge", { params: { path: { warningId } } })));
}

// listMessageReports returns one page of reports, newest first (FR-16.9).
export async function listMessageReports(status: ReportStatus, page: number): Promise<Result<MessageReportPage>> {
  return withData(await call(() => api.GET("/admin/message-reports", { params: { query: { status, page } } })));
}

// getMessageReport returns a report and one page of its conversation, newest first and none
// later than the report (NFR-13). Every request is recorded.
export async function getMessageReport(reportId: string, before?: string): Promise<Result<MessageReportDetail>> {
  return withData(
    await call(() =>
      api.GET("/admin/message-reports/{reportId}", { params: { path: { reportId }, query: before ? { before } : {} } }),
    ),
  );
}

// dismissMessageReport closes a report with no consequence. No user sees it, so it has a note
// and no reason.
export async function dismissMessageReport(reportId: string, note?: string): Promise<Result<MessageReport>> {
  return withData(
    await call(() =>
      api.POST("/admin/message-reports/{reportId}/resolve", {
        params: { path: { reportId } },
        body: { outcome: "dismissed", ...(note ? { note } : {}) },
      }),
    ),
  );
}

// warnUser warns a user (FR-15.5).
export async function warnUser(userId: string, body: ActionInput): Promise<Result<ModerationAction>> {
  return withData(await call(() => api.POST("/admin/users/{userId}/warnings", { params: { path: { userId } }, body })));
}

// restrictUser stops a user sending messages for 1 to 90 days (FR-15.6).
export async function restrictUser(userId: string, days: number, input: ActionInput): Promise<Result<ModerationAction>> {
  return withData(
    await call(() =>
      api.POST("/admin/users/{userId}/restrictions", {
        params: { path: { userId } },
        body: { kind: "messaging", days, ...input },
      }),
    ),
  );
}

// suspendUser suspends an account until a time, or for good without one (FR-15.7).
export async function suspendUser(userId: string, until: string | null, input: ActionInput): Promise<Result<ModerationAction>> {
  return withData(
    await call(() =>
      api.POST("/admin/users/{userId}/suspension", {
        params: { path: { userId } },
        body: { ...(until ? { until } : {}), ...input },
      }),
    ),
  );
}

// liftRestriction ends a restriction early (FR-15.6).
export async function liftRestriction(restrictionId: string, body: ActionInput): Promise<Result<undefined>> {
  return done(
    await call(() =>
      api.POST("/admin/restrictions/{restrictionId}/lift", { params: { path: { restrictionId } }, body }),
    ),
  );
}

// restoreUser ends a suspension (FR-15.3).
export async function restoreUser(userId: string, body: ActionInput): Promise<Result<ModerationAction>> {
  return withData(await call(() => api.POST("/admin/users/{userId}/restore", { params: { path: { userId } }, body })));
}

// getUserModeration returns a user's account and one page of their history, newest first
// (FR-15.9).
export async function getUserModeration(userId: string, page: number): Promise<Result<ModerationHistory>> {
  return withData(
    await call(() => api.GET("/admin/users/{userId}/moderation", { params: { path: { userId }, query: { page } } })),
  );
}
