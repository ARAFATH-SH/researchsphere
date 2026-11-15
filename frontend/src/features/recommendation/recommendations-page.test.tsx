import { render, screen, within } from "@testing-library/react";

import { useSession } from "@/features/auth/session";

import { getPeopleRecommendations } from "./api";
import { RecommendationsPage } from "./recommendations-page";
import { people, ready } from "./test-recommendations";

jest.mock("./api", () => ({ ...jest.requireActual("./api"), getPeopleRecommendations: jest.fn() }));
jest.mock("@/features/social/api");
jest.mock("@/features/notification/badges");
jest.mock("@/features/auth/session");

const mockGet = jest.mocked(getPeopleRecommendations);

beforeEach(() => {
  jest.mocked(useSession).mockReturnValue({
    status: "signedIn",
    user: { id: "0b8f4c1e-0000-4000-8000-000000000001", email: "ada@example.org", fullName: "Ada Lovelace", role: "researcher" },
  });
});
afterEach(() => jest.resetAllMocks());

describe("RecommendationsPage", () => {
  it("shows all 10 recommendations in wide rows, with one request and no link to itself", async () => {
    mockGet.mockResolvedValue(ready(people(1, 2, 3, 4, 5, 6, 7, 8, 9, 10)));
    render(<RecommendationsPage />);

    expect(screen.getByRole("heading", { level: 1, name: "Recommended collaborators" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Back to My Network" }).getAttribute("href")).toBe("/network");
    const list = await screen.findByRole("list", { name: "Recommended researchers" });
    expect(within(list).getAllByRole("button", { name: /^Connect with/ })).toHaveLength(10);
    expect(screen.getByRole("heading", { level: 2, name: "Researchers to work with" })).toBeTruthy();
    expect(screen.getByText("90 percent match")).toBeTruthy();
    expect(screen.queryByRole("link", { name: /See all/ })).toBeNull();
    expect(screen.getByRole("contentinfo", { name: "Site footer" })).toBeTruthy();
    expect(mockGet).toHaveBeenCalledTimes(1);
  });
});
