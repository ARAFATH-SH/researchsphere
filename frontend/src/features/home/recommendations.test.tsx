import { act, render, screen, waitFor } from "@testing-library/react";

import { useSession } from "@/features/auth/session";
import { getMyProfile } from "@/features/profile/api";
import { clearMyProfile } from "@/features/profile/my-profile";
import { adaID, emptyProfile, fullProfile } from "@/features/profile/test-profiles";

import { Recommendations } from "./recommendations";

jest.mock("@/features/profile/api");
jest.mock("@/features/auth/session");
const mockGetMyProfile = jest.mocked(getMyProfile);

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

describe("Recommendations", () => {
  it("asks for research interests when the profile has none", async () => {
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: emptyProfile(adaID, "Ada Lovelace") });
    render(<Recommendations />);

    expect(await screen.findByRole("heading", { name: "Get better recommendations" })).toBeTruthy();
    const add = screen.getByRole("link", { name: "Add interests" });
    expect(add.getAttribute("href")).toBe("/profile?edit=interests");
    expect(screen.queryByText("Recommendations are coming soon.")).toBeNull();
  });

  it("says recommendations are coming, with no people, when the profile has interests", async () => {
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: fullProfile(adaID, "Ada Lovelace") });
    const { container } = render(<Recommendations />);

    expect(await screen.findByRole("heading", { name: "Recommended collaborators" })).toBeTruthy();
    expect(screen.getByText("Recommendations are coming soon.")).toBeTruthy();
    expect(screen.queryByRole("link", { name: "Add interests" })).toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
    expect(container.querySelector("img")).toBeNull();
  });

  it("shows nothing while the profile loads", () => {
    mockGetMyProfile.mockReturnValue(new Promise(() => {}));
    const { container } = render(<Recommendations />);
    expect(container.innerHTML).toBe("");
  });

  it("shows nothing when the profile fails to load", async () => {
    mockGetMyProfile.mockResolvedValue({ status: "error" });
    const { container } = render(<Recommendations />);
    await waitFor(() => expect(mockGetMyProfile).toHaveBeenCalledTimes(1));
    // Let the resolved request reach the shared profile store.
    await act(async () => {});
    expect(container.innerHTML).toBe("");
  });
});
