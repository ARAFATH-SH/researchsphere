import type { SaveResult } from "../files/api";
import { textFile } from "../files/test-files";
import { Autosaver, autosaveDelayMs, retryDelaysMs, type SaveFn, statusText } from "./autosave";

const ok = (version: number): SaveResult => ({ ok: true, data: textFile("t1", "main.tex", { version }) });
const offline: SaveResult = { ok: false, status: 0, code: "network_error", message: "Could not reach the server." };
const conflict: SaveResult = {
  ok: false,
  status: 409,
  code: "version_conflict",
  message: "changed",
  current: { version: 7, content: "theirs", updatedAt: "2026-11-17T10:00:00Z" },
};

function saver(save: SaveFn) {
  return new Autosaver("t1", "start", 3, save);
}

const statusOf = (s: Autosaver) => s.getState().status;

beforeEach(() => jest.useFakeTimers());
afterEach(() => jest.useRealTimers());

describe("Autosaver", () => {
  it("saves 1.5 s after the last change, with the version it started from", async () => {
    const save = jest.fn<ReturnType<SaveFn>, Parameters<SaveFn>>().mockResolvedValue(ok(4));
    const s = saver(save);
    s.edit(() => "a");
    await jest.advanceTimersByTimeAsync(1000);
    s.edit(() => "ab");
    expect(statusText(s.getState())).toBe("Saving…");
    await jest.advanceTimersByTimeAsync(autosaveDelayMs - 1);
    expect(save).not.toHaveBeenCalled();
    await jest.advanceTimersByTimeAsync(1);
    expect(save).toHaveBeenCalledTimes(1);
    expect(save).toHaveBeenCalledWith("ab", 3, { keepalive: false });
    expect(statusText(s.getState())).toBe("Saved");
    expect(s.dirty).toBe(false);
    expect(s.version).toBe(4);
  });

  it("keeps a change made while saving and saves it next, with the new version", async () => {
    let finish: (r: SaveResult) => void = () => {};
    const save = jest
      .fn<ReturnType<SaveFn>, Parameters<SaveFn>>()
      .mockImplementationOnce(() => new Promise((resolve) => (finish = resolve)))
      .mockResolvedValue(ok(5));
    const s = saver(save);
    s.edit(() => "one");
    await jest.advanceTimersByTimeAsync(autosaveDelayMs);
    expect(statusOf(s)).toBe("saving");
    s.edit(() => "two");
    finish(ok(4));
    await jest.advanceTimersByTimeAsync(0);
    expect(statusOf(s)).toBe("pending");
    expect(s.dirty).toBe(true);
    await jest.advanceTimersByTimeAsync(autosaveDelayMs);
    expect(save).toHaveBeenLastCalledWith("two", 4, { keepalive: false });
    expect(statusOf(s)).toBe("saved");
  });

  it("flushes at once, with keepalive only for a small text, and does nothing when clean", async () => {
    const save = jest.fn<ReturnType<SaveFn>, Parameters<SaveFn>>().mockResolvedValue(ok(4));
    const s = saver(save);
    await s.flush();
    expect(save).not.toHaveBeenCalled();
    s.edit(() => "small");
    await s.flush({ keepalive: true });
    expect(save).toHaveBeenCalledWith("small", 3, { keepalive: true });
    const big = "x".repeat(30_000);
    s.edit(() => big);
    await s.flush({ keepalive: true });
    expect(save).toHaveBeenLastCalledWith(big, 4, { keepalive: false });
    // The debounce timer of the edits was cancelled.
    await jest.advanceTimersByTimeAsync(autosaveDelayMs * 2);
    expect(save).toHaveBeenCalledTimes(2);
  });

  it("keeps the text while offline and retries with backoff, and at once when back online", async () => {
    const save = jest.fn<ReturnType<SaveFn>, Parameters<SaveFn>>().mockResolvedValue(offline);
    const s = saver(save);
    s.edit(() => "kept");
    await jest.advanceTimersByTimeAsync(autosaveDelayMs);
    expect(statusText(s.getState())).toBe("Offline, changes kept");
    await jest.advanceTimersByTimeAsync(retryDelaysMs[0]);
    expect(save).toHaveBeenCalledTimes(2);
    await jest.advanceTimersByTimeAsync(retryDelaysMs[1] - 1);
    expect(save).toHaveBeenCalledTimes(2);
    await jest.advanceTimersByTimeAsync(1);
    expect(save).toHaveBeenCalledTimes(3);
    expect(s.text).toBe("kept");
    expect(s.dirty).toBe(true);

    save.mockResolvedValue(ok(4));
    s.retryNow();
    await jest.advanceTimersByTimeAsync(0);
    expect(save).toHaveBeenCalledTimes(4);
    expect(save).toHaveBeenLastCalledWith("kept", 3, { keepalive: false });
    expect(statusText(s.getState())).toBe("Saved");
  });

  it("retries a server error and a rate limit, saying it is retrying", async () => {
    const save = jest
      .fn<ReturnType<SaveFn>, Parameters<SaveFn>>()
      .mockResolvedValueOnce({ ok: false, status: 503, code: "internal_error", message: "down" })
      .mockResolvedValueOnce({ ok: false, status: 429, code: "rate_limited", message: "slow" })
      .mockResolvedValue(ok(4));
    const s = saver(save);
    s.edit(() => "x");
    await jest.advanceTimersByTimeAsync(autosaveDelayMs);
    expect(statusText(s.getState())).toBe("Not saved yet, retrying. Changes kept");
    await jest.advanceTimersByTimeAsync(retryDelaysMs[0] + retryDelaysMs[1]);
    expect(save).toHaveBeenCalledTimes(3);
    expect(statusOf(s)).toBe("saved");
  });

  it("stops on a conflict until the user chooses theirs, which replaces the text", async () => {
    const save = jest.fn<ReturnType<SaveFn>, Parameters<SaveFn>>().mockResolvedValue(conflict);
    const s = saver(save);
    s.edit(() => "mine");
    await jest.advanceTimersByTimeAsync(autosaveDelayMs);
    expect(statusOf(s)).toBe("conflict");
    expect(s.getState().current?.content).toBe("theirs");
    // More typing does not save over it.
    s.edit(() => "mine, more");
    await jest.advanceTimersByTimeAsync(autosaveDelayMs * 3);
    expect(save).toHaveBeenCalledTimes(1);
    expect(s.text).toBe("mine, more");

    expect(s.resolve("theirs")).toBe("theirs");
    expect(s.text).toBe("theirs");
    expect(s.version).toBe(7);
    expect(s.dirty).toBe(false);
    expect(statusOf(s)).toBe("saved");
  });

  it("saves mine over theirs with their version when the user keeps mine", async () => {
    const save = jest.fn<ReturnType<SaveFn>, Parameters<SaveFn>>().mockResolvedValueOnce(conflict).mockResolvedValue(ok(8));
    const s = saver(save);
    s.edit(() => "mine");
    await jest.advanceTimersByTimeAsync(autosaveDelayMs);
    expect(s.resolve("mine")).toBeNull();
    await jest.advanceTimersByTimeAsync(0);
    expect(save).toHaveBeenLastCalledWith("mine", 7, { keepalive: false });
    expect(statusOf(s)).toBe("saved");
    expect(s.version).toBe(8);
  });

  it("says why a save was refused for good, keeps the text and tries again on the next change", async () => {
    const save = jest
      .fn<ReturnType<SaveFn>, Parameters<SaveFn>>()
      .mockResolvedValueOnce({ ok: false, status: 413, code: "file_too_large", message: "too large" })
      .mockResolvedValue(ok(4));
    const s = saver(save);
    s.edit(() => "huge");
    await jest.advanceTimersByTimeAsync(autosaveDelayMs);
    expect(statusText(s.getState())).toBe("Not saved: the file is over 1 MiB");
    await jest.advanceTimersByTimeAsync(60_000);
    expect(save).toHaveBeenCalledTimes(1);
    expect(s.text).toBe("huge");
    s.edit(() => "small");
    await jest.advanceTimersByTimeAsync(autosaveDelayMs);
    expect(statusOf(s)).toBe("saved");
  });

  it("saves nothing after it is disposed", async () => {
    const save = jest.fn<ReturnType<SaveFn>, Parameters<SaveFn>>().mockResolvedValue(ok(4));
    const s = saver(save);
    s.edit(() => "x");
    s.dispose();
    await jest.advanceTimersByTimeAsync(autosaveDelayMs * 2);
    expect(save).not.toHaveBeenCalled();
  });
});
