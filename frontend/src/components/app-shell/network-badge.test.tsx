import { act, render, screen } from "@testing-library/react";

import { listReceivedRequests } from "@/features/social/api";
import {
  refreshPendingInvitations,
  resetPendingInvitations,
  setPendingInvitations,
} from "@/features/social/pending-invitations";

import { CurrentTabList } from "./tab-list";

const adaID = "0b8f4c1e-0000-4000-8000-000000000001";

jest.mock("next/navigation", () => ({
  usePathname: () => "/home",
  useRouter: () => ({ replace: jest.fn(), push: jest.fn() }),
}));
jest.mock("@/features/auth/session", () => ({
  useSession: () => ({
    status: "signedIn",
    user: { id: adaID, email: "ada@example.org", fullName: "Ada Lovelace", role: "researcher" },
  }),
}));
jest.mock("@/features/profile/my-profile", () => ({ useMyProfile: () => null }));
jest.mock("@/features/social/api");
const mockReceived = jest.mocked(listReceivedRequests);

function received(total: number) {
  return { ok: true as const, data: { items: [], total, page: 1, pageSize: 20 } };
}

beforeEach(() => resetPendingInvitations());
afterEach(() => jest.resetAllMocks());

describe("My Network badge", () => {
  it("shows the received requests' total on page load", async () => {
    mockReceived.mockResolvedValue(received(3));
    render(<CurrentTabList variant="top" />);

    expect(await screen.findByRole("link", { name: /^My Network\s*, 3 pending invitations$/ })).toBeTruthy();
    expect(mockReceived).toHaveBeenCalledTimes(1);
    expect(mockReceived).toHaveBeenCalledWith(1);
  });

  it("is hidden at 0", async () => {
    mockReceived.mockResolvedValue(received(0));
    render(<CurrentTabList variant="top" />);
    await act(async () => {});
    expect(screen.getByRole("link", { name: "My Network" })).toBeTruthy();
  });

  it("follows a refresh after an accept or ignore, and a count set by the network page", async () => {
    mockReceived.mockResolvedValue(received(2));
    render(<CurrentTabList variant="phone" />);
    expect(await screen.findByRole("link", { name: /^Network\s*, 2 pending invitations$/ })).toBeTruthy();

    mockReceived.mockResolvedValue(received(1));
    await act(() => refreshPendingInvitations(adaID));
    expect(screen.getByRole("link", { name: /^Network\s*, 1 pending invitations$/ })).toBeTruthy();

    act(() => setPendingInvitations(adaID, 0));
    expect(screen.getByRole("link", { name: "Network" })).toBeTruthy();
  });
});
