import { Awareness } from "y-protocols/awareness";
import * as Y from "yjs";

import type { ApiError, Result } from "@/lib/api/call";
import type { RefreshOutcome } from "@/lib/api/auth-fetch";

import type { ProjectRole } from "../../api";
import type { CollabTicket, ProjectFileDetail } from "../../files/api";
import { Autosaver, type SaveFn } from "../autosave";
import type { Link, LinkFactory } from "./link";
import {
  type CollabState,
  type EndReason,
  initialState,
  type PresenceFiles,
  type PresenceUser,
  type ReadOnlyReason,
  safeColour,
  textName,
  type YjsBinding,
} from "./types";

// CollabSession is one text file opened in the workspace (FR-12.4 to FR-12.9, NFR-2;
// docs/sync.md). The text lives in a Yjs document on the sync server; this class connects it,
// follows the server's session, presence and stored messages, and handles every refusal and
// close of the contract. When the sync server cannot be reached but the API can, it saves
// over REST with the 21.2 Autosaver instead, and goes back to the sync server when it returns.
// Text that cannot be merged is never dropped: the member chooses (a Conflict).
//
// It is loaded with the editor (dynamic import), so Yjs stays off every other route.

export type SessionDeps = {
  projectId: string;
  fileId: string;
  link: LinkFactory;
  ticket: (projectId: string, fileId: string) => Promise<Result<CollabTicket>>;
  getFile: (projectId: string, fileId: string) => Promise<Result<ProjectFileDetail>>;
  save: SaveFn;
  refresh: () => Promise<RefreshOutcome>;
  sessionLost: () => void;
};

export type Timing = {
  /** How long the file may stay without a live connection before REST is tried. */
  fallbackAfterMs: number;
  /** Delays between attempts to connect again, the last one repeating. */
  retryMs: number[];
  /** Delays between attempts to go back from REST to the sync server, the last one repeating. */
  probeMs: number[];
  /** How long one attempt to go back may take. */
  probeTimeoutMs: number;
};

export const defaultTiming: Timing = {
  fallbackAfterMs: 5000,
  retryMs: [1000, 2000, 4000, 8000, 16000, 30000],
  probeMs: [5000, 10000, 20000, 30000],
  probeTimeoutMs: 10000,
};

/** One Yjs history of the file and its connection (a new one after an epoch change). */
type Generation = {
  doc: Y.Doc;
  text: Y.Text;
  awareness: Awareness;
  undoManager: Y.UndoManager;
  link: Link | null;
  /** The history's name, from the session message. */
  epoch: string | null;
  /** The document has synced with the server once: it holds that epoch's state. */
  hasHistory: boolean;
  /** Synced on the current connection. */
  synced: boolean;
  /** Local changes made, and the count the server has acknowledged. */
  localSeq: number;
  ackedSeq: number;
  /** A try to go back from REST: the state changes only once it syncs. */
  probe: boolean;
  /** This member's text from the previous history, to compare once this one syncs. */
  rebaseMine: string | null;
  /** What the editor binds to; one object per history, so a reconnect keeps the editor. */
  binding: YjsBinding;
};

const roles = new Set<ProjectRole>(["owner", "editor", "viewer"]);

export class CollabSession {
  private state: CollabState = initialState;
  private readonly listeners = new Set<() => void>();
  private gen: Generation | null = null;
  private saver: Autosaver | null = null;
  private unsubscribeSaver: (() => void) | null = null;
  private readonly timers: Partial<Record<"retry" | "fallback" | "probe" | "probeTimeout", ReturnType<typeof setTimeout>>> = {};
  private attempt = 0;
  private probeAttempt = 0;
  private ticketError: ApiError | null = null;
  private ticketRetried = false;
  /** Why live editing was last refused, for the developer (state.fallbackReason). */
  private fallbackReason: string | null = null;
  private released = false;
  private disposed = false;
  private readonly timing: Timing;

  /** Called once the session has nothing left to send after release(), and on dispose. */
  onDisposed: (() => void) | null = null;
  /** Called when the file or its project was deleted (the tree must be read again). */
  onGone: (() => void) | null = null;

  constructor(
    private readonly deps: SessionDeps,
    timing: Partial<Timing> = {},
  ) {
    this.timing = { ...defaultTiming, ...timing };
  }

  get fileId(): string {
    return this.deps.fileId;
  }

