import { randomUUID } from "node:crypto";
import type { ServerResponse } from "node:http";
import { OutgoingMessage, Server, type Connection, type Document, type Extension, type onStoreDocumentPayload } from "@hocuspocus/server";
import type { Redis } from "ioredis";
import * as Y from "yjs";
import { ApiError, type AccessCheck, type AccessRole, type Api, type Role } from "./api.ts";
import { redisDb, type Config } from "./config.ts";
import { Bucket } from "./limits.ts";
import { LiveMarks, eventsChannel } from "./live.ts";
import type { Logger } from "./log.ts";
import { Sessions, type Entry } from "./sessions.ts";

// The real-time editing server (FR-12.4, FR-12.7 to FR-12.9, NFR-2, NFR-10.1, NFR-12;
// docs/sync.md). One Hocuspocus document per text file, named by the file's ID; its Y.Text
// "content" is the file's text.

/** The Y.Text that holds a file's text. */
export const textName = "content";

/**
 * Why the server refuses or ends a document connection. Hocuspocus sends the reason to the
 * provider (authenticationFailed, or a close message); docs/sync.md says what a client does.
 */
export const Reason = {
  originNotAllowed: "origin_not_allowed",
  ticketInvalid: "ticket_invalid",
  tooManyConnections: "too_many_connections",
  epochMismatch: "epoch_mismatch",
  unavailable: "unavailable",
  fileGone: "file_gone",
  accessEnded: "access_ended",
  sessionEnded: "session_ended",
  documentTooLarge: "document_too_large",
  rateLimited: "rate_limited",
  reload: "reload",
  shuttingDown: "shutting_down",
} as const;

/** Close codes beside each reason, for logs and the close event (docs/sync.md). */
const codes: Record<string, number> = {
  [Reason.fileGone]: 4404,
  [Reason.accessEnded]: 4403,
  [Reason.sessionEnded]: 4401,
  [Reason.documentTooLarge]: 4413,
  [Reason.rateLimited]: 4429,
  [Reason.reload]: 4409,
  [Reason.shuttingDown]: 1001,
};

/** What a connection is, from its redeemed ticket. */
export interface Context {
  userId: string;
  projectId: string;
  fileId: string;
  role: Role;
  displayName: string;
}

/** What this server knows about a loaded document. */
interface Meta {
  projectId: string;
  /** The Yjs history's name (docs/sync.md, Epochs). */
  epoch: string;
  /** The file's version as last loaded or stored: the base of the next store. */
  version: number;
  /** Built from the text just now: stored at once so the epoch is kept. */
  fresh: boolean;
  /** Changes since the last successful store, counted. */
  changes: number;
  /** When the oldest change not yet stored arrived, and the timer that stores it by the deadline. */
  firstUnsavedAt: number | null;
  deadline: NodeJS.Timeout | undefined;
  lastEditor: string | null;
  /** The file or project is gone, or the document must be read again: nothing more is stored. */
  gone: boolean;
  failures: number;
  retry: NodeJS.Timeout | undefined;
}

/**
 * A refusal Hocuspocus passes on: `reason` goes to the client, `code` to the close event. Its
 * message is empty, so Hocuspocus does not print it as an error; this server logs its own.
 */
export class Refusal extends Error {
  readonly reason: string;
  readonly code: number | undefined;
  constructor(reason: string) {
    super("");
    this.reason = reason;
    this.code = codes[reason];
  }
}

function refuse(reason: string): Refusal {
  return new Refusal(reason);
}

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/**
 * The provider's token is the ticket, or "<ticket>.<epoch>" when the client holds offline
 * edits made in that epoch (FR-12.9). A ticket is base64url, which has no ".".
 */
export function parseToken(token: string): { ticket: string; epoch: string | null } {
  const dot = token.indexOf(".");
  if (dot === -1) return { ticket: token, epoch: null };
  return { ticket: token.slice(0, dot), epoch: token.slice(dot + 1) || null };
}

export interface SyncServerOptions {
  config: Config;
  api: Api;
  /** A Redis client of this server; one more is made from it for pub/sub. */
  redis: Redis;
  logger: Logger;
  now?: () => number;
}

export interface SyncServer {
  /** Starts listening and returns the port. */
  start(): Promise<number>;
  /** SIGTERM: stop taking edits, store everything, close every connection, release the marks. */
  drain(): Promise<void>;
  /** Reads every open connection's role again now (also every recheckMs). */
  recheck(): Promise<void>;
  /** Whether Redis and the API answer, and what is open. */
  health(): Promise<Health>;
  readonly hocuspocus: Server<Context>["hocuspocus"];
}

