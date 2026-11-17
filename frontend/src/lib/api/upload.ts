import { type ApiError, networkError, unexpectedError } from "./call";
import { refreshSession, reportSessionLost } from "./client";

// The one exception to "every API call goes through client.ts" (docs/decisions.md,
// 2026-11-10): fetch cannot report how much of a request body has been sent, so a file upload
// that shows progress uses XMLHttpRequest, here and nowhere else. postToStorage, at the end,
// is the only request that leaves the API's origin: a figure's presigned POST (2026-11-18). It keeps the client's rules:
// the same /api/v1 origin with the cookies, one refresh and one retry on 401 through
// refreshSession, and the same lost-session report when the refresh fails.

const baseUrl = "/api/v1";

// UploadFailure is a refused or failed upload. status is the HTTP status, or 0 when the server
// could not be reached; the message for a 413 or 415 depends on it, as a proxy's answer may not
// be in the API's error shape. code is "aborted" when the caller cancelled it.
export type UploadFailure = { ok: false; status: number; retryAfter?: number } & ApiError;
export type UploadResult<T> = { ok: true; data: T } | UploadFailure;

export type UploadOptions = {
  // onProgress gets the share of the file sent so far, from 0 to 1.
  onProgress?: (share: number) => void;
  signal?: AbortSignal;
};

export const aborted: ApiError = { code: "aborted", message: "The upload was cancelled." };

type Answer = { status: number; body: unknown; retryAfter?: number };

function send(path: string, file: File, { onProgress, signal }: UploadOptions): Promise<Answer | "network" | "aborted"> {
  return new Promise((resolve) => {
    if (signal?.aborted) {
      resolve("aborted");
      return;
    }
    const xhr = new XMLHttpRequest();
    xhr.open("POST", `${baseUrl}${path}`);
    xhr.withCredentials = true;
    xhr.responseType = "text";
    xhr.setRequestHeader("Accept", "application/json");
    const abort = () => xhr.abort();
    signal?.addEventListener("abort", abort, { once: true });
    const done = (value: Answer | "network" | "aborted") => {
      signal?.removeEventListener("abort", abort);
      resolve(value);
    };
    xhr.upload.addEventListener("progress", (event) => {
      if (event.lengthComputable && event.total > 0) onProgress?.(Math.min(1, event.loaded / event.total));
    });
    xhr.addEventListener("load", () => {
      let body: unknown = null;
      try {
        body = xhr.responseText ? JSON.parse(xhr.responseText) : null;
      } catch {
        body = null;
      }
      const header = Number(xhr.getResponseHeader("Retry-After"));
      done({ status: xhr.status, body, retryAfter: header > 0 ? header : undefined });
    });
    xhr.addEventListener("error", () => done("network"));
    xhr.addEventListener("timeout", () => done("network"));
    xhr.addEventListener("abort", () => done("aborted"));
    const form = new FormData();
    form.append("file", file);
    xhr.send(form);
  });
}

function isApiError(body: unknown): body is ApiError {
  const e = body as Partial<ApiError> | null;
  return typeof e?.code === "string" && typeof e.message === "string";
}

// uploadFile posts a file as multipart form data, in the part named "file", to an API path
// such as "/conversations/{id}/attachments", and reports progress on the way.
export async function uploadFile<T>(path: string, file: File, options: UploadOptions = {}): Promise<UploadResult<T>> {
  let answer = await send(path, file, options);
  if (answer !== "network" && answer !== "aborted" && answer.status === 401) {
    if ((await refreshSession()) !== "ok") {
      reportSessionLost();
    } else {
      options.onProgress?.(0);
      answer = await send(path, file, options);
    }
  }
  if (answer === "aborted") return { ok: false, status: 0, ...aborted };
  if (answer === "network") return { ok: false, status: 0, ...networkError };
  const { status, body, retryAfter } = answer;
  if (status >= 200 && status < 300) {
    return body === null ? { ok: false, status, ...unexpectedError } : { ok: true, data: body as T };
  }
  const error = isApiError(body) ? { code: body.code, message: body.message } : unexpectedError;
  return { ok: false, status, ...error, ...(retryAfter ? { retryAfter } : {}) };
}

// StorageResult is the outcome of a POST to object storage: status is storage's HTTP status,
// 0 when it could not be reached.
export type StorageResult = { ok: true } | ({ ok: false; status: number } & ApiError);

// storageFailed is a POST storage refused: an expired or changed form, or a different size.
export const storageFailed: ApiError = {
  code: "storage_failed",
  message: "The file couldn’t be sent to storage. Try again.",
};

// postToStorage sends a file with a presigned POST straight to object storage (FR-12.2,
// docs/decisions.md 2026-11-18): fields first, then the file in the part named "file", to a
// URL on the storage service, not the API. It sends no cookies, since the form's signature is
// the permission, and reports progress on the way.
export function postToStorage(
  url: string,
  fields: Record<string, string>,
  file: File,
  { onProgress, signal }: UploadOptions = {},
): Promise<StorageResult> {
  return new Promise((resolve) => {
    if (signal?.aborted) {
      resolve({ ok: false, status: 0, ...aborted });
      return;
    }
    const xhr = new XMLHttpRequest();
    xhr.open("POST", url);
    xhr.withCredentials = false;
    const abort = () => xhr.abort();
    signal?.addEventListener("abort", abort, { once: true });
    const done = (value: StorageResult) => {
      signal?.removeEventListener("abort", abort);
      resolve(value);
    };
    xhr.upload.addEventListener("progress", (event) => {
      if (event.lengthComputable && event.total > 0) onProgress?.(Math.min(1, event.loaded / event.total));
    });
    xhr.addEventListener("load", () =>
      done(xhr.status >= 200 && xhr.status < 300 ? { ok: true } : { ok: false, status: xhr.status, ...storageFailed }),
    );
    xhr.addEventListener("error", () => done({ ok: false, status: 0, ...networkError }));
    xhr.addEventListener("timeout", () => done({ ok: false, status: 0, ...networkError }));
    xhr.addEventListener("abort", () => done({ ok: false, status: 0, ...aborted }));
    const form = new FormData();
    for (const [name, value] of Object.entries(fields)) form.append(name, value);
    form.append("file", file);
    xhr.send(form);
  });
}
