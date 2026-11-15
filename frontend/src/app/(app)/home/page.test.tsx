import { render, screen, within } from "@testing-library/react";

import { useSession } from "@/features/auth/session";
import { getMyProfile } from "@/features/profile/api";
import { clearMyProfile } from "@/features/profile/my-profile";
import { adaID, fullProfile } from "@/features/profile/test-profiles";
import { getPeopleRecommendations } from "@/features/recommendation/api";
import { needsProfile, people, ready } from "@/features/recommendation/test-recommendations";

import Page from "./page";

jest.mock("@/features/profile/api");
jest.mock("@/features/auth/session");
jest.mock("@/features/recommendation/api", () => ({
  ...jest.requireActual("@/features/recommendation/api"),
  getPeopleRecommendations: jest.fn(),
}));
const mockGetMyProfile = jest.mocked(getMyProfile);
const mockGetRecs = jest.mocked(getPeopleRecommendations);

let consoleError: jest.SpyInstance;

beforeEach(() => {
  consoleError = jest.spyOn(console, "error");
  jest.mocked(useSession).mockReturnValue({
    status: "signedIn",
    user: { id: adaID, email: "ada@example.org", fullName: "Ada Lovelace", role: "researcher" },
  });
});
afterEach(() => {
  // No act() or other React warnings while the page loads.
  expect(consoleError).not.toHaveBeenCalled();
  consoleError.mockRestore();
  jest.resetAllMocks();
  clearMyProfile();
});

describe("Home page", () => {
  it("lays out the profile card, the feed and the recommended collaborators with real data only", async () => {
    mockGetRecs.mockResolvedValue(ready(people(1, 2, 3, 4, 5, 6, 7, 8, 9, 10)));
    const photoUrl = "/api/v1/media/photo";
    mockGetMyProfile.mockResolvedValue({
      status: "ok",
      profile: { ...fullProfile(adaID, "Ada Lovelace"), photoUrl },
    });
    render(<Page />);

    const profile = screen.getByRole("complementary", { name: "Your profile" });
    const feed = screen.getByRole("region", { name: "Feed" });
    const right = screen.getByRole("complementary", { name: "Recommendations" });

    // The real name and photo, from GET /profiles/me.
    expect(await within(profile).findByRole("link", { name: "Ada Lovelace" })).toBeTruthy();
    expect(profile.querySelector(`img[src="${photoUrl}"]`)).toBeTruthy();

    // The composer is disabled and the feed is empty: no posts.
    expect((within(feed).getByRole("button", { name: "Post" }) as HTMLButtonElement).disabled).toBe(true);
    expect(within(feed).getByText(/Your feed is empty for now/)).toBeTruthy();
    expect(screen.queryAllByRole("article")).toHaveLength(0);

    // The top 3 of the one request, the link to all 10, then the compact footer.
    expect(await within(right).findByRole("link", { name: "Researcher 1" })).toBeTruthy();
    expect(within(right).getByRole("heading", { name: "Recommended collaborators" })).toBeTruthy();
    const list = within(right).getByRole("list", { name: "Recommended researchers" });
    expect(within(list).getAllByRole("button", { name: /^Connect with/ })).toHaveLength(3);
    expect(within(right).getByRole("link", { name: "See all 10 recommendations" }).getAttribute("href")).toBe(
      "/network/recommendations",
    );
    expect(within(right).getByRole("contentinfo", { name: "Site footer" })).toBeTruthy();
    expect(mockGetRecs).toHaveBeenCalledTimes(1);

    // The old placeholder is gone.
    expect(screen.queryByText("Coming soon")).toBeNull();
    expect(screen.queryByText(/Built in a later slice/)).toBeNull();
  });

  it("asks for a profile to match on in place of the list", async () => {
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: fullProfile(adaID, "Ada Lovelace") });
    mockGetRecs.mockResolvedValue(needsProfile);
    render(<Page />);

    const right = screen.getByRole("complementary", { name: "Recommendations" });
    expect(await within(right).findByRole("heading", { name: "Get better recommendations" })).toBeTruthy();
    expect(within(right).getByRole("link", { name: "Add interests" })).toBeTruthy();
    await within(screen.getByRole("complementary", { name: "Your profile" })).findByRole("link", { name: "Ada Lovelace" });
  });
});
