import { act, fireEvent, render, screen } from "@testing-library/react";

import { getPresence } from "@/features/realtime/api";
import { FakeWebSocket } from "@/features/realtime/fake-websocket";
import { startSocket, stopSocket } from "@/features/realtime/socket";

import { type Conversation, getConversation, listMessages, markConversationRead, type Message, sendMessage } from "./api";
import { adaID, bobID, conversation, conversationID, message } from "./test-data";
import { Thread } from "./thread";
import { refreshUnreadMessages } from "./unread-count";
import { ConversationsProvider } from "./use-conversations";

jest.mock("./api");
jest.mock("./unread-count");
jest.mock("@/features/realtime/api", () => ({ getPresence: jest.fn(), maxPresenceIds: 50 }));
jest.mock("@/features/auth/session", () => ({
  useSession: () => ({ status: "signedIn", user: { id: "0b8f4c1e-0000-4000-8000-000000000001" } }),
}));

const mockGet = jest.mocked(getConversation);
const mockMessages = jest.mocked(listMessages);
const mockRead = jest.mocked(markConversationRead);
const mockSend = jest.mocked(sendMessage);

function load(c: Conversation, items: Message[], extra: { nextBefore?: string | null; otherLastReadMessageId?: string | null } = {}) {
  mockGet.mockResolvedValue({ ok: true, data: c });
  mockMessages.mockResolvedValue({
    ok: true,
    data: { items: [...items].reverse(), nextBefore: extra.nextBefore ?? null, otherLastReadMessageId: extra.otherLastReadMessageId ?? null },
  });
}

async function connectAndRender() {
  await act(async () => FakeWebSocket.latest().accept());
  render(
    <ConversationsProvider>
      <Thread conversationId={conversationID} />
    </ConversationsProvider>,
  );
  await screen.findByRole("log");
}

const receive = (type: string, data: unknown) => act(async () => FakeWebSocket.latest().receive(type, data));
const created = (m: Message, extra: { conversationState?: string; clientId?: string } = {}) =>
  receive("message.created", { message: m, conversationState: "active", ...extra });
const count = (text: string) => screen.queryAllByText(text).length;
const box = () => screen.getByRole("textbox", { name: "Write a message" }) as HTMLTextAreaElement;
const sentFrames = () => FakeWebSocket.latest().sent.map((frame) => frame.type);

async function write(text: string) {
  fireEvent.change(box(), { target: { value: text } });
  await act(async () => fireEvent.keyDown(box(), { key: "Enter" }));
}

let focused: boolean;

beforeEach(() => {
  focused = true;
  jest.spyOn(document, "hasFocus").mockImplementation(() => focused);
  mockRead.mockResolvedValue({ ok: true, data: undefined });
  jest.mocked(refreshUnreadMessages).mockResolvedValue();
  jest.mocked(getPresence).mockResolvedValue({ ok: true, data: [] });
  Object.defineProperty(globalThis.crypto, "randomUUID", { configurable: true, value: jest.fn(() => "client-1") });
  FakeWebSocket.install();
  startSocket({ url: "ws://test", refresh: jest.fn().mockResolvedValue("ok"), onSessionLost: jest.fn() });
});
afterEach(() => {
  stopSocket();
  jest.resetAllMocks();
});

describe("message.created", () => {
  it("appends a message, once however often it arrives", async () => {
    load(conversation(), [message(1, bobID)]);
    await connectAndRender();
    await created(message(2, bobID, "hello"));
    await created(message(2, bobID, "hello"));
    expect(count("hello")).toBe(1);
  });

  it("ignores another conversation", async () => {
    load(conversation(), [message(1, bobID)]);
    await connectAndRender();
    await created({ ...message(2, bobID, "elsewhere"), conversationId: "other" });
    expect(count("elsewhere")).toBe(0);
  });

  it("marks read when the window is focused", async () => {
    load(conversation(), [message(1, bobID)]);
    await connectAndRender();
    mockRead.mockClear();
    await created(message(2, bobID));
    expect(mockRead).toHaveBeenCalledWith(conversationID, message(2, bobID).id);
  });

  it("waits for focus before marking read", async () => {
    load(conversation(), [message(1, adaID)]);
    focused = false;
    await connectAndRender();
    await created(message(2, bobID));
    expect(mockRead).not.toHaveBeenCalled();
    focused = true;
    await act(async () => window.dispatchEvent(new Event("focus")));
    expect(mockRead).toHaveBeenCalledTimes(1);
  });

  it("never marks a received request read before Accept", async () => {
    load(conversation({ state: "request", requestedBy: bobID, canMessage: false }), [message(1, bobID)]);
    await connectAndRender();
    await created(message(2, bobID), { conversationState: "request" });
    expect(mockRead).not.toHaveBeenCalled();
  });

  it("keeps a message that arrives during the first load", async () => {
    load(conversation(), [message(1, bobID, "stored")]);
    await act(async () => FakeWebSocket.latest().accept());
    render(
      <ConversationsProvider>
        <Thread conversationId={conversationID} />
      </ConversationsProvider>,
    );
    await created(message(2, bobID, "early"));
    await screen.findByRole("log");
    expect(count("stored")).toBe(1);
    expect(count("early")).toBe(1);
  });
});

