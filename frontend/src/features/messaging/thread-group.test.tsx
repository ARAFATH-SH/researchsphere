import "@/test/dialog-polyfill";

import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { getPresence } from "@/features/realtime/api";
import { FakeWebSocket } from "@/features/realtime/fake-websocket";
import { startSocket, stopSocket } from "@/features/realtime/socket";

import {
  type Conversation,
  getConversation,
  leaveGroup,
  listConversations,
  listMessages,
  markConversationRead,
  type Message,
  type MessagePage,
} from "./api";
import { highlightFor } from "./message-list";
import { adaID, group, groupID, message, nafisaID, page, rahimID, systemMessage } from "./test-data";
import { Thread } from "./thread";
import { ConversationList } from "./conversation-list";
import { refreshUnreadMessages, useUnreadMessages } from "./unread-count";
import { ConversationsProvider } from "./use-conversations";

let search = "";
jest.mock("next/navigation", () => ({
  useSearchParams: () => new URLSearchParams(search),
  usePathname: () => "/messaging",
  useRouter: () => ({ push: jest.fn(), replace: jest.fn() }),
}));
jest.mock("./api");
jest.mock("./unread-count");
jest.mock("@/features/realtime/api", () => ({ getPresence: jest.fn(), maxPresenceIds: 50 }));
jest.mock("@/features/auth/session", () => ({
  useSession: () => ({ status: "signedIn", user: { id: "0b8f4c1e-0000-4000-8000-000000000001" } }),
}));

const mockGet = jest.mocked(getConversation);
const mockMessages = jest.mocked(listMessages);

// inGroup puts a message in the test group.
const inGroup = (m: Message): Message => ({ ...m, conversationId: groupID });
const people = {
  [adaID]: { fullName: "Ada Lovelace", photoUrl: null, available: true },
  [nafisaID]: { fullName: "Nafisa Hasan", photoUrl: null, available: true },
  [rahimID]: { fullName: "Rahim Uddin", photoUrl: null, available: true },
  // Sadia left the group: she is named by the page, not by the members.
  sadia: { fullName: "Sadia Rahman", photoUrl: null, available: false },
};

function load(c: Conversation, items: Message[], extra: Partial<MessagePage> = {}) {
  mockGet.mockResolvedValue({ ok: true, data: c });
  mockMessages.mockResolvedValue({ ok: true, data: page([...items].reverse(), { people, ...extra }) });
}

async function renderThread(withList = false) {
  await act(async () => FakeWebSocket.latest().accept());
  render(
    <ConversationsProvider>
      {withList && <ConversationList />}
      <Thread conversationId={groupID} />
    </ConversationsProvider>,
  );
  await screen.findByRole("log");
}

const receive = (type: string, data: unknown) => act(async () => FakeWebSocket.latest().receive(type, data));
const log = () => screen.getByRole("log", { name: "Messages" });
const header = () => screen.getByRole("region", { name: /^Group/ });

beforeEach(() => {
  search = "";
  jest.spyOn(document, "hasFocus").mockImplementation(() => true);
  jest.mocked(markConversationRead).mockResolvedValue({ ok: true, data: undefined });
  jest.mocked(refreshUnreadMessages).mockResolvedValue();
  jest.mocked(useUnreadMessages).mockReturnValue({ total: 0, requests: 0 });
  jest.mocked(getPresence).mockResolvedValue({ ok: true, data: [] });
  jest.mocked(listConversations).mockResolvedValue({ ok: true, data: { items: [group()], nextCursor: null } });
  FakeWebSocket.install();
  startSocket({ url: "ws://test", refresh: jest.fn().mockResolvedValue("ok"), onSessionLost: jest.fn() });
});
afterEach(() => {
  stopSocket();
  jest.useRealTimers();
  jest.resetAllMocks();
});

