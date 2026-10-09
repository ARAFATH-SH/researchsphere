import { render, screen, within } from "@testing-library/react";

import { AppShell } from "./app-shell";

const mockPathname = jest.fn<string, []>();
jest.mock("next/navigation", () => ({ usePathname: () => mockPathname() }));

const want = [
  ["Home", "/home"],
  ["Network", "/connections"],
  ["Projects", "/projects"],
  ["Events", "/events"],
  ["Notifications", "/notifications"],
  ["Me", "/profile"],
];

describe("AppShell", () => {
  it("has the six tabs in order, in the top bar and the bottom bar", () => {
    mockPathname.mockReturnValue("/home");
    render(<AppShell>content</AppShell>);
    // jsdom applies no CSS, so both bars are present; CSS shows one per screen width.
    const bars = screen.getAllByRole("navigation", { name: "Main" });
    expect(bars).toHaveLength(2);
    for (const bar of bars) {
      const links = within(bar).getAllByRole("link");
      expect(links.map((l) => [l.textContent, l.getAttribute("href")])).toEqual(want);
    }
    expect(screen.getByText("content")).toBeTruthy();
  });

  it.each([
    ["/projects", "Projects"],
    ["/projects/123/workspace", "Projects"],
    ["/profile", "Me"],
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
