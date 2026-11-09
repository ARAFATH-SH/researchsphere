import "@/test/dialog-polyfill";

import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { getCurrentUser } from "@/features/auth/api";
import { blockUser, unblockUser } from "@/features/social/api";

import { type Conversation, getConversation, listMessages, markConversationRead, type Message, reportConversation, sendMessage } from "./api";
import { adaID, bobID, conversation, conversationID, message } from "./test-data";
import { Thread } from "./thread";
import { refreshUnreadMessages } from "./unread-count";
import { ConversationsProvider } from "./use-conversations";

jest.mock("./api");
jest.mock("./unread-count");
jest.mock("@/features/auth/api");
jest.mock("@/features/social/api");
jest.mock("@/features/auth/session", () => ({
  useSession: () => ({ status: "signedIn", user: { id: "0b8f4c1e-0000-4000-8000-000000000001" } }),
}));
let reconnect: () => void = () => {};
jest.mock("@/features/realtime/use-realtime", () => ({
  useRealtimeEvent: jest.fn(),
  useOnConnected: (handler: () => void) => {
    reconnect = handler;
  },
}));

const mockGet = jest.mocked(getConversation);
const mockMe = jest.mocked(getCurrentUser);
const user = { id: adaID, email: "ada@example.org", fullName: "Ada", role: "researcher" as const };
const until = "2026-12-03T14:05:00";

function restricted(end: string | null) {
  mockMe.mockResolvedValue({ ok: true, data: { ...user, messagingRestrictedUntil: end } });
}

function load(c: Conversation, items: Message[] = []) {
  mockGet.mockResolvedValue({ ok: true, data: c });
  jest.mocked(listMessages).mockResolvedValue({
    ok: true,
    data: { items: [...items].reverse(), nextBefore: null, otherLastReadMessageId: null, readers: null },
  });
}

async function renderThread() {
  render(
    <ConversationsProvider>
      <Thread conversationId={conversationID} />
    </ConversationsProvider>,
  );
  await screen.findByRole("log");
  await act(async () => {});
}

const composer = () => screen.queryByRole("textbox", { name: "Write a message" });
// The restriction comes from its own read, which can settle after the thread has loaded.
const restrictionLoaded = () => screen.findByText("You can't send messages until 3 Dec 2026, 2:05 PM.");

beforeEach(() => {
  jest.useFakeTimers({ now: new Date("2026-11-06T10:00:00"), doNotFake: ["nextTick", "queueMicrotask", "setTimeout", "clearTimeout"] });
  jest.spyOn(document, "hasFocus").mockReturnValue(true);
  jest.mocked(markConversationRead).mockResolvedValue({ ok: true, data: undefined });
  jest.mocked(refreshUnreadMessages).mockResolvedValue();
  restricted(null);
});
afterEach(() => {
  jest.useRealTimers();
  jest.resetAllMocks();
});

describe("Thread header pills", () => {
  it("shows Block and Report, as the design does, and the name links to the profile", async () => {
    load(conversation());
    await renderThread();
    expect(screen.getByRole("button", { name: "Block Bob Babbage" }).textContent).toBe("Block");
    expect(screen.getByRole("button", { name: "Report Bob Babbage" }).textContent).toBe("Report");
    expect(screen.getByRole("link", { name: "Bob Babbage" }).getAttribute("href")).toBe(`/profile/${bobID}`);
  });

  it("shows Unblock in place of Block when blockedByMe", async () => {
    load(conversation({ blockedByMe: true, canMessage: false }));
    await renderThread();
    expect(screen.getByRole("button", { name: "Unblock Bob Babbage" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Block Bob Babbage" })).toBeNull();
  });

  it("leaves Block out when the person is not available", async () => {
    const c = conversation({ canMessage: false });
    load({ ...c, otherMember: { ...c.otherMember, available: false } });
    await renderThread();
    expect(screen.queryByRole("button", { name: "Block Bob Babbage" })).toBeNull();
    expect(screen.getByRole("button", { name: "Report Bob Babbage" })).toBeTruthy();
  });

  it("asks before blocking, then reads the conversation again", async () => {
    load(conversation());
    jest.mocked(blockUser).mockResolvedValue({ ok: true, data: undefined });
    await renderThread();
    fireEvent.click(screen.getByRole("button", { name: "Block Bob Babbage" }));
    const dialog = screen.getByRole("dialog", { name: "Block Bob Babbage?" });
    expect(blockUser).not.toHaveBeenCalled();
    load(conversation({ blockedByMe: true, canMessage: false }));
    await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Block" })));
    expect(blockUser).toHaveBeenCalledWith(bobID);
    expect(await screen.findByText("You blocked Bob Babbage.")).toBeTruthy();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("goes from a sent report to the block confirmation", async () => {
    load(conversation());
    jest.mocked(reportConversation).mockResolvedValue({
      ok: true,
      data: { id: "r", createdAt: "2026-11-05T09:00:00Z", reportedUserId: bobID, blockedByMe: false },
    });
    await renderThread();
    fireEvent.click(screen.getByRole("button", { name: "Report Bob Babbage" }));
    fireEvent.change(screen.getByRole("combobox", { name: "Reason" }), { target: { value: "spam" } });
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Send report" })));
    expect(reportConversation).toHaveBeenCalledWith(conversationID, { reason: "spam" });
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Block Bob Babbage" }));
    expect(screen.getByRole("dialog", { name: "Block Bob Babbage?" })).toBeTruthy();
  });
});

describe("Report message", () => {
  const dots = () => screen.getAllByRole("button", { name: "More actions for this message" });

  it("is offered on their messages and not on mine", async () => {
    load(conversation(), [message(1, adaID, "mine"), message(2, bobID, "theirs")]);
    await renderThread();
    const [mine, theirs] = dots();
    fireEvent.click(theirs);
    expect(within(screen.getByRole("menu")).getByRole("menuitem", { name: "Report message" })).toBeTruthy();
    fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });
    fireEvent.click(mine);
    expect(within(screen.getByRole("menu")).queryByRole("menuitem", { name: "Report message" })).toBeNull();
  });

  it("reports that message", async () => {
    const theirs = message(2, bobID, "theirs");
    load(conversation(), [theirs]);
    jest.mocked(reportConversation).mockResolvedValue({
      ok: true,
      data: { id: "r", createdAt: "2026-11-05T09:00:00Z", reportedUserId: bobID, blockedByMe: false },
    });
    await renderThread();
    fireEvent.click(dots()[0]);
    fireEvent.click(screen.getByRole("menuitem", { name: "Report message" }));
    fireEvent.change(screen.getByRole("combobox", { name: "Reason" }), { target: { value: "other" } });
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Send report" })));
    expect(reportConversation).toHaveBeenCalledWith(conversationID, { reason: "other", messageId: theirs.id });
  });

  it("opens on a long press, and not on a tap or a scroll", async () => {
    load(conversation(), [message(2, bobID, "theirs")]);
    await renderThread();
    const bubble = screen.getByText("theirs");
    const pressFor = async (ms: number, end: "touchEnd" | "touchMove") => {
      fireEvent.touchStart(bubble);
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, ms));
      });
      fireEvent[end](bubble);
    };
    await pressFor(20, "touchEnd");
    expect(screen.queryByRole("menuitem", { name: "Report message" })).toBeNull();
    await pressFor(20, "touchMove");
    expect(screen.queryByRole("menuitem", { name: "Report message" })).toBeNull();
    await pressFor(550, "touchEnd");
    expect(screen.getByRole("menuitem", { name: "Report message" })).toBeTruthy();
  });

  it("closes with Escape and gives the focus back", async () => {
    load(conversation(), [message(2, bobID, "theirs")]);
    await renderThread();
    fireEvent.click(dots()[0]);
    fireEvent.keyDown(screen.getByRole("menuitem", { name: "Report message" }), { key: "Escape" });
    expect(screen.queryByRole("menuitem", { name: "Report message" })).toBeNull();
    expect(document.activeElement).toBe(dots()[0]);
  });
});

