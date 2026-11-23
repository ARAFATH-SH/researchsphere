import assert from "node:assert/strict";
import { spawn, type ChildProcess } from "node:child_process";
import { once } from "node:events";
import { afterEach, describe, it } from "node:test";
import { Redis } from "ioredis";
import { livePrefix } from "../src/live.ts";
import { FakeApi } from "./fake-api.ts";
import { connect, origin, sleep, startStack, testRedisUrl, until, user, type Client, type Stack } from "./helpers.ts";

// NFR-10.1: a document is stored within 5 s of an edit (debounce 2 s, at the latest 4 s after the
// first unsaved change), at once when the last member leaves and on SIGTERM, and a restart loses
// nothing a member sent. The live mark keeps REST saves out while the file is open.

describe("persistence", () => {
  let s: Stack | undefined;
  const clients: Client[] = [];
  const open = (url: string, fileId: string, token: string | (() => Promise<string>)) => {
    const c = connect(url, fileId, { token });
    clients.push(c);
    return c;
  };
  afterEach(async () => {
    for (const c of clients.splice(0)) c.destroy();
    await s?.stop();
    s = undefined;
  });

  it("stores about 2 s after the last edit, with the last editor and the Yjs state", async () => {
    s = await startStack();
    const ada = user();
    const pid = s.api.project({ [ada]: "editor" });
    const fid = s.api.file(pid, "a");
    const c = open(s.url, fid, s.api.ticket(ada, fid));
    await c.synced;
    // The new epoch is stored at once (a fresh history must survive a restart).
    await until(() => s!.api.stores.length === 1, "the epoch's store");
    const epoch = s.api.files.get(fid)!.epoch;
    assert.equal(epoch, c.session()!.epoch);

    c.type("b");
    const edited = Date.now();
    await until(() => s!.api.stores.length === 2, "the debounced store", 4000);
    const delay = s.api.stores[1]!.at - edited;
    assert.ok(delay >= 1800 && delay < 3000, `stored ${delay} ms after the edit`);
    const f = s.api.files.get(fid)!;
    assert.equal(f.content, "ab");
    assert.equal(f.updatedBy, ada);
    assert.equal(f.epoch, epoch, "the epoch stays");
    assert.ok(f.state && f.state.byteLength > 0);
  });

  it("stores within 5 s of each edit while members keep typing", async () => {
    s = await startStack();
    const ada = user();
    const pid = s.api.project({ [ada]: "editor" });
    const fid = s.api.file(pid, "");
    const c = open(s.url, fid, s.api.ticket(ada, fid));
    await c.synced;
    await until(() => s!.api.stores.length === 1, "the epoch's store");
    const typedAt = new Map<string, number>();
    for (let i = 0; i < 35; i++) {
      const token = `[${i}]`;
      typedAt.set(token, Date.now());
      c.type(token);
      await sleep(200);
    }
    await until(() => s!.api.files.get(fid)!.content.includes("[34]"), "the last edit stored", 6000);
    let worst = 0;
    for (const [token, at] of typedAt) {
      const stored = s.api.stores.find((r) => r.content.includes(token))!;
      worst = Math.max(worst, stored.at - at);
    }
    assert.ok(worst < 5000, `an edit waited ${worst} ms to be stored`);
    assert.ok(s.api.stores.length >= 3, "stored while typing, not only at the end");
  });

  it("stores by the 4 s deadline when typing stops just before it", async () => {
    s = await startStack();
    const ada = user();
    const pid = s.api.project({ [ada]: "editor" });
    const fid = s.api.file(pid, "");
    const c = open(s.url, fid, s.api.ticket(ada, fid));
    await c.synced;
    await until(() => s!.api.stores.length === 1, "the epoch's store");
    // Edits every 300 ms for 3.7 s, then none: Hocuspocus's own cap would wait 2 s more.
    const first = Date.now();
    c.type("[first]");
    while (Date.now() - first < 3700) {
      await sleep(300);
      c.type(".");
    }
    await until(() => s!.api.stores.some((r) => r.content.includes("[first]")), "the store", 6000);
    const stored = s.api.stores.find((r) => r.content.includes("[first]"))!;
    assert.ok(stored.at - first < 4500, `the first edit waited ${stored.at - first} ms`);
  });

  it("stores at once when the last member leaves, then closes the document and its live mark", async () => {
    s = await startStack();
    const ada = user();
    const pid = s.api.project({ [ada]: "editor" });
    const fid = s.api.file(pid, "a");
    const c = open(s.url, fid, s.api.ticket(ada, fid));
    await c.synced;
    const ttl = await s.redis.pttl(livePrefix + fid);
    assert.ok(ttl > 0 && ttl <= 30_000, `live mark TTL ${ttl}`);
    c.type("b");
    await sleep(100);
    const left = Date.now();
    c.destroy();
    await until(() => s!.api.files.get(fid)!.content === "ab", "the store", 1500);
    assert.ok(Date.now() - left < 1000);
    await until(() => s!.sync.hocuspocus.getDocumentsCount() === 0, "the unload");
    await until(() => true, "");
    assert.equal(await s.redis.exists(livePrefix + fid), 0);
  });

  it("keeps a document whose store fails and tries again; /health says degraded meanwhile", async () => {
    s = await startStack();
    const ada = user();
    const pid = s.api.project({ [ada]: "editor" });
    const fid = s.api.file(pid, "a");
    const c = open(s.url, fid, s.api.ticket(ada, fid));
    await c.synced;
    await until(() => s!.api.stores.length === 1, "the epoch's store");
    s.api.failStores = 2;
    c.type("b");
    c.destroy(); // the last member leaves while the API fails
    await until(() => s!.logs.some((l) => l.msg.startsWith("document not stored")), "the failure");
    assert.equal(s.sync.hocuspocus.getDocumentsCount(), 1, "kept in memory");
    assert.equal((await s.sync.health()).status, "degraded");
    await until(() => s!.api.files.get(fid)!.content === "ab", "the retried store", 8000);
    await until(() => s!.sync.hocuspocus.getDocumentsCount() === 0, "the unload after the store");
  });

  it("retires a document whose file changed behind it (a REST save in a gap of the live mark)", async () => {
    s = await startStack();
    const ada = user();
    const pid = s.api.project({ [ada]: "editor" });
    const fid = s.api.file(pid, "a");
    const c = open(s.url, fid, s.api.ticket(ada, fid));
    await c.synced;
    await until(() => s!.api.stores.length === 1, "the epoch's store");
    s.api.restSave(fid, "from REST");
    c.type("b");
    await until(() => c.closes.includes("reload"), "the reload", 4000);
    assert.equal(s.api.files.get(fid)!.content, "from REST", "the REST text stays");
  });

  it("refuses an update that would take the document over 1 MiB", async () => {
    s = await startStack();
    const ada = user();
    const pid = s.api.project({ [ada]: "editor" });
    const fid = s.api.file(pid, "x".repeat((1 << 20) - 10));
    const c = open(s.url, fid, s.api.ticket(ada, fid));
    await c.synced;
    c.type("y".repeat(100));
    await until(() => c.closes.includes("document_too_large"), "the refusal");
    await sleep(100);
    const doc = s.sync.hocuspocus.documents.get(fid);
    assert.ok(!doc || doc.getText("content").length === (1 << 20) - 10);
  });

  it("closes a connection over the message rate, and a socket sending too large a message", async () => {
    s = await startStack({ messagesPerSecond: 5, messageBurst: 5, maxMessageBytes: 64 << 10 });
    const ada = user();
    const pid = s.api.project({ [ada]: "editor" });
    const fid = s.api.file(pid, "");
    const c = open(s.url, fid, s.api.ticket(ada, fid));
    await c.synced;
    for (let i = 0; i < 20; i++) c.type("z");
    await until(() => c.closes.includes("rate_limited"), "the rate limit");

    const f2 = s.api.file(pid, "");
    const big = open(s.url, f2, s.api.ticket(ada, f2));
    await big.synced;
    big.type("q".repeat(100 << 10));
    await until(() => big.socketCloses.includes(1009), "the 1009 close");
  });

  it("answers /health", async () => {
    s = await startStack();
    const port = new URL(s.url).port;
    const res = await fetch(`http://127.0.0.1:${port}/health`);
    assert.equal(res.status, 200);
    const body = await res.json() as Record<string, unknown>;
    assert.equal(body.status, "ok");
    assert.equal(body.redis, "ok");
    assert.equal(body.api, "ok");
    s.api.down = true;
    await sleep(2100); // past the cached answer
    assert.equal((await fetch(`http://127.0.0.1:${port}/health`)).status, 503);
  });
});

