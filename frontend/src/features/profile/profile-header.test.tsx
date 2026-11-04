import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { acceptConnectionRequest, removeConnection } from "@/features/social/api";

import type { Profile } from "./api";
import { ProfileHeader } from "./profile-header";
import type { EditControls } from "./profile-sections";
import { adaID, bobID, fullProfile } from "./test-profiles";

const photoUrl = "/api/v1/media/photo";
const coverUrl = "/api/v1/media/cover";

function withImages(userId: string, name: string) {
  return { ...fullProfile(userId, name), photoUrl, coverUrl };
}

jest.mock("@/features/auth/session", () => ({
  useSession: () => ({
    status: "signedIn",
    user: { id: adaID, email: "ada@example.org", fullName: "Ada Lovelace", role: "researcher" },
  }),
}));
jest.mock("@/features/social/api");
jest.mock("@/features/social/pending-invitations");
jest.mock("@/features/notification/badges");
afterEach(() => jest.resetAllMocks());

// makeEditControls returns every EditControls member as a mock, with overrides applied.
function makeEditControls(overrides: Partial<EditControls> = {}): EditControls {
  return {
    open: jest.fn(),
    editExperience: jest.fn(),
    editLanguage: jest.fn(),
    ...overrides,
  };
}

describe("ProfileHeader", () => {
  it("lets the owner change the cover and the photo", () => {
    const open = jest.fn();
    const { container } = render(
      <ProfileHeader
        profile={withImages(adaID, "Ada Lovelace")}
        edit={makeEditControls({ open })}
      />,
    );

    const cover = screen.getByRole("button", { name: "Edit cover image" });
    expect(cover.id).toBe("edit-cover-button");
    fireEvent.click(cover);
    expect(open).toHaveBeenLastCalledWith("cover");

    const photo = screen.getByRole("button", { name: "Change profile photo" });
    expect(photo.id).toBe("edit-photo-button");
    expect(photo.querySelector("img")?.getAttribute("src")).toBe(photoUrl);
    fireEvent.click(photo);
    expect(open).toHaveBeenLastCalledWith("photo");

    expect(container.querySelector(`img[src="${coverUrl}"]`)).toBeTruthy();
  });

  it("shows another researcher's images without edit buttons", () => {
    const { container } = render(
      <ProfileHeader profile={withImages(bobID, "Bob Babbage")} />,
    );
    expect(
      screen.queryByRole("button", { name: "Edit cover image" }),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Change profile photo" }),
    ).toBeNull();
    expect(
      screen.getByRole("img", { name: "Bob Babbage" }).getAttribute("src"),
    ).toBe(photoUrl);
    expect(container.querySelector(`img[src="${coverUrl}"]`)).toBeTruthy();
  });

  it("shows the initials and the gradient when there are no images", () => {
    const { container } = render(
      <ProfileHeader profile={fullProfile(bobID, "Bob Babbage")} />,
    );
    expect(screen.getByRole("img", { name: "Bob Babbage" }).textContent).toBe(
      "BB",
    );
    expect(container.querySelector("img")).toBeNull();
  });
});

describe("ProfileHeader, connections", () => {
  const requestID = "7a1d2c3e-0000-4000-8000-0000000000bb";
  const bob = (over: Partial<Profile> = {}): Profile => ({
    ...fullProfile(bobID, "Bob Babbage"),
    ...over,
  });
  const card = () =>
    screen.getByRole("heading", { level: 1, name: "Bob Babbage" }).closest("div.overflow-hidden") as HTMLElement;

  it("links another user's count to their connections list", () => {
    render(<ProfileHeader profile={bob({ connectionCount: 312 })} />);
    expect(
      screen.getByRole("link", { name: "312 connections" }).getAttribute("href"),
    ).toBe(`/profile/${bobID}/connections`);
  });

  it("links the owner's count to My Network", () => {
    render(
      <ProfileHeader
        profile={{ ...fullProfile(adaID, "Ada Lovelace"), connectionCount: 1 }}
        edit={makeEditControls()}
      />,
    );
    expect(
      screen.getByRole("link", { name: "1 connection" }).getAttribute("href"),
    ).toBe("/network");
    expect(screen.queryByRole("button", { name: /connect/i })).toBeNull();
  });

  it("hides the count at 0", () => {
    render(<ProfileHeader profile={bob()} />);
    expect(screen.queryByRole("link", { name: /connections?$/ })).toBeNull();
  });

  it("puts Connect before More when not connected", () => {
    render(<ProfileHeader profile={bob()} />);
    const buttons = within(card()).getAllByRole("button");
    const names = buttons.map((b) => b.getAttribute("aria-label") ?? b.textContent);
    expect(names.slice(-2)).toEqual(["Connect with Bob Babbage", "More"]);
    expect(screen.queryByText("Connected")).toBeNull();
  });

  it("shows Connected as status text and offers Remove connection in More", async () => {
    jest.mocked(removeConnection).mockResolvedValue({ ok: true, data: undefined });
    render(
      <ProfileHeader profile={bob({ connectionStatus: "connected", connectionCount: 5 })} />,
    );
    const label = screen.getByText("Connected");
    expect(label.tagName).toBe("SPAN");
    expect(label.closest("button, a, [tabindex]")).toBeNull();
    expect(screen.queryByRole("button", { name: /connect with/i })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "More" }));
    const menu = screen.getByRole("menu", { name: "More" });
    expect(
      within(menu).getAllByRole("menuitem").map((item) => item.textContent),
    ).toEqual(["Remove connection", "Copy profile link"]);
    fireEvent.click(within(menu).getByRole("menuitem", { name: "Remove connection" }));
    expect(within(menu).getByText("Remove Bob Babbage from your connections?")).toBeTruthy();
    expect(removeConnection).not.toHaveBeenCalled();

    await act(async () =>
      fireEvent.click(within(menu).getByRole("button", { name: "Remove" })),
    );
    expect(removeConnection).toHaveBeenCalledWith(bobID);
    expect(screen.queryByText("Connected")).toBeNull();
    expect(screen.getByRole("button", { name: "Connect with Bob Babbage" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "4 connections" })).toBeTruthy();
  });

  it("shows Connected and counts one more after accepting their request", async () => {
    jest.mocked(acceptConnectionRequest).mockResolvedValue({
      ok: true,
      data: {
        connectedAt: "2026-11-01T10:00:00Z",
        userId: bobID,
        fullName: "Bob Babbage",
        headline: null,
        institution: null,
        photoUrl: null,
        connectionStatus: "connected",
        connectionRequestId: null,
      },
    });
    render(
      <ProfileHeader
        profile={bob({ connectionStatus: "pending_received", connectionRequestId: requestID })}
      />,
    );
    await act(async () =>
      fireEvent.click(screen.getByRole("button", { name: "Accept request from Bob Babbage" })),
    );
    expect(acceptConnectionRequest).toHaveBeenCalledWith(requestID);
    expect(screen.getByText("Connected")).toBeTruthy();
    expect(screen.getByRole("link", { name: "1 connection" })).toBeTruthy();
  });
});
