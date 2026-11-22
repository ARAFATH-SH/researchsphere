import type { Level } from "./log.ts";

// Settings come from the environment (AGENTS.md rule 8); see .env.example and docs/sync.md.

/** The limits of docs/sync.md. Tests shorten the times. */
export interface Limits {
  /** Editing members per file, SYNC_MAX_EDITORS_PER_FILE; a user's tabs count once (FR-12.8). */
  maxEditors: number;
  /** A document's text, as the API stores it (FR-12.1). */
  maxDocumentBytes: number;
  /** One WebSocket message; a larger one closes the socket with 1009. */
  maxMessageBytes: number;
  /** Messages per second per connection, and the burst allowed above it. */
  messagesPerSecond: number;
  messageBurst: number;
  /** Open document connections per user on this instance. */
  maxConnectionsPerUser: number;
  /** Store a document this long after its last change (NFR-10.1)... */
  debounceMs: number;
  /** ...but at the latest this long after its first unsaved change, so every edit is stored within 5 s with room for a stall. */
  maxDebounceMs: number;
  /** How often every open connection's role is read again (FR-12.7). */
  recheckMs: number;
  /** The live mark's TTL in Redis, and how often it is renewed. */
  liveTtlMs: number;
  liveRefreshMs: number;
  /** How long SIGTERM waits for stores before it closes connections. */
  shutdownMs: number;
  /** Ping interval; a connection silent this long is closed. */
  timeoutMs: number;
}

export const defaultLimits: Limits = {
  maxEditors: 4,
  maxDocumentBytes: 1 << 20,
  maxMessageBytes: 4 << 20,
  messagesPerSecond: 60,
  messageBurst: 120,
  maxConnectionsPerUser: 10,
  debounceMs: 2000,
  maxDebounceMs: 4000,
  recheckMs: 60_000,
  liveTtlMs: 30_000,
  liveRefreshMs: 10_000,
  shutdownMs: 20_000,
  timeoutMs: 60_000,
};

export interface Config {
  host: string;
  port: number;
  /** The API's internal base URL, ending in /api/v1 (INTERNAL_API_ADDR on the API side). */
  apiUrl: string;
  /** SYNC_SERVICE_TOKEN: the bearer token of the internal operations. Never logged. */
  serviceToken: string;
  /** Redis, the same server and database the API uses. Never logged (it may hold a password). */
  redisUrl: string;
  /** The web client's origin; a WebSocket from any other Origin is refused. */
  allowedOrigin: string;
  logLevel: Level;
  limits: Limits;
}

export const minTokenLength = 32;

/** The cursor colours (FR-12.5): more editors than this could not all have their own. */
export const maxEditorsLimit = 8;

/** Reads the configuration; every problem is listed at once. */
export function loadConfig(env: Record<string, string | undefined>): Config {
  const errors: string[] = [];
  const get = (k: string, d = "") => (env[k] ?? d).trim();

  const port = Number(get("SYNC_PORT", "8091"));
  if (!Number.isInteger(port) || port < 1 || port > 65535) errors.push("SYNC_PORT must be a port number");

  const apiUrl = get("API_INTERNAL_URL", "http://127.0.0.1:8081/api/v1").replace(/\/+$/, "");
  if (!/^https?:\/\/[^/?#]+(\/[^?#]*)?$/.test(apiUrl)) errors.push("API_INTERNAL_URL must be an http(s) URL without a query");

  const serviceToken = get("SYNC_SERVICE_TOKEN");
  if (serviceToken.length < minTokenLength) {
    errors.push(`SYNC_SERVICE_TOKEN must be at least ${minTokenLength} characters (the same value as the API's)`);
  }

  const redisUrl = get("REDIS_URL");
  if (!/^rediss?:\/\//.test(redisUrl)) errors.push("REDIS_URL must be a redis:// or rediss:// URL");

  let allowedOrigin = "";
  try {
    allowedOrigin = new URL(get("APP_BASE_URL", "http://localhost:3000")).origin;
  } catch {
    errors.push("APP_BASE_URL must be a URL");
  }

  const maxEditors = Number(get("SYNC_MAX_EDITORS_PER_FILE", String(defaultLimits.maxEditors)));
  if (!Number.isInteger(maxEditors) || maxEditors < 1 || maxEditors > maxEditorsLimit) {
    errors.push(`SYNC_MAX_EDITORS_PER_FILE must be a whole number from 1 to ${maxEditorsLimit}`);
  }

  const logLevel = get("LOG_LEVEL", "info").toLowerCase();
  if (!["debug", "info", "warn", "error"].includes(logLevel)) errors.push("LOG_LEVEL must be debug, info, warn or error");

  if (errors.length > 0) throw new Error("sync: invalid configuration:\n  " + errors.join("\n  "));
  return {
    host: get("SYNC_HOST", "127.0.0.1"),
    port,
    apiUrl,
    serviceToken,
    redisUrl,
    allowedOrigin,
    logLevel: logLevel as Level,
    limits: { ...defaultLimits, maxEditors },
  };
}

/** The Redis database number of a URL (the path), 0 when none is given. */
export function redisDb(url: string): number {
  const path = new URL(url).pathname.replace(/^\//, "");
  return path === "" ? 0 : Number(path);
}
