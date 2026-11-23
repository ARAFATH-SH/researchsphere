import { Redis } from "ioredis";
import { createApi } from "./api.ts";
import { loadConfig } from "./config.ts";
import { createLogger } from "./log.ts";
import { createSyncServer } from "./server.ts";

// The sync server's entry point (docs/sync.md). SIGTERM and SIGINT drain: no more edits are
// taken, every document is stored, then every connection closes and the process exits.

let config;
try {
  config = loadConfig(process.env);
} catch (err) {
  process.stderr.write(`${(err as Error).message}\n`);
  process.exit(2);
}
const logger = createLogger(config.logLevel);
const redis = new Redis(config.redisUrl, {
  // Fail fast: a live mark or an event that cannot reach Redis is handled, not queued.
  enableOfflineQueue: false,
  maxRetriesPerRequest: 1,
  commandTimeout: 2000,
  lazyConnect: false,
});
redis.on("error", (err: Error) => { logger.warn("redis", { error: err.message }); });

const sync = createSyncServer({ config, api: createApi(config.apiUrl, config.serviceToken), redis, logger });

let stopping = false;
const stop = (signal: string) => {
  if (stopping) return;
  stopping = true;
  logger.info("signal received", { signal });
  sync.drain()
    .catch((err: unknown) => { logger.error("drain failed", { error: err }); })
    .finally(() => {
      redis.disconnect();
      process.exit(0);
    });
};
process.on("SIGTERM", () => { stop("SIGTERM"); });
process.on("SIGINT", () => { stop("SIGINT"); });

// Redis may still be starting: documents refuse to open ("unavailable") until it answers, and
// /health says degraded meanwhile.
await sync.start();
