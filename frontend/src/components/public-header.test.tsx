import { render, screen, within } from "@testing-library/react";

import { PublicHeader } from "./public-header";

describe("PublicHeader", () => {
  it("has the logo linking home, then Events, Sign in and Join now", () => {
    render(<PublicHeader />);
    expect(screen.getByRole("link", { name: "ResearchSphere" }).getAttribute("href")).toBe("/");
    const visitor = within(screen.getByRole("navigation", { name: "Visitor" }));
    expect(visitor.getAllByRole("link").map((l) => [l.textContent, l.getAttribute("href")])).toEqual([
      ["Events", "/events"],
      ["Sign in", "/login"],
      ["Join now", "/register"],
    ]);
  });

  it("hides the Events tab under 640px, as the design does", () => {
    render(<PublicHeader />);
    expect(screen.getByRole("link", { name: "Events" }).className).toContain("max-[640px]:hidden");
  });
});
