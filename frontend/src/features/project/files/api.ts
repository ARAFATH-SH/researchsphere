import { type ApiError, call, networkError, type Result, unexpectedError } from "@/lib/api/call";
import { api, type components } from "@/lib/api/client";
import { aborted, postToStorage, type UploadOptions } from "@/lib/api/upload";

export type ProjectFile = components["schemas"]["ProjectFile"];
export type ProjectFileKind = components["schemas"]["ProjectFileKind"];
export type ProjectFileTree = components["schemas"]["ProjectFileTree"];
export type ProjectFileDetail = components["schemas"]["ProjectFileDetail"];
export type ProjectFileCurrent = components["schemas"]["ProjectFileCurrent"];
export type ProjectStorage = components["schemas"]["ProjectStorage"];

// maxTextBytes is the largest text file the server stores (1 MiB of UTF-8).
export const maxTextBytes = 1 << 20;
// defaultFigureMaxBytes is the largest figure when the server says nothing else (FR-12.2):
// 50 MB, FIGURE_MAX_BYTES' default. The tree's storage.figureMaxBytes is the real limit.
export const defaultFigureMaxBytes = 50 << 20;

const mb = 1 << 20;

// formatBytes writes a size as the server does (MB of 2^20 bytes): "120 MB", "12.5 MB",
// rounded down so a project just under its limit never reads as full; under 1 MB in KB.
export function formatBytes(n: number): string {
  if (n === 0) return "0 MB";
  if (n < mb) return `${Math.ceil(n / 1024)} KB`;
  if (n % mb === 0) return `${n / mb} MB`;
  return `${(Math.floor((n * 10) / mb) / 10).toFixed(1)} MB`;
}

