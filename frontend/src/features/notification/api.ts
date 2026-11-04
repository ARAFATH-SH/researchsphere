import { call, type Result, unexpectedError } from "@/lib/api/call";
import { api, type components } from "@/lib/api/client";

export type Notification = components["schemas"]["Notification"];
export type NotificationPage = components["schemas"]["NotificationPage"];

// listNotifications returns one page of the caller's notifications, newest first (FR-9.5).
// Leave cursor out for the first page; pass the previous page's nextCursor after that.
export async function listNotifications(cursor?: string): Promise<Result<NotificationPage>> {
  const res = await call(() =>
    api.GET("/notifications", { params: { query: cursor ? { cursor } : {} } }),
  );
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

// getUnreadNotificationCount counts the caller's unread notifications (FR-9.4).
export async function getUnreadNotificationCount(): Promise<Result<number>> {
  const res = await call(() => api.GET("/notifications/unread-count"));
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data.count } : { ok: false, ...unexpectedError };
}

// markNotificationRead marks one notification as read (FR-9.6).
export async function markNotificationRead(notificationId: string): Promise<Result<undefined>> {
  return call(() =>
    api.POST("/notifications/{notificationId}/read", { params: { path: { notificationId } } }),
  );
}

// markAllNotificationsRead marks every unread notification as read (FR-9.6) and returns
// how many there were.
export async function markAllNotificationsRead(): Promise<Result<number>> {
  const res = await call(() => api.POST("/notifications/read-all"));
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data.marked } : { ok: false, ...unexpectedError };
}
