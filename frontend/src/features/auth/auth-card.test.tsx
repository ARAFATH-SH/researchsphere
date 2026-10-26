import { render, screen } from "@testing-library/react";

import { AuthCard } from "./auth-card";

describe("AuthCard", () => {
  it("names the page with its title as the h1 and labels the card with it", () => {
    render(<AuthCard title="Sign in">form</AuthCard>);
    const h1 = screen.getByRole("heading", { level: 1, name: "Sign in" });
    expect(screen.getByRole("region", { name: "Sign in" }).contains(h1)).toBe(true);
  });

  it("links to sign in and to join only when asked", () => {
    const { rerender } = render(<AuthCard title="Join ResearchSphere" signInLink />);
    expect(screen.getByRole("link", { name: "Sign in" }).getAttribute("href")).toBe("/login");
    expect(screen.queryByRole("link", { name: "Join now" })).toBeNull();

    rerender(<AuthCard title="Sign in" joinLink />);
    expect(screen.getByRole("link", { name: "Join now" }).getAttribute("href")).toBe("/register");
    expect(screen.queryByRole("link", { name: "Sign in" })).toBeNull();
  });
});