describe("a group thread (FR-16.6)", () => {
  it("names the group, its size and members in the header", async () => {
    load(group(), [inGroup(message(1, nafisaID, "hi"))]);
    await renderThread();
    expect(within(header()).getByRole("button", { name: "CVS paper team" })).toBeTruthy();
    expect(within(header()).getByText("3 members")).toBeTruthy();
    expect(within(header()).getByRole("button", { name: "Group info" })).toBeTruthy();
    expect(within(header()).queryByRole("button", { name: /Block/ })).toBeNull();
  });

  it("names the sender above the first message of each run, a former member included", async () => {
    load(group(), [
      inGroup(message(1, nafisaID, "first")),
      inGroup(message(2, nafisaID, "second")),
      inGroup(message(3, rahimID, "third")),
      inGroup(message(4, "sadia", "from Sadia")),
      inGroup(message(5, adaID, "mine")),
    ]);
    await renderThread();
    expect(within(log()).getAllByText("Nafisa Hasan")).toHaveLength(1);
    expect(within(log()).getByText("Rahim Uddin")).toBeTruthy();
    expect(within(log()).getByText("Sadia Rahman")).toBeTruthy();
    expect(within(log()).queryByText("Ada Lovelace")).toBeNull();
  });

  it("shows system messages as centred lines, and starts a new run after one", async () => {
    load(group(), [
      systemMessage(1, adaID, { type: "group_created", userIds: [nafisaID], oldName: null, newName: "Lab" }),
      inGroup(message(2, nafisaID, "hello")),
      systemMessage(3, nafisaID, { type: "member_added", userIds: [rahimID], oldName: null, newName: null }),
      inGroup(message(4, nafisaID, "welcome")),
      systemMessage(5, "sadia", { type: "member_left", userIds: ["sadia"], oldName: null, newName: null }),
      systemMessage(6, adaID, { type: "member_removed", userIds: ["gone"], oldName: null, newName: null }),
      systemMessage(7, adaID, { type: "group_renamed", userIds: [], oldName: "Lab", newName: "CVS paper team" }),
      systemMessage(8, null, { type: "owner_changed", userIds: [nafisaID], oldName: null, newName: null }),
    ]);
    await renderThread();
    for (const line of [
      "You created the group",
      "Nafisa Hasan added Rahim Uddin",
      "Sadia Rahman left",
      "You removed Deleted account",
      "You renamed the group from Lab to CVS paper team",
      "Nafisa Hasan is now the owner",
    ]) {
      expect(within(log()).getByText(line)).toBeTruthy();
    }
    expect(within(log()).getAllByText("Nafisa Hasan")).toHaveLength(2);
  });

  it("shows Seen by N under my latest message others read, with the names on a tap", async () => {
    load(group(), [inGroup(message(1, adaID, "one")), inGroup(message(2, adaID, "two"))], {
      readers: [
        { userId: nafisaID, lastReadMessageId: message(2, adaID).id },
        { userId: rahimID, lastReadMessageId: message(2, adaID).id },
      ],
    });
    await renderThread();
    const seen = within(log()).getByRole("button", { name: "Seen by 2" });
    expect(seen.getAttribute("title")).toBe("Nafisa Hasan, Rahim Uddin");
    fireEvent.click(seen);
    expect(within(screen.getByRole("list", { name: "Seen by" })).getAllByRole("listitem").map((li) => li.textContent)).toEqual([
      "Nafisa Hasan",
      "Rahim Uddin",
    ]);
    expect(within(log()).queryByText("Seen")).toBeNull();
  });

  it("reads the markers again when one moves", async () => {
    load(group(), [inGroup(message(1, adaID, "one"))]);
    await renderThread();
    mockMessages.mockResolvedValue({ ok: true, data: page([], { readers: [{ userId: nafisaID, lastReadMessageId: message(1, adaID).id }] }) });
    await receive("conversation.read", { conversationId: groupID, messageId: message(1, adaID).id });
    expect(mockMessages).toHaveBeenLastCalledWith(groupID, {}, 1);
    expect(within(log()).getByRole("button", { name: "Seen by 1" })).toBeTruthy();
  });

  it.each([
    [[nafisaID], "Nafisa is typing…"],
    [[nafisaID, rahimID], "Nafisa and Rahim are typing…"],
    [[nafisaID, rahimID, "sadia"], "Several people are typing…"],
  ])("says who is typing: %o", async (typers, text) => {
    load(group(), [inGroup(message(1, nafisaID))]);
    await renderThread();
    for (const userId of typers) await receive("typing", { conversationId: groupID, userId, isTyping: true });
    expect(within(header()).getByText(text)).toBeTruthy();
    for (const userId of typers) await receive("typing", { conversationId: groupID, userId, isTyping: false });
  });

  it("reads the group again on conversation.members, for the header and the panel", async () => {
    load(group(), [inGroup(message(1, nafisaID))]);
    await renderThread();
    const renamed = group();
    renamed.group.name = "Lab 2";
    renamed.group.memberCount = 4;
    mockGet.mockResolvedValue({ ok: true, data: renamed });
    await receive("conversation.members", { conversationId: groupID, change: "renamed", actorId: nafisaID, userIds: [], name: "Lab 2" });
    expect(within(header()).getByRole("button", { name: "Lab 2" })).toBeTruthy();
    expect(within(header()).getByText("4 members")).toBeTruthy();
  });

  it("closes the thread and drops the row when access is lost", async () => {
    load(group(), [inGroup(message(1, nafisaID))]);
    await renderThread(true);
    expect(screen.getByRole("link", { name: /CVS paper team/ })).toBeTruthy();
    await receive("conversation.updated", { conversationId: groupID, state: "active", canMessage: false, accessLost: true });
    expect(screen.getByText(/You're no longer in this group\./)).toBeTruthy();
    expect(screen.queryByRole("log")).toBeNull();
    expect(screen.queryByRole("link", { name: /CVS paper team/ })).toBeNull();
  });

  it("opens the Group info panel and leaves from it", async () => {
    load(group(), [inGroup(message(1, nafisaID))]);
    jest.mocked(leaveGroup).mockResolvedValue({ ok: true, data: undefined });
    await renderThread(true);
    fireEvent.click(within(header()).getByRole("button", { name: "Group info" }));
    const panel = screen.getByRole("dialog", { name: "Group info" });
    fireEvent.click(within(panel).getByRole("button", { name: "Leave group" }));
    await act(async () => fireEvent.click(within(panel).getByRole("button", { name: "Leave" })));
    expect(leaveGroup).toHaveBeenCalledWith(groupID);
    expect(screen.getByText(/You're no longer in this group\./)).toBeTruthy();
    expect(screen.queryByRole("link", { name: /CVS paper team/ })).toBeNull();
  });

  it("reports the whole group from the panel, with no offer to block", async () => {
    load(group(), [inGroup(message(1, nafisaID))]);
    await renderThread();
    fireEvent.click(within(header()).getByRole("button", { name: "Group info" }));
    fireEvent.click(within(screen.getByRole("dialog", { name: "Group info" })).getByRole("button", { name: "Report group" }));
    expect(screen.getByRole("dialog", { name: "Report group" })).toBeTruthy();
    expect(screen.queryByRole("dialog", { name: "Group info" })).toBeNull();
  });
});

describe("opening at a search result (FR-16.8)", () => {
  const anchor = inGroup(message(5, nafisaID, "the result"));

  it("loads the page around the message, scrolls to it and marks it for a moment", async () => {
    jest.useFakeTimers();
    search = `around=${anchor.id}`;
    const scrolled = jest.fn();
    Element.prototype.scrollIntoView = scrolled;
    load(group(), [inGroup(message(4, rahimID, "before")), anchor, inGroup(message(6, rahimID, "after"))], { nextAfter: message(6, rahimID).id, nextBefore: message(4, rahimID).id });
    await renderThread();
    expect(mockMessages).toHaveBeenCalledWith(groupID, { around: anchor.id });
    const marked = log().querySelector(`[data-message-id="${anchor.id}"]`) as HTMLElement;
    expect(marked.dataset.highlighted).toBe("true");
    expect(scrolled).toHaveBeenCalledWith({ block: "center" });
    await act(async () => jest.advanceTimersByTime(highlightFor));
    expect(marked.dataset.highlighted).toBeUndefined();
  });

  it("loads newer messages from there, and holds live ones until the end is reached", async () => {
    search = `around=${anchor.id}`;
    load(group(), [anchor], { nextAfter: anchor.id });
    await renderThread();
    await receive("message.created", { message: inGroup(message(9, rahimID, "live")), conversationState: "active" });
    expect(within(log()).queryByText("live")).toBeNull();
    mockMessages.mockResolvedValue({ ok: true, data: page([inGroup(message(9, rahimID, "live")), inGroup(message(6, rahimID, "newer"))]) });
    await act(async () => fireEvent.click(within(log()).getByRole("button", { name: "Load newer messages" })));
    expect(mockMessages).toHaveBeenLastCalledWith(groupID, { after: anchor.id });
    expect(within(log()).getByText("newer")).toBeTruthy();
    expect(within(log()).getByText("live")).toBeTruthy();
    expect(within(log()).queryByRole("button", { name: "Load newer messages" })).toBeNull();
  });

  it("jumps to the newest messages", async () => {
    search = `around=${anchor.id}`;
    load(group(), [anchor], { nextAfter: anchor.id });
    await renderThread();
    mockMessages.mockResolvedValue({ ok: true, data: page([inGroup(message(20, rahimID, "latest"))]) });
    await act(async () => fireEvent.click(within(log()).getByRole("button", { name: "Jump to latest" })));
    expect(mockMessages).toHaveBeenLastCalledWith(groupID);
    expect(within(log()).getByText("latest")).toBeTruthy();
    expect(within(log()).queryByText("the result")).toBeNull();
  });

  it("opens at the newest messages when the result is gone, and says so", async () => {
    search = `around=${anchor.id}`;
    mockGet.mockResolvedValue({ ok: true, data: group() });
    mockMessages
      .mockResolvedValueOnce({ ok: false, code: "not_found", message: "This message does not exist." })
      .mockResolvedValueOnce({ ok: true, data: page([inGroup(message(20, rahimID, "latest"))], { people }) });
    await renderThread();
    expect(screen.getByText("That message isn't available any more. Showing the newest messages.")).toBeTruthy();
    expect(within(log()).getByText("latest")).toBeTruthy();
  });
});

describe("search in this conversation", () => {
  it("opens a scoped search from the header, with From listing the members", async () => {
    load(group(), [inGroup(message(1, nafisaID))]);
    await renderThread();
    fireEvent.click(within(header()).getByRole("button", { name: "Search in this conversation" }));
    const box = screen.getByRole("searchbox", { name: "Search in this conversation" });
    expect(document.activeElement).toBe(box);
    fireEvent.change(box, { target: { value: "paper" } });
    const from = screen.getByRole("combobox", { name: "From" });
    expect(within(from).getAllByRole("option").map((o) => o.textContent)).toEqual(["Anyone", "You", "Nafisa Hasan", "Rahim Uddin"]);
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.getByRole("log")).toBeTruthy();
  });
});
