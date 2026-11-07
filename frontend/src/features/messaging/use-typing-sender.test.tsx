import { act, renderHook } from "@testing-library/react";

import { FakeWebSocket } from "@/features/realtime/fake-websocket";
import { startSocket, stopSocket } from "@/features/realtime/socket";

import { idleAfter, startEvery, useTypingSender } from "./use-typing-sender";

const id = "c1";
const sent = () => FakeWebSocket.latest().sent.map((frame) => frame.type);
const start = { type: "typing.start", data: { conversationId: id } };

function setup(allowed = true) {
  return renderHook(({ allowed }) => useTypingSender(id, allowed), { initialProps: { allowed } });
}

const tick = (ms: number) => act(async () => void jest.advanceTimersByTime(ms));

beforeEach(async () => {
  jest.useFakeTimers();
  FakeWebSocket.install();
  startSocket({ url: "ws://test", refresh: jest.fn(), onSessionLost: jest.fn() });
  FakeWebSocket.latest().accept();
});
afterEach(() => {
  stopSocket();
  jest.useRealTimers();
});

describe("useTypingSender", () => {
  it("sends typing.start at most once per 3 s", async () => {
    const { result } = setup();
    result.current.input("a");
    expect(FakeWebSocket.latest().sent).toEqual([start]);
    await tick(1000);
    result.current.input("ab");
    await tick(startEvery - 1001);
    result.current.input("abc");
    expect(sent()).toEqual(["typing.start"]);
    await tick(1);
    result.current.input("abcd");
    expect(sent()).toEqual(["typing.start", "typing.start"]);
  });

  it("sends typing.stop after 4 s idle, only once", async () => {
    const { result } = setup();
    result.current.input("a");
    await tick(idleAfter - 1);
    expect(sent()).toEqual(["typing.start"]);
    await tick(1);
    expect(sent()).toEqual(["typing.start", "typing.stop"]);
    await tick(idleAfter * 2);
    expect(sent()).toHaveLength(2);
  });

  it("restarts the idle wait on each key", async () => {
    const { result } = setup();
    result.current.input("a");
    await tick(3000);
    result.current.input("ab");
    await tick(3000);
    expect(sent()).not.toContain("typing.stop");
  });

  it("stops on send or blur, and does not stop twice", async () => {
    const { result } = setup();
    result.current.input("a");
    result.current.stop();
    result.current.stop();
    expect(sent()).toEqual(["typing.start", "typing.stop"]);
    await tick(idleAfter);
    expect(sent()).toHaveLength(2);
  });

  it("sends no stop for someone who never started", () => {
    const { result } = setup();
    result.current.stop();
    expect(sent()).toEqual([]);
  });

  it("treats emptying the box as stopping", () => {
    const { result } = setup();
    result.current.input("a");
    result.current.input("  ");
    expect(sent()).toEqual(["typing.start", "typing.stop"]);
  });

  it("stops when the component goes", () => {
    const { result, unmount } = setup();
    result.current.input("a");
    unmount();
    expect(sent()).toEqual(["typing.start", "typing.stop"]);
  });

  it("sends nothing at all when the composer is not open", async () => {
    const { result } = setup(false);
    result.current.input("a");
    await tick(idleAfter);
    result.current.stop();
    expect(sent()).toEqual([]);
  });

  it("sends nothing more once the conversation closes mid-typing", async () => {
    const { result, rerender } = setup();
    result.current.input("a");
    rerender({ allowed: false });
    await tick(idleAfter);
    result.current.input("ab");
    expect(sent()).toEqual(["typing.start"]);
  });

  it("sends nothing while the socket is down, and starts when it can", async () => {
    const { result } = setup();
    const closed = FakeWebSocket.latest();
    closed.serverClose(1013);
    result.current.input("a");
    expect(closed.sent).toEqual([]);
    await tick(1000);
    FakeWebSocket.latest().accept();
    result.current.input("ab");
    expect(sent()).toEqual(["typing.start"]);
  });
});
