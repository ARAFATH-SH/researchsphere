import { render, screen, within } from "@testing-library/react";

import { AccountSettings } from "./account-settings";

jest.mock("./api");

describe("AccountSettings", () => {
  it("shows the built sections in the design's order", () => {
    render(<AccountSettings />);
    const nav = screen.getByRole("navigation", { name: "Settings" });
    expect(within(nav).getAllByRole("link").map((a) => a.textContent)).toEqual(["Sign-in & security", "Close account"]);
    expect(within(nav).getByRole("link", { name: "Sign-in & security" }).getAttribute("aria-current")).toBe("true");
    expect(screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent)).toEqual([
      "Change password",
      "Delete account",
    ]);
  });

  it("does not show the sections that are not built yet", () => {
    render(<AccountSettings />);
    for (const name of [/messaging/i, /ai assistant/i, /devices/i, /signed in/i]) {
      expect(screen.queryByRole("link", { name })).toBeNull();
      expect(screen.queryByRole("heading", { name })).toBeNull();
    }
  });
});
