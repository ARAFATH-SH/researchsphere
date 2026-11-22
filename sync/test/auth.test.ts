import assert from "node:assert/strict";
import { afterEach, beforeEach, describe, it } from "node:test";
import { connect, sleep, startStack, until, user, type Client, type Stack } from "./helpers.ts";

// FR-12.7: a member's ticket opens the file once; everyone else is refused; a viewer's
// connection is read-only and what it sends is dropped and logged.

describe("authentication", () => {
  let s: Stack;
  const clients: Client[] = [];
  const open = (fileId: string, token: string | (() => Promise<string>), origin?: string) => {
    const c = connect(s.url, fileId, origin === undefined ? { token } : { token, origin });
    clients.push(c);
    return c;
  };
  beforeEach(async () => { s = await startStack(); });
  afterEach(async () => {
    for (const c of clients.splice(0)) c.destroy();
    await s.stop();
  });

  it("opens the file with a valid ticket and says the role, colour and epoch", async () => {
    const ada = user();
    const pid = s.api.project({ [ada]: "editor" });
    const fid = s.api.file(pid, "\\section{Intro}");
    const c = open(fid, s.api.ticket(ada, fid));
    await c.synced;
    assert.equal(c.text.toJSON(), "\\section{Intro}");
    await until(() => c.session() !== undefined, "the session message");
    const sess = c.session()!;
    assert.equal(sess.role, "editor");
    assert.equal(sess.readOnly, null);
    assert.match(sess.colour ?? "", /^#[0-9A-F]{6}$/);
    assert.match(sess.epoch ?? "", /^[0-9a-f-]{36}$/);
  });

  it("refuses a ticket used twice, an expired one, one for another file and a non-member's", async () => {
    const ada = user();
    const stranger = user();
    const pid = s.api.project({ [ada]: "editor" });
    const fid = s.api.file(pid, "x");
    const other = s.api.file(pid, "y");

    const ticket = s.api.ticket(ada, fid);
    const first = open(fid, ticket);
    await first.synced;
    const again = open(fid, ticket);
    await until(() => again.failed !== undefined, "the refusal");
    assert.equal(again.failed, "ticket_invalid");

    const expired = open(fid, s.api.ticket(ada, fid, 1));
    await until(() => expired.failed !== undefined, "the refusal");
    assert.equal(expired.failed, "ticket_invalid");

    const wrongFile = open(other, s.api.ticket(ada, fid));
    await until(() => wrongFile.failed !== undefined, "the refusal");
    assert.equal(wrongFile.failed, "ticket_invalid");

    const outsider = open(fid, s.api.ticket(stranger, fid));
    await until(() => outsider.failed !== undefined, "the refusal");
    assert.equal(outsider.failed, "ticket_invalid");
    assert.equal(outsider.text.toJSON(), "", "a refused client receives nothing");
  });

  it("refuses another origin and logs it", async () => {
    const ada = user();
    const pid = s.api.project({ [ada]: "owner" });
    const fid = s.api.file(pid, "x");
    const c = open(fid, s.api.ticket(ada, fid), "https://evil.example");
    await until(() => c.failed !== undefined, "the refusal");
    assert.equal(c.failed, "origin_not_allowed");
    const line = s.logs.find((l) => l.msg === "connection refused: origin not allowed");
    assert.equal(line?.fields.origin, "https://evil.example");
  });

  it("accepts every configured origin", async () => {
    await s.stop();
    s = await startStack({}, { allowedOrigins: ["http://localhost:3000", "http://127.0.0.1:3001"] });
    const ada = user();
    const pid = s.api.project({ [ada]: "owner" });
    const fid = s.api.file(pid, "x");
    const c = open(fid, s.api.ticket(ada, fid), "http://127.0.0.1:3001");
    await c.synced;
    assert.equal(c.text.toJSON(), "x");
  });

  it("makes a viewer read-only and drops and logs what it sends", async () => {
    const ada = user();
    const vic = user();
    const pid = s.api.project({ [ada]: "editor", [vic]: "viewer" });
    const fid = s.api.file(pid, "base");
    const editor = open(fid, s.api.ticket(ada, fid));
    const viewer = open(fid, s.api.ticket(vic, fid));
    await Promise.all([editor.synced, viewer.synced]);
    await until(() => viewer.session() !== undefined, "the viewer's session");
    assert.equal(viewer.session()!.readOnly, "viewer");
    assert.equal(viewer.provider.authorizedScope, "readonly");

    viewer.type(" from the viewer");
    editor.type(" from the editor");
    await until(() => viewer.text.toJSON().includes("from the editor"), "the editor's text at the viewer");
    await sleep(200);
    assert.equal(editor.text.toJSON(), "base from the editor", "the viewer's edit reached the editor");
    await until(() => s.logs.some((l) => l.msg === "update from a read-only connection dropped"), "the log line");
    const line = s.logs.find((l) => l.msg === "update from a read-only connection dropped")!;
    assert.equal(line.fields.userId, vic);
    assert.equal(line.fields.reason, "viewer");

    editor.destroy();
    viewer.destroy();
    await until(() => s.api.stores.length > 0 && s.sync.hocuspocus.getDocumentsCount() === 0, "the store on last disconnect");
    assert.equal(s.api.files.get(fid)!.content, "base from the editor");
  });

  it("limits the connections of one user", async () => {
    await s.stop();
    s = await startStack({ maxConnectionsPerUser: 2 });
    const ada = user();
    const pid = s.api.project({ [ada]: "editor" });
    const files = [s.api.file(pid, "a"), s.api.file(pid, "b"), s.api.file(pid, "c")];
    const a = open(files[0]!, s.api.ticket(ada, files[0]!));
    const b = open(files[1]!, s.api.ticket(ada, files[1]!));
    await Promise.all([a.synced, b.synced]);
    const c = open(files[2]!, s.api.ticket(ada, files[2]!));
    await until(() => c.failed !== undefined, "the refusal");
    assert.equal(c.failed, "too_many_connections");
  });
});
