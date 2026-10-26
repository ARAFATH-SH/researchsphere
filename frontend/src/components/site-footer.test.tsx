import { render, screen, within } from "@testing-library/react";

import { CurrentSiteFooter, footerFor, SiteFooter } from "./site-footer";

const mockPathname = jest.fn<string, []>();
jest.mock("next/navigation", () => ({ usePathname: () => mockPathname() }));

const column = (name: string) =>
  within(screen.getByRole("navigation", { name }))
    .getAllByRole("link")
    .map((l) => [l.textContent, l.getAttribute("href")]);

describe("SiteFooter", () => {
  it("has the brand and the copyright, but no API status (not in the design)", () => {
    render(<SiteFooter />);
    expect(screen.getByRole("link", { name: "ResearchSphere" }).getAttribute("href")).toBe("/");
    expect(screen.getByText("ResearchSphere © 2026")).toBeTruthy();
    expect(screen.queryByText(/API:/)).toBeNull();
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

describe("SiteFooter compact", () => {
  it("is a bordered card with the design's links and the copyright", () => {
    render(<SiteFooter variant="compact" />);
    const footer = screen.getByRole("contentinfo", { name: "Site footer" });
    expect(footer.className).toContain("rounded-card");
    expect(footer.className).toContain("border-rs-nav-border");
    expect(
      within(footer)
        .getAllByRole("link")
        .map((l) => [l.textContent, l.getAttribute("href")]),
    ).toEqual([
      ["About", "#"],
      ["Accessibility", "#"],
      ["Help Center", "#"],
      ["Privacy & terms", "#"],
      ["Settings", "/settings/account"],
      ["Safety center", "#"],
    ]);
    expect(within(footer).getByText("ResearchSphere © 2026")).toBeTruthy();
    expect(within(footer).queryByRole("combobox")).toBeNull();
  });
});

describe("footerFor", () => {
  it.each([
    ["/home", "compact"],
    ["/messaging", "none"],
    ["/messaging/abc", "none"],
    ["/projects/123/workspace", "none"],
    ["/projects/123", "full"],
    ["/profile", "full"],
    ["/settings/account", "full"],
    ["/homework", "full"],
  ])("gives %s the %s footer", (path, variant) => {
    expect(footerFor(path)).toBe(variant);
  });
});

describe("CurrentSiteFooter", () => {
  it.each([
    ["/home", false],
    ["/messaging", false],
    ["/profile", true],
    ["/projects/123/workspace", false],
  ])("on %s shows the full footer: %s", (path, shown) => {
    mockPathname.mockReturnValue(path);
    render(<CurrentSiteFooter />);
    expect(screen.queryByRole("contentinfo") !== null).toBe(shown);
  });
});
