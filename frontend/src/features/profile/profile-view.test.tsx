import { fireEvent, render, screen, within } from "@testing-library/react";

import { useSession } from "@/features/auth/session";

import { getMyProfile, getProfile } from "./api";
import { ProfileView } from "./profile-view";
import { adaID, bobID, emptyProfile, fullProfile } from "./test-profiles";

jest.mock("./api");
jest.mock("@/features/auth/session");
const mockGetMyProfile = jest.mocked(getMyProfile);
const mockGetProfile = jest.mocked(getProfile);

// Ada is signed in.
beforeEach(() => {
  jest.mocked(useSession).mockReturnValue({
    status: "signedIn",
    user: { id: adaID, email: "ada@example.org", fullName: "Ada Lovelace", role: "researcher" },
  });
});
afterEach(() => jest.resetAllMocks());

function headings(level: number) {
  return screen.getAllByRole("heading", { level }).map((h) => h.textContent);
}

describe("ProfileView, own profile", () => {
  it("shows the profile with Edit profile and the strength card", async () => {
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: fullProfile(adaID, "Ada Lovelace") });
    render(<ProfileView />);

    expect(await screen.findByRole("heading", { level: 1, name: "Ada Lovelace" })).toBeTruthy();
    expect(headings(1)).toHaveLength(1);
    expect(screen.getByRole("img", { name: "Ada Lovelace" }).textContent).toBe("AL");
    expect(screen.getByText("Analyst of engines")).toBeTruthy();
    expect(screen.getByText("Researcher, University of London")).toBeTruthy();
    expect(screen.getByText("Mathematics")).toBeTruthy();
    expect(screen.getByText("United Kingdom")).toBeTruthy();

    const edit = screen.getByRole("link", { name: "Edit profile" });
    expect(edit.getAttribute("href")).toBe("/profile/edit");
    expect(screen.queryByRole("button", { name: /connect/i })).toBeNull();

    expect(screen.getByRole("heading", { level: 2, name: "Profile strength" })).toBeTruthy();
    expect(screen.getByRole("progressbar").getAttribute("aria-valuenow")).toBe("100");
    expect(mockGetProfile).not.toHaveBeenCalled();
  });

  it("shows sections as h2 and chips as lists", async () => {
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: fullProfile(adaID, "Ada Lovelace") });
    render(<ProfileView />);
    await screen.findByRole("heading", { level: 1 });

    expect(headings(2)).toEqual(
      expect.arrayContaining(["About", "Research interests", "Skills", "Education", "Publications"]),
    );
    const interests = screen.getByRole("list", { name: "Research interests" });
    expect(within(interests).getAllByRole("listitem").map((li) => li.textContent)).toEqual([
      "Machine Learning",
      "Robotics",
      "NLP",
    ]);
    const skills = screen.getByRole("list", { name: "Skills" });
    expect(within(skills).getAllByRole("listitem")).toHaveLength(3);
  });

  it("shows education years, with Present for a current entry", async () => {
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: fullProfile(adaID, "Ada Lovelace") });
    render(<ProfileView />);
    const education = within(await screen.findByRole("list", { name: "Education" }));
    const entries = education.getAllByRole("listitem");
    expect(within(entries[0]).getByText("ETH Zürich")).toBeTruthy();
    expect(within(entries[0]).getByText("PhD, Computer Science")).toBeTruthy();
    expect(within(entries[0]).getByText("2026 – Present")).toBeTruthy();
    expect(within(entries[1]).getByText("BSc")).toBeTruthy();
    expect(within(entries[1]).getByText("2022 – 2026")).toBeTruthy();
  });

  it("prompts for each empty section and lists what is missing", async () => {
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: emptyProfile(adaID, "Ada Lovelace") });
    render(<ProfileView />);
    await screen.findByRole("heading", { level: 1 });

    for (const [section, prompt] of [
      ["About", "Write a short bio"],
      ["Research interests", "Add your research interests"],
      ["Skills", "Add your skills"],
      ["Education", "Add your education"],
    ]) {
      const region = within(screen.getByRole("region", { name: section }));
      expect(region.getByRole("link", { name: prompt }).getAttribute("href")).toBe("/profile/edit");
    }
    expect(screen.getByText("No publications yet.")).toBeTruthy();

    expect(screen.getByRole("progressbar").getAttribute("aria-valuenow")).toBe("0");
    const checklist = screen.getByRole("list", { name: "Profile checklist" });
    const missing = within(checklist).getAllByRole("link");
    expect(missing).toHaveLength(6);
    expect(missing.every((a) => a.getAttribute("href") === "/profile/edit")).toBe(true);
  });

  it("shows the own version for /profile/<own id>", async () => {
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: fullProfile(adaID, "Ada Lovelace") });
    render(<ProfileView userId={adaID} />);
    expect(await screen.findByRole("link", { name: "Edit profile" })).toBeTruthy();
    expect(mockGetMyProfile).toHaveBeenCalled();
    expect(mockGetProfile).not.toHaveBeenCalled();
  });
});

