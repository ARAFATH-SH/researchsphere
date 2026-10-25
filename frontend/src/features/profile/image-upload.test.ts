import {
  checkImageFile,
  imageMessages,
  uploadErrorMessage,
} from "./image-upload";

const MB = 1024 * 1024;

// file makes a File of the given type whose size is reported as size, without
// allocating that many bytes.
function file(type: string, size: number): File {
  const f = new File(["x"], "picture", { type });
  Object.defineProperty(f, "size", { value: size });
  return f;
}

describe("checkImageFile", () => {
  it.each([
    ["photo", "image/jpeg", 5 * MB, undefined],
    ["photo", "image/png", 1, undefined],
    ["photo", "image/webp", 1, undefined],
    ["photo", "image/gif", 1, imageMessages.type],
    ["photo", "", 1, imageMessages.type],
    ["photo", "image/jpeg", 5 * MB + 1, "This file is too large (max 5 MB)."],
    ["photo", "image/jpeg", 8 * MB, "This file is too large (max 5 MB)."],
    ["cover", "image/jpeg", 8 * MB, undefined],
    ["cover", "image/png", 8 * MB + 1, "This file is too large (max 8 MB)."],
    ["cover", "image/svg+xml", 1, imageMessages.type],
  ] as const)("%s %j of %d bytes gives %j", (kind, type, size, want) => {
    expect(checkImageFile(kind, file(type, size))).toBe(want);
  });
});

describe("uploadErrorMessage", () => {
  it.each([
    ["photo", { status: 413 }, "This file is too large (max 5 MB)."],
    ["cover", { status: 413 }, "This file is too large (max 8 MB)."],
    [
      "photo",
      { status: 415, code: "unsupported_type" },
      "Use a JPG, PNG or WebP image.",
    ],
    [
      "photo",
      { status: 400, code: "image_too_large" },
      "This image is too large. Use one under 8000 px on each side.",
    ],
    [
      "photo",
      { status: 400, code: "invalid_image" },
      "Upload failed. Please try again.",
    ],
    [
      "photo",
      { status: 429, retryAfter: 1 },
      "You've reached the upload limit. Try again in 1 minute.",
    ],
    [
      "photo",
      { status: 429, retryAfter: 60 },
      "You've reached the upload limit. Try again in 1 minute.",
    ],
    [
      "photo",
      { status: 429, retryAfter: 61 },
      "You've reached the upload limit. Try again in 2 minutes.",
    ],
    [
      "photo",
      { status: 429, retryAfter: 3599 },
      "You've reached the upload limit. Try again in 60 minutes.",
    ],
    ["photo", { status: 429 }, "Upload failed. Please try again."],
    ["photo", { status: 500 }, "Upload failed. Please try again."],
    ["photo", { status: 0 }, "Upload failed. Please try again."],
  ] as const)("%s %j gives %j", (kind, failure, want) => {
    expect(uploadErrorMessage(kind, { ok: false, ...failure })).toBe(want);
  });
});
