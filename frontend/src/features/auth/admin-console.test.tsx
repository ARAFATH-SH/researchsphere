import { render, screen, within } from "@testing-library/react";

import { AdminConsole } from "./admin-console";
import { listUsers } from "./api";

jest.mock("./api");
jest.mock("@/features/moderation/api", () => ({
  listMessageReports: jest.fn(async () => ({ ok: true, data: { items: [], page: 1, pageSize: 20, total: 0 } })),
}));
const mockListUsers = jest.mocked(listUsers);

describe("AdminConsole", () => {
  afterEach(() => jest.resetAllMocks());

  it("shows only the built sections, in the design's order", async () => {
    mockListUsers.mockResolvedValue({ ok: true, data: { items: [], page: 1, pageSize: 20, total: 0 } });
    render(<AdminConsole />);
    await screen.findByRole("table");
    await screen.findByText("No open reports.");
    const nav = screen.getByRole("navigation", { name: "Admin sections" });
    const links = within(nav).getAllByRole("link");
    expect(links.map((a) => a.textContent)).toEqual(["Reported messages", "Users"]);
    expect(links[0].getAttribute("aria-current")).toBe("true");
    expect(screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent)).toEqual(["Reported conversations (0)", "Users"]);
    for (const name of [/system health|background jobs/i, /venues/i, /moderation|remove content/i]) {
      expect(screen.queryByRole("link", { name })).toBeNull();
      expect(screen.queryByRole("heading", { name })).toBeNull();
    }
  });
});
