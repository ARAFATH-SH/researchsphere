import { render, screen, within } from "@testing-library/react";

import { PublicHeader } from "./public-header";

describe("PublicHeader", () => {
  it("has the logo linking home, and Join now and Sign in on the right", () => {
    render(<PublicHeader />);
    expect(screen.getByRole("link", { name: "ResearchSphere" }).getAttribute("href")).toBe("/");
    const account = within(screen.getByRole("navigation", { name: "Account" }));
    expect(account.getByRole("link", { name: "Join now" }).getAttribute("href")).toBe("/register");
    expect(account.getByRole("link", { name: "Sign in" }).getAttribute("href")).toBe("/login");
  });
});
