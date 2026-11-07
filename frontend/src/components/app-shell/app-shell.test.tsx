import { render, screen, waitFor, within } from "@testing-library/react";

import type { Session } from "@/features/auth/session";
import { getMyProfile } from "@/features/profile/api";
import { clearMyProfile } from "@/features/profile/my-profile";
import { adaID, emptyProfile } from "@/features/profile/test-profiles";

import { AppShell } from "./app-shell";

const mockPathname = jest.fn<string, []>();
jest.mock("next/navigation", () => ({
  usePathname: () => mockPathname(),
  useRouter: () => ({ replace: jest.fn(), push: jest.fn() }),
  useSearchParams: () => new URLSearchParams(),
}));
const mockSession = jest.fn<Session, []>(() => ({ status: "loading" }));
jest.mock("@/features/auth/session", () => ({ useSession: () => mockSession() }));
jest.mock("@/features/auth/api");
// The My Network badge has its own tests (network-badge.test.tsx).
jest.mock("@/features/social/pending-invitations", () => ({ usePendingInvitations: () => 0 }));
// The badge poller has its own tests (features/notification/badges.test.tsx).
jest.mock("@/features/notification/badge-poller", () => ({ BadgePoller: () => null }));
jest.mock("@/features/realtime/realtime-connector", () => ({ RealtimeConnector: () => null }));
// The warning banner has its own tests (features/moderation/warning-banner.test.tsx).
jest.mock("@/features/moderation/warning-banner", () => ({ WarningBanner: () => null }));
// The Me menu fetches the signed-in user's own profile for its photo.
jest.mock("@/features/profile/api");
const mockGetMyProfile = jest.mocked(getMyProfile);
const photoUrl = "/api/v1/media/3f1c2b9e-8a41-4d7e-9c55-2f6a1b0e7d10";

// The own profile is shared between renders; start each case without one.
beforeEach(() => clearMyProfile());
afterEach(() => mockGetMyProfile.mockReset());

const ownID = "0b8f4c1e-0000-4000-8000-000000000001";
const signedInAs = (role: "researcher" | "admin"): Session => ({
  status: "signedIn",
  user: { id: ownID, email: "ada@example.org", fullName: "Ada Lovelace", role },
});

// jsdom applies no CSS, so both "Main" navs are present: the top bar's tabs (a bottom bar
// from 481 to 640px) and the phone bottom bar (480px and narrower).
const topBar = () => screen.getAllByRole("navigation", { name: "Main" })[0];
const phoneBar = () => screen.getAllByRole("navigation", { name: "Main" })[1];
const links = (nav: HTMLElement) =>
  within(nav)
    .getAllByRole("link")
    .map((l) => [l.textContent, l.getAttribute("href")]);
// The Me menu shows the photo once the own profile has loaded.
const profileLoaded = () => waitFor(() => expect(topBar().querySelector("img")?.getAttribute("src")).toBe(photoUrl));

