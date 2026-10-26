import { render, screen, within } from "@testing-library/react";

import { CurrentSiteFooter, SiteFooter } from "./site-footer";

jest.mock("@/features/health/api-status", () => ({ ApiStatus: () => <span>API status</span> }));
const mockPathname = jest.fn<string, []>();
jest.mock("next/navigation", () => ({ usePathname: () => mockPathname() }));

const column = (name: string) =>
  within(screen.getByRole("navigation", { name }))
    .getAllByRole("link")
    .map((l) => [l.textContent, l.getAttribute("href")]);

describe("SiteFooter", () => {
  it("has the brand, the copyright and the API status", () => {
    render(<SiteFooter />);
    expect(screen.getByRole("link", { name: "ResearchSphere" }).getAttribute("href")).toBe("/");
    expect(screen.getByText("ResearchSphere © 2026")).toBeTruthy();
    expect(screen.getByText("API status")).toBeTruthy();
  });

  it("has the three link columns, linking unbuilt pages to #", () => {
    render(<SiteFooter />);
    expect(column("About")).toEqual([
      ["About ResearchSphere", "#"],
      ["Community guidelines", "#"],
      ["Privacy & terms", "#"],
      ["Accessibility", "#"],
    ]);
    expect(column("For researchers")).toEqual([
      ["Find collaborators", "/connections"],
      ["Research projects", "/projects"],
      ["Events & venues", "/events"],
      ["LaTeX workspace", "#"],
    ]);
    expect(column("Platform")).toEqual([
      ["How AI matching works", "#"],
      ["Publications & ORCID", "#"],
      ["Safety center", "#"],
      ["Developers", "#"],
    ]);
  });

  it("has the help links, with the settings at /settings/account", () => {
    render(<SiteFooter />);
    expect(screen.getByRole("link", { name: "Manage your account and privacy" }).getAttribute("href")).toBe(
      "/settings/account",
    );
    expect(screen.getByText("Go to your Settings.")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Questions?" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Report a problem" })).toBeTruthy();
  });

  it("offers English as the only language (SRS 2.5)", () => {
    render(<SiteFooter />);
    const select = screen.getByRole("combobox", { name: "Select language" });
    expect(within(select).getAllByRole("option").map((o) => o.textContent)).toEqual(["English (English)"]);
  });

  it("uses six columns, three at 960px and two at 560px", () => {
    render(<SiteFooter />);
    const grid = screen.getByRole("contentinfo", { name: "Site footer" }).firstElementChild!;
    expect(grid.className).toContain("max-[960px]:grid-cols-3");
    expect(grid.className).toContain("max-[560px]:grid-cols-2");
  });
});

describe("CurrentSiteFooter", () => {
  it.each([
    ["/home", true],
    ["/profile", true],
    ["/projects/123/workspace", false],
  ])("on %s shows the footer: %s", (path, shown) => {
    mockPathname.mockReturnValue(path);
    render(<CurrentSiteFooter />);
    expect(screen.queryByRole("contentinfo") !== null).toBe(shown);
  });
});
