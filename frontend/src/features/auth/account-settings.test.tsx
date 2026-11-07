import { render, screen, within } from "@testing-library/react";

import { AccountSettings } from "./account-settings";

jest.mock("./api");
jest.mock("@/features/social/api", () => ({
  listBlockedPeople: jest.fn(async () => ({ ok: true, data: { items: [], page: 1, pageSize: 20, total: 0 } })),
}));

describe("AccountSettings", () => {
  it("shows the built sections in the design's order", async () => {
    render(<AccountSettings />);
    await screen.findByText("You haven't blocked anyone.");
    const nav = screen.getByRole("navigation", { name: "Settings" });
    expect(within(nav).getAllByRole("link").map((a) => a.textContent)).toEqual([
      "Sign-in & security",
      "Messaging & email",
      "Close account",
    ]);
    expect(within(nav).getByRole("link", { name: "Sign-in & security" }).getAttribute("aria-current")).toBe("true");
    expect(screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent)).toEqual([
      "Change password",
      "Messaging & email",
      "Delete account",
    ]);
  });

  it("does not show the sections that are not built yet", async () => {
    render(<AccountSettings />);
    await screen.findByText("You haven't blocked anyone.");
    for (const name of [/ai assistant/i, /devices/i, /signed in/i]) {
      expect(screen.queryByRole("link", { name })).toBeNull();
      expect(screen.queryByRole("heading", { name })).toBeNull();
    }
  });

  it("hides the digest toggle, which has no API yet (FR-16.11)", async () => {
    render(<AccountSettings />);
    await screen.findByText("You haven't blocked anyone.");
    expect(screen.queryByText("Email me unread messages")).toBeNull();
  });
});
