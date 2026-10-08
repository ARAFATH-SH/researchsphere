import { render, screen } from "@testing-library/react";

import { checkHealth } from "./api";
import { ApiStatus } from "./api-status";

jest.mock("./api");
const mockCheckHealth = jest.mocked(checkHealth);

describe("ApiStatus", () => {
  afterEach(() => jest.resetAllMocks());

  it("shows checking before the first answer", () => {
    mockCheckHealth.mockReturnValue(new Promise(() => {}));
    render(<ApiStatus />);
    expect(screen.getByRole("status").textContent).toBe("API: checking");
  });

  it.each([
    { name: "up when the API answers ok", state: "up" as const, want: "API: up" },
    { name: "down when the API is unavailable", state: "down" as const, want: "API: down" },
  ])("shows $name", async ({ state, want }) => {
    mockCheckHealth.mockResolvedValue(state);
    render(<ApiStatus />);
    expect(await screen.findByText(want)).toBe(screen.getByRole("status"));
    expect(mockCheckHealth).toHaveBeenCalledTimes(1);
  });
});
