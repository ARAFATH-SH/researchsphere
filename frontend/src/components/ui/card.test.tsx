import { render, screen } from "@testing-library/react";

import { Card } from "./card";

describe("Card", () => {
  it("stands out on the white page by its border and card shadow (design/screens)", () => {
    render(<Card className="p-4">content</Card>);
    const card = screen.getByText("content");
    for (const c of ["bg-rs-surface", "border", "border-rs-border", "shadow-card", "p-4"]) {
      expect(card.className.split(" ")).toContain(c);
    }
  });
});
