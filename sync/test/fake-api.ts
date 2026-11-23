import { randomBytes, randomUUID } from "node:crypto";
import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";
import type { AccessRole, Role } from "../src/api.ts";

// The API's internal operations in memory, served over HTTP with the same paths, token and
// answers (docs/openapi.yaml, tag `internal`), so the sync server is tested through its real
// client. Tests and the load test change members, files and failures directly.

export interface FakeFile {
  projectId: string;
  content: string;
  version: number;
  state: Uint8Array | null;
  epoch: string | null;
  updatedBy: string | null;
}

export interface StoreRecord {
  fileId: string;
  at: number;
  content: string;
  version: number;
  updatedBy: string | null;
}

export class FakeApi {
  readonly token = "fake-sync-service-token-0123456789abcdef";
  readonly files = new Map<string, FakeFile>();
  /** projectId -> userId -> role */
  readonly members = new Map<string, Map<string, Role>>();
  readonly names = new Map<string, string>();
  readonly suspended = new Set<string>();
  readonly stores: StoreRecord[] = [];
  private readonly tickets = new Map<string, { userId: string; fileId: string; expires: number }>();
  /** Delay every store by this long (the database's time). */
  storeDelayMs = 0;
  /** Fail this many stores with 503 before succeeding again. */
  failStores = 0;
  /** Answer every call with 503 while true. */
  down = false;
  /** Called on every successful store. */
  onStore: ((r: StoreRecord) => void) | undefined;
  private server: Server | undefined;
  url = "";

  async start(): Promise<string> {
    this.server = createServer((req, res) => { void this.handle(req, res); });
    await new Promise<void>((r) => this.server!.listen(0, "127.0.0.1", r));
    const port = (this.server.address() as AddressInfo).port;
    this.url = `http://127.0.0.1:${port}/api/v1`;
    return this.url;
  }

  async stop(): Promise<void> {
    this.server?.closeAllConnections();
    await new Promise<void>((r) => {
      if (this.server) this.server.close(() => { r(); });
      else r();
    });
  }

  /** A project with these members; returns its ID. */
  project(members: Record<string, Role>): string {
    const id = randomUUID();
    this.members.set(id, new Map(Object.entries(members)));
    for (const user of Object.keys(members)) if (!this.names.has(user)) this.names.set(user, `User ${user.slice(0, 4)}`);
    return id;
  }

  file(projectId: string, content: string): string {
    const id = randomUUID();
    this.files.set(id, { projectId, content, version: 1, state: null, epoch: null, updatedBy: null });
    return id;
  }

  setRole(projectId: string, userId: string, role: Role | null): void {
    const m = this.members.get(projectId)!;
    if (role === null) m.delete(userId);
    else m.set(userId, role);
  }

  /** A ticket, as POST /collab-ticket would give one. */
  ticket(userId: string, fileId: string, ttlMs = 30_000): string {
    const t = randomBytes(32).toString("base64url");
    this.tickets.set(t, { userId, fileId, expires: Date.now() + ttlMs });
    return t;
  }

  /** A REST save: new text, version + 1, the Yjs state and epoch cleared. */
  restSave(fileId: string, content: string): void {
    const f = this.files.get(fileId)!;
    Object.assign(f, { content, version: f.version + 1, state: null, epoch: null });
  }

  roleOf(userId: string, fileId: string): AccessRole {
    const f = this.files.get(fileId);
    if (!f || this.suspended.has(userId)) return "none";
    return this.members.get(f.projectId)?.get(userId) ?? "none";
  }

  private async handle(req: IncomingMessage, res: ServerResponse): Promise<void> {
    const send = (status: number, body?: unknown) => {
      res.writeHead(status, { "Content-Type": "application/json" });
      res.end(body === undefined ? "" : JSON.stringify(body));
    };
    if (req.headers.authorization !== `Bearer ${this.token}`) {
      send(401, { code: "unauthenticated", message: "A service token is required." });
      return;
    }
    if (this.down) {
      send(503, { code: "unavailable", message: "Down." });
      return;
    }
    const chunks: Buffer[] = [];
    for await (const c of req) chunks.push(c as Buffer);
    const body = chunks.length > 0 ? JSON.parse(Buffer.concat(chunks).toString()) as Record<string, unknown> : {};
    const path = (req.url ?? "").replace(/^\/api\/v1/, "");
    const notFound = () => { send(404, { code: "not_found", message: "Not found." }); };
    // Each branch answers once and returns.

    if (req.method === "POST" && path === "/internal/sync/tickets/redeem") {
      const t = this.tickets.get(body.ticket as string);
      this.tickets.delete(body.ticket as string);
      if (!t || t.expires <= Date.now() || t.fileId !== body.fileId) {
        notFound();
        return;
      }
      const role = this.roleOf(t.userId, t.fileId);
      if (role === "none") {
        notFound();
        return;
      }
      const f = this.files.get(t.fileId)!;
      send(200, { userId: t.userId, projectId: f.projectId, fileId: t.fileId, role, displayName: this.names.get(t.userId) ?? "" });
      return;
    }
    if (req.method === "POST" && path === "/internal/sync/access") {
      const checks = body.checks as { userId: string; fileId: string }[];
      send(200, { results: checks.map((c) => ({ ...c, role: this.roleOf(c.userId, c.fileId) })) });
      return;
    }
    const m = /^\/internal\/sync\/files\/([^/]+)(\/state)?$/.exec(path);
    const f = m ? this.files.get(m[1]!) : undefined;
    if (m && req.method === "GET" && !m[2]) {
      if (!f) {
        notFound();
        return;
      }
      send(200, {
        projectId: f.projectId, content: f.content, version: f.version,
        ydocState: f.state ? Buffer.from(f.state).toString("base64") : null, ydocEpoch: f.epoch,
      });
      return;
    }
    if (m && req.method === "PUT" && m[2]) {
      if (this.storeDelayMs > 0) await new Promise((r) => setTimeout(r, this.storeDelayMs));
      if (this.failStores > 0) {
        this.failStores--;
        send(503, { code: "unavailable", message: "Down." });
        return;
      }
      if (!f) {
        notFound();
        return;
      }
      if (f.version !== body.baseVersion) {
        send(409, { code: "version_conflict", message: "Stale." });
        return;
      }
      const content = body.content as string;
      if (Buffer.byteLength(content) > 1 << 20) {
        send(413, { code: "file_too_large", message: "Too large." });
        return;
      }
      Object.assign(f, {
        content, version: f.version + 1, state: new Uint8Array(Buffer.from(body.ydocState as string, "base64")),
        epoch: body.ydocEpoch as string, updatedBy: (body.updatedBy as string | null) ?? null,
      });
      const rec = { fileId: m[1]!, at: Date.now(), content, version: f.version, updatedBy: f.updatedBy };
      this.stores.push(rec);
      this.onStore?.(rec);
      send(200, { version: f.version });
      return;
    }
    notFound();
  }
}
