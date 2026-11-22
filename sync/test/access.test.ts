import assert from "node:assert/strict";
import { afterEach, describe, it } from "node:test";
import { eventsChannel } from "../src/live.ts";
import { redisDb } from "../src/config.ts";
import { connect, sleep, startStack, until, user, type Client, type Stack } from "./helpers.ts";

// FR-12.8: four editors at most by default (SYNC_MAX_EDITORS_PER_FILE), counted per user; FR-12.7: role changes and removals take
// effect within seconds, by event or by the periodic check; deleted files and ended sessions
// close their connections.

describe("capacity and access", () => {
  let s: Stack;
  const clients: Client[] = [];
  const open = (fileId: string, userId: string) => {
    const c = connect(s.url, fileId, { token: s.api.ticket(userId, fileId) });
    clients.push(c);
    return c;
  };
  const publish = (ev: unknown) => s.redis.publish(eventsChannel(redisDb(s.config.redisUrl)), JSON.stringify(ev));
  afterEach(async () => {
    for (const c of clients.splice(0)) c.destroy();
    await s.stop();
  });

  it("opens the file read-only for a fifth editor, says why, and promotes them when a slot frees", async () => {
    s = await startStack();
    assert.equal(s.config.limits.maxEditors, 4, "the default (FR-12.4)");
    const editors = Array.from({ length: 5 }, user);
    const vic = user();
    const pid = s.api.project({ ...Object.fromEntries(editors.map((u) => [u, "editor"])), [vic]: "viewer" });
    const fid = s.api.file(pid, "");

    const first4 = editors.slice(0, 4).map((u) => open(fid, u));
    // One editor's second tab and a viewer do not take a slot.
    const secondTab = open(fid, editors[0]!);
    const viewer = open(fid, vic);
    await Promise.all([...first4, secondTab, viewer].map((c) => c.synced));
    const fifth = open(fid, editors[4]!);
    await fifth.synced;
    await until(() => fifth.session() !== undefined, "the fifth's session");
    // The client builds the reason from the number (FR-12.8).
    assert.equal(fifth.session()!.readOnly, "capacity");
    assert.equal(fifth.session()!.maxEditors, 4);
    assert.equal(secondTab.session()!.readOnly, null);
    assert.equal(viewer.session()!.readOnly, "viewer");

    // Distinct colours for the four editors (FR-12.5).
    const colours = new Set(first4.map((c) => c.session()!.colour));
    assert.equal(colours.size, 4);

    fifth.type("not allowed");
    first4[1]!.type("allowed");
    await until(() => fifth.text.toJSON().includes("allowed") && first4[0]!.text.toJSON() === "allowed", "the editor's text");
    await sleep(150);
    assert.equal(first4[0]!.text.toJSON(), "allowed");

    // An editor leaves: the fifth may edit now and is told so. What it sent while read-only
    // was dropped then; once it may edit, the server asks for it, so both sides converge.
    first4[3]!.destroy();
    await until(() => fifth.session()!.readOnly === null, "the promotion");
    fifth.type("!");
    await until(() => first4[0]!.text.toJSON().endsWith("!"), "the promoted editor's text");
    await until(() => fifth.text.toJSON() === first4[0]!.text.toJSON(), "convergence");
  });

  it("takes the editor limit from the configuration (SYNC_MAX_EDITORS_PER_FILE)", async () => {
    s = await startStack({ maxEditors: 2 });
    const [a, b, c] = [user(), user(), user()];
    const pid = s.api.project({ [a]: "owner", [b]: "editor", [c]: "editor" });
    const fid = s.api.file(pid, "");
    const first = [open(fid, a), open(fid, b)];
    await Promise.all(first.map((x) => x.synced));
    const third = open(fid, c);
    await third.synced;
    await until(() => third.session() !== undefined, "the third's session");
    assert.equal(third.session()!.readOnly, "capacity");
    assert.equal(third.session()!.maxEditors, 2);
    assert.equal(first[0]!.session()!.maxEditors, 2, "every session message carries the limit");
  });

  it("ends a removed member's connection within seconds of the event", async () => {
    s = await startStack();
    const ada = user();
    const bob = user();
    const pid = s.api.project({ [ada]: "owner", [bob]: "editor" });
    const fid = s.api.file(pid, "x");
    const owner = open(fid, ada);
    const editor = open(fid, bob);
    await Promise.all([owner.synced, editor.synced]);

    s.api.setRole(pid, bob, null);
    const sent = Date.now();
    await publish({ kind: "members_changed", projectId: pid, userIds: [bob] });
    await until(() => editor.closes.includes("access_ended"), "the close");
    assert.ok(Date.now() - sent < 2000, "within 2 s");
    editor.type("after removal");
    await sleep(200);
    assert.equal(owner.text.toJSON(), "x");
  });

  it("makes a member turned viewer read-only in place", async () => {
    s = await startStack();
    const ada = user();
    const bob = user();
    const pid = s.api.project({ [ada]: "owner", [bob]: "editor" });
    const fid = s.api.file(pid, "x");
    const owner = open(fid, ada);
    const editor = open(fid, bob);
    await Promise.all([owner.synced, editor.synced]);

    s.api.setRole(pid, bob, "viewer");
    await publish({ kind: "members_changed", projectId: pid, userIds: [bob] });
    await until(() => editor.session()?.readOnly === "viewer", "the downgrade");
    assert.equal(editor.closes.length, 0, "the connection stays open");
    editor.type(" by a viewer");
    owner.type("!");
    await until(() => editor.text.toJSON().includes("!"), "the owner's text");
    await sleep(150);
    assert.equal(owner.text.toJSON(), "x!");

    // And back to editor: may edit again, as a slot is free.
    s.api.setRole(pid, bob, "editor");
    await publish({ kind: "members_changed", projectId: pid, userIds: [bob] });
    await until(() => editor.session()?.readOnly === null, "the upgrade");
  });

  it("catches a change the event missed at the periodic check", async () => {
    s = await startStack({ recheckMs: 300 });
    const ada = user();
    const bob = user();
    const pid = s.api.project({ [ada]: "owner", [bob]: "editor" });
    const fid = s.api.file(pid, "x");
    const editor = open(fid, bob);
    await editor.synced;
    s.api.setRole(pid, bob, null); // no event
    await until(() => editor.closes.includes("access_ended"), "the close", 3000);
  });

  it("closes the connections of a deleted file and of a user whose session ended", async () => {
    s = await startStack();
    const ada = user();
    const bob = user();
    const pid = s.api.project({ [ada]: "owner", [bob]: "editor" });
    const f1 = s.api.file(pid, "one");
    const f2 = s.api.file(pid, "two");
    const a1 = open(f1, ada);
    const b2 = open(f2, bob);
    const a2 = open(f2, ada);
    await Promise.all([a1.synced, b2.synced, a2.synced]);

    s.api.files.delete(f1);
    await publish({ kind: "files_deleted", projectId: pid, fileIds: [f1] });
    await until(() => a1.closes.includes("file_gone"), "the file's close");

    await publish({ kind: "user_ended", userIds: [bob] });
    await until(() => b2.closes.includes("session_ended"), "the session's close");
    assert.equal(a2.closes.length, 0, "other users stay");
  });
});
