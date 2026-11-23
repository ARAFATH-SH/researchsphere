import type { Redis } from "ioredis";

// The live mark (docs/decisions.md 2026-11-19): while this server holds a file's document, the
// key sync:live:<fileId> exists, and the API refuses REST saves of the file. The mark is set
// before the file is read and renewed while the document is open; a crashed server's marks
// expire after their TTL.

export const livePrefix = "sync:live:";

export class LiveMarks {
  private readonly redis: Redis;
  private readonly ttlMs: number;

  constructor(redis: Redis, ttlMs: number) {
    this.redis = redis;
    this.ttlMs = ttlMs;
  }

  async mark(fileId: string): Promise<void> {
    await this.redis.set(livePrefix + fileId, "1", "PX", this.ttlMs);
  }

  /** Renews the marks of the files given, in one round trip. */
  async renew(fileIds: string[]): Promise<void> {
    if (fileIds.length === 0) return;
    const p = this.redis.pipeline();
    for (const id of fileIds) p.set(livePrefix + id, "1", "PX", this.ttlMs);
    const results = await p.exec();
    const failed = results?.find(([err]) => err !== null);
    if (failed?.[0]) throw failed[0];
  }

  async unmark(fileIds: string[]): Promise<void> {
    if (fileIds.length === 0) return;
    await this.redis.del(...fileIds.map((id) => livePrefix + id));
  }
}

/** The pub/sub channel the API publishes access changes on (projectredis.SyncChannel). */
export function eventsChannel(db: number): string {
  return `sync:${db}:events`;
}

/**
 * The list a flush's answer goes to, followed by its request ID (projectredis.FlushAckPrefix):
 * the API waits on it with BLPOP.
 */
export const flushAckPrefix = "sync:flushack:";
