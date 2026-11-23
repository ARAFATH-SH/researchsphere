import type * as Y from "yjs";

import type { Result } from "@/lib/api/call";

import type { CollabTicket, ProjectFileDetail, SaveResult } from "../../files/api";
import { textFile } from "../../files/test-files";
import { FakeHub } from "./fake-hub";
import { CollabSession, type SessionDeps } from "./session";
import { canType, capacityText, collabStatus, type YjsBinding } from "./types";

// The client side of FR-12.4 to FR-12.9 and NFR-2 (docs/sync.md) against an in-memory sync
// server: merging, roles, capacity, offline edits and epochs, the REST fallback, tickets.

const fileId = "f-main";
const projectId = "p-1";
const ada = { id: "u-ada", name: "Ada Lovelace" };
const bob = { id: "u-bob", name: "Bob Kahn" };
const vic = { id: "u-vic", name: "Vic Viewer" };

let hub: FakeHub;
let tickets: number;
let fileText: string;
let fileVersion: number;
let getFileResult: (() => Result<ProjectFileDetail>) | null;
const sessions: CollabSession[] = [];

const save = jest.fn<Promise<SaveResult>, [string, number, { keepalive: boolean }]>();

function deps(member: { id: string; name: string }, extra: Partial<SessionDeps> = {}): SessionDeps {
  return {
    projectId,
    fileId,
    link: hub.link(member),
    ticket: async (): Promise<Result<CollabTicket>> => ({
      ok: true,
      data: { ticket: `ticket-${++tickets}`, expiresAt: "", role: hub.roles.get(member.id) ?? "viewer", displayName: member.name, epoch: null },
    }),
    getFile: async () =>
      getFileResult?.() ?? { ok: true, data: { file: textFile(fileId, "main.tex", { version: fileVersion }), content: fileText, download: null } },
    save,
    refresh: jest.fn(async () => "ok" as const),
    sessionLost: jest.fn(),
    ...extra,
  };
}

const timing = { fallbackAfterMs: 50, retryMs: [10, 20], probeMs: [20], probeTimeoutMs: 200 };

function open(member: { id: string; name: string }, extra: Partial<SessionDeps> = {}) {
  const s = new CollabSession(deps(member, extra), timing);
  sessions.push(s);
  s.start();
  return s;
}

const tick = (ms = 0) => new Promise((r) => setTimeout(r, ms));

async function until(cond: () => boolean, what: string, ms = 2000) {
  const end = Date.now() + ms;
  while (!cond()) {
    if (Date.now() > end) throw new Error(`timed out waiting for ${what}`);
    await tick(5);
  }
}

function yjs(s: CollabSession): YjsBinding {
  const b = s.getState().binding;
  if (b?.kind !== "yjs") throw new Error(`not live: ${s.getState().mode}`);
  return b;
}

const text = (s: CollabSession) => yjs(s).text.toString();
const live = (s: CollabSession) => until(() => s.getState().mode === "live" && s.getState().binding?.kind === "yjs", "live");

// A member's own edit: the editor's origin, tracked by the undo manager.
function type(s: CollabSession, at: number, value: string, origin: object = editorOrigin(s)) {
  const b = yjs(s);
  b.doc.transact(() => b.text.insert(at, value), origin);
}
const origins = new WeakMap<Y.UndoManager, object>();
function editorOrigin(s: CollabSession): object {
  const um = yjs(s).undoManager;
  let o = origins.get(um);
  if (!o) {
    o = {};
    um.addTrackedOrigin(o);
    origins.set(um, o);
  }
  return o;
}

beforeEach(() => {
  hub = new FakeHub();
  hub.seed(fileId, "Hello");
  hub.roles.set(ada.id, "owner").set(bob.id, "editor").set(vic.id, "viewer");
  tickets = 0;
  fileText = "Hello";
  fileVersion = 3;
  getFileResult = null;
  save.mockReset();
  save.mockImplementation(async (content, version) => ({ ok: true, data: textFile(fileId, "main.tex", { version: version + 1 }) }));
  jest.spyOn(console, "log").mockImplementation(() => {});
  jest.spyOn(console, "warn").mockImplementation(() => {});
});

afterEach(() => {
  for (const s of sessions.splice(0)) s.dispose();
  jest.restoreAllMocks();
});

