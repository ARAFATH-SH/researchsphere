import { fileErrorText, type ProjectFileCurrent, type SaveResult } from "../files/api";

// Autosave of one text file (FR-12.6, client half). The Autosaver holds the file's text between
// the editor and the server: the editor tells it about each change, it saves 1.5 s after the
// last one, on demand (Ctrl+S, a file switch, the tab hidden) and again with backoff while the
// server cannot be reached. The text stays here until a save succeeds, so nothing is lost while
// offline. A version conflict stops saving until the user chooses (resolve).
//
// It is the one place that owns a file's text outside the editor, so slice 22 can replace it
// with the Yjs document, whose sync server saves (docs/decisions.md, 2026-11-17).

export type SaveStatus = "saved" | "pending" | "saving" | "offline" | "retrying" | "conflict" | "failed";

export type SaveState = {
  status: SaveStatus;
  // message says why a save failed (status failed or retrying).
  message?: string;
  // current is the server's file on a conflict.
  current?: ProjectFileCurrent;
};

export type SaveFn = (text: string, version: number, options: { keepalive: boolean }) => Promise<SaveResult>;

export const autosaveDelayMs = 1500;
export const retryDelaysMs = [1000, 2000, 4000, 8000, 16000, 30000];
// A keepalive request may carry at most 64 KB, all requests of the page together.
const keepaliveBytes = 60 * 1024;

function retryable(res: Extract<SaveResult, { ok: false }>): boolean {
  // live_session_open: the sync server still holds the file (its mark lasts up to 30 s after
  // it stops), so the REST save waits for it (docs/sync.md).
  return res.status === 0 || res.status >= 500 || res.status === 429 || res.code === "live_session_open";
}

function failureText(res: Extract<SaveResult, { ok: false }>): string {
  if (res.code === "file_too_large" || res.code === "request_too_large") return "the file is over 1 MiB";
  return fileErrorText(res);
}

export class Autosaver {
  version: number;
  private read: () => string;
  private seq = 0;
  private savedSeq = 0;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private inFlight: Promise<void> | null = null;
  private attempt = 0;
  private state: SaveState = { status: "saved" };
  private readonly listeners = new Set<() => void>();
  private disposed = false;

  constructor(
    readonly fileId: string,
    text: string,
    version: number,
    private readonly save: SaveFn,
    private readonly delayMs = autosaveDelayMs,
  ) {
    this.read = () => text;
    this.version = version;
  }

  // text is the file's latest text, saved or not.
  get text(): string {
    return this.read();
  }

  // dirty says whether there are changes no save has stored yet.
  get dirty(): boolean {
    return this.seq !== this.savedSeq;
  }

  getState = (): SaveState => this.state;

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  // edit records a change. read returns the new text; it is called only when saving, so an
  // editor can pass its immutable document without turning it into a string on every key.
  edit(read: () => string): void {
    this.read = read;
    this.seq++;
    if (this.state.status === "conflict") return;
    this.attempt = 0;
    this.setState({ status: "pending" });
    this.schedule(this.delayMs);
  }

  // flush saves at once when there are changes. keepalive lets the request outlive the page.
  flush({ keepalive = false }: { keepalive?: boolean } = {}): Promise<void> {
    return this.run(keepalive);
  }

  // retryNow tries a waiting retry at once, as when the browser is back online.
  retryNow(): void {
    if (this.state.status === "offline" || this.state.status === "retrying") void this.run(false);
  }

  // resolve answers a conflict. "theirs" takes the server's text and returns it, for the
  // editor to show; "mine" saves this text over it and returns null.
  resolve(choice: "theirs" | "mine"): string | null {
    const current = this.state.current;
    if (this.state.status !== "conflict" || !current) return null;
    this.version = current.version;
    if (choice === "theirs") {
      const content = current.content;
      this.read = () => content;
      this.seq++;
      this.savedSeq = this.seq;
      this.setState({ status: "saved" });
      return content;
    }
    this.setState({ status: "pending" });
    void this.run(false);
    return null;
  }

  dispose(): void {
    this.disposed = true;
    clearTimeout(this.timer);
    this.listeners.clear();
  }

  private setState(state: SaveState): void {
    this.state = state;
    this.listeners.forEach((listener) => listener());
  }

  private schedule(delay: number): void {
    clearTimeout(this.timer);
    if (!this.disposed) this.timer = setTimeout(() => void this.run(false), delay);
  }

  private async run(keepalive: boolean): Promise<void> {
    clearTimeout(this.timer);
    while (this.inFlight) await this.inFlight;
    if (this.state.status === "conflict") return;
    if (!this.dirty) {
      if (this.state.status !== "saved") this.setState({ status: "saved" });
      return;
    }
    const attempt = this.attemptSave(keepalive);
    this.inFlight = attempt;
    try {
      await attempt;
    } finally {
      this.inFlight = null;
    }
  }

  private async attemptSave(keepalive: boolean): Promise<void> {
    const seq = this.seq;
    const text = this.read();
    // At most 3 bytes of UTF-8 per UTF-16 unit: a safe bound without encoding the text.
    const small = keepalive && text.length * 3 <= keepaliveBytes;
    this.setState({ status: "saving" });
    const res = await this.save(text, this.version, { keepalive: small });
    if (res.ok) {
      this.version = res.data.version;
      this.savedSeq = seq;
      this.attempt = 0;
      // A change made while saving has its own timer.
      this.setState({ status: this.dirty ? "pending" : "saved" });
      return;
    }
    if (res.code === "version_conflict" && res.current) {
      this.setState({ status: "conflict", current: res.current });
      return;
    }
    if (retryable(res)) {
      const delay = retryDelaysMs[Math.min(this.attempt, retryDelaysMs.length - 1)];
      this.attempt++;
      this.setState(
        res.status === 0 ? { status: "offline" } : { status: "retrying", message: failureText(res) },
      );
      this.schedule(delay);
      return;
    }
    // Refused for good (too large, no longer allowed, deleted): the text stays, the next edit
    // tries again.
    this.setState({ status: "failed", message: failureText(res) });
  }
}

// statusText is the save status the workspace bar shows.
export function statusText(state: SaveState): string {
  switch (state.status) {
    case "saved":
      return "Saved";
    case "pending":
    case "saving":
      return "Saving…";
    case "offline":
      return "Offline, changes kept";
    case "retrying":
      return "Not saved yet, retrying. Changes kept";
    case "conflict":
      return "Not saved: this file changed in another tab";
    case "failed":
      return `Not saved: ${state.message ?? "something went wrong"}`;
  }
}
