import { act, render, screen, within } from "@testing-library/react";

import { useSession } from "@/features/auth/session";
import { getPeopleRecommendations } from "@/features/recommendation/api";
import { RecommendationsProvider } from "@/features/recommendation/recommendations-provider";
import { needsProfile, people, ready } from "@/features/recommendation/test-recommendations";

import { ProfileSidebar } from "./profile-sidebar";
import { adaID, bobID, fullProfile } from "./test-profiles";

jest.mock("@/features/recommendation/api", () => ({
  ...jest.requireActual("@/features/recommendation/api"),
  getPeopleRecommendations: jest.fn(),
}));
jest.mock("@/features/social/api");
jest.mock("@/features/notification/badges");
jest.mock("@/features/auth/session");

const mockGet = jest.mocked(getPeopleRecommendations);

beforeEach(() => {
  jest.mocked(useSession).mockReturnValue({
    status: "signedIn",
    user: { id: adaID, email: "ada@example.org", fullName: "Ada Lovelace", role: "researcher" },
  });
});
afterEach(() => jest.resetAllMocks());

function renderSidebar(own: boolean) {
  const profile = own ? fullProfile(adaID, "Ada Lovelace") : fullProfile(bobID, "Bob Babbage");
  return render(
    <RecommendationsProvider>
      <ProfileSidebar profile={profile} onEditContact={own ? () => {} : undefined} />
    </RecommendationsProvider>,
  );
}

describe("ProfileSidebar recommendations", () => {
  it.each([
    ["another user's profile", false],
    ["the own profile", true],
  ])("shows the viewer's top 3 as Connect with researchers on %s", async (_, own) => {
    mockGet.mockResolvedValue(ready(people(1, 2, 3, 4, 5)));
    renderSidebar(own);

    const card = await screen.findByRole("region", { name: "Connect with researchers" });
    expect(within(card).getByText("Top 3 researchers whose profiles match yours most closely.")).toBeTruthy();
    expect(within(card).getAllByRole("button", { name: /^Connect with/ })).toHaveLength(3);
    expect(within(card).getByRole("link", { name: "See all 5 recommendations" })).toBeTruthy();
    expect(mockGet).toHaveBeenCalledTimes(1);
  });

  it("is hidden when the viewer has no recommendations", async () => {
    mockGet.mockResolvedValue(needsProfile);
    renderSidebar(false);

    await act(async () => {});
    expect(mockGet).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("region", { name: "Connect with researchers" })).toBeNull();
    expect(screen.queryByRole("heading", { name: "Get better recommendations" })).toBeNull();
  });
});
