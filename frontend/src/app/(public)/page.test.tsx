import { render, screen, within } from "@testing-library/react";

import Page from "./page";

describe("landing page", () => {
  it("has one h1, the design's headline", () => {
    render(<Page />);
    const h1s = screen.getAllByRole("heading", { level: 1 });
    expect(h1s).toHaveLength(1);
    expect(h1s[0].textContent).toBe("Find collaborators, write together, and pick the right venue.");
  });

  it("links the hero pills to /register and /login", () => {
    render(<Page />);
    expect(screen.getByRole("link", { name: "Create your research profile" }).getAttribute("href")).toBe("/register");
    expect(screen.getByRole("link", { name: "Sign in" }).getAttribute("href")).toBe("/login");
  });

  it("shows a fictional sample profile card labelled as sample data", () => {
    render(<Page />);
    const card = screen.getByRole("figure", { name: /sample profile card of a fictional researcher/i });
    expect(within(card).getByText("Sample data")).toBeTruthy();
    expect(within(card).getByText("87% research match with you")).toBeTruthy();
    // Connect is a picture of a button, not one.
    expect(within(card).queryByRole("button")).toBeNull();
    expect(within(card).queryByRole("link")).toBeNull();
  });

  it("shows the three feature cards", () => {
    render(<Page />);
    const cards = screen.getByRole("region", { name: "What you can do" });
    const titles = within(cards)
      .getAllByRole("heading", { level: 2 })
      .map((h) => h.textContent);
    expect(titles).toEqual(["AI-matched collaborators", "Shared LaTeX workspace", "Venues that fit your paper"]);
  });

  it.each([
    "Research together is harder than it should be",
    "How it works",
    "Everything a research team needs",
    "Who it is for",
    "From a profile to a paper",
    "Frequently asked questions",
    "Start your research network",
  ])("keeps the section heading %s", (name) => {
    render(<Page />);
    expect(screen.getByRole("heading", { level: 2, name })).toBeTruthy();
  });

  it.each([
    "AI collaborator matching",
    "Real-time LaTeX editor for up to 6 co-authors",
    "Venue and event recommendations with deadlines",
    "Research network",
  ])("keeps the feature %s", (name) => {
    render(<Page />);
    expect(screen.getByRole("heading", { level: 3, name })).toBeTruthy();
  });

  it("links Join now in the call to action to /register", () => {
    render(<Page />);
    expect(screen.getByRole("link", { name: "Join now" }).getAttribute("href")).toBe("/register");
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

  it("describes the illustrations, lazy-loads them and leaves the decorative cover without alt text", () => {
    const { container } = render(<Page />);
    const images = Array.from(container.querySelectorAll("img"));
    const cover = images.filter((img) => img.getAttribute("src")?.includes("sample-cover.svg"));
    expect(cover).toHaveLength(1);
    expect(cover[0].getAttribute("alt")).toBe("");
    const illustrations = images.filter((img) => !cover.includes(img));
    expect(illustrations).toHaveLength(2);
    for (const img of illustrations) {
      expect(img.getAttribute("alt")).not.toBe("");
      expect(img.getAttribute("loading")).toBe("lazy");
    }
  });

  it("labels the story as an example and every preview as sample data", () => {
    render(<Page />);
    expect(screen.getByText("Example")).toBeTruthy();
    // The hero's profile card and the three product previews.
    expect(screen.getAllByText("Sample data")).toHaveLength(4);
  });
});
