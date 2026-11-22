import { applyAwarenessUpdate, type Awareness, encodeAwarenessUpdate, removeAwarenessStates } from "y-protocols/awareness";
import * as Y from "yjs";

import type { ProjectRole } from "../../api";
import type { Link, LinkFactory, LinkOptions } from "./link";
import { textName } from "./types";

// FakeHub is the sync server in memory, for tests (docs/sync.md): it redeems tokens through
// the client's token function, keeps one Y.Doc and epoch per file, relays updates and awareness
// between links at once, admits at most maxEditors editors per file (FR-12.8), keeps viewers
// read-only (FR-12.7), sends the session, presence and stored messages, and lets a test take
// the network away, close or refuse connections and change roles.

type Member = { id: string; name: string };

const palette = ["#0B7A75", "#1F4E8C", "#B5520B", "#7A3E9D", "#C62828", "#2E7D32", "#8D6E00", "#AD1457"];

export class FakeHub {
  readonly files = new Map<string, { doc: Y.Doc; epoch: string }>();
  readonly roles = new Map<string, ProjectRole | null>();
  readonly links = new Set<FakeLink>();
  /** Every token a link sent, in order. */
  readonly tokens: string[] = [];
  maxEditors = 4;
  online = true;
  /** The sync server does not answer at all: every socket attempt fails. */
  unreachable = false;
  /** Refuse every connection with this reason while set (as "unavailable" when the API is down). */
  refuseAll: string | null = null;
  /** Refuse the next connections with these reasons, one each. */
  readonly refuseNext: string[] = [];
  private epochs = 0;

  /** A file with text, in a new epoch. */
  seed(fileId: string, text: string): void {
    const doc = new Y.Doc();
    doc.getText(textName).insert(0, text);
    this.files.set(fileId, { doc, epoch: `epoch-${++this.epochs}` });
    doc.on("update", (update: Uint8Array, origin: unknown) => {
      for (const l of this.links) if (l.fileId === fileId && l.connected && l !== origin) l.receive(update);
    });
  }

  /** The text on the server. */
  text(fileId: string): string {
    return this.files.get(fileId)!.doc.getText(textName).toString();
  }

  /** A REST save rebuilt the file: a new history (docs/sync.md, Epochs). */
  rebuild(fileId: string, text: string): void {
    for (const l of this.links) if (l.fileId === fileId) l.drop();
    this.seed(fileId, text);
  }

  /** A link factory for one member, as their browser would have. */
  link(member: Member): LinkFactory {
    return (options) => {
      const l = new FakeLink(this, member, options);
      this.links.add(l);
      void l.open();
      return l;
    };
  }

  offline(): void {
    this.online = false;
    for (const l of this.links) if (l.connected) l.lose();
  }

  reconnectAll(): void {
    this.online = true;
    for (const l of [...this.links]) if (!l.connected && l.retrying) void l.open();
  }

  /** Ends a member's connections to a file (or every file) with a close reason. */
  close(memberId: string, reason: string): void {
    for (const l of this.links) if (l.member.id === memberId && l.connected) l.closeWith(reason);
  }

  /** A role read again (members_changed): null removes the member. */
  setRole(memberId: string, role: ProjectRole | null): void {
    this.roles.set(memberId, role);
    for (const l of [...this.links]) {
      if (l.member.id !== memberId || !l.connected) continue;
      if (role === null) l.closeWith("access_ended");
      else l.admit();
    }
    this.rebalanceAll();
  }

  /** The server stored the file: tell its members. */
  stored(fileId: string, version: number): void {
    for (const l of this.links) if (l.fileId === fileId && l.connected) l.options.events.stateless(JSON.stringify({ type: "stored", version }));
  }

  editors(fileId: string, except?: FakeLink): Set<string> {
    const ids = new Set<string>();
    for (const l of this.links) if (l !== except && l.fileId === fileId && l.connected && l.readOnly === null) ids.add(l.member.id);
    return ids;
  }

  colourOf(fileId: string, memberId: string): string {
    const ids: string[] = [];
    for (const l of this.links) if (l.fileId === fileId && !ids.includes(l.member.id)) ids.push(l.member.id);
    return palette[Math.max(0, ids.indexOf(memberId)) % palette.length];
  }

  /** Promotes members waiting for a slot, earliest first. */
  rebalanceAll(): void {
    for (const l of this.links) {
      if (l.connected && l.readOnly === "capacity" && this.editors(l.fileId).size < this.maxEditors) l.admit();
    }
    this.presence();
  }

  presence(): void {
    const files: Record<string, { id: string; name: string; colour: string; readOnly: string | null }[]> = {};
    for (const l of this.links) {
      if (!l.connected || l.readOnly === "viewer") continue;
      const list = (files[l.fileId] ??= []);
      if (!list.some((u) => u.id === l.member.id)) {
        list.push({ id: l.member.id, name: l.member.name, colour: this.colourOf(l.fileId, l.member.id), readOnly: l.readOnly });
      }
    }
    const payload = JSON.stringify({ type: "presence", files });
    for (const l of this.links) if (l.connected) l.options.events.stateless(payload);
  }
}

export class FakeLink implements Link {
  connected = false;
  /** The link reconnects by itself when the network returns (the provider's retry). */
  retrying = true;
  readOnly: "viewer" | "capacity" | null = null;
  private pending = 0;
  private readonly onUpdate: (update: Uint8Array, origin: unknown) => void;
  private readonly onAwareness: (change: { added: number[]; updated: number[]; removed: number[] }, origin: unknown) => void;

