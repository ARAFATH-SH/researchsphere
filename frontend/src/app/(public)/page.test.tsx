import { render, screen } from "@testing-library/react";

import Page from "./page";

describe("landing page", () => {
  it("shows the headline", () => {
    render(<Page />);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Find collaborators. Write papers together.");
  });

  it("shows Join now linking to /register and Sign in linking to /login", () => {
    render(<Page />);
    expect(screen.getByRole("link", { name: "Join now" }).getAttribute("href")).toBe("/register");
    expect(screen.getByRole("link", { name: "Sign in" }).getAttribute("href")).toBe("/login");
  });
});