describe("ProfileView, another user's profile", () => {
  it("shows a disabled Connect with a tooltip and no strength card", async () => {
    mockGetProfile.mockResolvedValue({ status: "ok", profile: fullProfile(bobID, "Bob Babbage") });
    render(<ProfileView userId={bobID} />);

    const connect = await screen.findByRole("button", { name: "Connect" });
    expect(connect.getAttribute("aria-disabled")).toBe("true");
    expect(screen.getByRole("tooltip", { hidden: true }).textContent).toBe("Available soon");
    expect(connect.getAttribute("aria-describedby")).toBe(screen.getByRole("tooltip", { hidden: true }).id);
    expect(screen.queryByRole("link", { name: "Edit profile" })).toBeNull();
    expect(screen.queryByText("Profile strength")).toBeNull();
    expect(screen.getByText("More researchers like this will appear here")).toBeTruthy();
    expect(mockGetProfile).toHaveBeenCalledWith(bobID);
  });

  it("hides empty sections except Publications", async () => {
    mockGetProfile.mockResolvedValue({ status: "ok", profile: emptyProfile(bobID, "Bob Babbage") });
    render(<ProfileView userId={bobID} />);
    await screen.findByRole("heading", { level: 1, name: "Bob Babbage" });

    expect(headings(2)).not.toEqual(expect.arrayContaining(["About"]));
    for (const name of ["About", "Research interests", "Skills", "Education"]) {
      expect(screen.queryByRole("heading", { level: 2, name })).toBeNull();
    }
    expect(screen.getByRole("heading", { level: 2, name: "Publications" })).toBeTruthy();
    expect(screen.getByText("No publications yet.")).toBeTruthy();
    expect(screen.queryByRole("link", { name: /add your/i })).toBeNull();
  });
});

describe("ProfileView, About", () => {
  it("collapses a long bio behind Show more", async () => {
    const p = fullProfile(adaID, "Ada Lovelace");
    p.bio = ["one", "two", "three", "four", "five", "six", "seven"].join("\n");
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: p });
    render(<ProfileView />);

    const toggle = await screen.findByRole("button", { name: "Show more" });
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(toggle);
    expect(screen.getByRole("button", { name: "Show less" }).getAttribute("aria-expanded")).toBe("true");
  });

  it("shows a short bio without a toggle", async () => {
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: fullProfile(adaID, "Ada Lovelace") });
    render(<ProfileView />);
    await screen.findByRole("heading", { level: 1 });
    expect(screen.queryByRole("button", { name: "Show more" })).toBeNull();
  });
});

describe("ProfileView, states", () => {
  it("shows a skeleton while loading", () => {
    mockGetMyProfile.mockReturnValue(new Promise(() => {}));
    render(<ProfileView />);
    expect(screen.getByRole("status").textContent).toBe("Loading profile…");
  });

  it("shows Profile not found for a 404", async () => {
    mockGetProfile.mockResolvedValue({ status: "notFound" });
    render(<ProfileView userId={bobID} />);
    expect(await screen.findByRole("heading", { level: 1, name: "Profile not found" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Go to Home" }).getAttribute("href")).toBe("/home");
  });

  it("shows a generic message and tries again after another failure", async () => {
    mockGetProfile.mockResolvedValueOnce({ status: "error" });
    render(<ProfileView userId={bobID} />);
    expect(await screen.findByText("We couldn't load this profile.")).toBeTruthy();

    mockGetProfile.mockResolvedValueOnce({ status: "ok", profile: fullProfile(bobID, "Bob Babbage") });
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("heading", { level: 1, name: "Bob Babbage" })).toBeTruthy();
    expect(mockGetProfile).toHaveBeenCalledTimes(2);
  });
});
