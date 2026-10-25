import type { ImageFailure, ImageKind } from "./api";

const MB = 1024 * 1024;

// imageLimits are the server's upload limits (FR-4.6, docs/openapi.yaml). The client
// checks the type and size first to save a pointless upload; the server checks again.
export const imageLimits: Record<
  ImageKind,
  { maxBytes: number; maxMB: number }
> = {
  photo: { maxBytes: 5 * MB, maxMB: 5 },
  cover: { maxBytes: 8 * MB, maxMB: 8 },
};

export const imageTypes = ["image/jpeg", "image/png", "image/webp"];

export const imageMessages = {
  type: "Use a JPG, PNG or WebP image.",
  size: (kind: ImageKind) =>
    `This file is too large (max ${imageLimits[kind].maxMB} MB).`,
  pixels: "This image is too large. Use one under 8000 px on each side.",
  rateLimited: (minutes: number) =>
    `You've reached the upload limit. Try again in ${minutes} ${minutes === 1 ? "minute" : "minutes"}.`,
  upload: "Upload failed. Please try again.",
  remove: "Delete failed. Please try again.",
};

// checkImageFile returns why a chosen file cannot be uploaded, or undefined when it may be.
export function checkImageFile(
  kind: ImageKind,
  file: File,
): string | undefined {
  if (!imageTypes.includes(file.type)) return imageMessages.type;
  if (file.size > imageLimits[kind].maxBytes) return imageMessages.size(kind);
  return undefined;
}

// uploadErrorMessage turns a refused upload into the words the dialog shows. A 429
// without a usable Retry-After gets the general message.
export function uploadErrorMessage(
  kind: ImageKind,
  failure: ImageFailure,
): string {
  switch (failure.status) {
    case 413:
      return imageMessages.size(kind);
    case 415:
      return imageMessages.type;
    case 400:
      return failure.code === "image_too_large"
        ? imageMessages.pixels
        : imageMessages.upload;
    case 429:
      if (failure.retryAfter === undefined || failure.retryAfter < 0)
        return imageMessages.upload;
      return imageMessages.rateLimited(
        Math.max(1, Math.ceil(failure.retryAfter / 60)),
      );
    default:
      return imageMessages.upload;
  }
}
