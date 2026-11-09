import { act, render, screen, within } from "@testing-library/react";

import { getPresence } from "@/features/realtime/api";
import { FakeWebSocket } from "@/features/realtime/fake-websocket";
import { startSocket, stopSocket } from "@/features/realtime/socket";

import { type Conversation, getConversation, listConversations } from "./api";
import { ConversationList } from "./conversation-list";
import { adaID, bobID, conversation, conversationID, message } from "./test-data";
import { ConversationsProvider } from "./use-conversations";

jest.mock("./api");
jest.mock("./unread-count", () => ({ useUnreadMessages: () => ({ total: 0, requests: 0 }) }));
jest.mock("@/features/realtime/api", () => ({ getPresence: jest.fn(), maxPresenceIds: 50 }));
jest.mock("next/navigation", () => ({ usePathname: () => "/messaging" }));
jest.mock("@/features/auth/session", () => ({
  useSession: () => ({ status: "signedIn", user: { id: "0b8f4c1e-0000-4000-8000-000000000001" } }),
}));

const mockList = jest.mocked(listConversations);
const mockGet = jest.mocked(getConversation);

const otherID = "4c2f7d3e-0000-4000-8000-0000000000c2";
const carol: Conversation["otherMember"] = { userId: "u-carol", fullName: "Carol Clark", photoUrl: null, headline: null, available: true };
const second = conversation({ id: otherID, otherMember: carol, lastMessageAt: "2026-11-04T09:00:00Z" });
const first = conversation();

function lists(inbox: Conversation[], requests: Conversation[] = [], nextCursor: string | null = null) {
  mockList.mockImplementation(async (box) => ({ ok: true, data: { items: box === "inbox" ? inbox : requests, nextCursor: box === "inbox" ? nextCursor : null } }));
}

async function connectAndRender() {
  await act(async () => FakeWebSocket.latest().accept());
  render(
    <ConversationsProvider>
      <ConversationList />
    </ConversationsProvider>,
  );
  await screen.findByRole("list");
}

const receive = (type: string, data: unknown) => act(async () => FakeWebSocket.latest().receive(type, data));
const created = (conversationId: string, n: number, senderId: string, body: string, conversationState = "active") =>
  receive("message.created", { message: { ...message(n, senderId, body), conversationId }, conversationState });
const names = () =>
  within(screen.getByRole("list"))
    .getAllByRole("listitem")
    .map((row) => row.querySelector("strong")?.textContent);

beforeEach(() => {
  jest.mocked(getPresence).mockResolvedValue({ ok: true, data: [] });
  FakeWebSocket.install();
  startSocket({ url: "ws://test", refresh: jest.fn(), onSessionLost: jest.fn() });
});
afterEach(() => {
  stopSocket();
  jest.resetAllMocks();
});

describe("message.created", () => {
  it("moves the row to the top with the new text and an unread count", async () => {
    lists([first, second]);
    await connectAndRender();
    await created(otherID, 2, "u-carol", "new from Carol");
    expect(names()).toEqual(["Carol Clark", "Bob Babbage"]);
    expect(screen.getByText("new from Carol")).toBeTruthy();
    expect(screen.getByText("1 unread")).toBeTruthy();
  });

  it("counts a repeated event once", async () => {
    lists([first]);
    await connectAndRender();
    await created(conversationID, 2, bobID, "hello");
    await created(conversationID, 2, bobID, "hello");
    expect(screen.getByText("1 unread")).toBeTruthy();
  });

  it("does not count my own message from another tab", async () => {
    lists([first]);
    await connectAndRender();
    await created(conversationID, 2, adaID, "from my phone");
    expect(screen.getByText("You: from my phone")).toBeTruthy();
    expect(screen.queryByText(/unread$/)).toBeNull();
  });

  it("fetches a conversation it does not have yet", async () => {
    lists([first]);
    mockGet.mockResolvedValue({ ok: true, data: second });
    await connectAndRender();
    await created(otherID, 2, "u-carol", "first contact");
    expect(mockGet).toHaveBeenCalledWith(otherID);
    expect(names()).toEqual(["Carol Clark", "Bob Babbage"]);
  });

  // getConversation answers a group as not found (api.ts), so a group's message adds no row.
  it("adds no row for a group's message", async () => {
    lists([first]);
    mockGet.mockResolvedValue({ ok: false, code: "not_found", message: "This conversation does not exist." });
    await connectAndRender();
    await receive("message.created", {
      message: { ...message(2, "u-carol", "to the group"), conversationId: "group-1", kind: "text" },
      conversationState: "active",
    });
    expect(mockGet).toHaveBeenCalledWith("group-1");
    expect(names()).toEqual(["Bob Babbage"]);
    expect(screen.queryByText("to the group")).toBeNull();
  });

  it("fetches nothing for a request while the Requests box has not been opened", async () => {
    lists([first]);
    await connectAndRender();
    await created(otherID, 2, "u-carol", "first contact", "request");
    expect(mockGet).not.toHaveBeenCalled();
  });
});

