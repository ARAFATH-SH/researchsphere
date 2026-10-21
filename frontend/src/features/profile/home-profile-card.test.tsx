import { render, screen } from "@testing-library/react";

import { useSession } from "@/features/auth/session";

import { getMyProfile } from "./api";
import { HomeProfileCard } from "./home-profile-card";
import { clearMyProfile } from "./my-profile";
import { adaID, emptyProfile, fullProfile } from "./test-profiles";

jest.mock("./api");
jest.mock("@/features/auth/session");
const mockGetMyProfile = jest.mocked(getMyProfile);

// Ada is signed in.
beforeEach(() => {
  jest.mocked(useSession).mockReturnValue({
    status: "signedIn",
    user: { id: adaID, email: "ada@example.org", fullName: "Ada Lovelace", role: "researcher" },
  });
});
afterEach(() => {
  jest.resetAllMocks();
  clearMyProfile();
});

describe("HomeProfileCard", () => {
  it("shows a skeleton while the profile loads", () => {
    mockGetMyProfile.mockReturnValue(new Promise(() => {}));
    render(<HomeProfileCard />);
    expect(screen.getByRole("status").textContent).toBe("Loading profile…");
  });

  it("shows the name, headline, affiliation, country and strength from GET /profiles/me", async () => {
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: fullProfile(adaID, "Ada Lovelace") });
    render(<HomeProfileCard />);

    const name = await screen.findByRole("link", { name: "Ada Lovelace" });
    expect(name.getAttribute("href")).toBe("/profile");
    expect(name.textContent).toContain("AL");
    expect(screen.getByText("Analyst of engines")).toBeTruthy();
    expect(screen.getByText("Researcher, University of London")).toBeTruthy();
    expect(screen.getByText("United Kingdom")).toBeTruthy();
    const strength = screen.getByRole("link", { name: "Profile strength: 100%" });
    expect(strength.getAttribute("href")).toBe("/profile");
    expect(screen.queryByRole("status")).toBeNull();
    expect(mockGetMyProfile).toHaveBeenCalledTimes(1);
  });

  it("shows the cover image and photo when the profile has them", async () => {
    const photoUrl = "/api/v1/media/photo";
    const coverUrl = "/api/v1/media/cover";
    mockGetMyProfile.mockResolvedValue({
      status: "ok",
      profile: { ...fullProfile(adaID, "Ada Lovelace"), photoUrl, coverUrl },
    });
    const { container } = render(<HomeProfileCard />);
    const name = await screen.findByRole("link", { name: "Ada Lovelace" });
    expect(name.querySelector("img")?.getAttribute("src")).toBe(photoUrl);
    expect(name.textContent).not.toContain("AL");
    const srcs = Array.from(container.querySelectorAll("img")).map((img) => img.getAttribute("src"));
    expect(srcs).toEqual([coverUrl, photoUrl]);
  });

  it("leaves out fields the profile does not have", async () => {
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: emptyProfile(adaID, "Ada Lovelace") });
    render(<HomeProfileCard />);
    expect(await screen.findByRole("link", { name: "Ada Lovelace" })).toBeTruthy();
    expect(screen.queryByText("Analyst of engines")).toBeNull();
    expect(screen.getByRole("link", { name: "Profile strength: 0%" })).toBeTruthy();
  });

  it("links to the profile when the request fails", async () => {
    mockGetMyProfile.mockResolvedValue({ status: "error" });
    render(<HomeProfileCard />);
    expect(await screen.findByText("We couldn't load your profile.")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Go to your profile" }).getAttribute("href")).toBe("/profile");
  });
});
