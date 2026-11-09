import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";

import { getCurrentUser } from "@/features/auth/api";

import {
  acceptMessageRequest,
  type Conversation,
  declineMessageRequest,
  getConversation,
  listMessages,
  markConversationRead,
  type Message,
  sendMessage,
} from "./api";
import { adaID, bobID, conversation, conversationID, message } from "./test-data";
import { Thread } from "./thread";
import { refreshUnreadMessages } from "./unread-count";
import { ConversationsProvider } from "./use-conversations";

jest.mock("./api");
jest.mock("./unread-count");
jest.mock("@/features/auth/api");
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
    data: { items: [...items].reverse(), nextBefore: extra.nextBefore ?? null, otherLastReadMessageId: extra.otherLastReadMessageId ?? null, readers: null },
  });
}

async function renderThread() {
  const view = render(
    <ConversationsProvider>
      <Thread conversationId={conversationID} />
    </ConversationsProvider>,
  );
  await screen.findByRole("log");
  return view;
}

const log = () => screen.getByRole("log", { name: "Messages" });
const box = () => screen.getByRole("textbox", { name: "Write a message" }) as HTMLTextAreaElement;
async function write(text: string) {
  fireEvent.change(box(), { target: { value: text } });
  await act(async () => fireEvent.keyDown(box(), { key: "Enter" }));
}

let focused: boolean;

beforeEach(() => {
  focused = true;
  jest.mocked(getCurrentUser).mockResolvedValue({ ok: false, code: "network_error", message: "No." });
  jest.spyOn(document, "hasFocus").mockImplementation(() => focused);
  mockRead.mockResolvedValue({ ok: true, data: undefined });
  jest.mocked(refreshUnreadMessages).mockResolvedValue();
  let n = 0;
  Object.defineProperty(globalThis.crypto, "randomUUID", { configurable: true, value: jest.fn(() => `client-${++n}`) });
});
afterEach(() => jest.resetAllMocks());

