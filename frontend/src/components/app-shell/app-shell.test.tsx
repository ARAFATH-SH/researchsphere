import { render, screen, within } from "@testing-library/react";

import type { Session } from "@/features/auth/session";

import { AppShell } from "./app-shell";

const mockPathname = jest.fn<string, []>();
jest.mock("next/navigation", () => ({ usePathname: () => mockPathname(), useRouter: () => ({ replace: jest.fn() }) }));
const mockSession = jest.fn<Session, []>(() => ({ status: "loading" }));
jest.mock("@/features/auth/session", () => ({ useSession: () => mockSession() }));
jest.mock("@/features/auth/api");

const want = [
  ["Home", "/home"],
  ["Network", "/connections"],
  ["Projects", "/projects"],
  ["Events", "/events"],
  ["Notifications", "/notifications"],
];

describe("AppShell", () => {
  it("has the five page tabs and the Me menu, in the top bar and the bottom bar", () => {
    mockPathname.mockReturnValue("/home");
    render(<AppShell>content</AppShell>);
    // jsdom applies no CSS, so both bars are present; CSS shows one per screen width.
    const bars = screen.getAllByRole("navigation", { name: "Main" });
    expect(bars).toHaveLength(2);
    for (const bar of bars) {
      const links = within(bar).getAllByRole("link");
      // The Me menu is closed, so its "View profile" link is hidden.
      expect(links.map((l) => [l.textContent, l.getAttribute("href")])).toEqual(want);
      expect(within(bar).getByRole("button", { name: "Me" }).getAttribute("aria-expanded")).toBe("false");
    }
    expect(screen.getByText("content")).toBeTruthy();
  });

  it.each([
    ["/projects", "Projects"],
    ["/projects/123/workspace", "Projects"],
  ])("marks the tab for %s as current", (path, label) => {
    mockPathname.mockReturnValue(path);
    render(<AppShell>x</AppShell>);
    const bar = screen.getAllByRole("navigation", { name: "Main" })[0];
    const current = within(bar)
      .getAllByRole("link")
      .filter((l) => l.getAttribute("aria-current") === "page");
    expect(current.map((l) => l.textContent)).toEqual([label]);
  });

  describe("Admin tab", () => {
    const signedInAs = (role: "researcher" | "admin"): Session => ({
      status: "signedIn",
      user: { id: "0b8f4c1e-0000-4000-8000-000000000001", email: "ada@example.org", fullName: "Ada Lovelace", role },
    });
    afterEach(() => mockSession.mockReturnValue({ status: "loading" }));

    it("is hidden from a researcher", () => {
      mockPathname.mockReturnValue("/home");
      mockSession.mockReturnValue(signedInAs("researcher"));
      render(<AppShell>x</AppShell>);
      expect(screen.queryAllByRole("link", { name: "Admin" })).toHaveLength(0);
    });

    it("is hidden while the session is loading", () => {
      mockPathname.mockReturnValue("/home");
      render(<AppShell>x</AppShell>);
      expect(screen.queryAllByRole("link", { name: "Admin" })).toHaveLength(0);
    });

    it("shows to an administrator in both bars, before Me", () => {
      mockPathname.mockReturnValue("/admin");
      mockSession.mockReturnValue(signedInAs("admin"));
      render(<AppShell>x</AppShell>);
      for (const bar of screen.getAllByRole("navigation", { name: "Main" })) {
        const links = within(bar).getAllByRole("link");
        expect(links.map((l) => [l.textContent, l.getAttribute("href")])).toEqual([...want, ["Admin", "/admin"]]);
        expect(within(bar).getByRole("link", { name: "Admin" }).getAttribute("aria-current")).toBe("page");
      }
    });
  });

  it("puts the signed-in footer inside the main column, after the page", () => {
    mockPathname.mockReturnValue("/home");
    render(<AppShell>content</AppShell>);
    const main = within(screen.getByRole("main"));
    expect(main.getByRole("navigation", { name: "Footer" })).toBeTruthy();
    expect(main.getByRole("link", { name: "Go to your Settings" }).getAttribute("href")).toBe("/settings/account");
  });

  it("has a search box that goes to /search", () => {
    mockPathname.mockReturnValue("/home");
    render(<AppShell>x</AppShell>);
    const form = screen.getByRole("search");
    expect(form.getAttribute("action")).toBe("/search");
    expect(within(form).getByLabelText("Search").getAttribute("name")).toBe("q");
  });
});
