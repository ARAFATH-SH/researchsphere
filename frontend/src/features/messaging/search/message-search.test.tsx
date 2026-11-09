import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { useState } from "react";

import { type MessageSearchResult, searchMessages } from "../api";
import { adaID, conversationID, groupID, nafisaID } from "../test-data";
import { MessageSearchView, SearchBox } from "./message-search";
import { searchDelay } from "./use-message-search";

jest.mock("../api");
const mockSearch = jest.mocked(searchMessages);

function result(n: number, overrides: Partial<MessageSearchResult> = {}): MessageSearchResult {
  return {
    messageId: `019a5c2e-0000-7000-8000-${String(n).padStart(12, "0")}`,
    conversationId: conversationID,
    conversation: { kind: "direct", name: null, otherMember: { userId: nafisaID, fullName: "Nafisa Hasan", photoUrl: null }, avatars: [] },
    sender: { userId: nafisaID, fullName: "Nafisa Hasan" },
    createdAt: "2026-11-05T09:00:00Z",
    snippet: "Our paper draft",
    highlights: [{ start: 4, length: 5 }],
    matchedOn: "text",
    ...overrides,
  };
}

const found = (items: MessageSearchResult[], nextCursor: string | null = null) => ({
  ok: true as const,
  data: { items, nextCursor, people: {} },
});

// Search is the list column's box and the view it shows from 2 characters, as ConversationList
// wires them.
function Search({ conversationId, people }: { conversationId?: string; people?: { userId: string; fullName: string }[] }) {
  const [text, setText] = useState("");
  return (
    <>
      <SearchBox value={text} onChange={setText} label="Search messages" />
      {text.trim().length >= 2 ? <MessageSearchView text={text} me={adaID} conversationId={conversationId} people={people} /> : <p>The list</p>}
    </>
  );
}

const box = () => screen.getByRole("searchbox", { name: "Search messages" });
const type = (text: string) => fireEvent.change(box(), { target: { value: text } });
const wait = (ms = searchDelay) => act(async () => jest.advanceTimersByTime(ms));

beforeEach(() => jest.useFakeTimers());
afterEach(() => {
  jest.useRealTimers();
  jest.resetAllMocks();
});

