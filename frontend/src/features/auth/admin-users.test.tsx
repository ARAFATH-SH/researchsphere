import { fireEvent, render, screen, within } from "@testing-library/react";

import { AdminUsers } from "./admin-users";
import { type AdminUser, listUsers } from "./api";

jest.mock("./api");
const mockListUsers = jest.mocked(listUsers);

const ada: AdminUser = {
  id: "0b8f4c1e-0000-4000-8000-000000000001",
  fullName: "Ada Lovelace",
  email: "ada@example.org",
  role: "admin",
  verified: true,
  suspended: false,
  suspensionReason: null,
  suspendedUntil: null,
  createdAt: "2026-10-01T09:30:00Z",
};
const bob: AdminUser = {
  id: "0b8f4c1e-0000-4000-8000-000000000002",
  fullName: "Bob Babbage",
  email: "bob@example.org",
  role: "researcher",
  verified: false,
  suspended: true,
  suspensionReason: null,
  suspendedUntil: null,
  createdAt: "2026-09-15T12:00:00Z",
};

describe("AdminUsers", () => {
  afterEach(() => jest.resetAllMocks());

  it("links each user to their moderation page", async () => {
    mockListUsers.mockResolvedValue({ ok: true, data: { items: [ada, bob], page: 1, pageSize: 20, total: 2 } });
    render(<AdminUsers />);
    const link = await screen.findByRole("link", { name: "Bob Babbage" });
    expect(link.getAttribute("href")).toBe(`/admin/users/${bob.id}`);
  });

  it("shows every column for each user", async () => {
    mockListUsers.mockResolvedValue({ ok: true, data: { items: [ada, bob], page: 1, pageSize: 20, total: 2 } });
    render(<AdminUsers />);
    const table = await screen.findByRole("table");
    expect(within(table).getAllByRole("columnheader").map((h) => h.textContent)).toEqual([
      "Name",
      "Email",
      "Role",
      "Verified",
      "Status",
      "Joined",
    ]);
    const rows = within(table).getAllByRole("row").slice(1);
    const cells = rows.map((r) => within(r).getAllByRole("cell").map((c) => c.textContent));
    expect(cells).toEqual([
      ["Ada Lovelace", "ada@example.org", "Admin", "Yes", "Active", "1 Oct 2026"],
      ["Bob Babbage", "bob@example.org", "Researcher", "No", "Suspended", "15 Sept 2026"],
    ]);
    expect(screen.getByText("2 accounts")).toBeTruthy();
    // Read only until slice 27: no suspend action.
    expect(screen.queryByRole("button", { name: /suspend/i })).toBeNull();
  });

  it("pages through the users", async () => {
    mockListUsers.mockImplementation(async (page) => ({
      ok: true,
      data: { items: [page === 1 ? ada : bob], page, pageSize: 20, total: 21 },
    }));
    render(<AdminUsers />);
    expect(await screen.findByText("Page 1 of 2")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Previous" }) as HTMLButtonElement).disabled).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(await screen.findByText("Page 2 of 2")).toBeTruthy();
    expect(screen.getByText("Bob Babbage")).toBeTruthy();
    expect(mockListUsers).toHaveBeenLastCalledWith(2, 20);
    expect((screen.getByRole("button", { name: "Next" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("shows the server's message when the list is refused", async () => {
    mockListUsers.mockResolvedValue({ ok: false, code: "forbidden", message: "You do not have permission to do this." });
    render(<AdminUsers />);
    expect((await screen.findByRole("alert")).textContent).toBe("You do not have permission to do this.");
    expect(screen.queryByRole("table")).toBeNull();
  });
});
