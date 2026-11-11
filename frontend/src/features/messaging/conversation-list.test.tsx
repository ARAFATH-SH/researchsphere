import "@/test/dialog-polyfill";

import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { listMyConnections } from "@/features/social/api";

import { listConversations, type MessageUnreadCount, searchMessages } from "./api";
import { ConversationList } from "./conversation-list";
import { adaID, bobID, conversation, conversationID, group, groupID } from "./test-data";
import { ConversationsProvider } from "./use-conversations";

const mockCounts = jest.fn<MessageUnreadCount, []>();
jest.mock("./api");
jest.mock("@/features/social/api");
jest.mock("./unread-count", () => ({ useUnreadMessages: () => mockCounts() }));
jest.mock("next/navigation", () => ({ usePathname: () => `/messaging/${conversationID}`, useRouter: () => ({ push: jest.fn() }) }));
jest.mock("@/features/auth/session", () => ({
  useSession: () => ({ status: "signedIn", user: { id: "0b8f4c1e-0000-4000-8000-000000000001" } }),
}));

const mockList = jest.mocked(listConversations);
const page = (items: ReturnType<typeof conversation>[], nextCursor: string | null = null) => ({
  ok: true as const,
  data: { items, nextCursor },
});

function renderList() {
  return render(
    <ConversationsProvider>
      <ConversationList />
    </ConversationsProvider>,
  );
}

let intersect: ((entries: { isIntersecting: boolean }[]) => void) | null;

beforeEach(() => {
  intersect = null;
  mockCounts.mockReturnValue({ total: 0, requests: 0 });
  window.IntersectionObserver = jest.fn((callback: (entries: { isIntersecting: boolean }[]) => void) => {
    intersect = callback;
    return { observe: jest.fn(), disconnect: jest.fn() };
  }) as unknown as typeof IntersectionObserver;
});
afterEach(() => jest.resetAllMocks());

