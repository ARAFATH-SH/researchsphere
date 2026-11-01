import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { type Connection, type ConnectionPage, listProfileConnections } from "./api";
import { ProfileConnectionsPage } from "./profile-connections-page";

jest.mock("./api");
jest.mock("./pending-invitations");
jest.mock("@/features/auth/session", () => ({
  useSession: () => ({
    status: "signedIn",
    user: { id: adaID, email: "ada@example.org", fullName: "Ada Lovelace", role: "researcher" },
  }),
}));

const adaID = "0b8f4c1e-0000-4000-8000-000000000001";
const bobID = "0b8f4c1e-0000-4000-8000-000000000002";
const mockList = jest.mocked(listProfileConnections);

function connection(n: number, over: Partial<Connection> = {}): Connection {
  return {
    connectedAt: "2026-10-03T12:00:00Z",
    userId: `0b8f4c1e-0000-4000-8000-0000000001${String(n).padStart(2, "0")}`,
    fullName: `Person ${n}`,
    headline: `Headline ${n}`,
    institution: null,
    photoUrl: null,
    connectionStatus: "none",
    connectionRequestId: null,
    ...over,
  };
}

function page(items: Connection[], total = items.length, n = 1): ConnectionPage {
  return { items, total, page: n, pageSize: 20 };
}

afterEach(() => jest.resetAllMocks());

describe("ProfileConnectionsPage", () => {
  it("lists the user's connections with the viewer's own button on each row", async () => {
    mockList.mockResolvedValue({
      ok: true,
      data: page([
        connection(1),
        connection(2, { connectionStatus: "pending_sent", connectionRequestId: "7a1d2c3e-0000-4000-8000-000000000002" }),
        connection(3, { connectionStatus: "connected" }),
        connection(4, { userId: adaID, fullName: "Ada Lovelace", connectionStatus: "self" }),
      ]),
    });
    render(<ProfileConnectionsPage userId={bobID} />);

    const rows = within(await screen.findByRole("list", { name: "Connections" })).getAllByRole("listitem");
    expect(screen.getByRole("heading", { level: 1, name: "4 connections" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Back to profile" }).getAttribute("href")).toBe(`/profile/${bobID}`);
    expect(rows.map((li) => within(li).queryAllByRole("button").map((b) => b.textContent))).toEqual([
      ["Connect"],
      ["Pending"],
      [],
      [],
    ]);
    expect(within(rows[0]).getByText("Connected 3 Oct 2026")).toBeTruthy();
    expect(mockList).toHaveBeenCalledWith(bobID, 1);
  });

  it("pages with Show more", async () => {
    const first = Array.from({ length: 20 }, (_, i) => connection(i + 10));
    mockList.mockImplementation(async (_, n) =>
      n === 1 ? { ok: true, data: page(first, 21) } : { ok: true, data: page([connection(40)], 21, 2) },
    );
    render(<ProfileConnectionsPage userId={bobID} />);
    await screen.findByRole("list", { name: "Connections" });

    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Show more" })));
    expect(mockList).toHaveBeenLastCalledWith(bobID, 2);
    expect(screen.getAllByRole("listitem")).toHaveLength(21);
    expect(screen.queryByRole("button", { name: "Show more" })).toBeNull();
  });

  it("says when the user has no connections", async () => {
    mockList.mockResolvedValue({ ok: true, data: page([]) });
    render(<ProfileConnectionsPage userId={bobID} />);
    expect(await screen.findByText("No connections yet.")).toBeTruthy();
  });

  it("says when the profile is not available", async () => {
    mockList.mockResolvedValue({ ok: false, code: "not_found", message: "Not found." });
    render(<ProfileConnectionsPage userId={bobID} />);
    expect(await screen.findByText("This profile does not exist or is not available.")).toBeTruthy();
  });
});
