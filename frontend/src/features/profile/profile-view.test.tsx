import "@/test/dialog-polyfill";

import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";

import { useSession } from "@/features/auth/session";

import { getMyContact, getMyProfile, getProfile, updateMyProfile } from "./api";
import { ProfileView } from "./profile-view";
import { adaID, bobID, emptyProfile, fullProfile } from "./test-profiles";

jest.mock("./api");
jest.mock("@/features/auth/session");
const mockReplace = jest.fn();
let mockSearch = "";
jest.mock("next/navigation", () => ({
  useRouter: () => ({ replace: mockReplace }),
  useSearchParams: () => new URLSearchParams(mockSearch),
}));
const mockGetMyProfile = jest.mocked(getMyProfile);
const mockGetProfile = jest.mocked(getProfile);

// Ada is signed in.
beforeEach(() => {
  jest.mocked(useSession).mockReturnValue({
    status: "signedIn",
    user: { id: adaID, email: "ada@example.org", fullName: "Ada Lovelace", role: "researcher" },
  });
});
afterEach(() => {
  jest.resetAllMocks();
  mockSearch = "";
});

function headings(level: number) {
  return screen.getAllByRole("heading", { level }).map((h) => h.textContent);
}

describe("ProfileView, own profile", () => {
  it("shows the profile with pencils and the strength card", async () => {
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: fullProfile(adaID, "Ada Lovelace") });
    render(<ProfileView />);

    expect(await screen.findByRole("heading", { level: 1, name: "Ada Lovelace" })).toBeTruthy();
    expect(headings(1)).toHaveLength(1);
    expect(screen.getByRole("img", { name: "Ada Lovelace" }).textContent).toBe("AL");
    expect(screen.getByText("Analyst of engines")).toBeTruthy();
    expect(screen.getByText("Researcher, University of London")).toBeTruthy();
    expect(screen.getByText("Mathematics")).toBeTruthy();
    expect(screen.getByText("United Kingdom")).toBeTruthy();

    for (const name of ["Edit intro", "Edit about", "Edit research interests", "Edit skills", "Edit education"]) {
      expect(screen.getByRole("button", { name })).toBeTruthy();
    }
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

    for (const [section, prompt, href] of [
      ["About", "Write a short bio", "/profile?edit=about"],
      ["Research interests", "Add your research interests", "/profile?edit=interests"],
      ["Skills", "Add your skills", "/profile?edit=skills"],
      ["Education", "Add your education", "/profile?edit=education"],
    ]) {
      const region = within(screen.getByRole("region", { name: section }));
      expect(region.getByRole("link", { name: prompt }).getAttribute("href")).toBe(href);
    }
    expect(screen.getByText("No publications yet.")).toBeTruthy();

    expect(screen.getByRole("progressbar").getAttribute("aria-valuenow")).toBe("0");
    const checklist = screen.getByRole("list", { name: "Profile checklist" });
    const missing = within(checklist).getAllByRole("link");
    expect(missing.map((a) => a.getAttribute("href"))).toEqual([
      "/profile?edit=intro",
      "/profile?edit=about",
      "/profile?edit=intro",
      "/profile?edit=interests",
      "/profile?edit=skills",
      "/profile?edit=education",
    ]);
  });

  it("shows the own version for /profile/<own id>", async () => {
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: fullProfile(adaID, "Ada Lovelace") });
    render(<ProfileView userId={adaID} />);
    expect(await screen.findByRole("button", { name: "Edit intro" })).toBeTruthy();
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
    expect(screen.queryByRole("button", { name: /^Edit / })).toBeNull();
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

