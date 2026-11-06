import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";

import { getProfile } from "@/features/profile/api";
import { emptyProfile } from "@/features/profile/test-profiles";

import { getConversation, listConversations, sendDirectMessage } from "./api";
import { NewConversation } from "./new-conversation";
import { adaID, bobID, conversation, conversationID, message } from "./test-data";
import { refreshUnreadMessages } from "./unread-count";
import { ConversationsProvider } from "./use-conversations";

const mockReplace = jest.fn();
const mockRouter = { replace: mockReplace };
jest.mock("./api");
jest.mock("./unread-count");
jest.mock("@/features/profile/api");
jest.mock("next/navigation", () => ({ useRouter: () => mockRouter }));
jest.mock("@/features/auth/session", () => ({
  useSession: () => ({ status: "signedIn", user: { id: "0b8f4c1e-0000-4000-8000-000000000001" } }),
}));

const mockProfile = jest.mocked(getProfile);
const mockSend = jest.mocked(sendDirectMessage);
const bob = emptyProfile(bobID, "Bob Babbage");

const renderNew = (to: string | null = bobID) =>
  render(
    <ConversationsProvider>
      <NewConversation to={to} />
    </ConversationsProvider>,
  );

const box = () => screen.getByRole("textbox", { name: "Write a message" });
async function write(text: string) {
  fireEvent.change(box(), { target: { value: text } });
  await act(async () => fireEvent.keyDown(box(), { key: "Enter" }));
}

beforeEach(() => {
  jest.mocked(refreshUnreadMessages).mockResolvedValue();
  jest.mocked(listConversations).mockResolvedValue({ ok: true, data: { items: [], nextCursor: null } });
  Object.defineProperty(globalThis.crypto, "randomUUID", { configurable: true, value: jest.fn(() => "client-1") });
});
afterEach(() => jest.resetAllMocks());

describe("NewConversation", () => {
  it("sends the first message, then opens the conversation", async () => {
    mockProfile.mockResolvedValue({ status: "ok", profile: bob });
    mockSend.mockResolvedValue({ ok: true, data: { message: message(1, adaID), conversationState: "request" } });
    jest.mocked(getConversation).mockResolvedValue({ ok: true, data: conversation() });
    renderNew();
    expect(await screen.findByRole("link", { name: "Bob Babbage" })).toBeTruthy();
    await write("Hello");
    expect(mockSend).toHaveBeenCalledWith(bobID, "Hello", "client-1");
    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith(`/messaging/${conversationID}`));
    expect(refreshUnreadMessages).toHaveBeenCalledWith(adaID);
  });

  it("keeps a failed first message, and retries with the same clientId", async () => {
    mockProfile.mockResolvedValue({ status: "ok", profile: bob });
    mockSend.mockResolvedValueOnce({ ok: false, code: "rate_limited", message: "x", retryAfter: 60 });
    mockSend.mockResolvedValueOnce({ ok: true, data: { message: message(1, adaID), conversationState: "request" } });
    jest.mocked(getConversation).mockResolvedValue({ ok: true, data: conversation() });
    renderNew();
    await screen.findByRole("textbox", { name: "Write a message" });
    await write("Hello");
    expect(await screen.findByText("You're sending messages too quickly. Try again in a minute.")).toBeTruthy();
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Retry" })));
    expect(mockSend.mock.calls.map((c) => c[2])).toEqual(["client-1", "client-1"]);
    await waitFor(() => expect(mockReplace).toHaveBeenCalled());
  });

  it("opens the conversation when there is one already", async () => {
    const link = { id: conversationID, state: "active" as const, requestedBy: null, canMessage: true };
    mockProfile.mockResolvedValue({ status: "ok", profile: { ...bob, conversation: link } });
    renderNew();
    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith(`/messaging/${conversationID}`));
  });

  it.each([
    ["unknown or hidden", { status: "notFound" as const }, bobID],
    ["blocked by me", { status: "ok" as const, profile: { ...bob, blockedByMe: true } }, bobID],
    ["me", { status: "ok" as const, profile: emptyProfile(adaID, "Ada") }, adaID],
  ])("refuses someone %s", async (_, result, to) => {
    mockProfile.mockResolvedValue(result);
    renderNew(to);
    expect(await screen.findByText("You can't message this person.")).toBeTruthy();
    expect(screen.queryByRole("textbox")).toBeNull();
  });

  it("asks for a person without ?to=", () => {
    renderNew(null);
    expect(screen.getByText(/Choose someone/)).toBeTruthy();
    expect(mockProfile).not.toHaveBeenCalled();
  });

  it("says so when the profile cannot load", async () => {
    mockProfile.mockResolvedValue({ status: "error" });
    renderNew();
    expect((await screen.findByRole("alert")).textContent).toContain("Couldn't load");
  });
});
