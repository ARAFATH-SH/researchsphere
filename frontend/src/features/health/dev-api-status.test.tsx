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
});