  getState = (): CollabState => this.state;

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  start(): void {
    this.gen = this.newGen(false);
    this.scheduleFallbackCheck();
  }

  /** Whether something typed here is not on a server yet (the tab asks before closing). */
  hasUnsent(): boolean {
    if (this.state.mode === "ended" || this.disposed) return false;
    if (this.state.conflict) return true;
    if (this.saver) return this.saver.dirty;
    const g = this.gen;
    return g !== null && g.localSeq !== g.ackedSeq;
  }

  /** Ctrl+S: sends what REST holds now; on the sync server every change is sent at once. */
  flush(options?: { keepalive?: boolean }): Promise<void> {
    return this.saver?.flush(options) ?? Promise.resolve();
  }

  /**
   * Before a compile (FR-13.1): resolves once what this tab typed has reached a server. Live:
   * when the sync server has applied every update (the API's flush then stores them,
   * docs/sync.md); over REST: after a save. It resolves anyway after timeoutMs, as the compile
   * then reads what was stored.
   */
  whenSent(timeoutMs = 5000): Promise<void> {
    if (this.saver) return this.saver.flush();
    return new Promise((resolve) => {
      let finished = false;
      const finish = () => {
        if (finished) return;
        finished = true;
        clearTimeout(timer);
        unsubscribe();
        resolve();
      };
      const check = () => {
        const g = this.gen;
        const applied = g !== null && g.synced && g.link !== null && g.link.unsynced() === 0;
        if (this.disposed || this.state.mode === "ended" || this.saver || !this.hasUnsent() || applied) finish();
      };
      const timer = setTimeout(finish, timeoutMs);
      const unsubscribe = this.subscribe(check);
      check();
    });
  }

  /** The browser is online again: retry now instead of waiting for the next attempt. */
  retryNow(): void {
    this.saver?.retryNow();
    if (this.state.mode === "offline" || this.state.mode === "connecting") {
      clearTimeout(this.timers.retry);
      this.timers.retry = undefined;
      this.ticketRetried = false;
      this.gen?.link?.reconnect();
    }
  }

  /** The member left the file: close it once nothing waits to be sent (frees the editor slot). */
  release(): void {
    this.released = true;
    if (this.saver) void this.saver.flush();
    this.maybeFinishRelease();
  }

  /** The member opened the file again before it closed. */
  reopen(): void {
    this.released = false;
  }

  /**
   * Answers a conflict. "theirs" keeps the server's text; "mine" returns this member's text,
   * which the editor then puts in as an ordinary edit (so it can be undone and is saved).
   */
  resolveConflict(choice: "theirs" | "mine"): string | null {
    const c = this.state.conflict;
    if (!c) return null;
    this.setState({ conflict: null });
    this.maybeFinishRelease();
    return choice === "mine" ? c.mine : null;
  }

  dismissTooLarge(): void {
    this.setState({ tooLarge: false });
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    for (const t of Object.values(this.timers)) clearTimeout(t);
    this.destroyGen(this.gen);
    this.gen = null;
    this.dropSaver();
    this.onDisposed?.();
    this.listeners.clear();
  }

  // --- state

  private setState(patch: Partial<CollabState>): void {
    if (this.disposed) return;
    this.state = { ...this.state, ...patch };
    this.listeners.forEach((listener) => listener());
  }

  private maybeFinishRelease(): void {
    if (this.released && !this.disposed && !this.hasUnsent()) this.dispose();
  }

  // --- generations

  private newGen(probe: boolean): Generation {
    const doc = new Y.Doc();
    const g: Generation = {
      doc,
      text: doc.getText(textName),
      awareness: new Awareness(doc),
      undoManager: null as unknown as Y.UndoManager,
      link: null,
      epoch: null,
      hasHistory: false,
      synced: false,
      localSeq: 0,
      ackedSeq: 0,
      probe,
      rebaseMine: null,
      binding: null as unknown as YjsBinding,
    };
    // Only this member's own edits are undone (the editor adds its origin as tracked).
    g.undoManager = new Y.UndoManager(g.text, { trackedOrigins: new Set() });
    g.binding = { kind: "yjs", doc, text: g.text, awareness: g.awareness, undoManager: g.undoManager };
    doc.on("update", (_update: Uint8Array, _origin: unknown, _doc: Y.Doc, tr: Y.Transaction) => {
      if (this.gen !== g) return;
      if (tr.local) g.localSeq++;
      if (this.state.stored) this.setState({ stored: false });
    });
    const guard =
      <A extends unknown[]>(fn: (...args: A) => void) =>
      (...args: A) => {
        if (this.gen === g && !this.disposed) fn(...args);
      };
    this.gen = g;
    g.link = this.deps.link({
      fileId: this.deps.fileId,
      doc,
      awareness: g.awareness,
      token: () => this.token(g),
      events: {
        synced: guard(() => this.onSynced(g)),
        stateless: guard((payload: string) => this.onStateless(g, payload)),
        refused: guard((reason: string) => this.onRefused(g, reason)),
        closed: guard((reason: string) => this.onClosed(g, reason)),
        disconnected: guard(() => this.onDisconnected(g)),
        unsynced: guard((n: number) => {
          if (!g.probe) this.setState({ unsynced: n });
          this.checkAck(g);
        }),
      },
    });
    return g;
  }

