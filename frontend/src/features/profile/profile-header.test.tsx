import { fireEvent, render, screen } from "@testing-library/react";

import { ProfileHeader } from "./profile-header";
import { adaID, bobID, fullProfile } from "./test-profiles";

const photoUrl = "/api/v1/media/photo";
const coverUrl = "/api/v1/media/cover";

function withImages(userId: string, name: string) {
  return { ...fullProfile(userId, name), photoUrl, coverUrl };
}

describe("ProfileHeader", () => {
  it("lets the owner change the cover and the photo", () => {
    const open = jest.fn();
    const { container } = render(
      <ProfileHeader
        profile={withImages(adaID, "Ada Lovelace")}
        edit={{ open, editExperience: jest.fn() }}
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
