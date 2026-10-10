import { render, screen } from "@testing-library/react";

import Page from "./page";

describe("landing page", () => {
  it("has one h1, the headline", () => {
    render(<Page />);
    const h1s = screen.getAllByRole("heading", { level: 1 });
    expect(h1s).toHaveLength(1);
    expect(h1s[0].textContent).toBe("Find collaborators. Write papers together.");
  });

  it.each([
    "Research together is harder than it should be",
    "How it works",
    "Everything a research team needs",
    "Who it is for",
    "From a profile to a paper",
    "Frequently asked questions",
    "Start your research network",
  ])("shows the section heading %s", (name) => {
    render(<Page />);
    expect(screen.getByRole("heading", { level: 2, name })).toBeTruthy();
  });

  it.each([
    "AI collaborator matching",
    "Real-time LaTeX editor for up to 6 co-authors",
    "Venue and event recommendations with deadlines",
    "Research network",
  ])("shows the feature %s", (name) => {
    render(<Page />);
    expect(screen.getByRole("heading", { level: 3, name })).toBeTruthy();
  });

  it("links every Join now to /register and Sign in to /login", () => {
    render(<Page />);
    const joins = screen.getAllByRole("link", { name: "Join now" });
    expect(joins).toHaveLength(2);
    for (const link of joins) {
      expect(link.getAttribute("href")).toBe("/register");
    }
    expect(screen.getByRole("link", { name: "Sign in" }).getAttribute("href")).toBe("/login");
  });

  it("asks the five FAQ questions in expandable entries", () => {
    const { container } = render(<Page />);
    expect(container.querySelectorAll("details")).toHaveLength(5);
    for (const q of [
      "Is it free?",
      "Who can see my profile?",
      "How does AI matching work?",
      "Can I use my existing LaTeX files?",
      "Is my data used to train AI?",
    ]) {
      expect(screen.getByRole("heading", { level: 3, name: q })).toBeTruthy();
    }
  });

  it("gives every image alt text and loads only the hero eagerly", () => {
    render(<Page />);
    const images = screen.getAllByRole("img").filter((el) => el.tagName === "IMG");
    expect(images).toHaveLength(3);
    for (const img of images) {
      expect(img.getAttribute("alt")).not.toBe("");
    }
    const lazy = images.filter((img) => img.getAttribute("loading") === "lazy");
    expect(lazy).toHaveLength(2);
  });

  it("labels the story as an example and the previews as sample data", () => {
    render(<Page />);
    expect(screen.getByText("Example")).toBeTruthy();
    expect(screen.getAllByText("Sample data")).toHaveLength(3);
  });
});