  private destroyGen(g: Generation | null): void {
    if (!g) return;
    g.link?.destroy();
    g.link = null;
    g.undoManager.destroy();
  }

  // A ticket works once, so every connect asks for a new one (docs/sync.md). It is never logged
  // or kept. The epoch goes with it once the document holds a history, so the server refuses
  // to merge it into another one (FR-12.9).
  private async token(g: Generation): Promise<string> {
    const res = await this.deps.ticket(this.deps.projectId, this.deps.fileId);
    if (!res.ok) {
      this.ticketError = res;
      throw new Error("no ticket");
    }
    this.ticketError = null;
    if (!g.probe && this.state.role !== res.data.role) this.setState({ role: res.data.role });
    return g.hasHistory && g.epoch ? `${res.data.ticket}.${g.epoch}` : res.data.ticket;
  }

  private checkAck(g: Generation): void {
    if (g.synced && g.link && g.link.unsynced() === 0 && g.ackedSeq !== g.localSeq) {
      g.ackedSeq = g.localSeq;
      this.maybeFinishRelease();
    }
  }

  // --- events

  private onSynced(g: Generation): void {
    g.synced = true;
    g.hasHistory = true;
    this.attempt = 0;
    this.ticketRetried = false;
    clearTimeout(this.timers.retry);
    clearTimeout(this.timers.fallback);
    this.timers.fallback = undefined;
    if (g.probe) {
      // Back from REST: the saver had saved everything before this try.
      clearTimeout(this.timers.probeTimeout);
      g.probe = false;
      this.probeAttempt = 0;
      this.dropSaver();
    }
    this.fallbackReason = null;
    const patch: Partial<CollabState> = {
      mode: "live",
      waiting: false,
      fallbackReason: null,
      switching: false,
      binding: g.binding,
      unsynced: g.link?.unsynced() ?? 0,
    };
    if (g.rebaseMine !== null) {
      const theirs = g.text.toString();
      if (g.rebaseMine !== theirs) patch.conflict = { mine: g.rebaseMine, theirs };
      g.rebaseMine = null;
    }
    this.setState(patch);
    this.checkAck(g);
  }

  private onStateless(g: Generation, payload: string): void {
    let msg: Record<string, unknown>;
    try {
      msg = JSON.parse(payload) as Record<string, unknown>;
    } catch {
      return;
    }
    switch (msg.type) {
      case "session": {
        if (typeof msg.epoch === "string") g.epoch = msg.epoch;
        const role = roles.has(msg.role as ProjectRole) ? (msg.role as ProjectRole) : this.state.role;
        const readOnly: ReadOnlyReason = msg.readOnly === "viewer" || msg.readOnly === "capacity" ? msg.readOnly : null;
        this.setState({
          role,
          readOnly,
          maxEditors: typeof msg.maxEditors === "number" ? msg.maxEditors : this.state.maxEditors,
          colour: safeColour(msg.colour) ?? this.state.colour,
        });
        break;
      }
      case "presence":
        if (!g.probe) this.setState({ presence: readPresence(msg.files) });
        break;
      case "stored":
        if (g.synced && g.link && g.link.unsynced() === 0 && !g.probe) this.setState({ stored: true });
        break;
    }
  }

