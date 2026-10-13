import { render, screen, within } from "@testing-library/react";

import { AppShell } from "./app-shell";

const mockPathname = jest.fn<string, []>();
jest.mock("next/navigation", () => ({ usePathname: () => mockPathname(), useRouter: () => ({ replace: jest.fn() }) }));
jest.mock("@/features/auth/session", () => ({ useSession: () => ({ status: "loading" }) }));
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

  it("has a search box that goes to /search", () => {
    mockPathname.mockReturnValue("/home");
    render(<AppShell>x</AppShell>);
    const form = screen.getByRole("search");
    expect(form.getAttribute("action")).toBe("/search");
    expect(within(form).getByLabelText("Search").getAttribute("name")).toBe("q");
  });
});
