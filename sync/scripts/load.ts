import { spawn } from "node:child_process";
import { Redis } from "ioredis";
import { FakeApi } from "../test/fake-api.ts";
import { connect, origin, sleep, testRedisUrl, until, type Client } from "../test/helpers.ts";

// NFR-2 and NFR-12 (docs/evaluation.md, "Real-time editing"): 10 files, each edited by 4 members
// at once, typing 5 times a second. Reports the time from one member's edit to the other three
// seeing it (target: p95 under 1 s) and from an edit to its store (target: every edit within 5 s,
// NFR-10.1). The sync server runs in a process of its own; the API is the in-memory fake with a
// store time of STORE_MS, so this measures the sync server, not PostgreSQL.
//
// make sync-load [seconds=60]; needs Redis (make up); uses TEST_REDIS_URL (DB 15), which it empties.

const files = Number(process.env.LOAD_FILES ?? 10);
const editorsPerFile = Number(process.env.LOAD_EDITORS ?? 4);
const seconds = Number(process.env.LOAD_SECONDS ?? 60);
const perSecond = Number(process.env.LOAD_RATE ?? 5);
const storeMs = Number(process.env.STORE_MS ?? 20);
const port = 18900 + Math.floor(Math.random() * 90);

const api = new FakeApi();
await api.start();
api.storeDelayMs = storeMs;
const redisUrl = testRedisUrl();
const redis = new Redis(redisUrl);
await redis.flushdb();
redis.disconnect();

const server = spawn(process.execPath, ["src/main.ts"], {
  env: {
    ...process.env, SYNC_PORT: String(port), SYNC_HOST: "127.0.0.1", API_INTERNAL_URL: api.url,
    SYNC_SERVICE_TOKEN: api.token, REDIS_URL: redisUrl, APP_BASE_URL: origin, LOG_LEVEL: "warn",
    // Every simulated editor may edit; a run with LOAD_EDITORS above 8 is refused at startup.
    SYNC_MAX_EDITORS_PER_FILE: String(editorsPerFile),
  },
  stdio: ["ignore", "pipe", "inherit"],
});
let serverOut = "";
server.stdout.on("data", (d: Buffer) => { serverOut += d.toString(); });
await sleep(1500);

const sentAt = new Map<string, number>();
const storedAt = new Map<string, number>();
const latencies: number[] = [];
const tokenPattern = /\[f\d+u\d+n\d+\]/g;
api.onStore = (r) => {
  for (const t of r.content.match(tokenPattern) ?? []) if (!storedAt.has(t)) storedAt.set(t, r.at);
};

const clients: Client[][] = [];
for (let f = 0; f < files; f++) {
  const users = Array.from({ length: editorsPerFile }, () => crypto.randomUUID());
  const pid = api.project(Object.fromEntries(users.map((u) => [u, "editor" as const])));
  const fid = api.file(pid, "\\documentclass{article}\n");
  const row: Client[] = [];
  for (const u of users) {
    const c = connect(`ws://127.0.0.1:${port}`, fid, { token: async () => api.ticket(u, fid) });
    c.text.observe((ev) => {
      if (ev.transaction.local) return;
      const now = Date.now();
      for (const d of ev.delta) {
        if (typeof d.insert !== "string") continue;
        for (const t of d.insert.match(tokenPattern) ?? []) {
          const at = sentAt.get(t);
          if (at !== undefined) latencies.push(now - at);
        }
      }
    });
    row.push(c);
  }
  clients.push(row);
}
await Promise.all(clients.flat().map((c) => c.synced));
const failed = clients.flat().filter((c) => c.failed !== undefined);
if (failed.length > 0) throw new Error(`${failed.length} clients refused: ${failed[0]!.failed}`);
console.log(`connected: ${files} files x ${editorsPerFile} editors; typing ${perSecond}/s each for ${seconds} s`);

let seq = 0;
const stopAt = Date.now() + seconds * 1000;
const typers = clients.flatMap((row, f) => row.map(async (c, u) => {
  await sleep(Math.random() * (1000 / perSecond)); // spread the clients over the interval
  while (Date.now() < stopAt) {
    const t = `[f${f}u${u}n${seq++}]`;
    sentAt.set(t, Date.now());
    c.type(t);
    await sleep(1000 / perSecond);
  }
}));
await Promise.all(typers);
const lastEdit = Date.now();
await until(() => storedAt.size === sentAt.size, "every edit stored", 15_000).catch(() => undefined);
const settled = Date.now();

const pct = (xs: number[], p: number) => {
  if (xs.length === 0) return NaN;
  const sorted = [...xs].sort((a, b) => a - b);
  return sorted[Math.min(sorted.length - 1, Math.ceil((p / 100) * sorted.length) - 1)]!;
};
const saveDelays = [...sentAt].filter(([t]) => storedAt.has(t)).map(([t, at]) => storedAt.get(t)! - at);
const lost = [...sentAt.keys()].filter((t) => !storedAt.has(t));
const startedAt = Math.min(...sentAt.values());
const worst = [...sentAt].filter(([t]) => storedAt.has(t)).map(([t, at]) => ({ t, sentS: (at - startedAt) / 1000, delay: storedAt.get(t)! - at }))
  .sort((a, b) => b.delay - a.delay).slice(0, 5);
const expectedDeliveries = sentAt.size * (editorsPerFile - 1);
let diverged = 0;
for (const row of clients) {
  const texts = new Set(row.map((c) => c.text.toJSON()));
  if (texts.size !== 1) diverged++;
}

console.log(JSON.stringify({
  files, editorsPerFile, seconds, ratePerEditor: perSecond, storeMs,
  edits: sentAt.size,
  updateLatencyMs: { deliveries: latencies.length, expected: expectedDeliveries, p50: pct(latencies, 50), p95: pct(latencies, 95), p99: pct(latencies, 99), max: Math.max(...latencies) },
  saveDelayMs: { stored: saveDelays.length, p50: pct(saveDelays, 50), p95: pct(saveDelays, 95), max: Math.max(...saveDelays) },
  lastEditToSettledMs: settled - lastEdit,
  stores: api.stores.length,
  worstSaves: worst,
  lostEdits: lost.length,
  divergedFiles: diverged,
}, null, 2));

for (const c of clients.flat()) c.destroy();
server.kill("SIGTERM");
await new Promise((r) => server.once("exit", r));
await api.stop();
if (serverOut.includes('"level":"ERROR"')) console.log("server errors:\n" + serverOut);

const ok = pct(latencies, 95) < 1000 && Math.max(...saveDelays) < 5000 && lost.length === 0 && diverged === 0 &&
  latencies.length === expectedDeliveries;
console.log(ok ? "PASS: NFR-2 p95 < 1 s, NFR-10.1 every edit stored within 5 s, nothing lost" : "FAIL");
process.exit(ok ? 0 : 1);
