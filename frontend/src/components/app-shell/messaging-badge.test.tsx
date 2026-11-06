import { act, render, screen, within } from "@testing-library/react";

import { getMessageUnreadCount } from "@/features/messaging/api";
import { refreshUnreadMessages, resetUnreadMessages, setUnreadMessages } from "@/features/messaging/unread-count";

import { CurrentTabList, PhoneMessagingLink } from "./tab-list";

const adaID = "0b8f4c1e-0000-4000-8000-000000000001";

jest.mock("next/navigation", () => ({
  usePathname: () => "/home",
  useRouter: () => ({ replace: jest.fn(), push: jest.fn() }),
}));
jest.mock("@/features/auth/session", () => ({
  useSession: () => ({
    status: "signedIn",
    user: { id: "0b8f4c1e-0000-4000-8000-000000000001", email: "ada@example.org", fullName: "Ada Lovelace", role: "researcher" },
  }),
}));
jest.mock("@/features/profile/my-profile", () => ({ useMyProfile: () => null }));
jest.mock("@/features/social/pending-invitations", () => ({ usePendingInvitations: () => 0 }));
jest.mock("@/features/notification/unread-count", () => ({ useUnreadNotifications: () => 0 }));
jest.mock("@/features/messaging/api");

const mockCount = jest.mocked(getMessageUnreadCount);
const setCounts = async (total: number, requests: number) => {
  mockCount.mockResolvedValue({ ok: true, data: { total, requests } });
  await act(async () => refreshUnreadMessages(adaID));
};

beforeEach(() => resetUnreadMessages());
afterEach(() => jest.resetAllMocks());

describe("Messaging badge", () => {
  it("is hidden at 0", async () => {
    render(<CurrentTabList variant="top" />);
    await setCounts(0, 0);
    expect(within(screen.getByRole("link", { name: "Messaging" })).queryByText("0")).toBeNull();
  });

  it("adds unread messages and requests", async () => {
    render(<CurrentTabList variant="top" />);
    await setCounts(2, 1);
    const link = screen.getByRole("link", { name: /^Messaging\s*, 3 unread$/ });
    expect(within(link).getByText("3").getAttribute("aria-hidden")).toBe("true");
  });

  it("keeps the last count when a refresh fails", async () => {
    render(<CurrentTabList variant="top" />);
    await setCounts(2, 0);
    mockCount.mockResolvedValue({ ok: false, code: "network_error", message: "x" });
    await act(async () => refreshUnreadMessages(adaID));
    expect(within(screen.getByRole("link", { name: /Messaging/ })).getByText("2")).toBeTruthy();
  });

  it("shows on the phone top bar's icon too", async () => {
    render(<PhoneMessagingLink />);
    expect(screen.getByRole("link", { name: "Messaging" })).toBeTruthy();
    await setCounts(4, 0);
    const link = screen.getByRole("link", { name: "Messaging, 4 unread" });
    expect(within(link).getByText("4")).toBeTruthy();
  });
});

describe("Messaging badge from an event", () => {
  it("shows the counts unread.changed gives", async () => {
    render(<PhoneMessagingLink />);
    await act(async () => setUnreadMessages(adaID, { total: 2, requests: 1 }));
    expect(screen.getByRole("link", { name: /Messaging/ }).textContent).toContain("3");
  });
});
