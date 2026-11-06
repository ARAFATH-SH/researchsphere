import { act, render, screen } from "@testing-library/react";

import { FakeWebSocket } from "@/features/realtime/fake-websocket";
import { startSocket, stopSocket } from "@/features/realtime/socket";

import { typingTimeout, useIsTyping } from "./typing";

const id = "c1";

function Probe() {
  return <p>{useIsTyping(id) ? "typing" : "quiet"}</p>;
}

const receive = (type: string, data: unknown) => act(async () => FakeWebSocket.latest().receive(type, data));
const typing = (isTyping: boolean, conversationId = id) => receive("typing", { conversationId, userId: "bob", isTyping });
const message = (senderId: string) => receive("message.created", { message: { id: "m", conversationId: id, senderId }, conversationState: "active" });
const shown = () => screen.getByRole("paragraph").textContent;

beforeEach(async () => {
  jest.useFakeTimers();
  FakeWebSocket.install();
  startSocket({ url: "ws://test", refresh: jest.fn(), onSessionLost: jest.fn() });
  await act(async () => FakeWebSocket.latest().accept());
  render(<Probe />);
});
afterEach(() => {
  stopSocket();
  jest.useRealTimers();
});

describe("useIsTyping", () => {
  it("shows a start and clears on isTyping false", async () => {
    await typing(true);
    expect(shown()).toBe("typing");
    await typing(false);
    expect(shown()).toBe("quiet");
  });

  it("ignores another conversation", async () => {
    await typing(true, "c2");
    expect(shown()).toBe("quiet");
    await typing(false, "c2");
  });

  it("clears by itself after 6 s if no stop arrives", async () => {
    await typing(true);
    await act(async () => jest.advanceTimersByTime(typingTimeout - 1));
    expect(shown()).toBe("typing");
    await act(async () => jest.advanceTimersByTime(1));
    expect(shown()).toBe("quiet");
  });

  it("stays while starts keep coming", async () => {
    await typing(true);
    await act(async () => jest.advanceTimersByTime(4000));
    await typing(true);
    await act(async () => jest.advanceTimersByTime(4000));
    expect(shown()).toBe("typing");
    await typing(false);
  });

  it("clears when their message arrives, not on someone else's", async () => {
    await typing(true);
    await message("ada");
    expect(shown()).toBe("typing");
    await message("bob");
    expect(shown()).toBe("quiet");
  });

  it("clears on conversation.updated", async () => {
    await typing(true);
    await receive("conversation.updated", { conversationId: id, state: "active", canMessage: false });
    expect(shown()).toBe("quiet");
  });

  it("clears on sign-out", async () => {
    await typing(true);
    await act(async () => stopSocket());
    expect(shown()).toBe("quiet");
  });
});
