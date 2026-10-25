import "@/test/dialog-polyfill";

import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";

import { useSession } from "@/features/auth/session";

import { getMyContact, getMyProfile, getProfile, removeProfileImage, updateMyProfile } from "./api";
import { clearMyProfile } from "./my-profile";
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
  // Saving the own profile shares it through the store; forget it between cases.
  clearMyProfile();
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

  it("deletes the photo from the photo dialog, reloads, and gives the focus back to the photo", async () => {
    const photoUrl = "/api/v1/media/photo";
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: { ...fullProfile(adaID, "Ada Lovelace"), photoUrl } });
    jest.mocked(removeProfileImage).mockResolvedValue({ ok: true });
    render(<ProfileView />);
    const photo = await screen.findByRole("button", { name: "Change profile photo" });
    fireEvent.click(photo);
    const dialog = screen.getByRole("dialog", { name: "Profile photo" });

    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: fullProfile(adaID, "Ada Lovelace") });
    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));

    expect((await screen.findByRole("status")).textContent).toBe("Photo deleted");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(jest.mocked(removeProfileImage)).toHaveBeenCalledWith("photo");
    await waitFor(() => expect(photo.querySelector("img")).toBeNull());
    expect(document.activeElement).toBe(photo);
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

describe("ProfileView, header", () => {
  it("lists the current institution and the latest education entry", async () => {
    mockGetProfile.mockResolvedValue({ status: "ok", profile: fullProfile(bobID, "Bob Babbage") });
    render(<ProfileView userId={bobID} />);
    const list = within(await screen.findByRole("list", { name: "Affiliations" }));
    expect(list.getAllByRole("listitem").map((li) => li.textContent)).toEqual(["UUniversity of London", "EETH Zürich"]);
  });

  it("has no affiliations list when there is neither", async () => {
    mockGetProfile.mockResolvedValue({ status: "ok", profile: emptyProfile(bobID, "Bob Babbage") });
    render(<ProfileView userId={bobID} />);
    await screen.findByRole("heading", { level: 1 });
    expect(screen.queryByRole("list", { name: "Affiliations" })).toBeNull();
  });

  it("copies the own profile link as /profile/<own id> from More", async () => {
    const writeText = jest.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: fullProfile(adaID, "Ada Lovelace") });
    render(<ProfileView />);
    fireEvent.click(await screen.findByRole("button", { name: "More" }));
    await act(async () => fireEvent.click(screen.getByRole("menuitem", { name: "Copy profile link" })));
    expect(writeText).toHaveBeenCalledWith(`${window.location.origin}/profile/${adaID}`);
    expect(screen.getByRole("status").textContent).toBe("Link copied");
  });

  describe("sticky bar", () => {
    let report: (entry: Partial<IntersectionObserverEntry>) => void;
    beforeEach(() => {
      window.IntersectionObserver = jest.fn((callback: IntersectionObserverCallback) => {
        report = (entry) => callback([entry as IntersectionObserverEntry], {} as IntersectionObserver);
        return { observe: jest.fn(), disconnect: jest.fn() };
      }) as unknown as typeof IntersectionObserver;
    });
    afterEach(() => {
      delete (window as { IntersectionObserver?: unknown }).IntersectionObserver;
    });

    it("shows the name, headline and buttons once the header scrolls past the top bar", async () => {
      mockGetProfile.mockResolvedValue({ status: "ok", profile: fullProfile(bobID, "Bob Babbage") });
      render(<ProfileView userId={bobID} />);
      await screen.findByRole("heading", { level: 1 });
      expect(screen.queryByRole("region", { name: "Profile summary" })).toBeNull();

      act(() => report({ isIntersecting: false, boundingClientRect: { top: -200 } as DOMRect }));
      const bar = within(screen.getByRole("region", { name: "Profile summary" }));
      expect(bar.getByText("Bob Babbage")).toBeTruthy();
      expect(bar.getByText("Analyst of engines")).toBeTruthy();
      expect(bar.getByRole("button", { name: "Connect" })).toBeTruthy();
      expect(bar.getByRole("button", { name: "More" })).toBeTruthy();

      act(() => report({ isIntersecting: true, boundingClientRect: { top: 80 } as DOMRect }));
      expect(screen.queryByRole("region", { name: "Profile summary" })).toBeNull();
    });

    it("stays hidden while the header is below the screen", async () => {
      mockGetProfile.mockResolvedValue({ status: "ok", profile: fullProfile(bobID, "Bob Babbage") });
      render(<ProfileView userId={bobID} />);
      await screen.findByRole("heading", { level: 1 });
      act(() => report({ isIntersecting: false, boundingClientRect: { top: 2000 } as DOMRect }));
      expect(screen.queryByRole("region", { name: "Profile summary" })).toBeNull();
    });
  });
});

describe("ProfileView, Top skills and Show all", () => {
  const skills = ["A", "B", "C", "D", "E", "F", "G"].map((name, i) => ({ id: `s${i}`, name }));

  it("names the first five skills under the bio and links to the Skills card", async () => {
    const p = { ...fullProfile(bobID, "Bob Babbage"), skills };
    mockGetProfile.mockResolvedValue({ status: "ok", profile: p });
    render(<ProfileView userId={bobID} />);
    const about = within(await screen.findByRole("region", { name: "About" }));
    const top = about.getByRole("link", { name: /Top skills/ });
    expect(top.textContent).toContain("A • B • C • D • E");
    expect(top.textContent).not.toContain("F");
    expect(top.getAttribute("href")).toBe("#skills");
    expect(screen.getByRole("region", { name: "Skills" }).closest("#skills")).toBeTruthy();
  });

  it("shows six skills, then all of them after Show all", async () => {
    mockGetProfile.mockResolvedValue({ status: "ok", profile: { ...fullProfile(bobID, "Bob Babbage"), skills } });
    render(<ProfileView userId={bobID} />);
    const region = within(await screen.findByRole("region", { name: "Skills" }));
    expect(within(region.getByRole("list", { name: "Skills" })).getAllByRole("listitem")).toHaveLength(6);

    const more = region.getByRole("button", { name: "Show all 7" });
    expect(more.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(more);
    expect(within(region.getByRole("list", { name: "Skills" })).getAllByRole("listitem")).toHaveLength(7);
    expect(region.queryByRole("button", { name: /Show all/ })).toBeNull();
  });

  it("shows two education entries, then all of them after Show all", async () => {
    const p = fullProfile(adaID, "Ada Lovelace");
    p.education = [
      ...p.education,
      { id: "e3", institution: "Cambridge", degree: null, fieldOfStudy: null, startYear: 2018, endYear: 2021, grade: null, description: null },
    ];
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: p });
    render(<ProfileView />);
    const region = within(await screen.findByRole("region", { name: "Education" }));
    expect(region.getAllByRole("listitem")).toHaveLength(2);
    fireEvent.click(region.getByRole("button", { name: "Show all 3" }));
    expect(region.getAllByRole("listitem").map((li) => li.firstChild?.textContent)).toEqual([
      "ETH Zürich",
      "MIT",
      "Cambridge",
    ]);
  });

  it("has no Show all row when everything fits", async () => {
    mockGetProfile.mockResolvedValue({ status: "ok", profile: fullProfile(bobID, "Bob Babbage") });
    render(<ProfileView userId={bobID} />);
    await screen.findByRole("heading", { level: 1 });
    expect(screen.queryByRole("button", { name: /Show all/ })).toBeNull();
  });
});
