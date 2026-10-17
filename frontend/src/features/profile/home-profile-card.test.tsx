import { render, screen } from "@testing-library/react";

import { getMyProfile } from "./api";
import { HomeProfileCard } from "./home-profile-card";
import { adaID, emptyProfile, fullProfile } from "./test-profiles";

jest.mock("./api");
const mockGetMyProfile = jest.mocked(getMyProfile);

afterEach(() => jest.resetAllMocks());

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