describe("AppShell", () => {
  it("has the design's tabs and the Me menu in the top bar", () => {
    mockPathname.mockReturnValue("/home");
    render(<AppShell>content</AppShell>);
    // The Me menu is closed, so its links are hidden.
    expect(links(topBar())).toEqual([
      ["Home", "/home"],
      ["My Network", "/network"],
      ["Projects", "/projects"],
      ["Messaging", "/messaging"],
      ["Notifications", "/notifications"],
    ]);
    const me = within(topBar()).getByRole("button", { name: "Me" });
    expect(me.getAttribute("aria-expanded")).toBe("false");
    expect(me.getAttribute("aria-haspopup")).toBe("menu");
    expect(screen.getByText("content")).toBeTruthy();
  });

  it("moves the top bar's tabs to a fixed 56px bottom bar under 640px", () => {
    mockPathname.mockReturnValue("/home");
    render(<AppShell>x</AppShell>);
    const list = within(topBar()).getByRole("list");
    for (const c of ["max-[640px]:fixed", "max-[640px]:bottom-0", "max-[640px]:h-14"]) {
      expect(list.className).toContain(c);
    }
  });

  it("has the phone bottom bar at 480px and narrower", () => {
    mockPathname.mockReturnValue("/home");
    render(<AppShell>x</AppShell>);
    expect(phoneBar().className).toContain("max-[480px]:block");
    expect(links(phoneBar())).toEqual([
      ["Home", "/home"],
      ["Network", "/network"],
      ["Post", "/home"],
      ["Alerts", "/notifications"],
      ["Projects", "/projects"],
    ]);
    const current = within(phoneBar())
      .getAllByRole("link")
      .filter((l) => l.getAttribute("aria-current") === "page");
    expect(current.map((l) => l.textContent)).toEqual(["Home"]);
  });

  it.each([
    ["/projects", "Projects"],
    ["/projects/123/workspace", "Projects"],
    ["/network", "My Network"],
    ["/messaging", "Messaging"],
  ])("marks the tab for %s as current", (path, label) => {
    mockPathname.mockReturnValue(path);
    render(<AppShell>x</AppShell>);
    const current = within(topBar())
      .getAllByRole("link")
      .filter((l) => l.getAttribute("aria-current") === "page");
    expect(current.map((l) => l.textContent)).toEqual([label]);
  });

  it("marks the Me tab on the own profile", () => {
    mockPathname.mockReturnValue("/profile");
    render(<AppShell>x</AppShell>);
    expect(within(topBar()).getByRole("button", { name: "Me" }).className).toContain("after:bg-rs-accent");
    expect(within(topBar()).getByRole("link", { name: "Home" }).className).not.toContain("after:bg-rs-accent");
  });

  it.each([
    ["/profile/edit", true],
    [`/profile/${ownID}`, true],
    ["/profile/7e9d7526-3053-4ade-bb3e-8db0b1a34689", false],
  ])("on %s marks the Me tab: %s (another user's profile marks no tab)", async (path, current) => {
    mockPathname.mockReturnValue(path);
    mockSession.mockReturnValue(signedInAs("researcher"));
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: { ...emptyProfile(adaID, "Ada Lovelace"), photoUrl } });
    render(<AppShell>x</AppShell>);
    await profileLoaded();
    const me = within(topBar()).getByRole("button", { name: "Me" });
    expect(me.className.includes("after:bg-rs-accent")).toBe(current);
    const others = within(topBar()).getAllByRole("link").filter((l) => l.getAttribute("aria-current") === "page");
    expect(others).toEqual([]);
  });

  it("has no Admin tab, even for an administrator (the Me menu links the console)", async () => {
    mockPathname.mockReturnValue("/admin");
    mockSession.mockReturnValue(signedInAs("admin"));
    mockGetMyProfile.mockResolvedValue({ status: "ok", profile: { ...emptyProfile(adaID, "Ada Lovelace"), photoUrl } });
    render(<AppShell>x</AppShell>);
    await profileLoaded();
    for (const bar of screen.getAllByRole("navigation", { name: "Main" })) {
      expect(within(bar).queryByRole("link", { name: "Admin" })).toBeNull();
    }
    mockSession.mockReturnValue({ status: "loading" });
  });

  it("puts the full site footer after the page, outside main", () => {
    mockPathname.mockReturnValue("/profile");
    render(<AppShell>content</AppShell>);
    const footer = screen.getByRole("contentinfo", { name: "Site footer" });
    expect(screen.getByRole("main").contains(footer)).toBe(false);
    expect(screen.getByRole("main").compareDocumentPosition(footer) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it.each(["/home", "/messaging", "/projects/123/workspace"])("leaves its footer out of %s", (path) => {
    mockPathname.mockReturnValue(path);
    render(<AppShell>x</AppShell>);
    expect(screen.queryByRole("contentinfo")).toBeNull();
  });

  it("has search boxes that go to /search, in the top bar and the phone bar", () => {
    mockPathname.mockReturnValue("/home");
    render(<AppShell>x</AppShell>);
    const forms = screen.getAllByRole("search");
    expect(forms).toHaveLength(2);
    for (const form of forms) {
      expect(form.getAttribute("action")).toBe("/search");
      expect(within(form).getByLabelText("Search").getAttribute("name")).toBe("q");
    }
  });

  it("has the phone top bar's Messaging link", () => {
    mockPathname.mockReturnValue("/home");
    render(<AppShell>x</AppShell>);
    const messaging = screen.getAllByRole("link", { name: "Messaging" });
    expect(messaging.map((l) => l.getAttribute("href"))).toEqual(["/messaging", "/messaging"]);
  });
});
