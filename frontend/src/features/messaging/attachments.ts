import type { UploadFailure } from "@/lib/api/upload";

import { waitText } from "./format";
import { isStale, sendNotice } from "./send-errors";

// The server's attachment limits (FR-16.7, docs/openapi.yaml). The client checks the type and
// size first to save a pointless upload; the server reads the type from the content and
// checks again.
export const maxFiles = 5;
export const maxFileSize = 25 * 1024 * 1024;

type Kind = { label: string; tone: string };

const kinds = {
  pdf: { label: "PDF", tone: "bg-rs-danger-soft text-rs-danger-strong" },
  image: { label: "IMG", tone: "bg-rs-violet-soft text-rs-violet" },
  doc: { label: "DOC", tone: "bg-rs-accent-soft text-rs-accent-deep" },
  sheet: { label: "XLS", tone: "bg-rs-subtle text-rs-success" },
  slides: { label: "PPT", tone: "bg-rs-orange-soft text-rs-orange" },
  other: { label: "FILE", tone: "bg-rs-subtle text-rs-text-muted" },
} satisfies Record<string, Kind>;

const byType: Record<string, keyof typeof kinds> = {
  "application/pdf": "pdf",
  "image/png": "image",
  "image/jpeg": "image",
  "image/webp": "image",
  "application/vnd.openxmlformats-officedocument.wordprocessingml.document": "doc",
  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": "sheet",
  "application/vnd.openxmlformats-officedocument.presentationml.presentation": "slides",
};

const byExtension: Record<string, keyof typeof kinds> = {
  pdf: "pdf",
  png: "image",
  jpg: "image",
  jpeg: "image",
  webp: "image",
  docx: "doc",
  xlsx: "sheet",
  pptx: "slides",
};

// accept is the file picker's filter.
export const accept = [...Object.keys(byExtension).map((e) => `.${e}`), ...Object.keys(byType)].join(",");

function extension(name: string): string {
  const dot = name.lastIndexOf(".");
  return dot < 0 ? "" : name.slice(dot + 1).toLowerCase();
}

// fileKind is the badge of a file card: its type from the content type the server found, or
// from the name.
export function fileKind(contentType: string, name: string): Kind {
  return kinds[byType[contentType] ?? byExtension[extension(name)] ?? "other"];
}

// isImage says whether a pasted file is an image, the only kind a paste attaches.
export function isImage(file: File): boolean {
  return byType[file.type] === "image";
}

// checkFile returns why a file cannot be attached, or null when it may be. A browser may give
// an Office file no type, so the extension decides too.
export function checkFile(file: File): string | null {
  const known = byType[file.type] ?? byExtension[extension(file.name)];
  if (!known) return `${file.name} can't be attached. Use a PDF, PNG, JPEG, WebP, DOCX, XLSX or PPTX file.`;
  if (file.size > maxFileSize) return `${file.name} is over 25 MB.`;
  if (file.size === 0) return `${file.name} is empty.`;
  return null;
}

// attachmentUrl is where a file downloads from: the API checks access and the scan, then
// redirects to a short-lived link that saves the file.
export function attachmentUrl(attachmentId: string): string {
  return `/api/v1/attachments/${encodeURIComponent(attachmentId)}`;
}

// fileSize writes a size as the design does: "2.4 MB", "820 KB", "512 bytes".
export function fileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} ${bytes === 1 ? "byte" : "bytes"}`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  const mb = bytes / (1024 * 1024);
  return `${mb < 10 ? mb.toFixed(1) : Math.round(mb)} MB`;
}

// uploadError is the line on a chip whose upload was refused. 413 and 415 are read from the
// status, as a proxy's answer may not be in the API's error shape.
export function uploadError(failure: UploadFailure): string {
  if (failure.status === 413 || failure.code === "file_too_large") return "This file is over 25 MB.";
  if (failure.status === 415 || failure.code === "unsupported_type") {
    return "This type of file can't be sent. Use a PDF, PNG, JPEG, WebP, DOCX, XLSX or PPTX file.";
  }
  if (failure.status === 429 || failure.code === "rate_limited") {
    return `You've reached the upload limit. Try again ${waitText(failure.retryAfter)}.`;
  }
  if (failure.code === "network_error") return "Upload failed. Check your connection and try again.";
  if (isStale(failure.code)) return sendNotice(failure).text;
  if (failure.code === "messaging_restricted" || failure.code === "account_suspended") return failure.message;
  return "Upload failed. Try again.";
}
