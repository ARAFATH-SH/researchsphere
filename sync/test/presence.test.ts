import assert from "node:assert/strict";
import { afterEach, describe, it } from "node:test";
import { eventsChannel } from "../src/live.ts";
import { redisDb } from "../src/config.ts";
import { connect, sleep, startStack, until, user, type Client, type Presence, type Stack } from "./helpers.ts";

// FR-12.5: the presence of a project (who edits which file) goes to every member connected to
// one of its files, leaves viewers and other projects out, and stops reaching a removed member.
// NFR-10.1 on the client: a "stored" message after a store leaves nothing unsaved.

describe("presence and stored", () => {
  let s: Stack;
  const clients: Client[] = [];
  const open = (fileId: string, userId: string) => {
    const c = connect(s.url, fileId, { token: s.api.ticket(userId, fileId) });
    clients.push(c);
    return c;
  };
  const publish = (ev: unknown) => s.redis.publish(eventsChannel(redisDb(s.config.redisUrl)), JSON.stringify(ev));
  const last = (c: Client): Presence | undefined => c.presence.at(-1);
  const ids = (p: Presence | undefined, fileId: string) => (p?.files[fileId] ?? []).map((u) => u.id).sort();
  afterEach(async () => {
    for (const c of clients.splice(0)) c.destroy();
    await s.stop();
  });

  it("tells every member of the project who edits which file, with names and colours, without viewers", async () => {
    s = await startStack();
    const [ada, bob, vic] = [user(), user(), user()];
    s.api.names.set(ada, "Ada Lovelace");
    s.api.names.set(bob, "Bob Kahn");
    const pid = s.api.project({ [ada]: "owner", [bob]: "editor", [vic]: "viewer" });
    const main = s.api.file(pid, "main");
    const intro = s.api.file(pid, "intro");
    const a = open(main, ada);
    const b = open(intro, bob);
    const v = open(main, vic);
    await Promise.all([a.synced, b.synced, v.synced]);

    for (const c of [a, b, v]) {
      await until(() => ids(last(c), main).length === 1 && ids(last(c), intro).length === 1, "presence of both files");
    }
    const p = last(v)!;
    assert.deepEqual(ids(p, main), [ada], "the viewer is not listed");
    assert.deepEqual(ids(p, intro), [bob]);
    assert.equal(p.files[main]![0]!.name, "Ada Lovelace");
    assert.equal(p.files[main]![0]!.colour, a.session()!.colour, "the colour of that file");
    assert.equal(p.files[main]![0]!.readOnly, null);

    // Bob closes his file: everyone sees it go.
    b.destroy();
    await until(() => last(a)?.files[intro] === undefined, "bob gone");
  });

  it("never sends another project's files, and stops at a removed member", async () => {
    s = await startStack();
    const [ada, bob, eve] = [user(), user(), user()];
    const pid = s.api.project({ [ada]: "owner", [bob]: "editor" });
    const other = s.api.project({ [eve]: "owner" });
    const fa = s.api.file(pid, "");
    const fe = s.api.file(other, "");
    const a = open(fa, ada);
    const b = open(fa, bob);
    const e = open(fe, eve);
    await Promise.all([a.synced, b.synced, e.synced]);
    await until(() => ids(last(a), fa).length === 2, "both in the file");
    await until(() => last(e) !== undefined, "eve's presence");
    for (const c of [a, b]) for (const p of c.presence) assert.equal(p.files[fe], undefined, "no file of another project");
    for (const p of e.presence) assert.equal(p.files[fa], undefined, "no file of another project");

    s.api.setRole(pid, bob, null);
    await publish({ kind: "members_changed", projectId: pid, userIds: [bob] });
    await until(() => b.closes.includes("access_ended"), "bob's close");
    await until(() => ids(last(a), fa).join() === ada, "bob gone from presence");
    const seen = b.presence.length;
    a.type("x");
    await sleep(200);
    assert.equal(b.presence.length, seen, "a removed member gets no more presence");
  });

  it("lists an editor waiting for a slot as capacity", async () => {
    s = await startStack({ maxEditors: 1 });
    const [ada, bob] = [user(), user()];
    const pid = s.api.project({ [ada]: "owner", [bob]: "editor" });
    const fid = s.api.file(pid, "");
    const a = open(fid, ada);
    await a.synced;
    const b = open(fid, bob);
    await b.synced;
    await until(() => ids(last(a), fid).length === 2, "both listed");
    const byId = new Map(last(a)!.files[fid]!.map((u) => [u.id, u]));
    assert.equal(byId.get(ada)!.readOnly, null);
    assert.equal(byId.get(bob)!.readOnly, "capacity");
  });

  it("says stored with the version once a store leaves nothing unsaved", async () => {
    s = await startStack();
    const [ada, bob] = [user(), user()];
    const pid = s.api.project({ [ada]: "owner", [bob]: "editor" });
    const fid = s.api.file(pid, "");
    const a = open(fid, ada);
    const b = open(fid, bob);
    await Promise.all([a.synced, b.synced]);
    await until(() => s.api.stores.length === 1, "the epoch's store");
    a.type("hello");
    await until(() => s.api.stores.length === 2, "the store", 6000);
    const version = s.api.files.get(fid)!.version;
    await until(() => a.stored.at(-1) === version && b.stored.at(-1) === version, "stored on both clients");
  });
});
