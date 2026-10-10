import { render, screen } from "@testing-library/react";

import { SiteFooter } from "./site-footer";

jest.mock("@/features/health/api-status", () => ({ ApiStatus: () => <span>API status</span> }));

describe("SiteFooter", () => {
  it("shows the footer links and the copyright", () => {
    render(<SiteFooter />);
    for (const name of ["About", "Privacy", "Terms", "Contact"]) {
      expect(screen.getByRole("link", { name })).toBeTruthy();
    }
    expect(screen.getByText("© ResearchSphere")).toBeTruthy();
  });
});