describe("ConversationList", () => {
  it("shows a row: name, preview, time, unread count in words and in bold", async () => {
    mockList.mockResolvedValue(page([conversation({ unreadCount: 3 })]));
    mockCounts.mockReturnValue({ total: 3, requests: 1 });
    renderList();
    const row = await screen.findByRole("link", { name: /Bob Babbage/ });
    expect(row.getAttribute("href")).toBe(`/messaging/${conversationID}`);
    expect(row.getAttribute("aria-current")).toBe("page");
    expect(within(row).getByText("See you").className).toContain("font-bold");
    expect(within(row).getByText("3 unread")).toBeTruthy();
    expect(screen.getByRole("tab", { name: "Inbox · 3 unread" }).getAttribute("aria-selected")).toBe("true");
    expect(screen.getByRole("tab", { name: "Requests (1)" })).toBeTruthy();
  });

  it("shows a read row plainly, with You: before my own message", async () => {
    const lastMessage = { senderId: adaID, senderName: "Ada Lovelace", kind: "text" as const, event: null, body: "Hello", deleted: false, attachmentCount: 0, createdAt: "2026-11-05T09:00:00Z" };
    mockList.mockResolvedValue(page([conversation({ lastMessage })]));
    renderList();
    const preview = await screen.findByText("You: Hello");
    expect(preview.className).not.toContain("font-bold");
    expect(screen.queryByText(/unread/)).toBeNull();
    expect(screen.getByRole("tab", { name: "Inbox" })).toBeTruthy();
  });

  it("shows request rows by who is waiting, and loads Requests when opened", async () => {
    mockList.mockImplementation(async (box) =>
      box === "inbox"
        ? page([conversation({ state: "request", requestedBy: adaID })])
        : page([conversation({ id: "other", state: "request", requestedBy: bobID })]),
    );
    renderList();
    expect(await screen.findByText("Request sent")).toBeTruthy();
    expect(mockList).toHaveBeenCalledTimes(1);
    await act(async () => fireEvent.click(screen.getByRole("tab", { name: "Requests" })));
    expect(await screen.findByText("Wants to message you")).toBeTruthy();
    expect(mockList).toHaveBeenCalledWith("requests");
  });

  it("has an empty state for each tab", async () => {
    mockList.mockResolvedValue(page([]));
    renderList();
    expect(await screen.findByText(/No conversations yet/)).toBeTruthy();
    await act(async () => fireEvent.click(screen.getByRole("tab", { name: "Requests" })));
    expect(await screen.findByText("No message requests.")).toBeTruthy();
  });

  it("moves between tabs with the arrow keys", async () => {
    mockList.mockResolvedValue(page([]));
    renderList();
    await screen.findByText(/No conversations yet/);
    await act(async () => fireEvent.keyDown(screen.getByRole("tab", { name: "Inbox" }), { key: "ArrowRight" }));
    expect(screen.getByRole("tab", { name: "Requests" }).getAttribute("aria-selected")).toBe("true");
    await screen.findByText("No message requests.");
  });

  it("pages by cursor when the end of the list scrolls into view", async () => {
    mockList.mockResolvedValueOnce(page([conversation()], "next"));
    mockList.mockResolvedValueOnce(page([conversation({ id: "second", otherMember: { ...conversation().otherMember!, fullName: "Cy Young" } })]));
    renderList();
    await screen.findByRole("link", { name: /Bob Babbage/ });
    await act(async () => intersect?.([{ isIntersecting: true }]));
    expect(mockList).toHaveBeenLastCalledWith("inbox", "next");
    expect(await screen.findByRole("link", { name: /Cy Young/ })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Show more" })).toBeNull();
  });

  it("says so when the next page fails, and lets you try again", async () => {
    mockList.mockResolvedValueOnce(page([conversation()], "next"));
    mockList.mockResolvedValueOnce({ ok: false, code: "network_error", message: "x" });
    mockList.mockResolvedValueOnce(page([]));
    renderList();
    await screen.findByRole("link", { name: /Bob Babbage/ });
    await act(async () => intersect?.([{ isIntersecting: true }]));
    expect(await screen.findByText("Couldn't load more. Try again.")).toBeTruthy();
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Show more" })));
    expect(mockList).toHaveBeenCalledTimes(3);
    expect(screen.queryByText("Couldn't load more. Try again.")).toBeNull();
  });

  it("offers to try again when the list fails", async () => {
    mockList.mockResolvedValueOnce({ ok: false, code: "network_error", message: "x" });
    mockList.mockResolvedValueOnce(page([conversation()]));
    renderList();
    const again = await screen.findByRole("button", { name: "Try again" });
    await act(async () => fireEvent.click(again));
    expect(await screen.findByRole("link", { name: /Bob Babbage/ })).toBeTruthy();
  });
});

describe("groups and search in the list (FR-16.6, FR-16.8)", () => {
  it("shows a group row with its name, first members and a named preview", async () => {
    mockList.mockResolvedValue(page([group()]));
    renderList();
    const row = await screen.findByRole("link", { name: /CVS paper team/ });
    expect(row.getAttribute("href")).toBe(`/messaging/${groupID}`);
    expect(within(row).getByText("Nafisa: I'll check the references")).toBeTruthy();
    expect(within(row).getByText("NH")).toBeTruthy();
    expect(within(row).getByText("RU")).toBeTruthy();
  });

  it("opens the New group dialog from the heading", async () => {
    mockList.mockResolvedValue(page([conversation()]));
    jest.mocked(listMyConnections).mockResolvedValue({ ok: true, data: { items: [], page: 1, pageSize: 20, total: 0 } });
    renderList();
    await screen.findByRole("link", { name: /Bob Babbage/ });
    fireEvent.click(screen.getByRole("button", { name: "New group" }));
    expect(await screen.findByRole("dialog", { name: "New group" })).toBeTruthy();
  });

  it("puts the search results in the list's place from 2 characters, and brings the list back on Escape", async () => {
    jest.useFakeTimers();
    mockList.mockResolvedValue(page([conversation()]));
    jest.mocked(searchMessages).mockResolvedValue({ ok: true, data: { items: [], nextCursor: null, people: {} } });
    renderList();
    await screen.findByRole("link", { name: /Bob Babbage/ });
    const box = screen.getByRole("searchbox", { name: "Search messages" });
    fireEvent.change(box, { target: { value: "x" } });
    expect(screen.getByRole("tablist")).toBeTruthy();
    fireEvent.change(box, { target: { value: "xy" } });
    expect(screen.queryByRole("tablist")).toBeNull();
    await act(async () => jest.advanceTimersByTime(300));
    expect(screen.getByText("No messages match “xy”.")).toBeTruthy();
    fireEvent.keyDown(box, { key: "Escape" });
    expect(screen.getByRole("link", { name: /Bob Babbage/ })).toBeTruthy();
    jest.useRealTimers();
  });
});
