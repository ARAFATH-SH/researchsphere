import { act, render, screen } from "@testing-library/react";

import { FakeWebSocket } from "@/features/realtime/fake-websocket";
import { startSocket, stopSocket } from "@/features/realtime/socket";

import { noticeAfter, ReconnectingNotice } from "./reconnecting-notice";

const notice = () => screen.queryByText("Reconnecting…");
const tick = (ms: number) => act(async () => void jest.advanceTimersByTime(ms));

beforeEach(() => {
  jest.useFakeTimers();
  jest.spyOn(Math, "random").mockReturnValue(1);
  FakeWebSocket.install();
  startSocket({ url: "ws://test", refresh: jest.fn().mockResolvedValue("ok"), onSessionLost: jest.fn() });
  FakeWebSocket.latest().accept();
  render(<ReconnectingNotice />);
});
afterEach(() => {
  stopSocket();
  jest.useRealTimers();
  jest.restoreAllMocks();
});

describe("ReconnectingNotice", () => {
  it("shows nothing while connected", () => {
    expect(notice()).toBeNull();
  });

  it("appears after the socket has been down for 3 s, through failed attempts", async () => {
    await act(async () => FakeWebSocket.latest().serverClose(1013));
    await tick(noticeAfter - 1);
    expect(notice()).toBeNull();
    await tick(1);
    expect(notice()).toBeTruthy();
    expect(notice()?.closest('[role="status"]')).toBeTruthy();
  });

  it("does not appear for a reconnect within 3 s", async () => {
    await act(async () => FakeWebSocket.latest().serverClose(1001));
    await tick(1000);
    await act(async () => FakeWebSocket.latest().accept());
    await tick(noticeAfter);
    expect(notice()).toBeNull();
  });

  it("goes on reconnect", async () => {
    await act(async () => FakeWebSocket.latest().serverClose(1001));
    await tick(noticeAfter);
    await act(async () => FakeWebSocket.latest().accept());
    expect(notice()).toBeNull();
  });

  it("stays away from a tab replaced by a newer socket", async () => {
    await act(async () => FakeWebSocket.latest().serverClose(4409));
    await tick(noticeAfter * 2);
    expect(notice()).toBeNull();
  });
});
