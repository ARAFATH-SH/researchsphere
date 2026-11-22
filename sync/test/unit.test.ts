import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { loadConfig, redisDb } from "../src/config.ts";
import { Bucket } from "../src/limits.ts";
import { parseToken } from "../src/server.ts";
import { Sessions } from "../src/sessions.ts";

const token = "t".repeat(40);

describe("config", () => {
  it("reads the settings with their defaults", () => {
    const c = loadConfig({ SYNC_SERVICE_TOKEN: token, REDIS_URL: "redis://localhost:6379/0" });
    assert.equal(c.port, 8091);
    assert.equal(c.host, "127.0.0.1");
    assert.equal(c.apiUrl, "http://127.0.0.1:8081/api/v1");
    assert.deepEqual(c.allowedOrigins, ["http://localhost:3000", "http://localhost:3001", "http://127.0.0.1:3000", "http://127.0.0.1:3001"]);
    assert.equal(c.limits.maxEditors, 4);
    assert.ok(c.limits.debounceMs <= 2000 && c.limits.maxDebounceMs < 5000, "NFR-10.1");
  });

  it("allows APP_BASE_URL, SYNC_ALLOWED_ORIGINS and, outside production, the local dev servers", () => {
    const base = { SYNC_SERVICE_TOKEN: token, REDIS_URL: "redis://localhost:6379/0" };
    const dev = loadConfig({ ...base, APP_ENV: "development", APP_BASE_URL: "http://localhost:3002/app", SYNC_ALLOWED_ORIGINS: " https://a.example , https://b.example/x " });
    assert.deepEqual(dev.allowedOrigins, [
      "http://localhost:3002", "https://a.example", "https://b.example",
      "http://localhost:3000", "http://localhost:3001", "http://127.0.0.1:3000", "http://127.0.0.1:3001",
    ]);
    const prod = loadConfig({ ...base, APP_ENV: "production", APP_BASE_URL: "https://researchsphere.example" });
    assert.deepEqual(prod.allowedOrigins, ["https://researchsphere.example"], "no local origin in production");
    assert.throws(() => loadConfig({ ...base, SYNC_ALLOWED_ORIGINS: "not a url" }), /SYNC_ALLOWED_ORIGINS/);
    assert.throws(() => loadConfig({ ...base, SYNC_ALLOWED_ORIGINS: "ftp://x.example" }), /SYNC_ALLOWED_ORIGINS/);
    assert.throws(() => loadConfig({ ...base, APP_ENV: "staging" }), /APP_ENV/);
  });

  it("reads the editors per file (FR-12.8) and refuses a number outside 1 to 8", () => {
    const base = { SYNC_SERVICE_TOKEN: token, REDIS_URL: "redis://localhost:6379/0" };
    assert.equal(loadConfig({ ...base, SYNC_MAX_EDITORS_PER_FILE: "3" }).limits.maxEditors, 3);
    assert.equal(loadConfig({ ...base, SYNC_MAX_EDITORS_PER_FILE: "8" }).limits.maxEditors, 8);
    for (const bad of ["0", "9", "2.5", "x", "-1"]) {
      assert.throws(() => loadConfig({ ...base, SYNC_MAX_EDITORS_PER_FILE: bad }), /SYNC_MAX_EDITORS_PER_FILE/, bad);
    }
  });

  it("refuses bad settings, naming each, without repeating the token", () => {
    assert.throws(
      () => loadConfig({ SYNC_SERVICE_TOKEN: "short-secret", REDIS_URL: "localhost", SYNC_PORT: "x", API_INTERNAL_URL: "ftp://a", APP_BASE_URL: "::" }),
      (err: Error) => ["SYNC_PORT", "API_INTERNAL_URL", "SYNC_SERVICE_TOKEN", "REDIS_URL", "APP_BASE_URL"].every((k) => err.message.includes(k)) &&
        !err.message.includes("short-secret"),
    );
  });

  it("reads the Redis database number", () => {
    assert.equal(redisDb("redis://localhost:6379"), 0);
    assert.equal(redisDb("redis://:pw@localhost:6379/15"), 15);
  });
});

describe("token", () => {
  it("is the ticket, with the epoch of offline edits after a dot", () => {
    assert.deepEqual(parseToken("abc_-123"), { ticket: "abc_-123", epoch: null });
    assert.deepEqual(parseToken("abc.0193-e"), { ticket: "abc", epoch: "0193-e" });
    assert.deepEqual(parseToken("abc."), { ticket: "abc", epoch: null });
  });
});

describe("rate bucket", () => {
  it("allows the burst, then the rate", () => {
    let t = 0;
    const b = new Bucket(2, 3, () => t);
    assert.deepEqual([b.take(), b.take(), b.take(), b.take()], [true, true, true, false]);
    t = 500;
    assert.deepEqual([b.take(), b.take()], [true, false]);
  });
});

describe("sessions", () => {
  it("counts a user once, keeps viewers out of the count, and promotes the earliest waiting", () => {
    let t = 0;
    const s = new Sessions<string>(2, () => t++);
    assert.equal(s.admit("d", "a", "editor", "s1").reason, null);
    assert.equal(s.admit("d", "a", "editor", "s2").reason, null, "a second tab");
    assert.equal(s.admit("d", "v", "viewer", "s3").reason, "viewer");
    assert.equal(s.admit("d", "b", "owner", "s4").reason, null);
    assert.equal(s.admit("d", "c", "editor", "s5").reason, "capacity");
    assert.equal(s.admit("d", "e", "editor", "s6").reason, "capacity");
    assert.equal(s.editors("d"), 2);
    for (const [u, sock] of [["a", "s1"], ["a", "s2"], ["b", "s4"], ["c", "s5"], ["e", "s6"]] as const) s.attach("d", u, sock, `${u}-${sock}`);

    assert.deepEqual(s.detach("d", "a", "s1", "a-s1"), [], "a still has a tab");
    const promoted = s.detach("d", "a", "s2", "a-s2");
    assert.deepEqual(promoted.map((e) => e.userId), ["c"], "the earliest waiting");

    // b becomes a viewer: e takes the slot.
    const changed = s.setRole("d", "b", "viewer");
    assert.deepEqual(changed.map((e) => [e.userId, e.reason]), [["b", "viewer"], ["e", null]]);
    // b an editor again: no slot, so b waits.
    assert.deepEqual(s.setRole("d", "b", "editor").map((e) => [e.userId, e.reason]), [["b", "capacity"]]);
    assert.deepEqual(s.remove("d", "c").map((e) => e.userId), ["b"]);
    assert.equal(s.connectionsOf("e"), 1);
  });

  it("drops a reservation whose connection never came", () => {
    let t = 0;
    const s = new Sessions<string>(1, () => t);
    s.admit("d", "a", "editor", "s1");
    t = 31_000;
    assert.equal(s.admit("d", "b", "editor", "s2").reason, null, "a's lapsed place is free");
    assert.equal(s.connectionsOf("a"), 0);
  });
});
