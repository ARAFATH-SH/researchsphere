import { type ApiError, call, type Result, unexpectedError } from "@/lib/api/call";
import { api, type components } from "@/lib/api/client";

// The compile operations of slice 23 (docs/compile.md, FR-13). Every call goes through the one
// API client, downloads included, so a lapsed session is refreshed and a refusal has its text.

export type Compile = components["schemas"]["Compile"];
export type CompileStatus = components["schemas"]["CompileStatus"];
export type CompileError = components["schemas"]["CompileError"];
export type CompileFailure = components["schemas"]["CompileFailure"];

export type Download = { blob: Blob; name: string };

function withData<T>(res: Result<T | undefined>): Result<T> {
  if (!res.ok) return res;
  return res.data !== undefined ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

// startCompile queues a compile of the main file (FR-13.1). 409 compile_running names the
// compile in compileId (FR-13.4).
export async function startCompile(projectId: string): Promise<Result<Compile>> {
  return withData(await call(() => api.POST("/projects/{projectId}/compiles", { params: { path: { projectId } } })));
}

// getLatestCompile reads the project's newest compile in any status; 404 no_compile when none.
export async function getLatestCompile(projectId: string): Promise<Result<Compile>> {
  return withData(await call(() => api.GET("/projects/{projectId}/compiles/latest", { params: { path: { projectId } } })));
}

export async function getCompile(projectId: string, compileId: string): Promise<Result<Compile>> {
  return withData(
    await call(() => api.GET("/projects/{projectId}/compiles/{compileId}", { params: { path: { projectId, compileId } } })),
  );
}

// getCompileLog reads the log of the newest finished compile (FR-13.2); 404 no_log otherwise.
export async function getCompileLog(projectId: string, compileId: string): Promise<Result<string>> {
  return withData(
    await call(() =>
      api.GET("/projects/{projectId}/compiles/{compileId}/log", { params: { path: { projectId, compileId } }, parseAs: "text" }),
    ),
  );
}

// fetchPdf reads the latest PDF for the viewer (FR-13.1) with the compile it came from.
export async function fetchPdf(projectId: string): Promise<Result<{ data: ArrayBuffer; compileId: string | null }>> {
  let compileId: string | null = null;
  const res = await call(async () => {
    const r = await api.GET("/projects/{projectId}/pdf", {
      params: { path: { projectId }, query: { disposition: "inline" } },
      parseAs: "arrayBuffer",
    });
    compileId = r.response.headers?.get("X-Compile-Id") ?? null;
    return r as { data?: ArrayBuffer; error?: ApiError; response: Response };
  });
  if (!res.ok) return res;
  return res.data ? { ok: true, data: { data: res.data, compileId } } : { ok: false, ...unexpectedError };
}

async function download(
  request: () => Promise<{ data?: unknown; error?: unknown; response: Response }>,
  fallbackName: string,
): Promise<Result<Download>> {
  let name = fallbackName;
  const res = await call(async () => {
    const r = await request();
    name = fileName(r.response.headers?.get("Content-Disposition") ?? null) ?? fallbackName;
    return r as { data?: Blob; error?: ApiError; response: Response };
  });
  if (!res.ok) return res;
  return res.data ? { ok: true, data: { blob: res.data, name } } : { ok: false, ...unexpectedError };
}

// downloadPdf saves the latest PDF (FR-13.6).
export function downloadPdf(projectId: string): Promise<Result<Download>> {
  return download(
    () => api.GET("/projects/{projectId}/pdf", { params: { path: { projectId }, query: { disposition: "attachment" } }, parseAs: "blob" }),
    "project.pdf",
  );
}

// downloadArchive saves the project's files as a ZIP (FR-13.7).
export function downloadArchive(projectId: string): Promise<Result<Download>> {
  return download(() => api.GET("/projects/{projectId}/archive", { params: { path: { projectId } }, parseAs: "blob" }), "project.zip");
}

// fileName reads the name a Content-Disposition header gives, preferring filename*.
export function fileName(header: string | null): string | null {
  if (!header) return null;
  const star = /filename\*\s*=\s*(?:UTF-8|utf-8)''([^;]+)/.exec(header);
  if (star) {
    try {
      return clean(decodeURIComponent(star[1].trim()));
    } catch {
      // fall through to the plain name
    }
  }
  const plain = /filename\s*=\s*(?:"((?:[^"\\]|\\.)*)"|([^;]+))/.exec(header);
  const value = plain?.[1]?.replace(/\\(.)/g, "$1") ?? plain?.[2]?.trim();
  return value ? clean(value) : null;
}

// clean keeps a name to its last part, so a header cannot point a save into a folder.
function clean(name: string): string | null {
  const base = name.split(/[\\/]/).pop()?.trim() ?? "";
  return base === "" || base === "." || base === ".." ? null : base;
}

const startTexts: Record<string, string> = {
  compile_unavailable: "Compile service unavailable. Nothing was compiled; try again in a moment.",
  compile_busy: "Too many compiles are waiting right now. Try again in a minute.",
  main_file_missing: "The main file doesn’t exist. Create it, or choose another main file.",
  rate_limited: "You’ve started 10 compiles in the last minute. Wait a moment, then try again.",
  forbidden: "Only editors and the owner can compile.",
};

// compileErrorText says why a compile could not start.
export function compileErrorText(error: ApiError): string {
  return startTexts[error.code] ?? (error.code === "network_error" ? error.message : "The compile couldn’t start. Try again.");
}

const failureTexts: Record<CompileFailure, string> = {
  input_too_large: "The project’s files are larger than a compile can take.",
  output_too_large: "The PDF would be larger than 100 MB.",
  out_of_memory: "The compile ran out of memory (1 GB). Look for a very large figure or table.",
  no_pdf: "LaTeX finished without making a PDF.",
  worker_lost: "The compile service stopped answering. Try again.",
  sandbox_error: "The compile couldn’t run. Try again in a moment.",
  queue_expired: "No compile service took the compile within 5 minutes. Try again.",
};

export function failureText(failure: CompileFailure | null): string {
  return failure ? failureTexts[failure] : "LaTeX finished without making a PDF.";
}

const downloadTexts: Record<string, string> = {
  no_pdf: "There is no PDF yet. Compile the project first.",
  archive_too_large: "The project is too large to download as one ZIP.",
  rate_limited: "You’ve downloaded 10 archives in the last hour. Try again later.",
};

export function downloadErrorText(error: ApiError): string {
  return downloadTexts[error.code] ?? (error.code === "network_error" ? error.message : "The download didn’t work. Try again.");
}

// active says whether a compile is still queued or running (FR-13.4).
export function active(c: Compile): boolean {
  return c.status === "queued" || c.status === "running";
}
