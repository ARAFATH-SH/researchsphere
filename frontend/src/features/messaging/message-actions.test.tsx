import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { getCurrentUser } from "@/features/auth/api";

import { deleteMessage, editMessage, getConversation, listMessages, markConversationRead, type Message } from "./api";
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

const mockEdit = jest.mocked(editMessage);
const mockDelete = jest.mocked(deleteMessage);

// recent is a moment inside the edit window.
const recent = () => new Date(Date.now() - 60_000).toISOString();

function load(items: Message[]) {
  jest.mocked(getConversation).mockResolvedValue({ ok: true, data: conversation() });
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
}

// openMenu opens the "…" menu of the message showing text.
function openMenu(text: string) {
  const row = screen.getByText(text).closest(".group\\/message") as HTMLElement;
  fireEvent.click(within(row).getByRole("button", { name: "More actions for this message" }));
  return screen.getByRole("menu", { name: "Message actions" });
}

const editor = () => screen.getByRole("textbox", { name: "Edit message" }) as HTMLTextAreaElement;

beforeEach(() => {
  jest.mocked(getCurrentUser).mockResolvedValue({ ok: false, code: "network_error", message: "No." });
  jest.spyOn(document, "hasFocus").mockImplementation(() => true);
  jest.mocked(markConversationRead).mockResolvedValue({ ok: true, data: undefined });
  jest.mocked(refreshUnreadMessages).mockResolvedValue();
});
afterEach(() => {
  jest.useRealTimers();
  jest.resetAllMocks();
});

describe("Message actions", () => {
  it("offers Edit and Delete on my messages and Report on theirs", async () => {
    load([message(1, adaID, "mine", recent()), message(2, bobID, "theirs", recent())]);
    await renderThread();
    const mine = openMenu("mine");
    expect(within(mine).getAllByRole("menuitem").map((i) => i.textContent)).toEqual(["Edit", "Delete"]);
    fireEvent.keyDown(mine, { key: "Escape" });
    const theirs = openMenu("theirs");
    expect(within(theirs).getAllByRole("menuitem").map((i) => i.textContent)).toEqual(["Report message"]);
  });

  it("moves between items with the arrow keys and closes on Escape, back on the button", async () => {
    load([message(1, adaID, "mine", recent())]);
    await renderThread();
    const menu = openMenu("mine");
    const [edit, del] = within(menu).getAllByRole("menuitem");
    expect(document.activeElement).toBe(edit);
    fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(document.activeElement).toBe(del);
    fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(document.activeElement).toBe(edit);
    fireEvent.keyDown(menu, { key: "Escape" });
    expect(menu.hidden).toBe(true);
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "More actions for this message" }));
  });

  it("hides Edit once the 15-minute window from the server's createdAt has passed", async () => {
    jest.useFakeTimers({ now: new Date("2026-11-05T09:10:00Z"), doNotFake: ["queueMicrotask", "nextTick", "setImmediate"] });
    load([message(1, adaID, "mine", "2026-11-05T09:00:00Z")]);
    await renderThread();
    let menu = openMenu("mine");
    expect(within(menu).queryByRole("menuitem", { name: "Edit" })).toBeTruthy();
    fireEvent.keyDown(menu, { key: "Escape" });
    act(() => jest.advanceTimersByTime(5 * 60_000));
    menu = openMenu("mine");
    expect(within(menu).queryByRole("menuitem", { name: "Edit" })).toBeNull();
    expect(within(menu).getByRole("menuitem", { name: "Delete" })).toBeTruthy();
  });

  it("offers no Edit on an old message, and no menu on a deleted one", async () => {
    load([message(1, adaID, "old"), { ...message(2, adaID, "gone", recent()), body: null, deleted: true }]);
    await renderThread();
    expect(within(openMenu("old")).queryByRole("menuitem", { name: "Edit" })).toBeNull();
    const tombstone = screen.getByText("This message was deleted");
    expect(tombstone.closest(".group\\/message")).toBeNull();
  });

  it("does not offer Edit on a message with files and no text", async () => {
    const files: Message = {
      ...message(1, adaID, "", recent()),
      body: null,
      attachments: [{ id: "f1", name: "a.pdf", size: 10, contentType: "application/pdf", status: "clean" }],
    };
    load([files]);
    await renderThread();
    const row = screen.getByText("a.pdf").closest(".group\\/message") as HTMLElement;
    fireEvent.click(within(row).getByRole("button", { name: "More actions for this message" }));
    expect(within(screen.getByRole("menu")).getAllByRole("menuitem").map((i) => i.textContent)).toEqual(["Delete"]);
  });
});