describe("conversation.updated", () => {
  it("moves an accepted request out of Requests", async () => {
    lists([first], [second]);
    await connectAndRender();
    act(() => screen.getByRole("tab", { name: "Requests" }).click());
    await screen.findByText("Carol Clark");
    await receive("conversation.updated", { conversationId: otherID, state: "active", canMessage: true });
    expect(screen.queryByText("Carol Clark")).toBeNull();
    act(() => screen.getByRole("tab", { name: "Inbox" }).click());
    expect(names()).toEqual(["Carol Clark", "Bob Babbage"]);
  });

  it("removes a declined conversation", async () => {
    lists([first, second]);
    await connectAndRender();
    await receive("conversation.updated", { conversationId: otherID, state: "declined", canMessage: false });
    expect(names()).toEqual(["Bob Babbage"]);
  });

  it("leaves a row in its place when only canMessage changes", async () => {
    lists([first, second]);
    await connectAndRender();
    await receive("conversation.updated", { conversationId: otherID, state: "active", canMessage: false });
    expect(names()).toEqual(["Bob Babbage", "Carol Clark"]);
  });
});

describe("typing and presence", () => {
  it("shows typing in the row, in place of the preview", async () => {
    lists([first]);
    await connectAndRender();
    await receive("typing", { conversationId: conversationID, userId: bobID, isTyping: true });
    expect(screen.getByText("Bob Babbage is typing…")).toBeTruthy();
    expect(screen.queryByText("See you")).toBeNull();
    await receive("typing", { conversationId: conversationID, userId: bobID, isTyping: false });
    expect(screen.getByText("See you")).toBeTruthy();
  });

  it("shows the dot on a row's avatar", async () => {
    lists([first]);
    await connectAndRender();
    await receive("presence", { userId: bobID, online: true });
    expect(within(screen.getByRole("list")).getByRole("img", { name: "Online" })).toBeTruthy();
  });
});

describe("after a reconnect", () => {
  async function reconnect() {
    await act(async () => {
      stopSocket();
      startSocket({ url: "ws://test", refresh: jest.fn(), onSessionLost: jest.fn() });
      FakeWebSocket.latest().accept();
    });
  }

  it("reads the list again without showing Loading", async () => {
    lists([first]);
    await connectAndRender();
    lists([second, first]);
    await reconnect();
    expect(screen.queryByRole("status")).toBeNull();
    expect(names()).toEqual(["Carol Clark", "Bob Babbage"]);
  });

  it("does not load a box nobody opened", async () => {
    lists([first]);
    await connectAndRender();
    mockList.mockClear();
    await reconnect();
    expect(mockList).toHaveBeenCalledTimes(1);
    expect(mockList).toHaveBeenCalledWith("inbox");
  });

  it("keeps rows loaded past the first page, and drops one gone from it", async () => {
    const older = conversation({ id: "older", lastMessageAt: "2026-10-01T09:00:00Z", otherMember: { ...carol, fullName: "Old Page" } });
    const gone = conversation({ id: "gone", lastMessageAt: "2026-11-05T08:00:00Z", otherMember: { ...carol, fullName: "Gone Person" } });
    lists([first, gone, older]);
    await connectAndRender();
    lists([first, second], [], "next");
    await reconnect();
    expect(names()).toEqual(["Bob Babbage", "Carol Clark", "Old Page"]);
  });
});
