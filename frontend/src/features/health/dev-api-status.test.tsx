import { render, screen } from "@testing-library/react";

import { DevApiStatus } from "./dev-api-status";

jest.mock("./api-status", () => ({ ApiStatus: () => <span>API status</span> }));


describe("DevApiStatus", () => {
  it("shows the API status in development", () => {
    jest.replaceProperty(process.env, "NODE_ENV", "development");
    render(<DevApiStatus />);
    expect(screen.getByText("API status")).toBeTruthy();
  });

  it.each(["production", "test"] as const)("shows nothing when NODE_ENV is %s", (env) => {
    jest.replaceProperty(process.env, "NODE_ENV", env);
    const { container } = render(<DevApiStatus />);
    expect(container.innerHTML).toBe("");
  });

  it("shows inline in a page's own footer, and the fixed one then hides itself", () => {
    jest.replaceProperty(process.env, "NODE_ENV", "development");
    render(
      <>
        <DevApiStatus />
        <DevApiStatus inline />
      </>,
    );
    const [fixed, inline] = screen.getAllByTestId("dev-api-status");
    expect(inline.textContent).toBe("API status");
    expect(inline.className).not.toContain("fixed");
    expect(inline.hasAttribute("data-dev-api-inline")).toBe(true);
    // jsdom does no CSS; the class hides it while the page holds an inline one.
    expect(fixed.className).toContain("[body:has([data-dev-api-inline])_&]:hidden");
  });
});