describe("Inline edit", () => {
  async function startEditing(text = "hello") {
    load([message(1, adaID, text, recent())]);
    await renderThread();
    fireEvent.click(within(openMenu(text)).getByRole("menuitem", { name: "Edit" }));
    expect(document.activeElement).toBe(editor());
  }

  it("saves with Enter and shows Edited with the edit time in a tooltip", async () => {
    await startEditing();
    const editedAt = new Date().toISOString();
    mockEdit.mockResolvedValue({ ok: true, data: { ...message(1, adaID, "hello again", recent()), editedAt } });
    fireEvent.change(editor(), { target: { value: "  hello again " } });
    await act(async () => fireEvent.keyDown(editor(), { key: "Enter" }));
    expect(mockEdit).toHaveBeenCalledWith(conversationID, message(1, adaID).id, "hello again");
    expect(screen.queryByRole("textbox", { name: "Edit message" })).toBeNull();
    expect(screen.getByText("hello again")).toBeTruthy();
    const label = screen.getByText("Edited");
    expect(label.getAttribute("title")).toMatch(/^Edited \d+ \w{3} \d{4}, \d+:\d{2} [AP]M$/);
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "More actions for this message" }));
  });

  it("adds a line with Shift+Enter and saves with the button", async () => {
    await startEditing();
    mockEdit.mockResolvedValue({ ok: true, data: message(1, adaID, "a\nb", recent()) });
    fireEvent.change(editor(), { target: { value: "a" } });
    fireEvent.keyDown(editor(), { key: "Enter", shiftKey: true });
    expect(mockEdit).not.toHaveBeenCalled();
    fireEvent.change(editor(), { target: { value: "a\nb" } });
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Save" })));
    expect(mockEdit).toHaveBeenCalledWith(conversationID, message(1, adaID).id, "a\nb");
  });

  it("cancels with Escape or Cancel, keeping the text", async () => {
    await startEditing();
    fireEvent.change(editor(), { target: { value: "changed" } });
    fireEvent.keyDown(editor(), { key: "Escape" });
    expect(screen.queryByRole("textbox", { name: "Edit message" })).toBeNull();
    expect(screen.getByText("hello")).toBeTruthy();
    fireEvent.click(within(openMenu("hello")).getByRole("menuitem", { name: "Edit" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("textbox", { name: "Edit message" })).toBeNull();
    expect(mockEdit).not.toHaveBeenCalled();
  });

  it("sends nothing for unchanged text", async () => {
    await startEditing();
    fireEvent.change(editor(), { target: { value: " hello  " } });
    await act(async () => fireEvent.keyDown(editor(), { key: "Enter" }));
    expect(mockEdit).not.toHaveBeenCalled();
    expect(screen.queryByRole("textbox", { name: "Edit message" })).toBeNull();
  });

  it("does not save empty text", async () => {
    await startEditing();
    fireEvent.change(editor(), { target: { value: "   " } });
    expect((screen.getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("closes the editor after 409 edit_window_closed and says the message can no longer be edited", async () => {
    await startEditing();
    mockEdit.mockResolvedValue({ ok: false, code: "edit_window_closed", message: "Too late." });
    fireEvent.change(editor(), { target: { value: "new" } });
    await act(async () => fireEvent.keyDown(editor(), { key: "Enter" }));
    expect(screen.queryByRole("textbox", { name: "Edit message" })).toBeNull();
    expect(screen.getByRole("alert").textContent).toBe("This message can no longer be edited.");
    expect(screen.getByText("hello")).toBeTruthy();
    expect(within(openMenu("hello")).queryByRole("menuitem", { name: "Edit" })).toBeNull();
  });

  it("keeps the editor open with the reason for another refusal", async () => {
    await startEditing();
    mockEdit.mockResolvedValue({ ok: false, code: "rate_limited", message: "Slow down.", retryAfter: 30 });
    fireEvent.change(editor(), { target: { value: "new" } });
    await act(async () => fireEvent.keyDown(editor(), { key: "Enter" }));
    expect(editor().value).toBe("new");
    expect(screen.getByRole("alert").textContent).toBe("You're sending messages too quickly. Try again in 30 seconds.");
  });
});

describe("Delete", () => {
  async function confirmDelete() {
    load([message(1, adaID, "secret", recent()), message(2, bobID, "reply", recent())]);
    await renderThread();
    fireEvent.click(within(openMenu("secret")).getByRole("menuitem", { name: "Delete" }));
    return screen.getByRole("dialog", { name: "Delete message" });
  }

  it("asks in the menu, and Cancel deletes nothing", async () => {
    const dialog = await confirmDelete();
    expect(dialog.textContent).toContain("Delete this message for everyone?");
    expect(document.activeElement).toBe(within(dialog).getByRole("button", { name: "Cancel" }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(mockDelete).not.toHaveBeenCalled();
    expect(screen.getByText("secret")).toBeTruthy();
  });

  it("turns the message into a muted tombstone in its place", async () => {
    mockDelete.mockResolvedValue({ ok: true, data: undefined });
    const dialog = await confirmDelete();
    await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Delete" })));
    expect(mockDelete).toHaveBeenCalledWith(conversationID, message(1, adaID).id);
    expect(screen.queryByText("secret")).toBeNull();
    const text = screen.getByRole("log").textContent ?? "";
    expect(text.indexOf("This message was deleted")).toBeLessThan(text.indexOf("reply"));
    expect(screen.getByText("This message was deleted").className).toContain("text-rs-text-muted");
  });

  it("shows an error in the menu when the delete fails", async () => {
    mockDelete.mockResolvedValue({ ok: false, code: "internal_error", message: "Oops." });
    const dialog = await confirmDelete();
    await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Delete" })));
    expect(within(dialog).getByRole("alert").textContent).toBe("Couldn't delete the message. Try again.");
    expect(screen.getByText("secret")).toBeTruthy();
  });
});
