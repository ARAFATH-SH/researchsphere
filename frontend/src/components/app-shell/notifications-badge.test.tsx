import { act, render, screen, within } from "@testing-library/react";

import { resetUnreadNotifications, setUnreadNotifications } from "@/features/notification/unread-count";

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
// The My Network badge has its own tests (network-badge.test.tsx).
jest.mock("@/features/social/pending-invitations", () => ({ usePendingInvitations: () => 0 }));
// The count comes from the badge poller (features/notification/badges.test.tsx); here it is set.
jest.mock("@/features/notification/api");

beforeEach(() => resetUnreadNotifications());

describe("Notifications badge", () => {
  it("is hidden at 0", () => {
    render(<CurrentTabList variant="top" />);
    act(() => setUnreadNotifications(adaID, 0));
    const link = screen.getByRole("link", { name: "Notifications" });
    expect(within(link).queryByText("0")).toBeNull();
  });

  it("shows the count with an accessible label", () => {
    render(<CurrentTabList variant="top" />);
    act(() => setUnreadNotifications(adaID, 3));
    const link = screen.getByRole("link", { name: /^Notifications\s*, 3 unread notifications$/ });
    const badge = within(link).getByText("3");
    expect(badge.getAttribute("aria-hidden")).toBe("true");
    expect(badge.className).toContain("bg-rs-danger");
  });

  it("shows 99+ above 99 and keeps the full number for screen readers", () => {
    render(<CurrentTabList variant="phone" />);
    act(() => setUnreadNotifications(adaID, 150));
    const link = screen.getByRole("link", { name: /^Alerts\s*, 150 unread notifications$/ });
    expect(within(link).getByText("99+")).toBeTruthy();
  });

  it("shows 99 as it is", () => {
    render(<CurrentTabList variant="top" />);
    act(() => setUnreadNotifications(adaID, 99));
    expect(within(screen.getByRole("link", { name: /Notifications/ })).getByText("99")).toBeTruthy();
  });

  it("ignores a count kept for another user", () => {
    render(<CurrentTabList variant="top" />);
    act(() => setUnreadNotifications("0b8f4c1e-0000-4000-8000-000000000009", 7));
    expect(screen.getByRole("link", { name: "Notifications" })).toBeTruthy();
  });
});
