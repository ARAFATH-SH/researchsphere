// The API's internal operations (docs/openapi.yaml, tag `internal`; docs/sync.md). The sync
// server never reads the database itself.

export type Role = "owner" | "editor" | "viewer";
export type AccessRole = Role | "none";

export interface Session {
  userId: string;
  projectId: string;
  fileId: string;
  role: Role;
  displayName: string;
}

export interface StoredFile {
  projectId: string;
  content: string;
  version: number;
  ydocState: Uint8Array | null;
  ydocEpoch: string | null;
}

export interface StoreRequest {
  ydocState: Uint8Array;
  ydocEpoch: string;
  content: string;
  baseVersion: number;
  updatedBy: string | null;
}

export interface AccessCheck {
  userId: string;
  fileId: string;
}

/** The answer of an operation that has no success to give: why it failed. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

/** The internal API as the sync server uses it. Tests implement it with a fake HTTP server. */
export interface Api {
  /** Spends the ticket; null when it is not valid for this file (any reason). */
  redeem(ticket: string, fileId: string): Promise<Session | null>;
  /** The role now of each check, in order. */
  access(checks: AccessCheck[]): Promise<AccessRole[]>;
  /** The file; null when it is gone or not a text file. */
  load(fileId: string): Promise<StoredFile | null>;
  /** Stores state and text; returns the new version. ApiError 409 when the base is stale, 404 when gone. */
  store(fileId: string, req: StoreRequest): Promise<number>;
  /** Whether the API answers with this token (for /health). */
  ping(): Promise<boolean>;
}

/** The most checks one access request may hold (the API's MaxAccessChecks). */
export const maxAccessChecks = 200;

const callTimeoutMs = 5000;

export function createApi(baseUrl: string, token: string, fetchImpl: typeof fetch = fetch): Api {
  const call = async (method: string, path: string, body?: unknown): Promise<Response> => {
    const init: RequestInit = {
      method,
      headers: { Authorization: `Bearer ${token}`, ...(body === undefined ? {} : { "Content-Type": "application/json" }) },
      signal: AbortSignal.timeout(callTimeoutMs),
    };
    if (body !== undefined) init.body = JSON.stringify(body);
    return fetchImpl(baseUrl + path, init);
  };
  const fail = async (res: Response): Promise<never> => {
    let code = "unknown";
    let message = res.statusText;
    try {
      const b = (await res.json()) as { code?: string; message?: string };
      code = b.code ?? code;
      message = b.message ?? message;
    } catch {
      // not JSON
    }
    throw new ApiError(res.status, code, message);
  };

  return {
    async redeem(ticket, fileId) {
      const res = await call("POST", "/internal/sync/tickets/redeem", { ticket, fileId });
      if (res.status === 404 || res.status === 400) return null;
      if (!res.ok) return fail(res);
      return (await res.json()) as Session;
    },
    async access(checks) {
      const out: AccessRole[] = [];
      for (let i = 0; i < checks.length; i += maxAccessChecks) {
        const res = await call("POST", "/internal/sync/access", { checks: checks.slice(i, i + maxAccessChecks) });
        if (!res.ok) return fail(res);
        const b = (await res.json()) as { results: { role: AccessRole }[] };
        out.push(...b.results.map((r) => r.role));
      }
      return out;
    },
    async load(fileId) {
      const res = await call("GET", `/internal/sync/files/${encodeURIComponent(fileId)}`);
      if (res.status === 404) return null;
      if (!res.ok) return fail(res);
      const b = (await res.json()) as { projectId: string; content: string; version: number; ydocState: string | null; ydocEpoch: string | null };
      return {
        projectId: b.projectId,
        content: b.content,
        version: b.version,
        ydocState: b.ydocState === null ? null : new Uint8Array(Buffer.from(b.ydocState, "base64")),
        ydocEpoch: b.ydocEpoch,
      };
    },
    async store(fileId, req) {
      const res = await call("PUT", `/internal/sync/files/${encodeURIComponent(fileId)}/state`, {
        ydocState: Buffer.from(req.ydocState).toString("base64"),
        ydocEpoch: req.ydocEpoch,
        content: req.content,
        baseVersion: req.baseVersion,
        updatedBy: req.updatedBy,
      });
      if (!res.ok) return fail(res);
      return ((await res.json()) as { version: number }).version;
    },
    async ping() {
      try {
        // A UUID that names no file: 404 means the API answered and took the token.
        const res = await call("GET", "/internal/sync/files/00000000-0000-0000-0000-000000000000");
        return res.status === 404;
      } catch {
        return false;
      }
    },
  };
}
