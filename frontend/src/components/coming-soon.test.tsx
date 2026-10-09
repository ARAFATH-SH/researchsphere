import { render, screen } from "@testing-library/react";

import { ComingSoon } from "./coming-soon";

describe("ComingSoon", () => {
  it("shows the title, Coming soon and the requirements", () => {
    render(<ComingSoon title="Events" purpose="Browse venues." requirements={["FR-14"]} />);
    expect(screen.getByRole("heading", { name: "Events" })).toBeTruthy();
    expect(screen.getByText("Coming soon")).toBeTruthy();
    expect(screen.getByText("Built in a later slice: FR-14.")).toBeTruthy();
  });
});
