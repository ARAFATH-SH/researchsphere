import { act, render, screen } from "@testing-library/react";

import { FakeWebSocket } from "@/features/realtime/fake-websocket";
import { startSocket, stopSocket } from "@/features/realtime/socket";

import { typingText, typingTimeout, useIsTyping, useTypers } from "./typing";

const id = "c1";

function Probe() {
  const typers = useTypers(id);
  return (
    <>
      <p>{useIsTyping(id) ? "typing" : "quiet"}</p>
      <output>{typers.join(",")}</output>
    </>
  );
}

const receive = (type: string, data: unknown) => act(async () => FakeWebSocket.latest().receive(type, data));
const typing = (isTyping: boolean, conversationId = id, userId = "bob") => receive("typing", { conversationId, userId, isTyping });
const typers = () => screen.getByRole("status").textContent;
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

// FR-16.4 in a group: several people type at once, each with their own wait.
describe("useTypers", () => {
  it("lists each typer in the order they started, and drops each on their own stop or message", async () => {
    await typing(true, id, "nafisa");
    await typing(true, id, "rahim");
    await typing(true, id, "nafisa");
    expect(typers()).toBe("nafisa,rahim");
    await typing(false, id, "nafisa");
    expect(typers()).toBe("rahim");
    await message("rahim");
    expect(typers()).toBe("");
    expect(shown()).toBe("quiet");
  });

  it("times each typer out on their own", async () => {
    await typing(true, id, "nafisa");
    await act(async () => jest.advanceTimersByTime(3000));
    await typing(true, id, "rahim");
    await act(async () => jest.advanceTimersByTime(typingTimeout - 3000));
    expect(typers()).toBe("rahim");
    await act(async () => jest.advanceTimersByTime(3000));
    expect(typers()).toBe("");
  });
});

describe("typingText", () => {
  it("names one or two people, then says several", () => {
    expect(typingText(["Nafisa"])).toBe("Nafisa is typing…");
    expect(typingText(["Nafisa", "Rahim"])).toBe("Nafisa and Rahim are typing…");
    expect(typingText(["Nafisa", "Rahim", "Ada"])).toBe("Several people are typing…");
    expect(typingText([])).toBe("");
  });
});
