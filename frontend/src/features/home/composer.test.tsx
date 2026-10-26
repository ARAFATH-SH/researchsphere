import { render, screen, waitFor } from "@testing-library/react";

import { useSession } from "@/features/auth/session";
import { getMyProfile } from "@/features/profile/api";
import { clearMyProfile } from "@/features/profile/my-profile";
import { adaID, fullProfile } from "@/features/profile/test-profiles";

import { Composer } from "./composer";

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

describe("Composer", () => {
  it("disables every control until posting is built and says so", async () => {
    const photoUrl = "/api/v1/media/photo";
    mockGetMyProfile.mockResolvedValue({
      status: "ok",
      profile: { ...fullProfile(adaID, "Ada Lovelace"), photoUrl },
    });
    const { container } = render(<Composer />);

    expect(await screen.findByRole("heading", { name: "Share an update" })).toBeTruthy();
    expect(screen.getByText("Posting comes soon")).toBeTruthy();
    const post = screen.getByRole("button", { name: "Post" }) as HTMLButtonElement;
    expect(post.disabled).toBe(true);
    expect(post.getAttribute("aria-describedby")).toBe("composer-note");
    expect((screen.getByRole("textbox", { name: "Post text" }) as HTMLTextAreaElement).disabled).toBe(true);
    for (const label of ["Link publication", "Link project"]) {
      expect((screen.getByRole("button", { name: label }) as HTMLButtonElement).disabled).toBe(true);
    }
    expect(screen.getByText("0 / 5,000")).toBeTruthy();
    // The avatar is the user's own photo once the profile loads.
    await waitFor(() => expect(container.querySelector("img")?.getAttribute("src")).toBe(photoUrl));
  });

  it("shows the session name's initials while the profile loads", () => {
    mockGetMyProfile.mockReturnValue(new Promise(() => {}));
    const { container } = render(<Composer />);
    expect(container.textContent).toContain("AL");
    expect((screen.getByRole("button", { name: "Post" }) as HTMLButtonElement).disabled).toBe(true);
  });
});
