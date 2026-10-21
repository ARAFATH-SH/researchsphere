import { fireEvent, render, screen } from "@testing-library/react";

import { Avatar } from "./avatar";

const photo = "/api/v1/media/3f1c2b9e-8a41-4d7e-9c55-2f6a1b0e7d10";

describe("Avatar", () => {
  it("shows the photo with the full name as alt text", () => {
    render(<Avatar name="Ada Lovelace" src={photo} size="md" />);
    const img = screen.getByRole("img", { name: "Ada Lovelace" });
    expect(img.tagName).toBe("IMG");
    expect(img.getAttribute("src")).toBe(photo);
    expect(img.textContent).toBe("");
  });

  it("shows the initials when there is no photo", () => {
    render(<Avatar name="Ada King Lovelace" src={null} size="lg" />);
    const avatar = screen.getByRole("img", { name: "Ada King Lovelace" });
    expect(avatar.tagName).toBe("SPAN");
    expect(avatar.textContent).toBe("AL");
  });

  it("falls back to the initials when the photo does not load, and tries a new photo again", () => {
    const { rerender } = render(<Avatar name="Ada Lovelace" src={photo} size="sm" />);
    fireEvent.error(screen.getByRole("img", { name: "Ada Lovelace" }));
    expect(screen.getByRole("img", { name: "Ada Lovelace" }).textContent).toBe("AL");

    rerender(<Avatar name="Ada Lovelace" src="/api/v1/media/other" size="sm" />);
    expect(screen.getByRole("img", { name: "Ada Lovelace" }).tagName).toBe("IMG");
  });

  it("is hidden from screen readers when decorative", () => {
    const { container } = render(<Avatar name="Ada Lovelace" src={photo} size="sm" decorative />);
    expect(screen.queryByRole("img")).toBeNull();
    const img = container.querySelector("img");
    expect(img?.getAttribute("alt")).toBe("");
    expect(img?.getAttribute("aria-hidden")).toBe("true");
  });
});