describe("Thread header", () => {
  it("names the person, links to their profile, and has a way back", async () => {
    load(conversation(), []);
    await renderThread();
    expect(screen.getByRole("link", { name: "Bob Babbage" }).getAttribute("href")).toBe(`/profile/${bobID}`);
    expect(screen.getByText("Analyst")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Back to conversations" }).getAttribute("href")).toBe("/messaging");
  });
});

describe("Messages", () => {
  it("is a polite log, newest at the bottom, with date separators", async () => {
    const today = new Date().toISOString();
    load(conversation(), [message(1, bobID, "older", "2026-01-05T09:00:00Z"), message(2, adaID, "newer", today)]);
    await renderThread();
    expect(log().getAttribute("aria-live")).toBe("polite");
    const text = log().textContent ?? "";
    expect(text.indexOf("5 Jan 2026")).toBeLessThan(text.indexOf("older"));
    expect(text.indexOf("older")).toBeLessThan(text.indexOf("Today"));
    expect(text.indexOf("Today")).toBeLessThan(text.indexOf("newer"));
  });

  it("shows a run by one sender with one time", async () => {
    load(conversation(), [message(1, bobID, "one"), message(2, bobID, "two"), message(3, bobID, "three")]);
    const { container } = await renderThread();
    expect(within(log()).getAllByText(/[AP]M$/)).toHaveLength(1);
    expect(container.querySelectorAll('[role="log"] span[aria-hidden="true"]')).toHaveLength(1); // one avatar
  });

  it("keeps line breaks and makes http(s) links safe", async () => {
    load(conversation(), [message(1, bobID, "line one\nsee https://example.org/x.")]);
    await renderThread();
    const link = screen.getByRole("link", { name: "https://example.org/x" });
    expect(link.getAttribute("rel")).toBe("noopener noreferrer nofollow");
    expect(link.getAttribute("target")).toBe("_blank");
    expect(link.closest("p")?.textContent).toBe("line one\nsee https://example.org/x.");
    expect(link.closest("p")?.className).toContain("whitespace-pre-wrap");
  });

  it("never renders HTML from a message", async () => {
    const script = "<script>window.hacked = true</script>";
    const image = "<img src=x onerror=window.hacked=true>";
    load(conversation(), [message(1, bobID, script), message(2, adaID, image)]);
    await renderThread();
    expect(log().querySelector("script")).toBeNull();
    expect(log().querySelector("img")).toBeNull();
    expect(screen.getByText(script)).toBeTruthy();
    expect(screen.getByText(image)).toBeTruthy();
  });
});

describe("Load earlier", () => {
  function sizes(el: HTMLElement, heights: { scrollHeight: number }) {
    Object.defineProperty(el, "scrollHeight", { configurable: true, get: () => heights.scrollHeight });
    Object.defineProperty(el, "clientHeight", { configurable: true, get: () => 400 });
  }

  it("loads on scrolling up and keeps the reader's place", async () => {
    load(conversation(), [message(10, bobID)], { nextBefore: "cursor" });
    await renderThread();
    const heights = { scrollHeight: 1000 };
    sizes(log(), heights);
    mockMessages.mockResolvedValueOnce({
      ok: true,
      data: { items: [message(9, bobID, "earlier")], nextBefore: null, otherLastReadMessageId: null, readers: null },
    });

    log().scrollTop = 20;
    heights.scrollHeight = 1000;
    await act(async () => {
      fireEvent.scroll(log());
      heights.scrollHeight = 1600;
    });

    expect(mockMessages).toHaveBeenLastCalledWith(conversationID, "cursor");
    expect(await screen.findByText("earlier")).toBeTruthy();
    expect(log().scrollTop).toBe(620); // 20 + the 600 added above
    expect(screen.queryByRole("button", { name: "Load earlier messages" })).toBeNull();
  });

  it("asks once while a page is loading", async () => {
    load(conversation(), [message(10, bobID)], { nextBefore: "cursor" });
    await renderThread();
    sizes(log(), { scrollHeight: 1000 });
    mockMessages.mockClear();
    mockMessages.mockReturnValue(new Promise(() => {}));
    log().scrollTop = 0;
    await act(async () => {
      fireEvent.scroll(log());
      fireEvent.scroll(log());
    });
    expect(mockMessages).toHaveBeenCalledTimes(1);
    expect((screen.getByRole("button", { name: "Loading…" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("says so when earlier messages fail, and the button tries again", async () => {
    load(conversation(), [message(10, bobID)], { nextBefore: "cursor" });
    await renderThread();
    sizes(log(), { scrollHeight: 1000 });
    mockMessages.mockResolvedValueOnce({ ok: false, code: "network_error", message: "x" });
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Load earlier messages" })));
    expect(screen.getByText("Couldn't load earlier messages.")).toBeTruthy();
    mockMessages.mockResolvedValueOnce({
      ok: true,
      data: { items: [message(9, bobID, "earlier")], nextBefore: null, otherLastReadMessageId: null, readers: null },
    });
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Load earlier messages" })));
    expect(await screen.findByText("earlier")).toBeTruthy();
  });
});

describe("Seen", () => {
  it("shows under my last message they have read, and nowhere else", async () => {
    const messages = [message(1, adaID, "first"), message(2, adaID, "second"), message(3, bobID, "reply"), message(4, adaID, "last")];
    load(conversation(), messages, { otherLastReadMessageId: messages[2].id });
    await renderThread();
    const seen = screen.getAllByText("Seen");
    expect(seen).toHaveLength(1);
    const row = seen[0].closest("div");
    expect(row?.textContent).toContain("second");
  });

  it("does not show without their marker", async () => {
    load(conversation(), [message(1, adaID)]);
    await renderThread();
    expect(screen.queryByText("Seen")).toBeNull();
  });

  it.each<[string, Partial<Conversation>]>([
    ["a pending request", { state: "request", requestedBy: adaID, canMessage: false }],
    ["a declined request", { state: "declined", requestedBy: adaID, canMessage: false }],
  ])("does not show on %s, even with a marker", async (_, change) => {
    const messages = [message(1, adaID)];
    load(conversation(change), messages, { otherLastReadMessageId: messages[0].id });
    await renderThread();
    expect(screen.queryByText("Seen")).toBeNull();
  });
});

describe("Mark read", () => {
  it("sends once for the newest message, not on every render", async () => {
    const messages = [message(1, bobID), message(2, bobID)];
    load(conversation({ unreadCount: 2 }), messages);
    const { rerender } = await renderThread();
    await waitFor(() => expect(mockRead).toHaveBeenCalledTimes(1));
    expect(mockRead).toHaveBeenCalledWith(conversationID, messages[1].id);
    rerender(
      <ConversationsProvider>
        <Thread conversationId={conversationID} />
      </ConversationsProvider>,
    );
    await act(async () => window.dispatchEvent(new Event("focus")));
    expect(mockRead).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(refreshUnreadMessages).toHaveBeenCalledWith(adaID));
  });

  it("waits until the window has focus", async () => {
    focused = false;
    const messages = [message(1, bobID)];
    load(conversation(), messages);
    await renderThread();
    expect(mockRead).not.toHaveBeenCalled();
    focused = true;
    await act(async () => window.dispatchEvent(new Event("focus")));
    expect(mockRead).toHaveBeenCalledWith(conversationID, messages[0].id);
  });

  it("waits until the tab is visible", async () => {
    const visibility = jest.spyOn(document, "visibilityState", "get").mockReturnValue("hidden");
    load(conversation(), [message(1, bobID)]);
    await renderThread();
    expect(mockRead).not.toHaveBeenCalled();
    visibility.mockReturnValue("visible");
    await act(async () => document.dispatchEvent(new Event("visibilitychange")));
    expect(mockRead).toHaveBeenCalledTimes(1);
    visibility.mockRestore();
  });

  it("skips it when the newest message is mine", async () => {
    load(conversation(), [message(1, bobID), message(2, adaID)]);
    await renderThread();
    await act(async () => {});
    expect(mockRead).not.toHaveBeenCalled();
  });

  it("does not read a received request until it is accepted", async () => {
    const messages = [message(1, bobID)];
    const request = conversation({ state: "request", requestedBy: bobID, canMessage: false });
    load(request, messages);
    await renderThread();
    await act(async () => {});
    expect(mockRead).not.toHaveBeenCalled();
    jest.mocked(acceptMessageRequest).mockResolvedValue({ ok: true, data: conversation({ requestedBy: bobID }) });
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Accept" })));
    await waitFor(() => expect(mockRead).toHaveBeenCalledWith(conversationID, messages[0].id));
  });

  it("never reads a request after a decline", async () => {
    load(conversation({ state: "request", requestedBy: bobID, canMessage: false }), [message(1, bobID)]);
    await renderThread();
    jest.mocked(declineMessageRequest).mockResolvedValue({
      ok: true,
      data: conversation({ state: "declined", requestedBy: bobID, canMessage: false }),
    });
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Decline" })));
    await act(async () => window.dispatchEvent(new Event("focus")));
    expect(mockRead).not.toHaveBeenCalled();
  });

  it("does not read a declined conversation opened later", async () => {
    load(conversation({ state: "declined", requestedBy: bobID, canMessage: false }), [message(1, bobID)]);
    await renderThread();
    await act(async () => {});
    expect(mockRead).not.toHaveBeenCalled();
  });

  it("tries again after a failure, when the window comes back", async () => {
    mockRead.mockResolvedValueOnce({ ok: false, code: "network_error", message: "x" });
    load(conversation(), [message(1, bobID)]);
    await renderThread();
    await waitFor(() => expect(mockRead).toHaveBeenCalledTimes(1));
    await act(async () => {});
    focused = false;
    await act(async () => window.dispatchEvent(new Event("blur")));
    focused = true;
    await act(async () => window.dispatchEvent(new Event("focus")));
    expect(mockRead).toHaveBeenCalledTimes(2);
  });
});

describe("Request states", () => {
  it("waits on a request I sent", async () => {
    load(conversation({ state: "request", requestedBy: adaID, canMessage: false }), [message(1, adaID)]);
    await renderThread();
    expect(screen.getByText("Waiting for Bob Babbage to accept your message request.")).toBeTruthy();
    expect(box().disabled).toBe(true);
  });

  it("offers Accept and Decline on a request to me, and opens the composer on Accept", async () => {
    load(conversation({ state: "request", requestedBy: bobID, canMessage: false }), [message(1, bobID)]);
    await renderThread();
    expect(box().disabled).toBe(true);
    jest.mocked(acceptMessageRequest).mockResolvedValue({ ok: true, data: conversation({ requestedBy: bobID }) });
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Accept" })));
    expect(screen.queryByRole("button", { name: "Accept" })).toBeNull();
    expect(box().disabled).toBe(false);
    expect(refreshUnreadMessages).toHaveBeenCalledWith(adaID);
  });

  it("closes the conversation on Decline", async () => {
    load(conversation({ state: "request", requestedBy: bobID, canMessage: false }), [message(1, bobID)]);
    await renderThread();
    jest.mocked(declineMessageRequest).mockResolvedValue({
      ok: true,
      data: conversation({ state: "declined", requestedBy: bobID, canMessage: false }),
    });
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Decline" })));
    expect(screen.getByText("This conversation is closed.")).toBeTruthy();
    expect(box().disabled).toBe(true);
    expect(refreshUnreadMessages).toHaveBeenCalledWith(adaID);
  });

  it("keeps the request when an answer fails", async () => {
    load(conversation({ state: "request", requestedBy: bobID, canMessage: false }), [message(1, bobID)]);
    await renderThread();
    jest.mocked(acceptMessageRequest).mockResolvedValue({ ok: false, code: "network_error", message: "x" });
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Accept" })));
    expect(screen.getByRole("alert").textContent).toBe("Couldn't accept the request. Try again.");
    expect(screen.getByRole("button", { name: "Decline" })).toBeTruthy();
  });

  it("says when a request can no longer be answered", async () => {
    load(conversation({ state: "request", requestedBy: bobID, canMessage: false }), [message(1, bobID)]);
    await renderThread();
    jest.mocked(acceptMessageRequest).mockResolvedValue({ ok: false, code: "not_found", message: "x" });
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Accept" })));
    expect(screen.getByRole("alert").textContent).toBe("This request can no longer be answered.");
  });

  it("is closed when declined", async () => {
    load(conversation({ state: "declined", requestedBy: adaID, canMessage: false }), [message(1, adaID)]);
    await renderThread();
    expect(screen.getByText("This conversation is closed.")).toBeTruthy();
    expect(box().disabled).toBe(true);
  });

  it("is read-only when I can't message", async () => {
    load(conversation({ canMessage: false }), [message(1, bobID)]);
    await renderThread();
    expect(screen.getByText("You can't reply to this conversation.")).toBeTruthy();
    expect(box().disabled).toBe(true);
  });
});

describe("Sending", () => {
  it("shows the message at once as Sending…, then sent", async () => {
    load(conversation(), []);
    await renderThread();
    let finish: (value: Awaited<ReturnType<typeof sendMessage>>) => void = () => {};
    mockSend.mockReturnValue(new Promise((resolve) => (finish = resolve)));
    await write("Hello");
    expect(screen.getByText("Hello")).toBeTruthy();
    expect(screen.getByText("Sending…")).toBeTruthy();
    expect(mockSend).toHaveBeenCalledWith(conversationID, "Hello", "client-1");
    await act(async () => finish({ ok: true, data: { message: message(1, adaID, "Hello"), conversationState: "active" } }));
    expect(screen.queryByText("Sending…")).toBeNull();
    expect(screen.getAllByText("Hello")).toHaveLength(1);
    expect(refreshUnreadMessages).toHaveBeenCalledWith(adaID);
  });

  it("offers Retry after a failure, with the same clientId", async () => {
    load(conversation(), []);
    await renderThread();
    mockSend.mockResolvedValueOnce({ ok: false, code: "network_error", message: "Could not reach the server." });
    await write("Hello");
    expect(screen.getByText(/Not sent\./)).toBeTruthy();
    expect(screen.getByRole("alert").textContent).toBe("Could not reach the server.");

    mockSend.mockResolvedValueOnce({ ok: true, data: { message: message(1, adaID, "Hello"), conversationState: "active" } });
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Retry" })));
    expect(mockSend.mock.calls.map((c) => c[2])).toEqual(["client-1", "client-1"]);
    expect(screen.queryByText(/Not sent\./)).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getAllByText("Hello")).toHaveLength(1);
  });

  it("gives each new message its own clientId", async () => {
    load(conversation(), []);
    await renderThread();
    mockSend.mockReturnValue(new Promise(() => {}));
    await write("one");
    await write("two");
    expect(mockSend.mock.calls.map((c) => c[2])).toEqual(["client-1", "client-2"]);
  });

  it("maps 429 with Retry-After", async () => {
    load(conversation(), []);
    await renderThread();
    mockSend.mockResolvedValue({ ok: false, code: "rate_limited", message: "x", retryAfter: 120 });
    await write("Hello");
    expect(screen.getByRole("alert").textContent).toBe("You're sending messages too quickly. Try again in 2 minutes.");
    expect(mockGet).toHaveBeenCalledTimes(1); // a limit does not change the conversation
  });

  it.each([
    ["request_pending", "You can send more messages once your request is accepted."],
    ["conversation_closed", "This conversation is closed."],
    ["cannot_message", "You can't message this person."],
  ])("maps %s and reads the conversation again", async (code, text) => {
    load(conversation(), []);
    await renderThread();
    mockSend.mockResolvedValue({ ok: false, code, message: "x" });
    mockGet.mockResolvedValue({ ok: true, data: conversation({ state: "declined", requestedBy: bobID, canMessage: false }) });
    await write("Hello");
    expect(screen.getByRole("alert").textContent).toBe(text);
    await waitFor(() => expect(mockGet).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(box().disabled).toBe(true));
  });
});

describe("Loading", () => {
  it("says when a conversation is not available", async () => {
    mockGet.mockResolvedValue({ ok: false, code: "not_found", message: "x" });
    mockMessages.mockResolvedValue({ ok: false, code: "not_found", message: "x" });
    render(
      <ConversationsProvider>
        <Thread conversationId={conversationID} />
      </ConversationsProvider>,
    );
    expect(await screen.findByText(/isn't available/)).toBeTruthy();
    expect(mockRead).not.toHaveBeenCalled();
  });

  it("offers to try again after a failure", async () => {
    mockGet.mockResolvedValueOnce({ ok: false, code: "network_error", message: "x" });
    mockMessages.mockResolvedValueOnce({ ok: false, code: "network_error", message: "x" });
    render(
      <ConversationsProvider>
        <Thread conversationId={conversationID} />
      </ConversationsProvider>,
    );
    load(conversation(), [message(1, bobID, "back")]);
    const again = await screen.findByRole("button", { name: "Try again" });
    await act(async () => fireEvent.click(again));
    expect(await screen.findByText("back")).toBeTruthy();
  });
});
