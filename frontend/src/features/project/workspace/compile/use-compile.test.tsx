import { act, renderHook } from "@testing-library/react";

import type { RealtimeEvent } from "@/features/realtime/events";

import { projectID } from "../../test-projects";
import { compileErrorText, failureText, fileName, getCompile, getLatestCompile, startCompile } from "./api";
import { compile, succeeded } from "./test-compiles";
import { newer, useCompile } from "./use-compile";

jest.mock("./api", () => ({
  ...jest.requireActual("./api"),
  startCompile: jest.fn(),
  getLatestCompile: jest.fn(),
  getCompile: jest.fn(),
}));
const mockRealtime = { events: new Set<(e: RealtimeEvent) => void>(), connected: new Set<() => void>() };
jest.mock("@/features/realtime/socket", () => ({
  onRealtimeEvent: (l: (e: RealtimeEvent) => void) => {
    mockRealtime.events.add(l);
    return () => mockRealtime.events.delete(l);
  },
  onConnected: (l: () => void) => {
    mockRealtime.connected.add(l);
    return () => mockRealtime.connected.delete(l);
  },
}));

const emit = (projectId: string, c: ReturnType<typeof compile>) =>
  act(() => mockRealtime.events.forEach((l) => l({ type: "compile.updated", data: { projectId, compile: c } })));

async function setup(flush: () => Promise<void> = async () => {}) {
  const hook = renderHook(() => useCompile(projectID, flush));
  await act(async () => {});
  return hook;
}

beforeEach(() => {
  jest.mocked(getLatestCompile).mockResolvedValue({ ok: false, code: "no_compile", message: "x" });
});
afterEach(() => {
  mockRealtime.events.clear();
  mockRealtime.connected.clear();
  jest.clearAllMocks();
});

describe("useCompile", () => {
  it("reads the newest compile on open and again after a reconnect", async () => {
    jest.mocked(getLatestCompile).mockResolvedValue({ ok: true, data: succeeded("c1") });
    const { result } = await setup();
    expect(result.current.compile?.id).toBe("c1");
    jest.mocked(getLatestCompile).mockResolvedValue({ ok: true, data: compile("c2", { createdAt: "2026-11-23T10:05:00Z" }) });
    await act(async () => mockRealtime.connected.forEach((l) => l()));
    expect(getLatestCompile).toHaveBeenCalledTimes(2);
    expect(result.current.compile?.id).toBe("c2");
  });

  it("follows queued, running and the end from events, ignoring other projects and late states", async () => {
    const { result } = await setup();
    emit(projectID, compile("c1"));
    expect(result.current.compile?.status).toBe("queued");
    emit(projectID, compile("c1", { status: "running" }));
    emit("other-project", compile("x1", { status: "failed", createdAt: "2026-12-01T00:00:00Z" }));
    expect(result.current.compile?.status).toBe("running");
    emit(projectID, compile("c1"));
    expect(result.current.compile?.status).toBe("running");
    emit(projectID, compile("c1", { status: "timed_out" }));
    expect(result.current.compile?.status).toBe("timed_out");
  });

  it("waits for the flush before it asks for a compile", async () => {
    let release = () => {};
    const flush = jest.fn(() => new Promise<void>((r) => (release = r)));
    jest.mocked(startCompile).mockResolvedValue({ ok: true, data: compile("c1") });
    const { result } = await setup(flush);
    let done: Promise<void> = Promise.resolve();
    act(() => {
      done = result.current.start();
    });
    expect(flush).toHaveBeenCalled();
    expect(startCompile).not.toHaveBeenCalled();
    expect(result.current.starting).toBe(true);
    await act(async () => {
      release();
      await done;
    });
    expect(startCompile).toHaveBeenCalledWith(projectID);
    expect(result.current.starting).toBe(false);
    expect(result.current.compile?.id).toBe("c1");
  });

  it("follows the compile already running on a 409, with who started it", async () => {
    jest.mocked(startCompile).mockResolvedValue({ ok: false, code: "compile_running", message: "x", compileId: "c9" });
    jest.mocked(getCompile).mockResolvedValue({ ok: true, data: compile("c9", { status: "running", requestedBy: "u-bob" }) });
    const { result } = await setup();
    await act(() => result.current.start());
    expect(result.current.compile?.id).toBe("c9");
    expect(result.current.notice).toEqual({ kind: "running", by: "u-bob" });
  });

  it.each([
    ["compile_unavailable", { kind: "unavailable" }],
    ["compile_busy", { kind: "error", text: compileErrorText({ code: "compile_busy", message: "" }) }],
    ["main_file_missing", { kind: "error", text: compileErrorText({ code: "main_file_missing", message: "" }) }],
    ["rate_limited", { kind: "error", text: compileErrorText({ code: "rate_limited", message: "" }) }],
  ])("says why it could not start: %s", async (code, notice) => {
    jest.mocked(startCompile).mockResolvedValue({ ok: false, code, message: "x" });
    const { result } = await setup();
    await act(() => result.current.start());
    expect(result.current.notice).toEqual(notice);
    expect(result.current.compile).toBeNull();
  });
});

describe("newer", () => {
  it("takes a later job, or a later state of the same job", () => {
    const a = compile("a", { createdAt: "2026-11-23T10:00:00Z" });
    const b = compile("b", { createdAt: "2026-11-23T10:01:00Z" });
    expect(newer(null, a)).toBe(true);
    expect(newer(a, b)).toBe(true);
    expect(newer(b, a)).toBe(false);
    expect(newer(compile("a", { status: "running" }), compile("a"))).toBe(false);
    expect(newer(compile("a", { status: "running" }), succeeded("a"))).toBe(true);
  });
});

describe("texts", () => {
  it("has words for every start refusal and failure", () => {
    expect(compileErrorText({ code: "compile_unavailable", message: "" })).toMatch(/^Compile service unavailable/);
    expect(compileErrorText({ code: "something_new", message: "" })).toBe("The compile couldn’t start. Try again.");
    expect(failureText("out_of_memory")).toContain("1 GB");
    expect(failureText(null)).toBe("LaTeX finished without making a PDF.");
  });

  it("reads a download's name and never a path", () => {
    expect(fileName(`attachment; filename="My paper.pdf"; filename*=UTF-8''My%20paper%20%E2%9C%93.pdf`)).toBe("My paper ✓.pdf");
    expect(fileName(`attachment; filename="paper.zip"`)).toBe("paper.zip");
    expect(fileName(`attachment; filename="../../etc/passwd"`)).toBe("passwd");
    expect(fileName(`attachment; filename=".."`)).toBeNull();
    expect(fileName(null)).toBeNull();
  });
});