  private onRefused(g: Generation, reason: string): void {
    if (g.probe) return this.abortProbe(g);
    const ticketError = this.ticketError;
    if (ticketError) {
      this.ticketError = null;
      return this.onTicketError(ticketError);
    }
    switch (reason) {
      case "ticket_invalid":
        // Once at once with a new ticket; then as any other failure.
        if (!this.ticketRetried) {
          this.ticketRetried = true;
          g.link?.reconnect();
          return;
        }
        return this.retryLater();
      case "epoch_mismatch":
        return this.rebase();
      default:
        // Any other refusal (unavailable, origin_not_allowed, too_many_connections, ...): the
        // file opens over REST at once and live editing is tried again in the background.
        return this.refusedLive(reason);
    }
  }

  /** Live editing was refused: REST now (if the API answers), the sync server again later. */
  private refusedLive(reason: string): void {
    this.fallbackReason = reason;
    this.retryLater();
    void this.checkFallback();
  }

  private onTicketError(err: ApiError): void {
    switch (err.code) {
      case "unauthorized":
        return this.end("session_lost");
      case "not_a_text_file":
        return this.end("file_gone");
      default:
        // No ticket (rate limited, the API failing, suspended, ...): REST decides. A member
        // without access gets 404 there too and the file closes; offline, the edits wait.
        return this.refusedLive(`ticket_${err.code}`);
    }
  }

  private onClosed(g: Generation, reason: string): void {
    if (g.probe) return this.abortProbe(g);
    g.synced = false;
    switch (reason) {
      case "access_ended":
        return this.end("access_ended");
      case "session_ended":
        this.lost();
        void this.deps.refresh().then((outcome) => {
          if (this.gen !== g || this.disposed) return;
          if (outcome === "ok") g.link?.reconnect();
          else if (outcome === "refused") {
            this.deps.sessionLost();
            this.end("session_lost");
          } else this.retryLater();
        });
        return;
      case "file_gone":
        this.end("file_gone");
        this.onGone?.();
        return;
      case "document_too_large":
        return this.tooLarge(g);
      case "reload":
        return this.rebase();
      case "rate_limited":
        return this.retryLater(2000);
      default:
        return this.retryLater();
    }
  }

  private onDisconnected(g: Generation): void {
    if (g.probe) return this.abortProbe(g);
    g.synced = false;
    // The sync server never answered: open the file over REST now. A connection that was live
    // keeps its edits in the document and waits a little first (FR-12.9).
    if (!g.hasHistory) return this.refusedLive("sync_unreachable");
    this.lost();
  }

  /** The live connection is gone for now; the document keeps every edit. */
  private lost(): void {
    if (this.state.mode === "live" || this.state.mode === "connecting") {
      this.setState({ mode: this.gen?.hasHistory ? "offline" : "connecting" });
    }
    this.scheduleFallbackCheck();
  }

  private retryLater(delay?: number): void {
    this.lost();
    const ms = delay ?? this.timing.retryMs[Math.min(this.attempt, this.timing.retryMs.length - 1)];
    this.attempt++;
    clearTimeout(this.timers.retry);
    const g = this.gen;
    this.timers.retry = setTimeout(() => {
      this.timers.retry = undefined;
      if (this.gen !== g || this.disposed || this.state.mode === "fallback" || this.state.mode === "ended") return;
      this.ticketRetried = false;
      g?.link?.reconnect();
    }, ms);
  }

  private end(reason: EndReason): void {
    for (const key of ["retry", "fallback", "probe", "probeTimeout"] as const) clearTimeout(this.timers[key]);
    this.gen?.link?.destroy();
    if (this.gen) this.gen.link = null;
    this.setState({ mode: "ended", ended: reason, switching: false, presence: null });
    this.maybeFinishRelease();
  }

  // --- a new history (epoch_mismatch, reload, too large)

  /**
   * Opens the file again in a new document. This member's text, if it holds edits the server
   * never acknowledged, is compared with the server's once synced; if they differ the member
   * chooses (FR-12.9; docs/sync.md, Epochs).
   */
  private rebase(): void {
    const old = this.gen;
    const mine = old && old.localSeq !== old.ackedSeq ? old.text.toString() : null;
    this.destroyGen(old);
    this.setState({ switching: true, mode: this.state.mode === "live" ? "offline" : this.state.mode });
    const g = this.newGen(false);
    g.rebaseMine = mine;
  }

  /** The last edit would take the file over 1 MiB: undo it here and open the file again. */
  private tooLarge(g: Generation): void {
    g.undoManager.undo();
    this.setState({ tooLarge: true });
    this.rebase();
  }

  // --- REST fallback