describe("Why the composer is off", () => {
  it("blockedByMe says so and offers Unblock", async () => {
    load(conversation({ blockedByMe: true, canMessage: false }));
    jest.mocked(unblockUser).mockResolvedValue({ ok: true, data: undefined });
    await renderThread();
    expect(screen.getByText("You blocked Bob Babbage.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Unblock" }));
    load(conversation());
    const dialog = screen.getByRole("dialog", { name: "Unblock Bob Babbage?" });
    await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Unblock" })));
    expect(unblockUser).toHaveBeenCalledWith(bobID);
    expect(screen.queryByText("You blocked Bob Babbage.")).toBeNull();
  });

  it("blockedByMe comes before a closed conversation", async () => {
    load(conversation({ state: "declined", blockedByMe: true, canMessage: false }));
    await renderThread();
    expect(screen.getByText("You blocked Bob Babbage.")).toBeTruthy();
    expect(screen.queryByText("This conversation is closed.")).toBeNull();
  });

  it("otherwise never says that they blocked me", async () => {
    load(conversation({ canMessage: false }));
    await renderThread();
    expect(screen.getByText("You can't reply to this conversation.")).toBeTruthy();
    const section = screen.getByRole("region", { name: "Conversation with Bob Babbage" });
    expect(section.textContent).not.toMatch(/You blocked|blocked you/);
  });
});

describe("Messaging restriction", () => {
  it("replaces the composer before any send", async () => {
    restricted(until);
    load(conversation());
    await renderThread();
    await restrictionLoaded();
    expect(composer()).toBeNull();
    expect(sendMessage).not.toHaveBeenCalled();
  });

  it("reads it again when the window gets the focus", async () => {
    load(conversation());
    await renderThread();
    expect(composer()).toBeTruthy();
    restricted(until);
    await act(async () => fireEvent.focus(window));
    expect(composer()).toBeNull();
  });

  it("reads it again after a socket reconnect, so a lift brings the composer back", async () => {
    restricted(until);
    load(conversation());
    await renderThread();
    await restrictionLoaded();
    expect(composer()).toBeNull();
    restricted(null);
    await act(async () => reconnect());
    expect(composer()).toBeTruthy();
  });

  it("keeps the last answer when a read fails", async () => {
    restricted(until);
    load(conversation());
    await renderThread();
    await restrictionLoaded();
    mockMe.mockResolvedValue({ ok: false, code: "network_error", message: "No." });
    await act(async () => fireEvent.focus(window));
    expect(composer()).toBeNull();
  });

  it("falls back to a refused send (403)", async () => {
    load(conversation());
    jest.mocked(sendMessage).mockResolvedValue({ ok: false, code: "messaging_restricted", message: "Restricted.", until });
    Object.defineProperty(globalThis.crypto, "randomUUID", { configurable: true, value: jest.fn(() => "client-1") });
    await renderThread();
    fireEvent.change(composer()!, { target: { value: "hello" } });
    await act(async () => fireEvent.keyDown(composer()!, { key: "Enter" }));
    expect(screen.getByText("You can't send messages until 3 Dec 2026, 2:05 PM.")).toBeTruthy();
    expect(composer()).toBeNull();
  });
});