  constructor(
    readonly hub: FakeHub,
    readonly member: Member,
    readonly options: LinkOptions,
  ) {
    this.onUpdate = (update, origin) => {
      if (origin === this) return;
      if (!this.connected) {
        this.pending++;
        this.options.events.unsynced(this.pending);
        return;
      }
      // What a read-only connection sends is dropped (FR-12.7).
      if (this.readOnly === null) Y.applyUpdate(this.server, update, this);
    };
    this.onAwareness = ({ added, updated, removed }, origin) => {
      if (origin === this || !this.connected) return;
      const update = encodeAwarenessUpdate(this.awareness, [...added, ...updated, ...removed]);
      for (const l of this.peers()) applyAwarenessUpdate(l.awareness, update, l);
    };
    options.doc.on("update", this.onUpdate);
    options.awareness.on("update", this.onAwareness);
  }

  get fileId(): string {
    return this.options.fileId;
  }

  get awareness(): Awareness {
    return this.options.awareness;
  }

  private get server(): Y.Doc {
    return this.hub.files.get(this.fileId)!.doc;
  }

  private peers(): FakeLink[] {
    return [...this.hub.links].filter((l) => l !== this && l.fileId === this.fileId && l.connected);
  }

  async open(): Promise<void> {
    if (this.hub.unreachable) return this.options.events.disconnected();
    if (!this.hub.online) return;
    let token: string;
    try {
      token = await this.options.token();
    } catch (err) {
      this.options.events.refused(`Failed to get token during sendToken(): ${String(err)}`);
      return;
    }
    if (!this.hub.links.has(this)) return;
    this.hub.tokens.push(token);
    if (this.hub.refuseAll) return this.options.events.refused(this.hub.refuseAll);
    const once = this.hub.refuseNext.shift();
    if (once) return this.options.events.refused(once);
    const file = this.hub.files.get(this.fileId);
    if (!file) return this.options.events.refused("ticket_invalid");
    const role = this.hub.roles.get(this.member.id);
    if (!role) return this.options.events.refused("ticket_invalid");
    const dot = token.indexOf(".");
    if (dot !== -1 && token.slice(dot + 1) !== file.epoch) return this.options.events.refused("epoch_mismatch");

    this.connected = true;
    this.readOnly = role === "viewer" ? "viewer" : this.hub.editors(this.fileId, this).size < this.hub.maxEditors ? null : "capacity";
    // Sync: the server's state to the client, the client's to the server if it may edit.
    Y.applyUpdate(this.options.doc, Y.encodeStateAsUpdate(file.doc, Y.encodeStateVector(this.options.doc)), this);
    if (this.readOnly === null) {
      Y.applyUpdate(file.doc, Y.encodeStateAsUpdate(this.options.doc, Y.encodeStateVector(file.doc)), this);
    }
    this.pending = 0;
    // The server sets the name and colour (docs/sync.md, Awareness).
    this.awareness.setLocalStateField("user", { id: this.member.id, name: this.member.name, colour: this.hub.colourOf(this.fileId, this.member.id) });
    for (const l of this.peers()) {
      applyAwarenessUpdate(this.awareness, encodeAwarenessUpdate(l.awareness, [l.options.doc.clientID]), this);
    }
    this.options.events.synced();
    this.sendSession();
    this.options.events.unsynced(0);
    this.hub.presence();
  }

  /** Reads the role again and tells the client (a session message). */
  admit(): void {
    const role = this.hub.roles.get(this.member.id);
    const before = this.readOnly;
    if (role === "viewer") this.readOnly = "viewer";
    else if (this.readOnly !== null && this.hub.editors(this.fileId, this).size < this.hub.maxEditors) this.readOnly = null;
    else if (this.readOnly === "viewer") this.readOnly = "capacity";
    if (before !== null && this.readOnly === null) {
      // May edit again: take what the client holds.
      Y.applyUpdate(this.server, Y.encodeStateAsUpdate(this.options.doc, Y.encodeStateVector(this.server)), this);
    }
    this.sendSession();
  }

  sendSession(): void {
    const file = this.hub.files.get(this.fileId)!;
    this.options.events.stateless(
      JSON.stringify({
        type: "session",
        role: this.hub.roles.get(this.member.id),
        readOnly: this.readOnly,
        maxEditors: this.hub.maxEditors,
        colour: this.hub.colourOf(this.fileId, this.member.id),
        epoch: file.epoch,
      }),
    );
  }

  receive(update: Uint8Array): void {
    Y.applyUpdate(this.options.doc, update, this);
  }

  /** The socket is lost; the provider reconnects when the network returns. */
  lose(): void {
    this.disconnect();
    this.options.events.disconnected();
  }

  /** The server ends this document's connection. */
  closeWith(reason: string): void {
    this.disconnect();
    this.retrying = false;
    this.options.events.closed(reason);
    this.hub.rebalanceAll();
  }

  /** The server drops the document (a rebuild); the client hears nothing until it reconnects. */
  drop(): void {
    this.disconnect();
  }

  private disconnect(): void {
    if (!this.connected) return;
    this.connected = false;
    for (const l of this.peers()) removeAwarenessStates(l.awareness, [this.options.doc.clientID], l);
  }

  unsynced(): number {
    return this.connected ? 0 : this.pending;
  }

  reconnect(): void {
    this.disconnect();
    this.retrying = true;
    void this.open();
  }

  destroy(): void {
    this.disconnect();
    this.hub.links.delete(this);
    this.options.doc.off("update", this.onUpdate);
    this.options.awareness.off("update", this.onAwareness);
    this.options.awareness.destroy();
    this.hub.rebalanceAll();
  }
}
