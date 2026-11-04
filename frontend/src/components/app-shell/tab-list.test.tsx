import { render, screen, within } from "@testing-library/react";

import { TabList } from "./tab-list";

jest.mock("next/navigation", () => ({ useRouter: () => ({ replace: jest.fn() }) }));
jest.mock("@/features/auth/session", () => ({ useSession: () => ({ status: "loading" }) }));
jest.mock("@/features/profile/my-profile", () => ({ useMyProfile: () => undefined }));

describe("TabList", () => {
  it("shows an unread badge that screen readers hear after the label", () => {
    render(<TabList pathname="/home" variant="top" counts={{ "/messaging": 2 }} />);
    const messaging = screen.getByRole("link", { name: /^Messaging\s*, 2 unread$/ });
    const badge = within(messaging).getByText("2");
    expect(badge.getAttribute("aria-hidden")).toBe("true");
    expect(badge.className).toContain("bg-rs-danger");
  });

  it("shows the Alerts badge in the phone bar but none on Post", () => {
    render(<TabList pathname="/home" variant="phone" counts={{ "/notifications": 3, "/home": 9 }} />);
    expect(screen.getByRole("link", { name: /^Alerts\s*, 3 unread notifications$/ })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Post" })).toBeTruthy();
  });

  it("shows no badge without counts", () => {
    render(<TabList pathname="/home" variant="top" />);
    expect(screen.queryByText(/unread/)).toBeNull();
    expect(screen.getByRole("link", { name: "Messaging" })).toBeTruthy();
  });
});
