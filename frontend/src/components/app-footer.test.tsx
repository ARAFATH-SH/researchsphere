import { render, screen, within } from "@testing-library/react";

import { AppFooter, CurrentAppFooter } from "./app-footer";

const mockPathname = jest.fn<string, []>();
jest.mock("next/navigation", () => ({ usePathname: () => mockPathname() }));

describe("AppFooter", () => {
  it("has the three link columns", () => {
    render(<AppFooter />);
    const nav = within(screen.getByRole("navigation", { name: "Footer" }));
    expect(nav.getAllByRole("list")).toHaveLength(3);
    expect(nav.getAllByRole("link").map((l) => l.textContent)).toEqual([
      "About",
      "Accessibility",
      "Help Center",
      "Privacy Policy",
      "Terms of Use",
      "Community Guidelines",
      "AI transparency",
      "Contact",
      "Careers",
    ]);
  });

  it("links the settings help row to /settings/account", () => {
    render(<AppFooter />);
    expect(screen.getByText("Manage your account and privacy")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Go to your Settings" }).getAttribute("href")).toBe("/settings/account");
    expect(screen.getByRole("link", { name: "Visit our Help Center" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Learn how recommendations are made" })).toBeTruthy();
  });

  it("offers English as the only language and shows the copyright", () => {
    render(<AppFooter />);
    const select = screen.getByRole("combobox", { name: "Language" });
    expect(within(select).getAllByRole("option").map((o) => o.textContent)).toEqual(["English"]);
    expect(screen.getByText("ResearchSphere © 2026")).toBeTruthy();
  });
});

describe("CurrentAppFooter", () => {
  it.each([
    ["/home", true],
    ["/profile", true],
    ["/projects/123/workspace", false],
  ])("on %s shows the footer: %s", (path, shown) => {
    mockPathname.mockReturnValue(path);
    render(<CurrentAppFooter />);
    expect(screen.queryByRole("navigation", { name: "Footer" }) !== null).toBe(shown);
  });
});