  private scheduleFallbackCheck(): void {
    if (this.timers.fallback || this.state.mode === "fallback" || this.state.mode === "ended") return;
    this.timers.fallback = setTimeout(() => {
      this.timers.fallback = undefined;
      void this.checkFallback();
    }, this.timing.fallbackAfterMs);
  }

  /** Live, on REST or closed: nothing to fall back from. */
  private settled(): boolean {
    const mode = this.state.mode;
    return mode === "live" || mode === "fallback" || mode === "ended";
  }

  /** When the API answers but the sync server does not, save over REST meanwhile. */
  private async checkFallback(): Promise<void> {
    clearTimeout(this.timers.fallback);
    this.timers.fallback = undefined;
    if (this.settled()) return;
    const res = await this.deps.getFile(this.deps.projectId, this.deps.fileId);
    if (this.disposed || this.settled()) return;
    if (res.ok) return this.enterFallback(res.data);
    if (res.code === "not_found") return this.end("access_ended");
    // Offline: the edits stay in the document and sync when the connection returns.
    if (!this.state.waiting) this.setState({ waiting: true });
    this.scheduleFallbackCheck();
  }

  private enterFallback(detail: ProjectFileDetail): void {
    const g = this.gen;
    const theirs = detail.content ?? "";
    const pending = g !== null && g.localSeq !== g.ackedSeq;
    const mine = g?.hasHistory || pending ? g!.text.toString() : null;
    clearTimeout(this.timers.retry);
    this.destroyGen(g);
    this.gen = null;
    const saver = new Autosaver(this.deps.fileId, theirs, detail.file.version, this.deps.save);
    this.useSaver(saver);
    this.setState({
      mode: "fallback",
      binding: { kind: "rest", saver },
      conflict: pending && mine !== null && mine !== theirs ? { mine, theirs } : null,
      switching: false,
      unsynced: 0,
      stored: false,
      presence: null,
      waiting: false,
      fallbackReason: this.fallbackReason,
    });
    this.scheduleProbe();
  }

  private useSaver(saver: Autosaver): void {
    this.dropSaver();
    this.saver = saver;
    this.unsubscribeSaver = saver.subscribe(() => {
      this.listeners.forEach((listener) => listener());
      this.maybeFinishRelease();
    });
  }

  private dropSaver(): void {
    this.unsubscribeSaver?.();
    this.unsubscribeSaver = null;
    this.saver?.dispose();
    this.saver = null;
  }

  private scheduleProbe(): void {
    clearTimeout(this.timers.probe);
    const ms = this.timing.probeMs[Math.min(this.probeAttempt, this.timing.probeMs.length - 1)];
    this.probeAttempt++;
    this.timers.probe = setTimeout(() => void this.probe(), ms);
  }

  /** Tries the sync server again, once REST holds nothing unsaved. */
  private async probe(): Promise<void> {
    this.timers.probe = undefined;
    const saver = this.saver;
    if (this.disposed || this.state.mode !== "fallback" || !saver) return;
    if (saver.dirty) await saver.flush();
    if (this.disposed || this.saver !== saver) return;
    if (saver.dirty || this.state.conflict || saver.getState().status === "conflict") return this.scheduleProbe();
    // No typing while the file moves; the REST text is all saved, so the server's is current.
    this.setState({ switching: true });
    const g = this.newGen(true);
    this.timers.probeTimeout = setTimeout(() => this.abortProbe(g), this.timing.probeTimeoutMs);
  }

  private abortProbe(g: Generation): void {
    if (this.gen !== g || !g.probe) return;
    clearTimeout(this.timers.probeTimeout);
    this.destroyGen(g);
    this.gen = null;
    this.ticketError = null;
    this.setState({ switching: false });
    this.scheduleProbe();
  }
}

function readPresence(files: unknown): PresenceFiles {
  const out: PresenceFiles = {};
  if (typeof files !== "object" || files === null) return out;
  for (const [fileId, list] of Object.entries(files as Record<string, unknown>)) {
    if (!Array.isArray(list)) continue;
    const users: PresenceUser[] = [];
    for (const u of list as Record<string, unknown>[]) {
      const colour = safeColour(u?.colour);
      if (typeof u?.id !== "string" || !colour) continue;
      users.push({ id: u.id, name: typeof u.name === "string" ? u.name : "", colour, readOnly: u.readOnly === "capacity" ? "capacity" : null });
    }
    if (users.length > 0) out[fileId] = users;
  }
  return out;
}
