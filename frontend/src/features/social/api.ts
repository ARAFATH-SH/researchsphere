import { call, networkError, type Result, unexpectedError } from "@/lib/api/call";
import { api, type components } from "@/lib/api/client";

export type ConnectionStatus = components["schemas"]["ConnectionStatus"];
export type ConnectionRequest = components["schemas"]["ConnectionRequest"];
export type Connection = components["schemas"]["Connection"];
export type ConnectionPage = components["schemas"]["ConnectionPage"];
export type ConnectionRequestPage = components["schemas"]["ConnectionRequestPage"];
export type BlockedPerson = components["schemas"]["BlockedPerson"];
export type BlockedPersonPage = components["schemas"]["BlockedPersonPage"];

// SendResult is the answer to a connection request. A 409 request_pending_from_them
// carries the other user's requestId, so the caller can offer Accept instead (FR-7.2).
export type SendResult =
  | { ok: true; data: ConnectionRequest }
  | { ok: false; code: string; message: string; requestId?: string };

// sendConnectionRequest asks userId to connect (FR-7.1). call() keeps only code and
// message, so this reads the reply itself to keep the conflict's requestId.
export async function sendConnectionRequest(userId: string): Promise<SendResult> {
  try {
    const { data, error, response } = await api.POST("/connections/requests", { body: { userId } });
    if (response.ok && data) return { ok: true, data };
    if (error?.code && error.message) {
      const requestId = "requestId" in error ? error.requestId : undefined;
      return { ok: false, code: error.code, message: error.message, ...(requestId ? { requestId } : {}) };
    }
    return { ok: false, ...unexpectedError };
  } catch {
    return { ok: false, ...networkError };
  }
}

// cancelConnectionRequest withdraws a request the caller sent (FR-7.3).
export async function cancelConnectionRequest(requestId: string): Promise<Result<undefined>> {
  return call(() =>
    api.DELETE("/connections/requests/{requestId}", { params: { path: { requestId } } }),
  );
}

// acceptConnectionRequest accepts a request sent to the caller (FR-7.3) and returns the
// new connection.
export async function acceptConnectionRequest(requestId: string): Promise<Result<Connection>> {
  const res = await call(() =>
    api.POST("/connections/requests/{requestId}/accept", { params: { path: { requestId } } }),
  );
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

// rejectConnectionRequest ignores a request sent to the caller (FR-7.3, FR-7.6); the
// sender is not told.
export async function rejectConnectionRequest(requestId: string): Promise<Result<undefined>> {
  return call(() =>
    api.POST("/connections/requests/{requestId}/reject", { params: { path: { requestId } } }),
  );
}

// removeConnection ends the caller's connection with userId (FR-7.5).
export async function removeConnection(userId: string): Promise<Result<undefined>> {
  return call(() => api.DELETE("/connections/{userId}", { params: { path: { userId } } }));
}

async function page<T>(
  request: () => Promise<{ data?: T; error?: components["schemas"]["Error"]; response: Response }>,
): Promise<Result<T>> {
  const res = await call(request);
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

// The lists of FR-7.4, 20 items a page, newest first.

export function listMyConnections(n: number): Promise<Result<ConnectionPage>> {
  return page(() => api.GET("/connections", { params: { query: { page: n } } }));
}

export function listReceivedRequests(n: number): Promise<Result<ConnectionRequestPage>> {
  return page(() => api.GET("/connections/requests/received", { params: { query: { page: n } } }));
}

export function listSentRequests(n: number): Promise<Result<ConnectionRequestPage>> {
  return page(() => api.GET("/connections/requests/sent", { params: { query: { page: n } } }));
}

// listProfileConnections lists any user's connections; an unknown, suspended or
// unverified user fails with code not_found.
export function listProfileConnections(userId: string, n: number): Promise<Result<ConnectionPage>> {
  return page(() =>
    api.GET("/profiles/{userId}/connections", { params: { path: { userId }, query: { page: n } } }),
  );
}

// blockUser blocks someone (FR-16.9). Blocking them twice changes nothing.
export async function blockUser(userId: string): Promise<Result<undefined>> {
  const res = await call(() => api.POST("/blocks", { body: { userId } }));
  return res.ok ? { ok: true, data: undefined } : res;
}

// unblockUser ends a block. Nothing the block removed comes back.
export async function unblockUser(userId: string): Promise<Result<undefined>> {
  const res = await call(() => api.DELETE("/blocks/{userId}", { params: { path: { userId } } }));
  return res.ok ? { ok: true, data: undefined } : res;
}

// listBlockedPeople returns one page of the people the caller has blocked, newest first.
export async function listBlockedPeople(page: number): Promise<Result<BlockedPersonPage>> {
  const res = await call(() => api.GET("/blocks", { params: { query: { page } } }));
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}
