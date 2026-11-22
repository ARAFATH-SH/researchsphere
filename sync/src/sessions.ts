import type { Role } from "./api.ts";

// Who has each document open, with which rights (FR-12.7, FR-12.8). A user counts once per
// document however many tabs they have; all their tabs share one state. At most maxEditors
// users edit a document at a time; an editor who comes later opens it read-only with the reason
// "capacity" and is promoted, earliest first, when a slot frees.

/** Why a connection is read-only, or null when it may edit. */
export type ReadOnlyReason = "viewer" | "capacity" | null;

/** The colours members' cursors take (FR-12.5), one per user in a document, distinct while there are enough. */
export const palette = ["#0B7A75", "#1F4E8C", "#B5520B", "#7A3E9D", "#C62828", "#2E7D32", "#8D6E00", "#AD1457"] as const;

export interface Entry<C> {
  userId: string;
  role: Role;
  reason: ReadOnlyReason;
  colour: string;
  /** When the user joined; the order of promotion. */
  since: number;
  conns: Set<C>;
  /** Admitted sockets whose connection is not set up yet, with when the reservation lapses. */
  pending: Map<string, number>;
}

/** How long an admitted socket keeps its place before its connection exists. */
const pendingMs = 30_000;

export class Sessions<C> {
  private readonly docs = new Map<string, Map<string, Entry<C>>>();
  private readonly maxEditors: number;
  private readonly now: () => number;

  constructor(maxEditors: number, now: () => number = Date.now) {
    this.maxEditors = maxEditors;
    this.now = now;
  }

  private users(doc: string): Map<string, Entry<C>> {
    let users = this.docs.get(doc);
    if (!users) {
      users = new Map();
      this.docs.set(doc, users);
    }
    this.sweep(users);
    return users;
  }

  /** Drops lapsed reservations and users left with nothing. */
  private sweep(users: Map<string, Entry<C>>): void {
    const now = this.now();
    for (const [id, e] of users) {
      for (const [socket, until] of e.pending) if (until <= now) e.pending.delete(socket);
      if (e.conns.size === 0 && e.pending.size === 0) users.delete(id);
    }
  }

  private editing(users: Map<string, Entry<C>>): number {
    let n = 0;
    for (const e of users.values()) if (e.reason === null) n++;
    return n;
  }

  private colourFor(users: Map<string, Entry<C>>, userId: string): string {
    const used = new Set([...users.values()].map((e) => e.colour));
    const free = palette.find((c) => !used.has(c));
    if (free) return free;
    let h = 0;
    for (const ch of userId) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
    return palette[h % palette.length] ?? palette[0];
  }

  /**
   * Admits socketId of userId to doc with role, reserving the user's place at once (no await
   * between the decision and the reservation, so two editors cannot take the last slot). A
   * user already there keeps their state, with the role brought up to date.
   */
  admit(doc: string, userId: string, role: Role, socketId: string): Entry<C> {
    const users = this.users(doc);
    let e = users.get(userId);
    if (!e) {
      const reason: ReadOnlyReason = role === "viewer" ? "viewer" : this.editing(users) < this.maxEditors ? null : "capacity";
      e = { userId, role, reason, colour: this.colourFor(users, userId), since: this.now(), conns: new Set(), pending: new Map() };
      users.set(userId, e);
    } else if (e.role !== role) {
      this.applyRole(doc, e, role);
    }
    e.pending.set(socketId, this.now() + pendingMs);
    return e;
  }

  /** The admitted socket's connection now exists. */
  attach(doc: string, userId: string, socketId: string, conn: C): Entry<C> | undefined {
    const e = this.docs.get(doc)?.get(userId);
    if (!e) return undefined;
    e.pending.delete(socketId);
    e.conns.add(conn);
    return e;
  }

  /**
   * The connection (or the admitted socket that never got one) is gone. Returns the users
   * promoted into the slot it freed, if any.
   */
  detach(doc: string, userId: string, socketId: string, conn: C | undefined): Entry<C>[] {
    const users = this.docs.get(doc);
    const e = users?.get(userId);
    if (!users || !e) return [];
    e.pending.delete(socketId);
    if (conn !== undefined) e.conns.delete(conn);
    if (e.conns.size > 0 || e.pending.size > 0) return [];
    users.delete(userId);
    const promoted = this.rebalance(users);
    if (users.size === 0) this.docs.delete(doc);
    return promoted;
  }

  /**
   * Applies a role read again (FR-12.7): a viewer becomes read-only; an editor who was a
   * viewer may edit if a slot is free, else waits; an editor who left a slot frees it.
   * Returns every entry whose state changed, the user's own first.
   */
  setRole(doc: string, userId: string, role: Role): Entry<C>[] {
    const e = this.docs.get(doc)?.get(userId);
    if (!e) return [];
    const before = e.reason;
    const promoted = this.applyRole(doc, e, role);
    return e.reason !== before ? [e, ...promoted] : promoted;
  }

  private applyRole(doc: string, e: Entry<C>, role: Role): Entry<C>[] {
    const users = this.users(doc);
    e.role = role;
    if (role === "viewer") {
      const freed = e.reason === null;
      e.reason = "viewer";
      return freed ? this.rebalance(users) : [];
    }
    if (e.reason === "viewer") {
      e.reason = "capacity";
      e.since = this.now(); // waits behind those already waiting
      return this.rebalance(users).filter((x) => x !== e);
    }
    return [];
  }

  /** Removes a user from a document (access ended). Returns the users promoted. */
  remove(doc: string, userId: string): Entry<C>[] {
    const users = this.docs.get(doc);
    if (!users?.delete(userId)) return [];
    const promoted = this.rebalance(users);
    if (users.size === 0) this.docs.delete(doc);
    return promoted;
  }

  /** Promotes waiting users, earliest first, while slots are free. */
  private rebalance(users: Map<string, Entry<C>>): Entry<C>[] {
    const promoted: Entry<C>[] = [];
    const waiting = [...users.values()].filter((e) => e.reason === "capacity").sort((a, b) => a.since - b.since);
    let editing = this.editing(users);
    for (const e of waiting) {
      if (editing >= this.maxEditors) break;
      e.reason = null;
      editing++;
      promoted.push(e);
    }
    return promoted;
  }

  entry(doc: string, userId: string): Entry<C> | undefined {
    return this.docs.get(doc)?.get(userId);
  }

  /** Every user with doc open. */
  entries(doc: string): Entry<C>[] {
    return [...(this.docs.get(doc)?.values() ?? [])];
  }

  /** The documents open now. */
  documents(): string[] {
    return [...this.docs.keys()];
  }

  /** How many users edit doc now. */
  editors(doc: string): number {
    const users = this.docs.get(doc);
    return users ? this.editing(users) : 0;
  }

  /** The user's connections and reservations across all documents. */
  connectionsOf(userId: string): number {
    let n = 0;
    for (const users of this.docs.values()) {
      const e = users.get(userId);
      if (e) n += e.conns.size + e.pending.size;
    }
    return n;
  }
}