describe("the sending tab", () => {
  it("shows one message when the echo beats the send's answer", async () => {
    load(conversation(), []);
    let answer: (value: Awaited<ReturnType<typeof sendMessage>>) => void = () => {};
    mockSend.mockReturnValue(new Promise((resolve) => (answer = resolve)));
    await connectAndRender();
    await write("mine");
    expect(count("mine")).toBe(1);
    await created(message(5, adaID, "mine"), { clientId: "client-1" });
    expect(count("mine")).toBe(1);
    await act(async () => answer({ ok: true, data: { message: message(5, adaID, "mine"), conversationState: "active" } }));
    expect(count("mine")).toBe(1);
  });

  it("shows one message when the answer beats the echo", async () => {
    load(conversation(), []);
    mockSend.mockResolvedValue({ ok: true, data: { message: message(5, adaID, "mine"), conversationState: "active" } });
    await connectAndRender();
    await write("mine");
    await created(message(5, adaID, "mine"), { clientId: "client-1" });
    expect(count("mine")).toBe(1);
  });

  it("shows no failure when the echo arrived and only the answer was lost", async () => {
    load(conversation(), []);
    let answer: (value: Awaited<ReturnType<typeof sendMessage>>) => void = () => {};
    mockSend.mockReturnValue(new Promise((resolve) => (answer = resolve)));
    await connectAndRender();
    await write("mine");
    await created(message(5, adaID, "mine"), { clientId: "client-1" });
    await act(async () => answer({ ok: false, code: "network_error", message: "Could not reach the server." }));
    expect(count("mine")).toBe(1);
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("shows a message sent from another tab", async () => {
    load(conversation(), []);
    await connectAndRender();
    await created(message(5, adaID, "from my phone"), { clientId: "someone-elses" });
    expect(count("from my phone")).toBe(1);
  });
});

describe("conversation.read", () => {
  it("moves Seen live, and never back", async () => {
    load(conversation(), [message(1, adaID), message(2, adaID)]);
    await connectAndRender();
    expect(screen.queryByText("Seen")).toBeNull();
    await receive("conversation.read", { conversationId: conversationID, messageId: message(2, adaID).id });
    expect(screen.getByText("Seen")).toBeTruthy();
    await receive("conversation.read", { conversationId: conversationID, messageId: message(1, adaID).id });
    const text = screen.getByRole("log").textContent ?? "";
    expect(text.indexOf("message 2")).toBeLessThan(text.indexOf("Seen"));
  });
});

describe("conversation.updated", () => {
  it("opens the composer when a sent request is accepted", async () => {
    load(conversation({ state: "request", requestedBy: adaID, canMessage: false }), [message(1, adaID)]);
    await connectAndRender();
    expect(box().disabled).toBe(true);
    expect(screen.getByText(/Waiting for Bob Babbage/)).toBeTruthy();
    mockGet.mockResolvedValue({ ok: true, data: conversation({ requestedBy: adaID }) });
    await receive("conversation.updated", { conversationId: conversationID, state: "active", canMessage: true });
    expect(box().disabled).toBe(false);
    expect(screen.queryByText(/Waiting for/)).toBeNull();
  });

  it("closes the conversation when a request is declined", async () => {
    load(conversation({ state: "request", requestedBy: adaID, canMessage: false }), [message(1, adaID)]);
    await connectAndRender();
    mockGet.mockResolvedValue({ ok: true, data: conversation({ state: "declined", requestedBy: adaID, canMessage: false }) });
    await receive("conversation.updated", { conversationId: conversationID, state: "declined", canMessage: false });
    expect(screen.getByText("This conversation is closed.")).toBeTruthy();
  });

  it("turns the composer off when canMessage goes false, and reads the conversation again", async () => {
    load(conversation(), [message(1, bobID)]);
    await connectAndRender();
    mockGet.mockClear();
    mockGet.mockResolvedValue({ ok: true, data: conversation({ canMessage: false }) });
    await receive("conversation.updated", { conversationId: conversationID, state: "active", canMessage: false });
    expect(box().disabled).toBe(true);
    expect(screen.getByText("You can't reply to this conversation.")).toBeTruthy();
    expect(mockGet).toHaveBeenCalledTimes(1);
  });
});

describe("typing", () => {
  it("shows it under the header in place of the headline, until their message", async () => {
    load(conversation(), [message(1, bobID)]);
    await connectAndRender();
    await receive("typing", { conversationId: conversationID, userId: bobID, isTyping: true });
    expect(screen.getByText("Bob Babbage is typing…")).toBeTruthy();
    expect(screen.queryByText("Analyst")).toBeNull();
    await created(message(2, bobID));
    expect(screen.queryByText("Bob Babbage is typing…")).toBeNull();
    expect(screen.getByText("Analyst")).toBeTruthy();
  });

  it("sends typing.start from the composer, and typing.stop on send and on blur", async () => {
    load(conversation(), []);
    mockSend.mockResolvedValue({ ok: true, data: { message: message(5, adaID, "x"), conversationState: "active" } });
    await connectAndRender();
    await write("x");
    expect(sentFrames()).toEqual(["typing.start", "typing.stop"]);
  });

  it("stops on blur", async () => {
    load(conversation(), []);
    await connectAndRender();
    fireEvent.change(box(), { target: { value: "x" } });
    fireEvent.blur(box());
    expect(sentFrames()).toEqual(["typing.start", "typing.stop"]);
  });

  it.each<[string, Partial<Conversation>]>([
    ["a sent request", { state: "request", requestedBy: adaID, canMessage: false }],
    ["a received request", { state: "request", requestedBy: bobID, canMessage: false }],
    ["a declined conversation", { state: "declined", canMessage: false }],
    ["a blocked conversation", { canMessage: false, blockedByMe: true }],
  ])("never sends typing in %s", async (_, overrides) => {
    load(conversation(overrides), [message(1, bobID)]);
    await connectAndRender();
    fireEvent.change(box(), { target: { value: "x" } });
    fireEvent.blur(box());
    expect(sentFrames()).toEqual([]);
  });
});

describe("after a reconnect", () => {
  async function reconnect() {
    await act(async () => FakeWebSocket.latest().serverClose(1013));
    await act(async () => {
      startSocket({ url: "ws://test", refresh: jest.fn().mockResolvedValue("ok"), onSessionLost: jest.fn() });
      FakeWebSocket.latest().accept();
    });
  }

  it("merges the newest messages with no duplicates, and moves Seen", async () => {
    load(conversation(), [message(1, adaID, "one"), message(2, bobID, "two")]);
    await connectAndRender();
    load(conversation(), [message(1, adaID, "one"), message(2, bobID, "two"), message(3, bobID, "missed")], {
      otherLastReadMessageId: message(1, adaID).id,
    });
    await reconnect();
    expect(count("one")).toBe(1);
    expect(count("two")).toBe(1);
    expect(count("missed")).toBe(1);
    expect(screen.getByText("Seen")).toBeTruthy();
    const text = screen.getByRole("log").textContent ?? "";
    expect(text.indexOf("two")).toBeLessThan(text.indexOf("missed"));
  });

  it("replaces the list when more was missed than one page holds, so no gap is hidden", async () => {
    load(conversation(), [message(1, bobID, "old")]);
    await connectAndRender();
    load(conversation(), [message(60, bobID, "new")], { nextBefore: "cursor" });
    await reconnect();
    expect(count("old")).toBe(0);
    expect(count("new")).toBe(1);
    expect(screen.getByRole("button", { name: /earlier/i })).toBeTruthy();
  });

  it("picks up a state that changed meanwhile", async () => {
    load(conversation(), [message(1, bobID)]);
    await connectAndRender();
    load(conversation({ canMessage: false }), [message(1, bobID)]);
    await reconnect();
    expect(box().disabled).toBe(true);
  });

  it("keeps what is shown when the refetch fails", async () => {
    load(conversation(), [message(1, bobID, "kept")]);
    await connectAndRender();
    mockMessages.mockResolvedValue({ ok: false, code: "network_error", message: "x" });
    await reconnect();
    expect(count("kept")).toBe(1);
  });
});

describe("presence", () => {
  it("shows the dot on the header avatar", async () => {
    load(conversation(), []);
    await connectAndRender();
    await receive("presence", { userId: bobID, online: true });
    expect(screen.getByRole("img", { name: "Online" })).toBeTruthy();
  });
});
