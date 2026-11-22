import { HocuspocusProvider, HocuspocusProviderWebsocket } from "@hocuspocus/provider";
import { Redis } from "ioredis";
import * as Y from "yjs";
import { createApi } from "../src/api.ts";
import { defaultLimits, redisDb, type Config, type Limits } from "../src/config.ts";
import type { Logger } from "../src/log.ts";
import { createSyncServer, textName, type SyncServer } from "../src/server.ts";
import { FakeApi } from "./fake-api.ts";

export const origin = "http://localhost:3000";

/**
 * The Redis the tests use: TEST_REDIS_URL (DB 15 by default), flushed before each test. It must
 * not be DB 0 or the database REDIS_URL names, so a test never empties development data.
 */
export function testRedisUrl(): string {
  const url = process.env.TEST_REDIS_URL ?? "redis://localhost:6379/15";
  const db = redisDb(url);
  const dev = process.env.REDIS_URL;
  if (db === 0 || (dev !== undefined && redisDb(dev) === db)) {
    throw new Error(`TEST_REDIS_URL must name a Redis database other than 0 and REDIS_URL's (got ${db})`);
  }
  return url;
}

export interface LogLine {
  level: string;
  msg: string;
  fields: Record<string, unknown>;
}

export interface Stack {
  api: FakeApi;
  redis: Redis;
  sync: SyncServer;
  url: string;
  logs: LogLine[];
  config: Config;
  stop(): Promise<void>;
}

export async function startStack(limits: Partial<Limits> = {}): Promise<Stack> {
  const api = new FakeApi();
  const apiUrl = await api.start();
  const redisUrl = testRedisUrl();
  const redis = new Redis(redisUrl, { maxRetriesPerRequest: 1 });
  await redis.flushdb();
  const logs: LogLine[] = [];
  const at = (level: string) => (msg: string, fields: Record<string, unknown> = {}) => { logs.push({ level, msg, fields }); };
  const logger: Logger = { debug: at("debug"), info: at("info"), warn: at("warn"), error: at("error") };
  const config: Config = {
    host: "127.0.0.1", port: 0, apiUrl, serviceToken: api.token, redisUrl, allowedOrigin: origin,
    logLevel: "error", limits: { ...defaultLimits, ...limits },
  };
  const sync = createSyncServer({ config, api: createApi(apiUrl, api.token), redis, logger });
  const port = await sync.start();
  return {
    api, redis, sync, url: `ws://127.0.0.1:${port}`, logs, config,
    async stop() {
      await sync.drain();
      await api.stop();
      redis.disconnect();
    },
  };
}

/** The browser sends Origin; Node's WebSocket sends it when asked. */
export function originSocket(o: string) {
  return class extends WebSocket {
    constructor(url: string | URL) {
      super(url, { headers: { Origin: o } } as unknown as string[]);
    }
  };
}

export interface Session {
  type: string;
  role?: string;
  readOnly?: string | null;
  maxEditors?: number;
  colour?: string;
  epoch?: string | null;
}

export interface Client {
  doc: Y.Doc;
  text: Y.Text;
  provider: HocuspocusProvider;
  socket: HocuspocusProviderWebsocket;
  sessions: Session[];
  /** The reason of a refused authentication, if any. */
  failed: string | undefined;
  /** Reasons of close messages for this document. */
  closes: string[];
  /** Codes of WebSocket closes. */
  socketCloses: number[];
  synced: Promise<void>;
  /** The last session message. */
  session(): Session | undefined;
  type(s: string, at?: number): void;
  destroy(): void;
}

export interface ConnectOptions {
  /** A token, or a function the provider calls on every connect (a fresh ticket each time). */
  token: string | (() => Promise<string>);
  origin?: string;
  doc?: Y.Doc;
}

export function connect(url: string, fileId: string, opts: ConnectOptions): Client {
  const sessions: Session[] = [];
  const closes: string[] = [];
  const socketCloses: number[] = [];
  const doc = opts.doc ?? new Y.Doc();
  let resolveSynced: () => void = () => undefined;
  const synced = new Promise<void>((r) => { resolveSynced = r; });
  const client = {} as Client;
  const socket = new HocuspocusProviderWebsocket({
    url, WebSocketPolyfill: originSocket(opts.origin ?? origin), delay: 50, minDelay: 50, maxDelay: 200, factor: 1, jitter: false,
    onClose: ({ event }) => { socketCloses.push(event.code); },
  });
  const provider = new HocuspocusProvider({
    websocketProvider: socket, name: fileId, document: doc, token: opts.token,
    onSynced: () => { resolveSynced(); },
    onStateless: ({ payload }) => { sessions.push(JSON.parse(payload) as Session); },
    onAuthenticationFailed: ({ reason }) => { client.failed = reason; },
    onClose: ({ event }) => { if (event.reason) closes.push(event.reason); },
  });
  provider.attach();
  Object.assign(client, {
    doc, text: doc.getText(textName), provider, socket, sessions, failed: undefined, closes, socketCloses, synced,
    session: () => sessions.at(-1),
    type(s: string, at?: number) {
      const t = doc.getText(textName);
      t.insert(at ?? t.length, s);
    },
    destroy() {
      provider.destroy();
      socket.destroy();
    },
  });
  return client;
}

/** Waits until cond holds, polling. */
export async function until(cond: () => boolean, what: string, timeoutMs = 5000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!cond()) {
    if (Date.now() > deadline) throw new Error(`timed out waiting for ${what}`);
    await new Promise((r) => setTimeout(r, 20));
  }
}

export const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

/** A user ID. */
export const user = () => crypto.randomUUID();
