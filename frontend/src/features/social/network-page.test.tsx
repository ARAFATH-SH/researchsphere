import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { useSession } from "@/features/auth/session";

import {
  acceptConnectionRequest,
  cancelConnectionRequest,
  type Connection,
  type ConnectionPage,
  type ConnectionRequest,
  type ConnectionRequestPage,
  listMyConnections,
  listReceivedRequests,
  listSentRequests,
  rejectConnectionRequest,
  removeConnection,
} from "./api";
import { NetworkPage } from "./network-page";
import { setPendingInvitations } from "./pending-invitations";

jest.mock("./api");
jest.mock("./pending-invitations");
jest.mock("@/features/notification/badges");
jest.mock("@/features/auth/session");
jest.mock("@/features/home/recommendations", () => ({
  Recommendations: () => <p>Recommendations are coming soon.</p>,
}));
jest.mock("next/navigation", () => ({ usePathname: () => "/network" }));

const adaID = "0b8f4c1e-0000-4000-8000-000000000001";

const mockReceived = jest.mocked(listReceivedRequests);
const mockSent = jest.mocked(listSentRequests);
const mockConnections = jest.mocked(listMyConnections);
const mockAccept = jest.mocked(acceptConnectionRequest);
const mockReject = jest.mocked(rejectConnectionRequest);
const mockCancel = jest.mocked(cancelConnectionRequest);
const mockRemove = jest.mocked(removeConnection);
const mockSetPending = jest.mocked(setPendingInvitations);

function person(n: number) {
  return {
    userId: `0b8f4c1e-0000-4000-8000-0000000001${String(n).padStart(2, "0")}`,
    fullName: `Person ${n}`,
    headline: `Headline ${n}`,
    institution: null,
    photoUrl: null,
  };
}

function request(n: number): ConnectionRequest {
  return {
    ...person(n),
    requestId: `7a1d2c3e-0000-4000-8000-0000000002${String(n).padStart(2, "0")}`,
    createdAt: new Date().toISOString(),
  };
}

function connection(n: number, connectedAt = "2026-10-03T12:00:00Z"): Connection {
  return { ...person(n), connectedAt, connectionStatus: "connected", connectionRequestId: null };
}

function requests(items: ConnectionRequest[], total = items.length, page = 1): ConnectionRequestPage {
  return { items, total, page, pageSize: 20 };
}

function connections(items: Connection[], total = items.length, page = 1): ConnectionPage {
  return { items, total, page, pageSize: 20 };
}

beforeEach(() => {
  jest.mocked(useSession).mockReturnValue({
    status: "signedIn",
    user: { id: adaID, email: "ada@example.org", fullName: "Ada Lovelace", role: "researcher" },
  });
  mockReceived.mockResolvedValue({ ok: true, data: requests([request(1), request(2)]) });
  mockSent.mockResolvedValue({ ok: true, data: requests([request(3)]) });
  mockConnections.mockResolvedValue({ ok: true, data: connections([connection(4)]) });
});
afterEach(() => jest.resetAllMocks());

async function renderPage() {
  render(<NetworkPage />);
  await screen.findByRole("list", { name: "Connections" });
}

const card = (name: RegExp | string) => screen.getByRole("region", { name });
const summary = () => screen.getByRole("region", { name: "Network summary" });
const countLink = (label: string) =>
  within(summary())
    .getAllByRole("link")
    .find((link) => link.textContent?.endsWith(label))!;

