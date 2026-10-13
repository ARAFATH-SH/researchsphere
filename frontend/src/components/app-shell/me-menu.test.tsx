import { fireEvent, render, screen, waitFor } from "@testing-library/react";

import { logout } from "@/features/auth/api";
import { setSignedOut, useSession } from "@/features/auth/session";

import { initials, MeMenu } from "./me-menu";

jest.mock("@/features/auth/api");
jest.mock("@/features/auth/session");
const mockLogout = jest.mocked(logout);
const mockSetSignedOut = jest.mocked(setSignedOut);
const mockUseSession = jest.mocked(useSession);

const mockReplace = jest.fn();
jest.mock("next/navigation", () => ({ useRouter: () => ({ replace: mockReplace }) }));

const user = { id: "0b8f4c1e-0000-4000-8000-000000000001", email: "ada@example.org", fullName: "Ada King Lovelace" };

describe("initials", () => {
  it.each([
    ["Ada King Lovelace", "AL"],
    ["ada", "A"],
    ["  Émile   Zola ", "ÉZ"],
    ["", ""],
  ])("%j gives %j", (name, want) => expect(initials(name)).toBe(want));
});

describe("MeMenu", () => {
  afterEach(() => jest.resetAllMocks());

  const renderMenu = () => render(<MeMenu variant="top" active={false} tabClass="" />);
  const toggle = () => screen.getByRole("button", { name: /Me$/ });

  it("shows the user's initials, and their name and email in the menu", () => {
    mockUseSession.mockReturnValue({ status: "signedIn", user });
    renderMenu();
    expect(toggle().textContent).toBe("ALMe");
    expect(toggle().getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(toggle());
    expect(toggle().getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByText("Ada King Lovelace")).toBeTruthy();
    expect(screen.getByText("ada@example.org")).toBeTruthy();
    expect(screen.getByRole("link", { name: "View profile" }).getAttribute("href")).toBe("/profile");
  });

  it("links to the account settings and closes when the link is used", () => {
    mockUseSession.mockReturnValue({ status: "signedIn", user });
    renderMenu();
    fireEvent.click(toggle());
    const settings = screen.getByRole("link", { name: "Settings" });
    expect(settings.getAttribute("href")).toBe("/settings/account");
    // jsdom cannot follow a link; cancel only the browser's navigation, so the menu's own
    // click handler still runs.
    settings.addEventListener("click", (e) => e.preventDefault());
    fireEvent.click(settings);
    expect(toggle().getAttribute("aria-expanded")).toBe("false");
  });

  it("closes on Escape", () => {
    mockUseSession.mockReturnValue({ status: "signedIn", user });
    renderMenu();
    fireEvent.click(toggle());
    fireEvent.keyDown(document, { key: "Escape" });
    expect(toggle().getAttribute("aria-expanded")).toBe("false");
  });

  it("signs out, then goes to /login (FR-2.2)", async () => {
    mockUseSession.mockReturnValue({ status: "signedIn", user });
    mockLogout.mockResolvedValue({ ok: true, data: undefined });
    renderMenu();
    fireEvent.click(toggle());
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith("/login"));
    expect(mockLogout).toHaveBeenCalledTimes(1);
    expect(mockSetSignedOut).toHaveBeenCalledTimes(1);
  });
});