// FR-12.9: edits made while disconnected merge on reconnect, if the role still allows editing
// and the history is the same.
describe("offline edits", () => {
  let s: Stack;
  const clients: Client[] = [];
  afterEach(async () => {
    for (const c of clients.splice(0)) c.destroy();
    await s.stop();
  });

  // A client that gets a fresh ticket on every connect, declaring the epoch it has.
  const reconnecting = (fileId: string, userId: string) => {
    let epoch: string | null = null;
    const c = connect(s.url, fileId, {
      token: async () => {
        const t = s.api.ticket(userId, fileId);
        return epoch ? `${t}.${epoch}` : t;
      },
    });
    clients.push(c);
    c.provider.on("stateless", ({ payload }: { payload: string }) => {
      const m = JSON.parse(payload) as { epoch?: string };
      if (m.epoch) epoch = m.epoch;
    });
    return c;
  };

  it("merges a member's offline edits with what others wrote meanwhile", async () => {
    s = await startStack();
    const ada = user();
    const bob = user();
    const pid = s.api.project({ [ada]: "editor", [bob]: "editor" });
    const fid = s.api.file(pid, "line\n");
    const a = reconnecting(fid, ada);
    const b = reconnecting(fid, bob);
    await Promise.all([a.synced, b.synced]);
    await until(() => a.session() !== undefined, "a's session");

    a.socket.disconnect();
    await until(() => a.socketCloses.length > 0, "a offline");
    a.type("offline by ada\n");
    b.type("online by bob\n", 0);
    await sleep(300);
    await a.socket.connect();
    const both = (t: string) => t.includes("offline by ada") && t.includes("online by bob");
    await until(() => both(a.text.toJSON()) && both(b.text.toJSON()), "the merge");
    assert.equal(a.text.toJSON(), b.text.toJSON());
    a.destroy();
    b.destroy();
    await until(() => both(s.api.files.get(fid)!.content), "the merged store");
  });

  it("drops offline edits of a member who became a viewer meanwhile", async () => {
    s = await startStack();
    const ada = user();
    const bob = user();
    const pid = s.api.project({ [ada]: "editor", [bob]: "owner" });
    const fid = s.api.file(pid, "x");
    const a = reconnecting(fid, ada);
    const b = reconnecting(fid, bob);
    await Promise.all([a.synced, b.synced]);
    await until(() => a.session() !== undefined, "a's session");
    a.socket.disconnect();
    await until(() => a.socketCloses.length > 0, "a offline");
    a.type(" offline");
    s.api.setRole(pid, ada, "viewer");
    await a.socket.connect();
    await until(() => a.session()?.readOnly === "viewer", "the viewer's session");
    await sleep(300);
    assert.equal(b.text.toJSON(), "x");
  });

  it("refuses offline edits made in another history (after a REST save rebuilt the document)", async () => {
    s = await startStack();
    const ada = user();
    const pid = s.api.project({ [ada]: "editor" });
    const fid = s.api.file(pid, "x");
    const a = reconnecting(fid, ada);
    await a.synced;
    await until(() => a.session()?.epoch !== undefined && s.api.stores.length === 1, "the epoch");
    a.socket.disconnect();
    await until(() => s.sync.hocuspocus.getDocumentsCount() === 0, "the unload");
    a.type(" offline");
    s.api.restSave(fid, "rewritten over REST");
    await a.socket.connect();
    await until(() => a.failed !== undefined, "the refusal");
    assert.equal(a.failed, "epoch_mismatch");
    assert.equal(s.api.files.get(fid)!.content, "rewritten over REST");
  });
});