describe("NetworkPage", () => {
  it("shows the summary and the three lists", async () => {
    await renderPage();
    expect(screen.getByRole("heading", { level: 1, name: "My Network" })).toBeTruthy();
    expect(countLink("Connections").textContent).toBe("1Connections");
    expect(countLink("Connections").getAttribute("href")).toBe("#connections-heading");
    expect(countLink("Invitations").textContent).toBe("2Invitations");
    expect(countLink("Invitations").className).toContain("bg-rs-accent-soft");
    expect(countLink("Sent").textContent).toBe("1Sent");

    const invitations = card("Invitations received (2)");
    expect(within(invitations).getByRole("link", { name: "Person 1" }).getAttribute("href")).toBe(
      `/profile/${person(1).userId}`,
    );
    expect(within(invitations).getByText("Headline 1")).toBeTruthy();
    expect(within(card("Invitations sent (1)")).getByText("Sent today · Pending")).toBeTruthy();
    const conns = card("1 connection");
    expect(within(conns).getByText("Headline 4")).toBeTruthy();
    expect(within(conns).getByText("Connected 3 Oct 2026")).toBeTruthy();
    // The right column keeps the home page's placeholder card and the compact footer.
    const aside = screen.getByRole("complementary", { name: "Recommendations" });
    expect(within(aside).getByText("Recommendations are coming soon.")).toBeTruthy();
    expect(within(aside).getByRole("contentinfo", { name: "Site footer" })).toBeTruthy();
    expect(screen.queryByText(/AI matches/)).toBeNull();
    expect(mockSetPending).toHaveBeenLastCalledWith(adaID, 2);
  });

  it("moves an accepted person from Invitations to Connections without a reload", async () => {
    mockAccept.mockResolvedValue({ ok: true, data: connection(1, new Date().toISOString()) });
    await renderPage();
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Accept request from Person 1" })));

    expect(mockAccept).toHaveBeenCalledWith(request(1).requestId);
    const invitations = card("Invitations received (1)");
    expect(within(invitations).queryByText("Person 1")).toBeNull();
    const conns = card("2 connections");
    const rows = within(conns).getAllByRole("listitem");
    expect(within(rows[0]).getByRole("link", { name: "Person 1" })).toBeTruthy();
    expect(within(rows[0]).getByText("Connected today")).toBeTruthy();
    expect(countLink("Connections").textContent).toBe("2Connections");
    expect(countLink("Invitations").textContent).toBe("1Invitations");
    expect(mockSetPending).toHaveBeenLastCalledWith(adaID, 1);
    expect(mockConnections).toHaveBeenCalledTimes(1);
  });

  it("ignores an invitation", async () => {
    mockReject.mockResolvedValue({ ok: true, data: undefined });
    await renderPage();
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Ignore request from Person 2" })));

    expect(mockReject).toHaveBeenCalledWith(request(2).requestId);
    expect(within(card("Invitations received (1)")).queryByText("Person 2")).toBeNull();
    expect(card("1 connection")).toBeTruthy();
  });

  it("keeps the row and says so when accepting fails", async () => {
    mockAccept.mockResolvedValue({ ok: false, code: "not_found", message: "Gone." });
    await renderPage();
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Accept request from Person 1" })));

    const invitations = card("Invitations received (2)");
    expect(within(invitations).getByRole("alert").textContent).toBe("Couldn't accept the request. Try again.");
  });

  it("withdraws a sent invitation", async () => {
    mockCancel.mockResolvedValue({ ok: true, data: undefined });
    await renderPage();
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Withdraw request to Person 3" })));

    expect(mockCancel).toHaveBeenCalledWith(request(3).requestId);
    expect(within(card("Invitations sent (0)")).getByText("No invitations waiting for an answer.")).toBeTruthy();
  });

  it("removes a connection after asking inside its menu", async () => {
    mockRemove.mockResolvedValue({ ok: true, data: undefined });
    await renderPage();
    fireEvent.click(screen.getByRole("button", { name: "More actions for Person 4" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Remove connection" }));
    expect(screen.getByText("Remove Person 4 from your connections?")).toBeTruthy();
    expect(mockRemove).not.toHaveBeenCalled();

    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Remove" })));
    expect(mockRemove).toHaveBeenCalledWith(person(4).userId);
    expect(within(card("0 connections")).getByText(/No connections yet/)).toBeTruthy();
    expect(within(card("0 connections")).getByRole("link", { name: "Search for researchers" })).toBeTruthy();
  });

  it("shows an empty state for each list", async () => {
    mockReceived.mockResolvedValue({ ok: true, data: requests([]) });
    mockSent.mockResolvedValue({ ok: true, data: requests([]) });
    mockConnections.mockResolvedValue({ ok: true, data: connections([]) });
    render(<NetworkPage />);

    expect(await screen.findByText(/No pending invitations/)).toBeTruthy();
    expect(screen.getByText("No invitations waiting for an answer.")).toBeTruthy();
    expect(screen.getByText(/No connections yet/)).toBeTruthy();
    expect(countLink("Invitations").className).not.toContain("bg-rs-accent-soft");
    expect(screen.queryByRole("button", { name: "Show more" })).toBeNull();
  });

  it("pages a list with Show more", async () => {
    const first = Array.from({ length: 20 }, (_, i) => connection(i + 10));
    mockConnections.mockImplementation(async (page) =>
      page === 1
        ? { ok: true, data: connections(first, 21) }
        : { ok: true, data: connections([connection(40)], 21, 2) },
    );
    await renderPage();
    expect(within(card("21 connections")).getAllByRole("listitem")).toHaveLength(20);

    await act(async () => fireEvent.click(within(card("21 connections")).getByRole("button", { name: "Show more" })));
    expect(mockConnections).toHaveBeenLastCalledWith(2);
    expect(within(card("21 connections")).getAllByRole("listitem")).toHaveLength(21);
    expect(within(card("21 connections")).queryByRole("button", { name: "Show more" })).toBeNull();
  });

  it("offers a retry when a list fails to load", async () => {
    mockSent.mockResolvedValueOnce({ ok: false, code: "internal_error", message: "Oops." });
    await renderPage();
    const sent = card("Invitations sent");
    expect(within(sent).getByText("We couldn't load your sent invitations.")).toBeTruthy();

    await act(async () => fireEvent.click(within(sent).getByRole("button", { name: "Try again" })));
    expect(within(card("Invitations sent (1)")).getByText("Person 3")).toBeTruthy();
  });
});