describe("message search (FR-16.8)", () => {
  it("searches nothing under 2 characters, then once, 300 ms after the last key", async () => {
    mockSearch.mockResolvedValue(found([result(1)]));
    render(<Search />);
    type("p");
    await wait();
    expect(mockSearch).not.toHaveBeenCalled();
    expect(screen.getByText("The list")).toBeTruthy();
    type("pa");
    await wait(100);
    type("pap");
    await wait(searchDelay - 1);
    expect(mockSearch).not.toHaveBeenCalled();
    await wait(1);
    expect(mockSearch).toHaveBeenCalledTimes(1);
    expect(mockSearch).toHaveBeenCalledWith({ q: "pap", conversationId: undefined, from: undefined, after: undefined });
  });

  it("shows each result with the conversation, sender, date and the snippet's matches, opening it at the message", async () => {
    mockSearch.mockResolvedValue(
      found([
        result(1),
        result(2, {
          conversationId: groupID,
          conversation: { kind: "group", name: "CVS paper team", otherMember: null, avatars: [{ userId: nafisaID, fullName: "Nafisa Hasan", photoUrl: null }] },
          sender: { userId: adaID, fullName: "Ada Lovelace" },
          snippet: "Thesis_paper.pdf",
          highlights: [{ start: 7, length: 5 }],
          matchedOn: "attachment",
        }),
      ]),
    );
    render(<Search />);
    type("paper");
    await wait();
    const [direct, group] = within(screen.getByRole("list", { name: "Search results" })).getAllByRole("link");
    expect(direct.getAttribute("href")).toBe(`/messaging/${conversationID}?around=${result(1).messageId}`);
    expect(within(direct).getByText("paper").tagName).toBe("MARK");
    expect(within(direct).getAllByText("Nafisa Hasan")).toHaveLength(2); // the conversation and the sender
    expect(within(direct).queryByRole("img", { name: "In a file name" })).toBeNull();
    expect(within(direct).getByText(/AM|PM|Thu|Nov/)).toBeTruthy();
    expect(within(group).getByText("CVS paper team")).toBeTruthy();
    expect(within(group).getByText("You")).toBeTruthy();
    expect(within(group).getByRole("img", { name: "In a file name" })).toBeTruthy();
    expect(within(group).getByText("paper").tagName).toBe("MARK");
  });

  it("highlights Bangla by code points", async () => {
    mockSearch.mockResolvedValue(found([result(1, { snippet: "আমি গবেষণা করি", highlights: [{ start: 4, length: 6 }] })]));
    render(<Search />);
    type("গবেষণা");
    await wait();
    expect(screen.getByText("গবেষণা").tagName).toBe("MARK");
  });

  it("pages with the cursor on Show more", async () => {
    mockSearch.mockResolvedValueOnce(found([result(2)], "next")).mockResolvedValueOnce(found([result(1, { snippet: "older paper", highlights: [] })]));
    render(<Search />);
    type("paper");
    await wait();
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Show more" })));
    expect(mockSearch).toHaveBeenLastCalledWith(expect.objectContaining({ q: "paper", cursor: "next" }));
    expect(screen.getByText("older paper")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Show more" })).toBeNull();
  });

  it("says when nothing matches, and when searching too quickly", async () => {
    mockSearch.mockResolvedValueOnce(found([]));
    render(<Search />);
    type("zzz");
    await wait();
    expect(screen.getByText("No messages match “zzz”.")).toBeTruthy();
    mockSearch.mockResolvedValueOnce({ ok: false, code: "rate_limited", message: "Slow down.", retryAfter: 30 });
    type("zzzz");
    await wait();
    expect(screen.getByRole("alert").textContent).toBe("You're searching too quickly. Try again in 30 seconds.");
  });

  it("returns to the list on Escape or when the box is cleared", async () => {
    mockSearch.mockResolvedValue(found([result(1)]));
    render(<Search />);
    type("paper");
    await wait();
    fireEvent.keyDown(box(), { key: "Escape" });
    expect(screen.getByText("The list")).toBeTruthy();
    type("paper");
    type("");
    expect(screen.getByText("The list")).toBeTruthy();
  });

  it("drops an answer to an older text", async () => {
    let answer: (v: ReturnType<typeof found>) => void = () => {};
    mockSearch.mockReturnValueOnce(new Promise((resolve) => (answer = resolve))).mockResolvedValueOnce(found([result(2, { snippet: "newer", highlights: [] })]));
    render(<Search />);
    type("old");
    await wait();
    type("newer");
    await wait();
    await act(async () => answer(found([result(1, { snippet: "stale", highlights: [] })])));
    expect(screen.getByText("newer")).toBeTruthy();
    expect(screen.queryByText("stale")).toBeNull();
  });
});

describe("filters", () => {
  it("scopes to the conversation, and sends From and a date preset as after (RFC 3339)", async () => {
    jest.setSystemTime(new Date("2026-11-10T12:00:00Z"));
    mockSearch.mockResolvedValue(found([result(1)]));
    render(
      <Search
        conversationId={groupID}
        people={[
          { userId: adaID, fullName: "Ada Lovelace" },
          { userId: nafisaID, fullName: "Nafisa Hasan" },
        ]}
      />,
    );
    type("paper");
    await wait();
    expect(mockSearch).toHaveBeenLastCalledWith({ q: "paper", conversationId: groupID, from: undefined, after: undefined });
    const from = screen.getByRole("combobox", { name: "From" });
    expect(within(from).getAllByRole("option").map((o) => o.textContent)).toEqual(["Anyone", "You", "Nafisa Hasan"]);
    fireEvent.change(from, { target: { value: nafisaID } });
    fireEvent.change(screen.getByRole("combobox", { name: "When" }), { target: { value: "week" } });
    await wait();
    expect(mockSearch).toHaveBeenLastCalledWith({ q: "paper", conversationId: groupID, from: nafisaID, after: expect.stringMatching(/^2026-11-03T12:00:00\.\d{3}Z$/) });
  });

  it("offers From only in one conversation", async () => {
    mockSearch.mockResolvedValue(found([]));
    render(<Search />);
    type("paper");
    await wait();
    expect(screen.queryByRole("combobox", { name: "From" })).toBeNull();
    expect(screen.getByRole("combobox", { name: "When" })).toBeTruthy();
  });
});