function withData<T>(res: Result<T | undefined>): Result<T> {
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

// listFiles returns the project's whole file tree, sorted by path ignoring case. It is not
// paginated: a project has at most 200 entries (docs/decisions.md, 2026-11-17).
export async function listFiles(projectId: string): Promise<Result<ProjectFileTree>> {
  return withData(await call(() => api.GET("/projects/{projectId}/files", { params: { path: { projectId } } })));
}

// getFile reads one file: a text file with its content, a clean figure with a download URL
// valid for 5 minutes. A figure still being scanned is 409 scan_pending.
export async function getFile(projectId: string, fileId: string): Promise<Result<ProjectFileDetail>> {
  return withData(
    await call(() => api.GET("/projects/{projectId}/files/{fileId}", { params: { path: { projectId, fileId } } })),
  );
}

// createFile creates an empty text file or a folder (FR-12.1); missing folders on the way are created.
export async function createFile(
  projectId: string,
  body: { path: string; kind: "text" | "folder" },
): Promise<Result<ProjectFile>> {
  return withData(
    await call(() => api.POST("/projects/{projectId}/files", { params: { path: { projectId } }, body })),
  );
}

// moveFile renames or moves a file or folder, a folder with everything in it (FR-12.1).
export async function moveFile(projectId: string, fileId: string, path: string): Promise<Result<ProjectFile>> {
  return withData(
    await call(() =>
      api.PATCH("/projects/{projectId}/files/{fileId}", { params: { path: { projectId, fileId } }, body: { path } }),
    ),
  );
}

// deleteFile deletes a file, or a folder with everything in it (FR-12.1).
export async function deleteFile(projectId: string, fileId: string): Promise<Result<undefined>> {
  const res = await call(() =>
    api.DELETE("/projects/{projectId}/files/{fileId}", { params: { path: { projectId, fileId } } }),
  );
  return res.ok ? { ok: true, data: undefined } : res;
}

// SaveResult is a content save's outcome. status is the HTTP status, 0 when the server could
// not be reached; current is the file as it is now, on a version_conflict.
export type SaveResult =
  | { ok: true; data: ProjectFile }
  | ({ ok: false; status: number; current?: ProjectFileCurrent } & ApiError);

// saveContent stores a text file's whole content if it is still at version (FR-12.6). keepalive
// lets the request outlive the page, for the save sent while the tab closes (bodies up to 64 KB).
export async function saveContent(
  projectId: string,
  fileId: string,
  content: string,
  version: number,
  { keepalive = false }: { keepalive?: boolean } = {},
): Promise<SaveResult> {
  try {
    const { data, error, response } = await api.PUT("/projects/{projectId}/files/{fileId}/content", {
      params: { path: { projectId, fileId } },
      body: { content, version },
      keepalive,
    });
    if (response.ok && data) return { ok: true, data };
    if (error?.code && error.message) {
      // Only a 409 version_conflict carries current (FileVersionConflict).
      const { current } = error as { current?: ProjectFileCurrent };
      return {
        ok: false,
        status: response.status,
        code: error.code,
        message: error.message,
        ...(current ? { current } : {}),
      };
    }
    return { ok: false, status: response.status, ...unexpectedError };
  } catch {
    return { ok: false, status: 0, ...networkError };
  }
}

// UploadStep is the step of a figure upload: the API's start, the POST to storage, the API's
// completion.
export type UploadStep = "start" | "storage" | "complete";

// FigureUploadFailure says which step failed; status is storage's HTTP status for the storage
// step (0 when it could not be reached, as for a network or CORS failure).
export type FigureUploadFailure = { ok: false; step: UploadStep; status?: number } & ApiError;
export type FigureUploadResult = { ok: true; data: ProjectFile } | FigureUploadFailure;

function failed(step: UploadStep, error: ApiError, status?: number): FigureUploadFailure {
  const failure: FigureUploadFailure = { ok: false, step, code: error.code, message: error.message, ...(status === undefined ? {} : { status }) };
  if (process.env.NODE_ENV === "development" && error.code !== "aborted") {
    // The browser hides a CORS refusal from scripts: storage's status 0 is all it says.
    const hint = step === "storage" && status === 0 ? " (network error, or storage refused this origin: CORS)" : "";
    console.warn(`Figure upload failed at the ${step} step${hint}:`, failure);
  }
  return failure;
}

// uploadFigure sends a PNG, JPG or PDF figure to path (FR-12.2) in the server's three steps
// (docs/decisions.md 2026-11-18): the API signs a POST of exactly this size, the browser sends
// the file straight to storage with progress, and the API checks and records it. The file
// never passes through the API. A failure names its step.
export async function uploadFigure(
  projectId: string,
  path: string,
  file: File,
  { onProgress, signal }: UploadOptions = {},
): Promise<FigureUploadResult> {
  const started = withData(
    await call(() =>
      api.POST("/projects/{projectId}/files/uploads", {
        params: { path: { projectId } },
        body: { path, size: file.size },
      }),
    ),
  );
  if (!started.ok) return failed("start", started);
  if (signal?.aborted) return failed("start", aborted);
  const sent = await postToStorage(started.data.url, started.data.fields, file, { onProgress, signal });
  if (!sent.ok) return failed("storage", sent, sent.status);
  const done = withData(
    await call(() =>
      api.POST("/projects/{projectId}/files/uploads/{uploadId}/complete", {
        params: { path: { projectId, uploadId: started.data.id } },
      }),
    ),
  );
  return done.ok ? done : failed("complete", done);
}

const fileErrors: Record<string, string> = {
  path_taken: "Something with this name is already there. Names ignore letter case.",
  parent_not_folder: "A part of this path is a file, not a folder.",
  file_limit_reached: "A project can have at most 200 files and folders.",
  invalid_move: "A folder can’t move into itself or a folder inside it.",
  type_mismatch: "The file’s content doesn’t match its extension. Use .png, .jpg or .pdf to match the file.",
  unsupported_type: "Only PNG, JPG and PDF figures can be uploaded.",
  invalid_image: "This image could not be read. It may be damaged.",
  upload_incomplete: "The file didn’t reach storage. Upload it again.",
  storage_failed: "The file couldn’t be sent to storage. Try again.",
  rate_limited: "Too many uploads. Wait a while and try again.",
  forbidden: "Only editors and the owner can change files.",
  account_suspended: "Your account is suspended, so you can’t change files.",
  not_found: "This file no longer exists. Reload the page to see the current files.",
  not_a_text_file: "Only text files can be edited.",
};

// serverWords are the codes whose message the server writes with the numbers that matter:
// the path rules, the figure limit, the project's storage in use.
const serverWords = new Set(["invalid_path", "file_too_large", "storage_limit_reached"]);

// fileErrorText is what to tell the user about a refused file action.
export function fileErrorText(error: ApiError): string {
  if (serverWords.has(error.code)) return error.message;
  return fileErrors[error.code] ?? error.message;
}