// A restart: SIGTERM stores everything first; even after a crash, members' clients send what the
// server had not stored when they reconnect.
describe("restart", () => {
  let api: FakeApi;
  let child: ChildProcess | undefined;
  const clients: Client[] = [];
  afterEach(async () => {
    for (const c of clients.splice(0)) c.destroy();
    child?.kill("SIGKILL");
    await api.stop();
  });

  const port = 18000 + Math.floor(Math.random() * 1000);
  const startChild = async () => {
    const p = spawn(process.execPath, ["src/main.ts"], {
      env: {
        ...process.env, SYNC_PORT: String(port), SYNC_HOST: "127.0.0.1", API_INTERNAL_URL: api.url,
        SYNC_SERVICE_TOKEN: api.token, REDIS_URL: testRedisUrl(), APP_BASE_URL: origin, LOG_LEVEL: "info",
      },
      stdio: ["ignore", "pipe", "inherit"],
    });
    let out = "";
    p.stdout.on("data", (d: Buffer) => { out += d.toString(); });
    await until(() => out.includes('"msg":"listening"'), "the child to listen", 10_000);
    child = p;
    return { p, log: () => out };
  };

  it("stores everything on SIGTERM and serves it again after a restart", async () => {
    api = new FakeApi();
    await api.start();
    const redis = new Redis(testRedisUrl());
    await redis.flushdb();
    redis.disconnect();
    const ada = user();
    const pid = api.project({ [ada]: "editor" });
    const fid = api.file(pid, "start");
    const first = await startChild();
    const url = `ws://127.0.0.1:${port}`;
    const c = connect(url, fid, { token: async () => api.ticket(ada, fid) });
    clients.push(c);
    await c.synced;
    await until(() => api.stores.length === 1, "the epoch's store");
    c.type(" edited");
    await sleep(100); // the server has it, well before its 2 s debounce
    first.p.kill("SIGTERM");
    const [code] = await once(first.p, "exit") as [number];
    assert.equal(code, 0);
    assert.equal(api.files.get(fid)!.content, "start edited");
    assert.ok(c.closes.includes("shutting_down") || c.socketCloses.includes(1001), "the client was told");
    assert.ok(first.log().includes('"msg":"shut down"'));

    await startChild();
    const d = connect(url, fid, { token: api.ticket(ada, fid) });
    clients.push(d);
    await d.synced;
    assert.equal(d.text.toJSON(), "start edited");
  });

  it("recovers edits the server had not stored when it crashed, from the members' clients", async () => {
    api = new FakeApi();
    await api.start();
    const ada = user();
    const pid = api.project({ [ada]: "editor" });
    const fid = api.file(pid, "start");
    const first = await startChild();
    const url = `ws://127.0.0.1:${port}`;
    let epoch: string | null = null;
    const c = connect(url, fid, { token: async () => (epoch ? `${api.ticket(ada, fid)}.${epoch}` : api.ticket(ada, fid)) });
    c.provider.on("stateless", ({ payload }: { payload: string }) => { epoch = (JSON.parse(payload) as { epoch: string }).epoch; });
    clients.push(c);
    await c.synced;
    await until(() => api.stores.length === 1, "the epoch's store");
    c.type(" unsaved");
    await sleep(100);
    first.p.kill("SIGKILL");
    await once(first.p, "exit");
    assert.equal(api.files.get(fid)!.content, "start", "lost from the server");

    await startChild();
    // The provider reconnects by itself with a new ticket and its epoch, and sends what it has.
    await until(() => api.files.get(fid)!.content === "start unsaved", "the recovered edit", 10_000);
  });
});
