import { render, screen, within } from "@testing-library/react";

import { AdminConsole } from "./admin-console";
import { listUsers } from "./api";

jest.mock("./api");
jest.mock("@/features/moderation/api", () => ({
  listMessageReports: jest.fn(async () => ({ ok: true, data: { items: [], page: 1, pageSize: 20, total: 0 } })),
}));
jest.mock("@/features/event/api", () => ({
  listResearchFields: jest.fn(async () => ({ ok: true, data: [] })),
  listVenues: jest.fn(async () => ({
    ok: true,
    data: { items: [], page: 1, pageSize: 20, total: 0, facets: { types: [], fields: [] } },
  })),
  getVenueSyncStatus: jest.fn(async () => ({ ok: true, data: { daily: false, sources: [] } })),
}));
const mockListUsers = jest.mocked(listUsers);

describe("AdminConsole", () => {
  afterEach(() => jest.resetAllMocks());

  it("shows only the built sections, in the design's order", async () => {
    mockListUsers.mockResolvedValue({ ok: true, data: { items: [], page: 1, pageSize: 20, total: 0 } });
    render(<AdminConsole />);
    await screen.findByText("No venues match.");
    await screen.findByText("No open reports.");
    await screen.findByText("Daily schedule off");
    const nav = screen.getByRole("navigation", { name: "Admin sections" });
    const links = within(nav).getAllByRole("link");
    expect(links.map((a) => a.textContent)).toEqual(["Venues", "Reported messages", "Users"]);
    expect(links[0].getAttribute("aria-current")).toBe("true");
    expect(screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent)).toEqual([
      "Venues",
      "Automatic venue import",
      "Reported conversations (0)",
      "Users",
    ]);
    for (const name of [/system health|background jobs/i, /moderation|remove content/i]) {
      expect(screen.queryByRole("link", { name })).toBeNull();
      expect(screen.queryByRole("heading", { name })).toBeNull();
    }
  });
});
