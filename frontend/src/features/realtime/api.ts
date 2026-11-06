import { call, type Result, unexpectedError } from "@/lib/api/call";
import { api, type components } from "@/lib/api/client";

export type PresenceStatus = components["schemas"]["PresenceStatus"];

// maxPresenceIds is the most IDs one GET /presence takes.
export const maxPresenceIds = 50;

// getPresence reads who is online; anyone the caller may not see reads offline (FR-16.4).
export async function getPresence(userIds: string[]): Promise<Result<PresenceStatus[]>> {
  const res = await call(() => api.GET("/presence", { params: { query: { userIds: userIds.join(",") } } }));
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data.items } : { ok: false, ...unexpectedError };
}
