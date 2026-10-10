import { render, screen } from "@testing-library/react";

import { Container, type ContainerSize } from "./Container";

describe("Container", () => {
  it.each<[ContainerSize, string | null]>([
    ["marketing", "max-w-marketing"],
    ["app", "max-w-page"],
    ["full", null],
  ])("sets the %s width", (size, widthClass) => {
    render(<Container size={size}>content</Container>);
    const el = screen.getByText("content");
    expect(el.className).toContain("mx-auto");
    expect(el.className).toContain("w-full");
    if (widthClass) {
      expect(el.className).toContain(widthClass);
    } else {
      expect(el.className).not.toMatch(/max-w-/);
    }
  });

  it("uses the app width by default", () => {
    render(<Container>content</Container>);
    expect(screen.getByText("content").className).toContain("max-w-page");
  });

  it("sets side padding of 16, 24 and 32px", () => {
    render(<Container>content</Container>);
    const { className } = screen.getByText("content");
    expect(className).toContain("px-4");
    expect(className).toContain("md:px-6");
    expect(className).toContain("lg:px-8");
  });

  it("renders as the given element and keeps extra classes", () => {
    render(
      <Container as="main" className="py-8">
        content
      </Container>,
    );
    const el = screen.getByRole("main");
    expect(el.tagName).toBe("MAIN");
    expect(el.className).toContain("py-8");
  });
});
