import "@/test/dialog-polyfill";

import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { type BlockedPerson, type BlockedPersonPage, listBlockedPeople, unblockUser } from "./api";
import { BlockedPeople } from "./blocked-people";

jest.mock("./api");
// The card's digest switch has its own tests (features/messaging/digest-toggle.test.tsx).
jest.mock("@/features/messaging/api", () => ({
  getMySettings: async () => ({ ok: true, data: { unreadMessageDigest: true } }),
  setUnreadMessageDigest: async () => ({ ok: true, data: { unreadMessageDigest: true } }),
}));
const mockList = jest.mocked(listBlockedPeople);
afterEach(() => jest.resetAllMocks());

function person(n: number): BlockedPerson {
  return { userId: `user-${n}`, fullName: `Person ${n}`, photoUrl: null, headline: null, blockedAt: "2026-11-05T09:00:00Z" };
}

function page(items: BlockedPerson[], total = items.length): { ok: true; data: BlockedPersonPage } {
  return { ok: true, data: { items, page: 1, pageSize: 1, total } };
}

describe("BlockedPeople", () => {
  it("says so when nobody is blocked", async () => {
    mockList.mockResolvedValue(page([]));
    render(<BlockedPeople />);
    expect(await screen.findByText("You haven't blocked anyone.")).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Messaging & email" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Blocked people" })).toBeTruthy();
  });

  it("lists people and pages through them", async () => {
    mockList.mockResolvedValueOnce(page([person(1)], 2)).mockResolvedValueOnce(page([person(2)], 2));
    render(<BlockedPeople />);
    await screen.findByText("Person 1");
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Show more" })));
    expect(mockList).toHaveBeenLastCalledWith(2);
    expect(screen.getByText("Person 2")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Show more" })).toBeNull();
  });

  it("asks before unblocking, then takes the person off the list", async () => {
    mockList.mockResolvedValue({ ok: true, data: { items: [person(1), person(2)], page: 1, pageSize: 20, total: 2 } });
    jest.mocked(unblockUser).mockResolvedValue({ ok: true, data: undefined });
    render(<BlockedPeople />);
    fireEvent.click(await screen.findByRole("button", { name: "Unblock Person 1" }));
    expect(unblockUser).not.toHaveBeenCalled();
    const dialog = screen.getByRole("dialog", { name: "Unblock Person 1?" });
    await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Unblock" })));
    expect(unblockUser).toHaveBeenCalledWith("user-1");
    expect(screen.queryByText("Person 1")).toBeNull();
    expect(screen.getByText("Person 2")).toBeTruthy();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("offers to try again after a failure", async () => {
    mockList.mockResolvedValueOnce({ ok: false, code: "internal_error", message: "No." }).mockResolvedValueOnce(page([]));
    render(<BlockedPeople />);
    expect((await screen.findByRole("alert")).textContent).toContain("Couldn't load");
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Try again" })));
    expect(screen.getByText("You haven't blocked anyone.")).toBeTruthy();
  });
});