describe("ProfileView, editing", () => {
  it("opens a section's dialog from its pencil and gives the focus back on close", async () => {
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: fullProfile(adaID, "Ada Lovelace") });
    render(<ProfileView />);
    const pencil = await screen.findByRole("button", { name: "Edit about" });
    fireEvent.click(pencil);
    expect(screen.getByRole("dialog", { name: "Edit about" })).toBeTruthy();
    expect(screen.getByText("24 / 2000")).toBeTruthy();
    fireEvent.change(screen.getByRole("textbox", { name: "Bio" }), { target: { value: "Hello" } });
    expect(screen.getByText("5 / 2000")).toBeTruthy();
    fireEvent.change(screen.getByRole("textbox", { name: "Bio" }), { target: { value: "First line.\nSecond line." } });

    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(pencil);
    expect(mockReplace).not.toHaveBeenCalled();
  });

  it("opens the dialog named by ?edit= and clears it on close", async () => {
    mockSearch = "edit=skills";
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: fullProfile(adaID, "Ada Lovelace") });
    render(<ProfileView />);
    expect(await screen.findByRole("dialog", { name: "Edit skills" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(mockReplace).toHaveBeenCalledWith("/profile", { scroll: false });
  });

  it("ignores ?edit= on someone else's profile", async () => {
    mockSearch = "edit=skills";
    mockGetProfile.mockResolvedValue({ status: "ok", profile: fullProfile(bobID, "Bob Babbage") });
    render(<ProfileView userId={bobID} />);
    await screen.findByRole("heading", { level: 1, name: "Bob Babbage" });
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("closes after saving, reloads the profile and shows Saved", async () => {
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: fullProfile(adaID, "Ada Lovelace") });
    jest.mocked(updateMyProfile).mockResolvedValue({ ok: true, data: undefined });
    render(<ProfileView />);
    fireEvent.click(await screen.findByRole("button", { name: "Edit intro" }));

    const reloaded = { ...fullProfile(adaID, "Ada King"), headline: "Countess" };
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: reloaded });
    fireEvent.change(screen.getByRole("textbox", { name: "Full name" }), { target: { value: "Ada King" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect((await screen.findByRole("status")).textContent).toBe("Saved");
    expect(screen.queryByRole("dialog")).toBeNull();
    await waitFor(() => expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Ada King"));
    expect(mockGetMyProfile).toHaveBeenCalledTimes(2);
  });
});

describe("ProfileView, contact info", () => {
  it("hides Contact info on another profile when nothing is visible", async () => {
    mockGetProfile.mockResolvedValue({ status: "ok", profile: fullProfile(bobID, "Bob Babbage") });
    render(<ProfileView userId={bobID} />);
    await screen.findByRole("heading", { level: 1, name: "Bob Babbage" });
    expect(screen.queryByRole("button", { name: "Contact info" })).toBeNull();
  });

  it("shows another user only what the API returned, with links as icons and labels", async () => {
    const bob = fullProfile(bobID, "Bob Babbage");
    bob.contact = {
      phone: "+441234567890",
      links: [{ id: "l1", kind: "google_scholar", url: "https://scholar.example.org/bob" }],
    };
    mockGetProfile.mockResolvedValue({ status: "ok", profile: bob });
    render(<ProfileView userId={bobID} />);
    const button = await screen.findByRole("button", { name: "Contact info" });
    fireEvent.click(button);

    const dialog = within(screen.getByRole("dialog", { name: "Contact info for Bob Babbage" }));
    expect(dialog.getByRole("link", { name: "+441234567890" }).getAttribute("href")).toBe("tel:+441234567890");
    const scholar = dialog.getByRole("link", { name: /Google Scholar/ });
    expect(scholar.getAttribute("href")).toBe("https://scholar.example.org/bob");
    expect(scholar.getAttribute("rel")).toContain("noopener");
    expect(scholar.querySelector("svg")?.getAttribute("aria-hidden")).toBe("true");
    expect(dialog.queryByText("Email")).toBeNull();
    expect(dialog.queryByText("Birthday")).toBeNull();

    fireEvent.click(dialog.getByRole("button", { name: "Close" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(button);
  });

  it("shows Contact info on another profile when only a link is visible", async () => {
    const bob = fullProfile(bobID, "Bob Babbage");
    bob.contact = { links: [{ id: "l1", kind: "website", url: "https://bob.example.org" }] };
    mockGetProfile.mockResolvedValue({ status: "ok", profile: bob });
    render(<ProfileView userId={bobID} />);
    expect(await screen.findByRole("button", { name: "Contact info" })).toBeTruthy();
  });

  it("opens the edit dialog from Contact info on the own profile", async () => {
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: fullProfile(adaID, "Ada Lovelace") });
    jest.mocked(getMyContact).mockResolvedValue({
      ok: true,
      data: {
        email: "ada@example.org",
        emailVisibility: "only_me",
        phone: null,
        phoneVisibility: "only_me",
        birthday: null,
        birthdayVisibility: "only_me",
        links: [],
      },
    });
    render(<ProfileView />);
    fireEvent.click(await screen.findByRole("button", { name: "Contact info" }));
    expect(screen.getByRole("dialog", { name: "Contact info" })).toBeTruthy();
    expect(await screen.findByText("ada@example.org")).toBeTruthy();
  });
});
