import { render, screen, within } from "@testing-library/react";

import { useSession } from "@/features/auth/session";
import { getMyProfile } from "@/features/profile/api";
import { clearMyProfile } from "@/features/profile/my-profile";
import { adaID, fullProfile } from "@/features/profile/test-profiles";

import Page from "./page";

jest.mock("@/features/profile/api");
jest.mock("@/features/auth/session");
const mockGetMyProfile = jest.mocked(getMyProfile);

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
  it("lays out the profile card, the feed and the recommendations with real data only", async () => {
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

    // Ada has interests: the coming-soon card, no sample people, then the compact footer.
    expect(await within(right).findByText("Recommendations are coming soon.")).toBeTruthy();
    expect(within(right).queryByRole("button")).toBeNull();
    expect(within(right).getByRole("contentinfo", { name: "Site footer" })).toBeTruthy();
    expect(screen.queryByText(/Connect|match/)).toBeNull();

    // The old placeholder is gone.
    expect(screen.queryByText("Coming soon")).toBeNull();
    expect(screen.queryByText(/Built in a later slice/)).toBeNull();
  });
});