describe("CollabSession", () => {
  it("merges two members' edits and shows Live, then Saved once the server stored them (FR-12.4, NFR-2)", async () => {
    const a = open(ada);
    const b = open(bob);
    await live(a);
    await live(b);
    expect(collabStatus(a.getState(), null).text).toBe("Live");
    type(a, 0, "A: ");
    type(b, text(b).length, " from Bob");
    expect(text(a)).toBe("A: Hello from Bob");
    expect(text(b)).toBe("A: Hello from Bob");
    expect(hub.text(fileId)).toBe("A: Hello from Bob");

    hub.stored(fileId, 4);
    expect(collabStatus(a.getState(), null)).toMatchObject({ text: "Saved", saved: true });
    type(a, 0, "!");
    expect(collabStatus(a.getState(), null).text).toBe("Live");
  });

  it("follows the session message: role, colour, limit and epoch", async () => {
    const a = open(ada);
    await live(a);
    expect(a.getState()).toMatchObject({ role: "owner", readOnly: null, maxEditors: 4, colour: "#0B7A75" });
    expect(canType(a.getState(), "owner")).toBe(true);
  });

  it("keeps a viewer read-only and drops what it sends (FR-12.7)", async () => {
    const a = open(ada);
    const v = open(vic);
    await live(a);
    await live(v);
    expect(v.getState()).toMatchObject({ role: "viewer", readOnly: "viewer" });
    expect(canType(v.getState(), "viewer")).toBe(false);
    type(v, 0, "not allowed ");
    expect(hub.text(fileId)).toBe("Hello");
    type(a, 5, "!");
    expect(text(v)).toContain("Hello!");
  });

  it("opens the file read-only for a member over the limit, says why, and lets them edit when a slot frees (FR-12.8)", async () => {
    hub.maxEditors = 1;
    const a = open(ada);
    await live(a);
    const b = open(bob);
    await live(b);
    expect(b.getState()).toMatchObject({ readOnly: "capacity", maxEditors: 1 });
    expect(canType(b.getState(), "editor")).toBe(false);
    expect(capacityText(b.getState().maxEditors)).toBe(
      "This file already has 1 people editing. You can read it and will be able to edit when someone leaves.",
    );
    expect(a.getState().presence?.[fileId]?.map((u) => [u.id, u.readOnly])).toEqual([
      [ada.id, null],
      [bob.id, "capacity"],
    ]);

    a.release();
    await until(() => b.getState().readOnly === null, "the promotion");
    expect(canType(b.getState(), "editor")).toBe(true);
    type(b, 0, ">");
    expect(hub.text(fileId)).toBe(">Hello");
  });

  it("turns read-only at once when the member is made viewer, and closes when removed (FR-12.7)", async () => {
    const b = open(bob);
    await live(b);
    hub.setRole(bob.id, "viewer");
    expect(b.getState()).toMatchObject({ role: "viewer", readOnly: "viewer" });
    expect(canType(b.getState(), "editor")).toBe(false);

    hub.setRole(bob.id, null);
    expect(b.getState()).toMatchObject({ mode: "ended", ended: "access_ended" });
    expect(canType(b.getState(), "editor")).toBe(false);
  });

  it("keeps offline edits in the document, says so, and merges them on reconnect (FR-12.9)", async () => {
    const a = open(ada);
    const b = open(bob);
    await live(a);
    await live(b);
    hub.offline();
    expect(a.getState().mode).toBe("offline");
    expect(collabStatus(a.getState(), null).text).toBe("Offline, changes will sync");
    type(a, 0, "[ada]");
    type(b, text(b).length, "[bob]");
    expect(a.hasUnsent()).toBe(true);

    // The network is back before REST is tried; each connects with its epoch.
    const before = hub.tokens.length;
    hub.reconnectAll();
    await live(a);
    await live(b);
    expect(hub.tokens.slice(before).every((t) => t.endsWith(".epoch-1"))).toBe(true);
    expect(text(a)).toBe("[ada]Hello[bob]");
    expect(text(b)).toBe("[ada]Hello[bob]");
    expect(hub.text(fileId)).toBe("[ada]Hello[bob]");
    expect(a.hasUnsent()).toBe(false);
  });

  describe("whenSent, before a compile (FR-13.1)", () => {
    it("resolves at once when nothing waits to be sent", async () => {
      const a = open(ada);
      await live(a);
      await a.whenSent(10_000);
    });

    it("waits until the sync server has applied what this tab typed", async () => {
      const a = open(ada);
      await live(a);
      hub.offline();
      type(a, 0, "[ada]");
      let done = false;
      void a.whenSent(10_000).then(() => (done = true));
      await tick(20);
      expect(done).toBe(false);
      hub.reconnectAll();
      await until(() => done, "whenSent");
      expect(hub.text(fileId)).toBe("[ada]Hello");
    });

    it("gives up after the timeout, as the compile then reads what was stored", async () => {
      const a = open(ada);
      await live(a);
      hub.offline();
      type(a, 0, "[ada]");
      await a.whenSent(30);
      expect(a.hasUnsent()).toBe(true);
    });

    it("saves first over REST", async () => {
      hub.refuseAll = "unavailable";
      const a = open(ada);
      await until(() => a.getState().mode === "fallback", "the fallback");
      const binding = a.getState().binding;
      if (binding?.kind !== "rest") throw new Error("not REST");
      binding.saver.edit(() => "Hello REST");
      await a.whenSent();
      expect(save).toHaveBeenCalledWith("Hello REST", 3, { keepalive: false });
    });
  });

  it("asks before replacing text it could not merge into a new history (epoch_mismatch)", async () => {
    const a = open(ada);
    await live(a);
    hub.offline();
    type(a, 5, " offline");
    // Meanwhile a REST save started a new history.
    hub.rebuild(fileId, "Rewritten");
    hub.reconnectAll();
    await until(() => a.getState().conflict !== null, "the conflict");
    expect(a.getState().conflict).toEqual({ mine: "Hello offline", theirs: "Rewritten" });
    expect(text(a)).toBe("Rewritten");
    expect(a.hasUnsent()).toBe(true);
    // The member chooses; "mine" goes back to the editor as an edit.
    expect(a.resolveConflict("mine")).toBe("Hello offline");
    expect(a.getState().conflict).toBeNull();
  });

  it("opens a new history quietly when it holds nothing unsent", async () => {
    const a = open(ada);
    await live(a);
    hub.offline();
    hub.rebuild(fileId, "Rewritten");
    hub.reconnectAll();
    await until(() => text(a) === "Rewritten", "the new text");
    expect(a.getState().conflict).toBeNull();
  });

  it("saves over REST when the sync server cannot be reached, and goes back when it returns", async () => {
    hub.refuseAll = "unavailable";
    const a = open(ada);
    await until(() => a.getState().mode === "fallback", "the fallback");
    const binding = a.getState().binding;
    if (binding?.kind !== "rest") throw new Error("not REST");
    expect(binding.saver.text).toBe("Hello");
    expect(collabStatus(a.getState(), binding.saver.getState()).text).toBe("Live editing unavailable, saving normally");
    binding.saver.edit(() => "Hello REST");
    await binding.saver.flush();
    expect(save).toHaveBeenCalledWith("Hello REST", 3, { keepalive: false });

    // The REST save started a new history; the sync server is back.
    hub.rebuild(fileId, "Hello REST");
    hub.refuseAll = null;
    await live(a);
    expect(text(a)).toBe("Hello REST");
    // It came back without an epoch: the REST save started a new history.
    expect(hub.tokens.at(-1)).not.toContain(".");
  });

  it("does not leave REST while a save waits", async () => {
    hub.refuseAll = "unavailable";
    save.mockResolvedValue({ ok: false, status: 409, code: "live_session_open", message: "live" });
    const a = open(ada);
    await until(() => a.getState().mode === "fallback", "the fallback");
    const binding = a.getState().binding;
    if (binding?.kind !== "rest") throw new Error("not REST");
    binding.saver.edit(() => "kept");
    hub.refuseAll = null;
    await tick(100);
    expect(a.getState().mode).toBe("fallback");
    expect(binding.saver.getState().status).toBe("retrying");
    expect(a.hasUnsent()).toBe(true);
  });

  it("shows unsent offline edits as a conflict when it falls back to REST with other text", async () => {
    const a = open(ada);
    await live(a);
    hub.offline();
    type(a, 5, " mine");
    fileText = "Theirs";
    hub.online = true;
    hub.refuseAll = "unavailable";
    hub.reconnectAll();
    await until(() => a.getState().mode === "fallback", "the fallback");
    expect(a.getState().conflict).toEqual({ mine: "Hello mine", theirs: "Theirs" });
  });

  it("stays offline (not REST) while the API cannot be reached either", async () => {
    getFileResult = () => ({ ok: false, code: "network_error", message: "offline" });
    const a = open(ada);
    await live(a);
    hub.offline();
    await tick(150);
    expect(a.getState().mode).toBe("offline");
  });

  it("asks for a new ticket on every connect, sends each once and never logs one", async () => {
    const a = open(ada);
    await live(a);
    hub.offline();
    hub.reconnectAll();
    await live(a);
    a.retryNow();
    expect(hub.tokens).toEqual(["ticket-1", "ticket-2.epoch-1"]);
    for (const spy of [console.log, console.warn]) {
      for (const call of jest.mocked(spy).mock.calls) expect(JSON.stringify(call)).not.toMatch(/ticket-\d/);
    }
  });

  it("gets a new ticket at once when one is refused, then backs off", async () => {
    hub.refuseNext.push("ticket_invalid");
    const a = open(ada);
    await live(a);
    expect(hub.tokens).toEqual(["ticket-1", "ticket-2"]);
  });

  it("closes the file for a member who has no access when asking for a ticket", async () => {
    getFileResult = () => ({ ok: false, code: "not_found", message: "Not found." });
    const a = open(ada, { ticket: async () => ({ ok: false, code: "not_found", message: "Not found." }) });
    await until(() => a.getState().ended === "access_ended", "the end");
  });

  describe("falls back to REST, with the reason, whenever live editing is refused", () => {
    async function inFallback(a: CollabSession) {
      await until(() => a.getState().mode === "fallback", "the fallback");
      const b = a.getState().binding;
      if (b?.kind !== "rest") throw new Error("not REST");
      expect(b.saver.text).toBe("Hello");
      expect(a.getState().ended).toBeNull();
      expect(canType(a.getState(), "owner")).toBe(true);
      return b.saver;
    }

    it.each(["origin_not_allowed", "too_many_connections", "unavailable", "shutting_down"])("refused with %s", async (reason) => {
      hub.refuseAll = reason;
      const a = open(ada);
      await inFallback(a);
      expect(a.getState().fallbackReason).toBe(reason);
    });

    it("when the sync server does not answer at all, at once", async () => {
      hub.unreachable = true;
      const a = new CollabSession(deps(ada), { ...timing, fallbackAfterMs: 60_000 });
      sessions.push(a);
      a.start();
      await inFallback(a);
      expect(a.getState().fallbackReason).toBe("sync_unreachable");
    });

    it.each(["rate_limited", "internal_error", "account_suspended"])("when the ticket is refused (%s)", async (code) => {
      const a = open(ada, { ticket: async () => ({ ok: false, code, message: "No." }) });
      const saver = await inFallback(a);
      expect(a.getState().fallbackReason).toBe(`ticket_${code}`);
      saver.edit(() => "Hello REST");
      await saver.flush();
      expect(save).toHaveBeenCalledWith("Hello REST", 3, { keepalive: false });
    });

    it("says it waits when neither the sync server nor the API answers", async () => {
      hub.unreachable = true;
      getFileResult = () => ({ ok: false, code: "network_error", message: "offline" });
      const a = open(ada);
      await until(() => a.getState().waiting, "waiting");
      expect(a.getState()).toMatchObject({ mode: "connecting", binding: null, ended: null });
      getFileResult = null;
      await inFallback(a);
      expect(a.getState().waiting).toBe(false);
    });
  });

  it("refreshes the session after session_ended, and stops when the refresh is refused", async () => {
    const refresh = jest.fn(async () => "ok" as const);
    const a = open(ada, { refresh });
    await live(a);
    hub.close(ada.id, "session_ended");
    await live(a);
    expect(refresh).toHaveBeenCalledTimes(1);

    const lost = jest.fn();
    const b = open(bob, { refresh: async () => "refused", sessionLost: lost });
    await live(b);
    hub.close(bob.id, "session_ended");
    await until(() => b.getState().ended === "session_lost", "the end");
    expect(lost).toHaveBeenCalled();
  });

  it("closes a deleted file and says so to the workspace", async () => {
    const a = open(ada);
    const gone = jest.fn();
    a.onGone = gone;
    await live(a);
    hub.close(ada.id, "file_gone");
    expect(a.getState().ended).toBe("file_gone");
    expect(gone).toHaveBeenCalled();
  });

  it("undoes an edit that made the file too large, and opens it again", async () => {
    const a = open(ada);
    await live(a);
    type(a, 5, " huge");
    hub.close(ada.id, "document_too_large");
    expect(a.getState().tooLarge).toBe(true);
    await live(a);
    expect(text(a)).toBe(hub.text(fileId));
  });

  it("undoes only this member's own edits", async () => {
    const a = open(ada);
    const b = open(bob);
    await live(a);
    await live(b);
    type(a, 0, "A");
    type(b, text(b).length, "B");
    yjs(a).undoManager.undo();
    expect(text(a)).toBe("HelloB");
    expect(text(b)).toBe("HelloB");
  });

  it("closes after release once nothing waits, freeing the slot", async () => {
    const a = open(ada);
    const disposed = jest.fn();
    a.onDisposed = disposed;
    await live(a);
    expect(hub.editors(fileId).size).toBe(1);
    a.release();
    expect(disposed).toHaveBeenCalled();
    expect(hub.editors(fileId).size).toBe(0);
  });

  it("keeps a released file open while edits wait, then closes it", async () => {
    const a = open(ada);
    const disposed = jest.fn();
    a.onDisposed = disposed;
    await live(a);
    hub.offline();
    type(a, 0, "x");
    a.release();
    expect(disposed).not.toHaveBeenCalled();
    hub.reconnectAll();
    await until(() => disposed.mock.calls.length > 0, "the close");
    expect(hub.text(fileId)).toBe("xHello");
  });
});
