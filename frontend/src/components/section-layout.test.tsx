import { fireEvent, render, screen, within } from "@testing-library/react";

import { SectionCard, SectionLayout } from "./section-layout";

const sections = [
  { id: "first", label: "First section" },
  { id: "second", label: "Second section" },
];

function renderLayout() {
  render(
    <SectionLayout title="Settings" navLabel="Settings" sections={sections}>
      <SectionCard id="first" title="First">
        one
      </SectionCard>
      <SectionCard id="second" title="Second" tone="danger">
        two
      </SectionCard>
    </SectionLayout>,
  );
  return screen.getByRole("navigation", { name: "Settings" });
}

describe("SectionLayout", () => {
  it("links to each section in order and marks the first as current", () => {
    const nav = renderLayout();
    const links = within(nav).getAllByRole("link");
    expect(links.map((a) => [a.textContent, a.getAttribute("href")])).toEqual([
      ["First section", "#first"],
      ["Second section", "#second"],
    ]);
    expect(links[0].getAttribute("aria-current")).toBe("true");
    expect(links[1].getAttribute("aria-current")).toBeNull();
  });

  it("marks the section whose link was clicked", () => {
    const nav = renderLayout();
    fireEvent.click(within(nav).getByRole("link", { name: "Second section" }));
    expect(within(nav).getByRole("link", { name: "Second section" }).getAttribute("aria-current")).toBe("true");
    expect(within(nav).getByRole("link", { name: "First section" }).getAttribute("aria-current")).toBeNull();
  });

  it("names each section card by its heading, at the id the nav links to", () => {
    renderLayout();
    const card = screen.getByRole("region", { name: "Second" });
    expect(card.id).toBe("second");
    expect(within(card).getByRole("heading", { level: 2 }).className).toContain("text-rs-danger-strong");
  });
});