export interface Health {
  status: "ok" | "degraded";
  redis: "ok" | "unavailable";
  api: "ok" | "unavailable";
  documents: number;
  connections: number;
  unsaved: number;
}

export function createSyncServer(opts: SyncServerOptions): SyncServer {
  const { config, api, redis, logger } = opts;
  const now = opts.now ?? Date.now;
  const limits = config.limits;
  const sessions = new Sessions<Connection>(limits.maxEditors, now);
  const metas = new Map<string, Meta>();
  const live = new LiveMarks(redis, limits.liveTtlMs);
  const buckets = new WeakMap<Connection, Bucket>();
  const droppedLogged = new WeakMap<Connection, number>();
  // Pub/sub keeps its queue and resubscribes after a reconnect; the live marks fail fast.
  const subscriber = redis.duplicate({ enableOfflineQueue: true, maxRetriesPerRequest: null, commandTimeout: undefined });
  subscriber.on("error", (err: Error) => { logger.warn("redis subscriber", { error: err.message }); });
  let draining = false;
  let timers: NodeJS.Timeout[] = [];
  let healthCache: { at: number; value: Health } | undefined;

  const ctxOf = (c: Connection): Context => c.context as Context;

  const sendSession = (conn: Connection, e: Entry<Connection>) => {
    const meta = metas.get(conn.document.name);
    conn.sendStateless(JSON.stringify({
      type: "session", role: e.role, readOnly: e.reason, maxEditors: limits.maxEditors, colour: e.colour, epoch: meta?.epoch ?? null,
    }));
  };

  /**
   * Brings the connections of the entries in line with their state and tells them. A
   * connection that may edit again is asked for what it has that the server lacks (SyncStep1):
   * updates dropped while it was read-only would otherwise hold back every later edit of it,
   * as Yjs waits for the items they build on.
   */
  const announce = (entries: Entry<Connection>[]) => {
    for (const e of entries) {
      for (const conn of e.conns) {
        const wasReadOnly = conn.readOnly;
        conn.readOnly = draining || e.reason !== null;
        sendSession(conn, e);
        if (wasReadOnly && !conn.readOnly) {
          conn.send(new OutgoingMessage(conn.messageAddress).createSyncMessage().writeFirstSyncStepFor(conn.document).toUint8Array());
        }
      }
    }
  };

  const closeConn = (conn: Connection, reason: string) => {
    conn.close({ code: codes[reason] ?? 4000, reason });
  };

  /** Ends a user's connections to a document and frees their place. */
  const endUser = (doc: string, userId: string, reason: string) => {
    const e = sessions.entry(doc, userId);
    if (!e) return;
    for (const conn of [...e.conns]) closeConn(conn, reason);
    announce(sessions.remove(doc, userId));
    logger.info("connections ended", { fileId: doc, userId, reason });
  };

  const hocuspocusDoc = (name: string): Document | undefined => server.hocuspocus.documents.get(name);

  /** The document can no longer be stored (deleted, or changed behind this server): close it. */
  const retire = (name: string, reason: string) => {
    const meta = metas.get(name);
    if (meta) {
      meta.gone = true;
      clearTimeout(meta.retry);
      clearTimeout(meta.deadline);
    }
    for (const e of sessions.entries(name)) endUser(name, e.userId, reason);
    const doc = hocuspocusDoc(name);
    for (const conn of doc?.getConnections() ?? []) closeConn(conn, reason);
    live.unmark([name]).catch((err: unknown) => { logger.warn("live mark not removed", { fileId: name, error: err }); });
  };

  const applyRole = (doc: string, userId: string, role: AccessRole) => {
    if (role === "none") {
      endUser(doc, userId, Reason.accessEnded);
      return;
    }
    announce(sessions.setRole(doc, userId, role));
  };

  /** Reads the role of the given users on the given documents again and applies it. */
  const recheckPairs = async (pairs: AccessCheck[]) => {
    if (pairs.length === 0) return;
    let roles: AccessRole[];
    try {
      roles = await api.access(pairs);
    } catch (err) {
      logger.warn("access check failed; connections keep their role until the next check", { error: err });
      return;
    }
    pairs.forEach((p, i) => {
      const role = roles[i];
      if (role !== undefined) applyRole(p.fileId, p.userId, role);
    });
  };

  const allPairs = (filter: (doc: string, userId: string) => boolean = () => true): AccessCheck[] => {
    const out: AccessCheck[] = [];
    for (const doc of sessions.documents()) {
      for (const e of sessions.entries(doc)) if (filter(doc, e.userId)) out.push({ userId: e.userId, fileId: doc });
    }
    return out;
  };

  /** An event from the API (docs/sync.md, Events). */
  const onEvent = (raw: string) => {
    let ev: { kind?: string; projectId?: string; userIds?: string[]; fileIds?: string[] };
    try {
      ev = JSON.parse(raw) as typeof ev;
    } catch {
      logger.warn("unreadable sync event");
      return;
    }
    const users = new Set(ev.userIds ?? []);
    const inProject = (doc: string) => metas.get(doc)?.projectId === ev.projectId;
    switch (ev.kind) {
      case "members_changed":
        void recheckPairs(allPairs((doc, userId) => inProject(doc) && users.has(userId)));
        break;
      case "files_deleted":
        for (const id of ev.fileIds ?? []) if (metas.has(id)) retire(id, Reason.fileGone);
        break;
      case "project_deleted":
        for (const doc of [...metas.keys()]) if (inProject(doc)) retire(doc, Reason.fileGone);
        break;
      case "user_ended":
        for (const doc of sessions.documents()) for (const id of users) endUser(doc, id, Reason.sessionEnded);
        break;
      default:
        logger.warn("unknown sync event", { kind: ev.kind });
    }
  };

  /** The epoch the document has, or will have when loaded (null: it will get a new one). */
  const epochOf = async (name: string): Promise<string | null> => {
    const meta = metas.get(name);
    if (meta && !meta.gone) return meta.epoch;
    const f = await api.load(name);
    return f?.ydocState ? f.ydocEpoch : null;
  };

  const storePayload = (document: Document): onStoreDocumentPayload => ({
    instance: server.hocuspocus, clientsCount: document.getConnectionsCount(), document,
    lastContext: {}, lastTransactionOrigin: undefined, documentName: document.name,
  });

  const store = async (document: Document) => {
    const name = document.name;
    const meta = metas.get(name);
    if (!meta || meta.gone) return;
    const seen = meta.changes;
    const snapshotAt = now();
    const content = document.getText(textName).toJSON();
    try {
      meta.version = await api.store(name, {
        ydocState: Y.encodeStateAsUpdate(document), ydocEpoch: meta.epoch, content,
        baseVersion: meta.version, updatedBy: meta.lastEditor,
      });
      meta.fresh = false;
      meta.changes -= seen;
      clearTimeout(meta.deadline);
      meta.deadline = undefined;
      meta.firstUnsavedAt = null;
      // Changes that came while storing are at most as old as the snapshot.
      if (meta.changes > 0) armDeadline(document, meta, snapshotAt);
      if (meta.failures > 0) logger.info("document stored again", { fileId: name, after: meta.failures });
      meta.failures = 0;
      logger.debug("document stored", { fileId: name, version: meta.version, bytes: Buffer.byteLength(content) });
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) {
        logger.info("file gone while open", { fileId: name });
        retire(name, Reason.fileGone);
        return;
      }
      if (err instanceof ApiError && err.status === 409) {
        // Only a REST save in a gap of the live mark (Redis was down) can do this. The text the
        // API has now wins; members' clients still hold their edits and show a conflict.
        logger.error("file changed behind the sync server; members must reload", { fileId: name });
        retire(name, Reason.reload);
        return;
      }
      if (err instanceof ApiError && err.status === 413) {
        logger.error("document over the stored size", { fileId: name });
        retire(name, Reason.documentTooLarge);
        return;
      }
      meta.failures++;
      const delay = Math.min(30_000, 1000 * 2 ** Math.min(meta.failures - 1, 5));
      logger.warn("document not stored; it stays in memory and is tried again", { fileId: name, error: err, retryMs: delay, failures: meta.failures });
      clearTimeout(meta.retry);
      meta.retry = setTimeout(() => {
        void server.hocuspocus.storeDocumentHooks(document, storePayload(document), true);
      }, delay);
      meta.retry.unref();
      throw err; // Hocuspocus keeps the document loaded
    }
  };

  /**
   * Stores the document at the latest maxDebounceMs after its oldest unsaved change (NFR-10.1).
   * Hocuspocus's own maxDebounce is checked only when another change arrives, so when typing
   * stops just before it, the store would wait a whole debounce more.
   */
  const armDeadline = (document: Document, meta: Meta, since: number) => {
    if (meta.firstUnsavedAt !== null) return;
    meta.firstUnsavedAt = since;
    meta.deadline = setTimeout(() => {
      meta.deadline = undefined;
      if (!meta.gone && meta.changes > 0) {
        void server.hocuspocus.storeDocumentHooks(document, storePayload(document), true);
      }
    }, Math.max(0, since + limits.maxDebounceMs - now()));
    meta.deadline.unref();
  };

  const extension: Extension<Context> = {
    extensionName: "researchsphere",

    async onAuthenticate({ token, documentName, requestHeaders, connectionConfig, socketId }) {
      if (draining) throw refuse(Reason.shuttingDown);
      if (requestHeaders.get("origin") !== config.allowedOrigin) throw refuse(Reason.originNotAllowed);
      if (!uuidPattern.test(documentName)) throw refuse(Reason.ticketInvalid);
      const { ticket, epoch } = parseToken(token);
      let session;
      try {
        session = await api.redeem(ticket, documentName);
      } catch (err) {
        logger.warn("ticket not redeemed: API unavailable", { error: err });
        throw refuse(Reason.unavailable);
      }
      if (!session) throw refuse(Reason.ticketInvalid);
      if (sessions.connectionsOf(session.userId) >= limits.maxConnectionsPerUser) {
        logger.warn("connection refused: too many for one user", { userId: session.userId });
        throw refuse(Reason.tooManyConnections);
      }
      if (epoch !== null) {
        let current: string | null;
        try {
          current = await epochOf(documentName);
        } catch (err) {
          logger.warn("epoch not read: API unavailable", { error: err });
          throw refuse(Reason.unavailable);
        }
        if (current !== epoch) throw refuse(Reason.epochMismatch);
      }
      // No await from here: the decision and the reservation are one step (FR-12.8).
      const e = sessions.admit(documentName, session.userId, session.role, socketId);
      connectionConfig.readOnly = e.reason !== null;
      logger.info("connection admitted", { fileId: documentName, userId: session.userId, role: session.role, readOnly: e.reason });
      const ctx: Context = { userId: session.userId, projectId: session.projectId, fileId: documentName, role: session.role, displayName: session.displayName };
      return ctx;
    },

    async connected({ connection, documentName, socketId }) {
      const ctx = ctxOf(connection);
      const e = sessions.attach(documentName, ctx.userId, socketId, connection);
      if (!e) {
        // Removed while the document loaded.
        closeConn(connection, Reason.accessEnded);
        return;
      }
      connection.readOnly = draining || e.reason !== null;
      connection.onClose(() => {
        announce(sessions.detach(documentName, ctx.userId, socketId, connection));
      });
      sendSession(connection, e);
    },

    async beforeHandleMessage({ connection }) {
      let bucket = buckets.get(connection);
      if (!bucket) {
        bucket = new Bucket(limits.messagesPerSecond, limits.messageBurst, now);
        buckets.set(connection, bucket);
      }
      if (!bucket.take()) {
        logger.warn("connection over the message rate", { fileId: connection.document.name, userId: ctxOf(connection).userId });
        throw refuse(Reason.rateLimited);
      }
    },

    async beforeSync({ connection, document, type, payload }) {
      // 1: SyncStep2 (a client's missing updates, offline edits included), 2: an update.
      if (type !== 1 && type !== 2) return;
      if (connection.readOnly) {
        // Hocuspocus discards it (FR-12.7). A viewer's own SyncStep2 is normally what the
        // server already has; log only what would have changed the document.
        if (type === 1 && Y.snapshotContainsUpdate(Y.snapshot(document), payload)) return;
        const last = droppedLogged.get(connection) ?? 0;
        if (now() - last >= 60_000) {
          droppedLogged.set(connection, now());
          const ctx = ctxOf(connection);
          logger.warn("update from a read-only connection dropped", {
            fileId: document.name, userId: ctx.userId, role: ctx.role, bytes: payload.byteLength,
            reason: sessions.entry(document.name, ctx.userId)?.reason ?? "viewer",
          });
        }
        return;
      }
      // An update adds at most its own length of text: refuse before applying anything that
      // could take the document past its limit.
      const text = document.getText(textName);
      if (text.length * 3 + payload.byteLength > limits.maxDocumentBytes) {
        const bytes = Buffer.byteLength(text.toJSON());
        if (bytes + payload.byteLength > limits.maxDocumentBytes) {
          logger.warn("update refused: document at its size limit", { fileId: document.name, userId: ctxOf(connection).userId, bytes });
          throw refuse(Reason.documentTooLarge);
        }
      }
    },

    async beforeHandleAwareness({ connection, states }) {
      // Names and colours come from the server, not the client (FR-12.5).
      if (!connection) return;
      const ctx = ctxOf(connection);
      const e = sessions.entry(connection.document.name, ctx.userId);
      for (const state of states.values()) {
        state.user = { id: ctx.userId, name: ctx.displayName, colour: e?.colour ?? null };
      }
    },

    async onChange({ documentName, document, connection }) {
      const meta = metas.get(documentName);
      if (!meta) return;
      meta.changes++;
      armDeadline(document, meta, now());
      if (connection) meta.lastEditor = ctxOf(connection).userId;
    },

    async onLoadDocument({ document, documentName }) {
      // Marked before reading, so a REST save after this read sees the mark (docs/decisions.md).
      try {
        await live.mark(documentName);
      } catch (err) {
        logger.error("document not opened: live mark not set", { fileId: documentName, error: err });
        throw refuse(Reason.unavailable);
      }
      let f;
      try {
        f = await api.load(documentName);
      } catch (err) {
        logger.error("document not opened: API unavailable", { fileId: documentName, error: err });
        await live.unmark([documentName]).catch(() => undefined);
        throw refuse(Reason.unavailable);
      }
      if (!f) {
        await live.unmark([documentName]).catch(() => undefined);
        throw refuse(Reason.fileGone);
      }
      const meta: Meta = {
        projectId: f.projectId, epoch: "", version: f.version, fresh: false, changes: 0, firstUnsavedAt: null, deadline: undefined,
        lastEditor: null, gone: false, failures: 0, retry: undefined,
      };
      if (f.ydocState && f.ydocEpoch) {
        Y.applyUpdate(document, f.ydocState);
        meta.epoch = f.ydocEpoch;
        const text = document.getText(textName).toJSON();
        if (text !== f.content) {
          // Never expected: the API writes both together and clears both on a REST save.
          logger.error("stored state and text differ; the state wins", { fileId: documentName });
        }
      } else {
        // A new history from the text (a new file, or one a REST save rewrote).
        document.getText(textName).insert(0, f.content);
        meta.epoch = randomUUID();
        meta.fresh = true;
      }
      metas.set(documentName, meta);
      logger.info("document opened", { fileId: documentName, version: f.version, fresh: meta.fresh });
    },

    async afterLoadDocument({ document, documentName }) {
      // A new epoch is stored at once, so a member who goes offline now can merge later.
      if (metas.get(documentName)?.fresh) {
        void server.hocuspocus.storeDocumentHooks(document, storePayload(document), true);
      }
    },

    async onStoreDocument({ document }) {
      await store(document);
    },

    async beforeUnloadDocument({ documentName }) {
      const meta = metas.get(documentName);
      // Keep a document with unsaved changes until a store succeeds.
      if (meta && !meta.gone && (meta.changes > 0 || meta.fresh)) {
        throw refuse("unsaved");
      }
    },

    async afterUnloadDocument({ documentName }) {
      const meta = metas.get(documentName);
      clearTimeout(meta?.retry);
      clearTimeout(meta?.deadline);
      metas.delete(documentName);
      try {
        await live.unmark([documentName]);
      } catch (err) {
        logger.warn("live mark not removed; it expires by itself", { fileId: documentName, error: err });
      }
      logger.info("document closed", { fileId: documentName });
    },

    async onRequest({ request, response }) {
      if (request.url === "/health" && request.method === "GET") {
        const h = await health();
        respond(response, h.status === "ok" ? 200 : 503, h);
        // Hocuspocus's convention: a falsy rejection ends the request without its default answer.
        return Promise.reject(null); // eslint-disable-line @typescript-eslint/prefer-promise-reject-errors
      }
      respond(response, 404, { code: "not_found", message: "Not found." });
      return Promise.reject(null); // eslint-disable-line @typescript-eslint/prefer-promise-reject-errors
    },
  };

  const respond = (response: ServerResponse, status: number, body: unknown) => {
    response.writeHead(status, { "Content-Type": "application/json", "Cache-Control": "no-store" });
    response.end(JSON.stringify(body));
  };

  const health = async (): Promise<Health> => {
    if (healthCache && now() - healthCache.at < 2000) return healthCache.value;
    const [redisOk, apiOk] = await Promise.all([
      redis.ping().then(() => true, () => false),
      api.ping(),
    ]);
    let connections = 0;
    let unsaved = 0;
    for (const doc of server.hocuspocus.documents.values()) connections += doc.getConnectionsCount();
    for (const m of metas.values()) if (m.failures > 0) unsaved++;
    const value: Health = {
      status: redisOk && apiOk && unsaved === 0 ? "ok" : "degraded",
      redis: redisOk ? "ok" : "unavailable",
      api: apiOk ? "ok" : "unavailable",
      documents: server.hocuspocus.getDocumentsCount(),
      connections,
      unsaved,
    };
    healthCache = { at: now(), value };
    return value;
  };

  const server = new Server<Context>({
    name: "researchsphere-sync",
    port: config.port,
    address: config.host,
    quiet: true,
    stopOnSignals: false,
    timeout: limits.timeoutMs,
    debounce: limits.debounceMs,
    maxDebounce: limits.maxDebounceMs,
    unloadImmediately: true,
    websocketOptions: { maxPayload: limits.maxMessageBytes },
    extensions: [extension],
  });

  const recheck = async () => {
    await recheckPairs(allPairs());
  };

  const start = async (): Promise<number> => {
    const channel = eventsChannel(redisDb(config.redisUrl));
    subscriber.on("message", (ch: string, msg: string) => {
      if (ch === channel) onEvent(msg);
    });
    await subscriber.subscribe(channel);
    await server.listen();
    timers = [
      setInterval(() => { void recheck(); }, limits.recheckMs),
      setInterval(() => {
        live.renew([...metas.keys()]).catch((err: unknown) => {
          logger.error("live marks not renewed; REST saves may slip in", { error: err });
        });
      }, limits.liveRefreshMs),
    ];
    for (const t of timers) t.unref();
    const port = server.address.port;
    logger.info("listening", { host: config.host, port, maxEditors: limits.maxEditors });
    return port;
  };

  const drain = async () => {
    if (draining) return;
    draining = true;
    logger.info("shutting down: storing every document");
    server.httpServer.close();
    for (const t of timers) clearInterval(t);
    // Take no more edits: what a client sends now stays unsynced on its side and goes to the
    // next server after it reconnects.
    for (const doc of server.hocuspocus.documents.values()) {
      for (const conn of doc.getConnections()) conn.readOnly = true;
    }
    for (const doc of server.hocuspocus.documents.values()) {
      const meta = metas.get(doc.name);
      if (meta && !meta.gone && (meta.changes > 0 || meta.fresh || meta.failures > 0)) {
        clearTimeout(meta.retry);
        void server.hocuspocus.storeDocumentHooks(doc, storePayload(doc), true);
      }
    }
    const deadline = now() + limits.shutdownMs;
    const pending = () => [...server.hocuspocus.documents.values()].filter((d) => {
      const m = metas.get(d.name);
      return d.saveMutex.isLocked() || server.hocuspocus.debouncer.isDebounced(`onStoreDocument-${d.name}`) ||
        (m !== undefined && !m.gone && (m.changes > 0 || m.fresh));
    });
    while (pending().length > 0 && now() < deadline) {
      for (const d of pending()) {
        const m = metas.get(d.name);
        if (m && m.failures > 0 && m.retry !== undefined) {
          clearTimeout(m.retry);
          m.retry = undefined;
          void server.hocuspocus.storeDocumentHooks(d, storePayload(d), true);
        }
      }
      await new Promise((r) => setTimeout(r, 50));
    }
    const left = pending().map((d) => d.name);
    if (left.length > 0) logger.error("shut down with unsaved documents; members' clients still hold the edits", { fileIds: left });
    for (const doc of [...server.hocuspocus.documents.values()]) {
      for (const conn of doc.getConnections()) {
        closeConn(conn, Reason.shuttingDown);
        try {
          conn.webSocket.close(1001, Reason.shuttingDown);
        } catch {
          // already closed
        }
      }
    }
    try {
      await live.unmark([...metas.keys()]);
    } catch (err) {
      logger.warn("live marks not removed; they expire by themselves", { error: err });
    }
    await subscriber.quit().catch(() => undefined);
    logger.info("shut down", { unsaved: left.length });
  };

  return { start, drain, recheck, health, hocuspocus: server.hocuspocus };
}

