import { act, render, screen } from "@testing-library/react";

import { getPresence } from "@/features/realtime/api";
import { FakeWebSocket } from "@/features/realtime/fake-websocket";
import { startSocket, stopSocket } from "@/features/realtime/socket";

import { PresenceAvatar } from "./presence-avatar";
import { presenceRefresh } from "./presence";

jest.mock("@/features/realtime/api", () => ({ getPresence: jest.fn(), maxPresenceIds: 50 }));

const mockPresence = jest.mocked(getPresence);

function people(ids: string[]) {
  return render(
    <>
      {ids.map((id) => (
        <PresenceAvatar key={id} userId={id} name={id} src={null} size="post" />
      ))}
    </>,
  );
}

async function advance(ms: number) {
  await act(async () => {
    await jest.advanceTimersByTimeAsync(ms);
  });
}

const dots = () => screen.queryAllByRole("img", { name: "Online" });
const answer = (online: string[]) =>
  mockPresence.mockImplementation(async (ids) => ({ ok: true, data: ids.map((userId) => ({ userId, online: online.includes(userId) })) }));

beforeEach(() => {
  jest.useFakeTimers();
  FakeWebSocket.install();
  answer([]);
});
afterEach(() => {
  stopSocket();
  jest.useRealTimers();
  jest.resetAllMocks();
});

describe("presence", () => {
  it("asks once for everyone on screen, and shows a labelled dot", async () => {
    answer(["a"]);
    people(["a", "b"]);
    await advance(0);
    expect(mockPresence).toHaveBeenCalledTimes(1);
    expect(mockPresence).toHaveBeenCalledWith(["a", "b"]);
    expect(dots()).toHaveLength(1);
  });

  it("asks for at most 50 people at a time", async () => {
    people(Array.from({ length: 120 }, (_, i) => `u${i}`));
    await advance(0);
    expect(mockPresence.mock.calls.map(([ids]) => ids.length)).toEqual([50, 50, 20]);
  });

  it("refreshes every 60 s while someone is on screen, and stops after", async () => {
    const view = people(["a"]);
    await advance(0);
    mockPresence.mockClear();
    await advance(presenceRefresh);
    expect(mockPresence).toHaveBeenCalledTimes(1);
    answer(["a"]);
    await advance(presenceRefresh);
    expect(dots()).toHaveLength(1);
    view.unmount();
    mockPresence.mockClear();
    await advance(presenceRefresh * 3);
    expect(mockPresence).not.toHaveBeenCalled();
  });

  it("refreshes after every connect", async () => {
    people(["a"]);
    await advance(0);
    mockPresence.mockClear();
    startSocket({ url: "ws://test", refresh: jest.fn(), onSessionLost: jest.fn() });
    await act(async () => FakeWebSocket.latest().accept());
    expect(mockPresence).toHaveBeenCalledWith(["a"]);
  });

  it("follows presence events, and repeats change nothing", async () => {
    people(["a"]);
    await advance(0);
    startSocket({ url: "ws://test", refresh: jest.fn(), onSessionLost: jest.fn() });
    await act(async () => FakeWebSocket.latest().accept());
    await act(async () => FakeWebSocket.latest().receive("presence", { userId: "a", online: true }));
    await act(async () => FakeWebSocket.latest().receive("presence", { userId: "a", online: true }));
    expect(dots()).toHaveLength(1);
    await act(async () => FakeWebSocket.latest().receive("presence", { userId: "a", online: false }));
    expect(dots()).toHaveLength(0);
  });

  it("forgets who was online on sign-out", async () => {
    answer(["a"]);
    people(["a"]);
    await advance(0);
    startSocket({ url: "ws://test", refresh: jest.fn(), onSessionLost: jest.fn() });
    await act(async () => stopSocket());
    expect(dots()).toHaveLength(0);
  });
});
