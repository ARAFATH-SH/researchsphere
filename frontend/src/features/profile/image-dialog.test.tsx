import "@/test/dialog-polyfill";

import { fireEvent, render, screen } from "@testing-library/react";

import { type ImageKind, removeProfileImage, uploadProfileImage } from "./api";
import { ImageDialog } from "./image-dialog";

jest.mock("./api");
const mockUpload = jest.mocked(uploadProfileImage);
const mockRemove = jest.mocked(removeProfileImage);

const MB = 1024 * 1024;
const photoUrl = "/api/v1/media/3f1c2b9e-8a41-4d7e-9c55-2f6a1b0e7d10";

// jsdom has no object URLs.
beforeAll(() => {
  URL.createObjectURL = jest.fn(() => "blob:preview");
  URL.revokeObjectURL = jest.fn();
});
afterEach(() => jest.clearAllMocks());

function renderDialog(kind: ImageKind = "photo", currentUrl: string | null = photoUrl) {
  const onClose = jest.fn();
  const onChanged = jest.fn();
  render(
    <ImageDialog kind={kind} name="Ada Lovelace" currentUrl={currentUrl} onClose={onClose} onChanged={onChanged} />,
  );
  return { onClose, onChanged };
}

// choose picks a file in the hidden file input; size is reported without allocating it.
function choose(type: string, size = 1000, kind: ImageKind = "photo") {
  const file = new File(["x"], "picture", { type });
  Object.defineProperty(file, "size", { value: size });
  const label = kind === "photo" ? "Choose a profile photo" : "Choose a cover image";
  fireEvent.change(screen.getByLabelText(label), { target: { files: [file] } });
  return file;
}

const button = (name: string) => screen.getByRole("button", { name });

describe("ImageDialog", () => {
  it("shows the current photo with Upload photo, Delete and Cancel", () => {
    const { onClose } = renderDialog();
    expect(screen.getByRole("heading", { name: "Profile photo" })).toBeTruthy();
    expect(screen.getByRole("img", { name: "Ada Lovelace" }).getAttribute("src")).toBe(photoUrl);
    expect(document.activeElement).toBe(button("Upload photo"));
    expect(button("Delete")).toBeTruthy();
    fireEvent.click(button("Cancel"));
    expect(onClose).toHaveBeenCalled();
  });

  it("shows the initials and no Delete when there is no photo", () => {
    renderDialog("photo", null);
    expect(screen.getByRole("img", { name: "Ada Lovelace" }).textContent).toBe("AL");
    expect(screen.queryByRole("button", { name: "Delete" })).toBeNull();
  });

  it("is titled Cover image for the cover", () => {
    renderDialog("cover", null);
    expect(screen.getByRole("heading", { name: "Cover image" })).toBeTruthy();
    expect(button("Upload image")).toBeTruthy();
  });

  it.each([
    ["photo", "image/gif", 1000, "Use a JPG, PNG or WebP image."],
    ["photo", "image/jpeg", 5 * MB + 1, "This file is too large (max 5 MB)."],
    ["cover", "image/png", 8 * MB + 1, "This file is too large (max 8 MB)."],
  ] as const)("refuses a %s file of type %j and %d bytes without uploading", (kind, type, size, message) => {
    renderDialog(kind, null);
    choose(type, size, kind);
    expect(screen.getByRole("alert").textContent).toBe(message);
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
    expect(mockUpload).not.toHaveBeenCalled();
  });

  it("previews a chosen file and uploads it on Save, disabling the buttons meanwhile", async () => {
    let finish: (value: { ok: true }) => void = () => {};
    mockUpload.mockReturnValue(new Promise((resolve) => (finish = resolve)));
    const { onChanged } = renderDialog();
    const file = choose("image/png");

    expect(screen.getByRole("img", { name: "Preview of the new profile photo" }).getAttribute("src")).toBe(
      "blob:preview",
    );
    expect(document.activeElement).toBe(button("Save"));
    fireEvent.click(button("Save"));

    expect(mockUpload).toHaveBeenCalledWith("photo", file);
    expect(button("Uploading…")).toHaveProperty("disabled", true);
    expect(button("Cancel")).toHaveProperty("disabled", true);
    finish({ ok: true });
    await screen.findByRole("button", { name: "Save" });
    expect(onChanged).toHaveBeenCalledWith("Photo updated");
  });

  it("goes back to the current photo when the chosen file is cancelled", () => {
    renderDialog();
    choose("image/jpeg");
    fireEvent.click(button("Cancel"));
    expect(screen.getByRole("img", { name: "Ada Lovelace" }).getAttribute("src")).toBe(photoUrl);
    expect(document.activeElement).toBe(button("Upload photo"));
    expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:preview");
  });

  it.each([
    [{ status: 413 }, "This file is too large (max 5 MB)."],
    [{ status: 415, code: "unsupported_type" }, "Use a JPG, PNG or WebP image."],
    [{ status: 400, code: "image_too_large" }, "This image is too large. Use one under 8000 px on each side."],
    [{ status: 429, retryAfter: 1500 }, "You've reached the upload limit. Try again in 25 minutes."],
    [{ status: 500, code: "internal_error" }, "Upload failed. Please try again."],
    [{ status: 0 }, "Upload failed. Please try again."],
  ])("shows the message for a refused upload %j", async (failure, message) => {
    mockUpload.mockResolvedValue({ ok: false, ...failure });
    const { onChanged } = renderDialog();
    choose("image/jpeg");
    fireEvent.click(button("Save"));
    expect((await screen.findByRole("alert")).textContent).toBe(message);
    expect(onChanged).not.toHaveBeenCalled();
    // The file stays chosen, so Save can be tried again.
    expect(button("Save")).toHaveProperty("disabled", false);
  });

  it("asks inside the dialog before deleting, and Keep leaves the photo", () => {
    const confirm = jest.spyOn(window, "confirm");
    renderDialog();
    fireEvent.click(button("Delete"));
    expect(screen.getByText("Delete your profile photo?")).toBeTruthy();
    expect(document.activeElement).toBe(button("Keep"));
    fireEvent.click(button("Keep"));
    expect(screen.queryByText("Delete your profile photo?")).toBeNull();
    expect(document.activeElement).toBe(button("Delete"));
    expect(mockRemove).not.toHaveBeenCalled();
    expect(confirm).not.toHaveBeenCalled();
    confirm.mockRestore();
  });

  it("deletes after confirmation", async () => {
    mockRemove.mockResolvedValue({ ok: true });
    const { onChanged } = renderDialog("cover", "/api/v1/media/cover");
    fireEvent.click(button("Delete"));
    expect(screen.getByText("Delete your cover image?")).toBeTruthy();
    fireEvent.click(button("Delete"));
    expect(button("Deleting…")).toHaveProperty("disabled", true);
    expect(button("Keep")).toHaveProperty("disabled", true);
    expect(mockRemove).toHaveBeenCalledWith("cover");
    await screen.findByRole("button", { name: "Delete" });
    expect(onChanged).toHaveBeenCalledWith("Cover image deleted");
  });

  it("says so when deleting fails", async () => {
    mockRemove.mockResolvedValue({ ok: false, status: 500 });
    const { onChanged } = renderDialog();
    fireEvent.click(button("Delete"));
    fireEvent.click(button("Delete"));
    expect((await screen.findByRole("alert")).textContent).toBe("Delete failed. Please try again.");
    expect(onChanged).not.toHaveBeenCalled();
  });
});
