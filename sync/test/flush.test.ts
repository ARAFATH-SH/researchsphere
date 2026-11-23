import assert from "node:assert/strict";
import { afterEach, describe, it } from "node:test";
import { eventsChannel, flushAckPrefix } from "../src/live.ts";
import { redisDb } from "../src/config.ts";
import { connect, startStack, until, user, type Client, type Stack } from "./helpers.ts";

// Slice 23: a compile asks the sync server to store the project's open documents now, so it
// compiles live edits, and waits for the answer on a Redis list (docs/sync.md, Flush).

describe("flush for a compile", () => {
  let s: Stack;
  const clients: Client[] = [];
  afterEach(async () => {
    for (const c of clients.splice(0)) c.destroy();
    await s.stop();
  });
  const flush = async (projectId: string, requestId: string): Promise<string | null> => {
    // The API waits on a connection of its own; the stack's client is the server's too.
    const waiter = s.redis.duplicate();
    try {
      await s.redis.publish(eventsChannel(redisDb(s.config.redisUrl)), JSON.stringify({ kind: "flush", projectId, requestId }));
      const res = await waiter.blpop(flushAckPrefix + requestId, 5);
      return res === null ? null : res[1];
    } finally {
      waiter.disconnect();
    }
  };
  const id = () => crypto.randomUUID().replaceAll("-", "");

  it("stores the project's unsaved documents at once and answers ok", async () => {
    // A long debounce: without the flush nothing would be stored within the test.
    s = await startStack({ debounceMs: 60_000, maxDebounceMs: 120_000 });
    const u = user();
    const pid = s.api.project({ [u]: "editor" });
    const other = s.api.project({ [u]: "editor" });
    const fid = s.api.file(pid, "");
    const elsewhere = s.api.file(other, "");
    const c = connect(s.url, fid, { token: s.api.ticket(u, fid) });
    const d = connect(s.url, elsewhere, { token: s.api.ticket(u, elsewhere) });
    clients.push(c, d);
    await Promise.all([c.synced, d.synced]);
    c.type("live edit");
    d.type("other project");
    await until(() => s.api.files.get(fid) !== undefined && c.text.toJSON() === "live edit", "the edit");
    await new Promise((r) => setTimeout(r, 200)); // the update reaches the server
    const before = s.api.stores.length;

    assert.equal(await flush(pid, id()), "ok");
    const stored = s.api.stores.slice(before);
    assert.ok(stored.some((r) => r.fileId === fid && r.content === "live edit"), "the live text is stored");
    assert.ok(!stored.some((r) => r.fileId === elsewhere), "another project's document is not stored");
  });

  it("answers ok at once when nothing is unsaved, and failed when a store fails", async () => {
    s = await startStack({ debounceMs: 60_000, maxDebounceMs: 120_000 });
    const u = user();
    const pid = s.api.project({ [u]: "editor" });
    assert.equal(await flush(pid, id()), "ok", "no open document");

    const fid = s.api.file(pid, "");
    const c = connect(s.url, fid, { token: s.api.ticket(u, fid) });
    clients.push(c);
    await c.synced;
    c.type("x");
    await new Promise((r) => setTimeout(r, 200));
    s.api.failStores = 100;
    assert.equal(await flush(pid, id()), "failed");
    s.api.failStores = 0;
  });

  it("ignores a request without a valid ID", async () => {
    s = await startStack();
    await s.redis.publish(eventsChannel(redisDb(s.config.redisUrl)), JSON.stringify({ kind: "flush", projectId: "p", requestId: "../x" }));
    await until(() => s.logs.some((l) => l.msg === "flush request without a project or a valid request ID"), "the warning");
  });
});
