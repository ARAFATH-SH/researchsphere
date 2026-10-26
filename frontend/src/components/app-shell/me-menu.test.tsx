import { fireEvent, render, screen, waitFor } from "@testing-library/react";

import { logout } from "@/features/auth/api";
import { setSignedOut, useSession } from "@/features/auth/session";
import { useMyProfile } from "@/features/profile/my-profile";
import { fullProfile } from "@/features/profile/test-profiles";

import { MeMenu } from "./me-menu";

jest.mock("@/features/auth/api");
jest.mock("@/features/auth/session");
jest.mock("@/features/profile/my-profile");
const mockUseMyProfile = jest.mocked(useMyProfile);
const mockLogout = jest.mocked(logout);
const mockSetSignedOut = jest.mocked(setSignedOut);
const mockUseSession = jest.mocked(useSession);

const mockReplace = jest.fn();
jest.mock("next/navigation", () => ({ useRouter: () => ({ replace: mockReplace }) }));

const user = { id: "0b8f4c1e-0000-4000-8000-000000000001", email: "ada@example.org", fullName: "Ada King Lovelace", role: "researcher" as const };

describe("MeMenu", () => {
  beforeEach(() => mockUseSession.mockReturnValue({ status: "signedIn", user }));
  afterEach(() => jest.resetAllMocks());

  const renderMenu = () => render(<MeMenu placement="tab" />);
  const toggle = () => screen.getByRole("button", { name: /Me$/ });
  // The closed menu is hidden, so it has no accessible name to find it by.
  const menu = () => document.getElementById(toggle().getAttribute("aria-controls")!)!;
  const items = () => screen.getAllByRole("menuitem").map((i) => i.textContent);

  it("shows the user's initials, and their name and email in the menu", () => {
    renderMenu();
    expect(toggle().textContent).toBe("ALMe");
    expect(toggle().getAttribute("aria-expanded")).toBe("false");
    expect(menu().hidden).toBe(true);
    expect(menu().getAttribute("role")).toBe("menu");
    fireEvent.click(toggle());
    expect(toggle().getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByText("Ada King Lovelace")).toBeTruthy();
    expect(screen.getByText("ada@example.org")).toBeTruthy();
    expect(screen.getByRole("menuitem", { name: "View profile" }).getAttribute("href")).toBe("/profile");
  });

  it("shows the photo and the headline from the user's own profile once it has loaded", () => {
    const photoUrl = "/api/v1/media/3f1c2b9e-8a41-4d7e-9c55-2f6a1b0e7d10";
    mockUseMyProfile.mockReturnValue({ status: "ok", profile: { ...fullProfile(user.id, user.fullName), photoUrl } });
    renderMenu();
    expect(toggle().querySelector("img")?.getAttribute("src")).toBe(photoUrl);
    // The name follows in the menu, so the photo is not announced as well.
    expect(toggle().querySelector("img")?.getAttribute("alt")).toBe("");
    expect(toggle().textContent).toBe("Me");
    fireEvent.click(toggle());
    expect(screen.getByText("Analyst of engines")).toBeTruthy();
    expect(screen.queryByText("ada@example.org")).toBeNull();
  });

  it("has the design's items, and no admin console for a researcher", () => {
    renderMenu();
    fireEvent.click(toggle());
    expect(items()).toEqual(["View profile", "Settings & privacy", "Events & venues", "Help", "Sign out"]);
    expect(screen.getByRole("menuitem", { name: "Settings & privacy" }).getAttribute("href")).toBe("/settings/account");
    expect(screen.getByRole("menuitem", { name: "Events & venues" }).getAttribute("href")).toBe("/events");
  });

  it("links the admin console for an administrator (FR-3.2)", () => {
    mockUseSession.mockReturnValue({ status: "signedIn", user: { ...user, role: "admin" } });
    renderMenu();
    fireEvent.click(toggle());
    expect(screen.getByRole("menuitem", { name: "Admin console" }).getAttribute("href")).toBe("/admin");
  });

  it("closes when a link is used", () => {
    renderMenu();
    fireEvent.click(toggle());
    const settings = screen.getByRole("menuitem", { name: "Settings & privacy" });
    // jsdom cannot follow a link; cancel only the browser's navigation, so the menu's own
    // click handler still runs.
    settings.addEventListener("click", (e) => e.preventDefault());
    fireEvent.click(settings);
    expect(toggle().getAttribute("aria-expanded")).toBe("false");
  });

  it.each(["ArrowDown", "Enter", " "])("opens on %j and focuses the first item", (key) => {
    renderMenu();
    fireEvent.keyDown(toggle(), { key });
    expect(toggle().getAttribute("aria-expanded")).toBe("true");
    expect(document.activeElement?.textContent).toBe("View profile");
  });

  it("opens on ArrowUp at the last item", () => {
    renderMenu();
    fireEvent.keyDown(toggle(), { key: "ArrowUp" });
    expect(document.activeElement?.textContent).toBe("Sign out");
  });

  it("moves with the arrow keys, wrapping, and with Home and End", () => {
    renderMenu();
    fireEvent.keyDown(toggle(), { key: "ArrowDown" });
    const press = (key: string) => {
      fireEvent.keyDown(document.activeElement!, { key });
      return document.activeElement?.textContent;
    };
    expect(press("ArrowDown")).toBe("Settings & privacy");
    expect(press("ArrowUp")).toBe("View profile");
    expect(press("ArrowUp")).toBe("Sign out");
    expect(press("ArrowDown")).toBe("View profile");
    expect(press("End")).toBe("Sign out");
    expect(press("Home")).toBe("View profile");
  });

  it("closes on Escape and returns the focus to the button", () => {
    renderMenu();
    fireEvent.keyDown(toggle(), { key: "ArrowDown" });
    fireEvent.keyDown(document.activeElement!, { key: "Escape" });
    expect(toggle().getAttribute("aria-expanded")).toBe("false");
    expect(document.activeElement).toBe(toggle());
  });

  it("closes on Tab", () => {
    renderMenu();
    fireEvent.keyDown(toggle(), { key: "ArrowDown" });
    fireEvent.keyDown(document.activeElement!, { key: "Tab" });
    expect(toggle().getAttribute("aria-expanded")).toBe("false");
  });

  it("closes on a click outside", () => {
    renderMenu();
    fireEvent.click(toggle());
    fireEvent.mouseDown(document.body);
    expect(toggle().getAttribute("aria-expanded")).toBe("false");
  });

  it("is the avatar without a visible label in the phone top bar", () => {
    render(<MeMenu placement="phone" />);
    expect(screen.getByText("Me").className).toBe("sr-only");
    fireEvent.click(toggle());
    expect(menu().hidden).toBe(false);
  });

  it("signs out, then goes to /login (FR-2.2)", async () => {
    mockLogout.mockResolvedValue({ ok: true, data: undefined });
    renderMenu();
    fireEvent.click(toggle());
    fireEvent.click(screen.getByRole("menuitem", { name: "Sign out" }));
    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith("/login"));
    expect(mockLogout).toHaveBeenCalledTimes(1);
    expect(mockSetSignedOut).toHaveBeenCalledTimes(1);
  });
});
