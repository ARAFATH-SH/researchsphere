import { refreshSession, reportSessionLost } from "./client";
import { uploadFile } from "./upload";

jest.mock("./client", () => ({ refreshSession: jest.fn(), reportSessionLost: jest.fn() }));

// FakeXHR records each request; a test answers it with respond, fail or progress.
class FakeXHR {
  static all: FakeXHR[] = [];
  method = "";
  url = "";
  withCredentials = false;
  responseType = "";
  status = 0;
  responseText = "";
  headers: Record<string, string> = {};
  body: FormData | null = null;
  private listeners: Record<string, (() => void)[]> = {};
  private uploadListeners: ((e: { lengthComputable: boolean; loaded: number; total: number }) => void)[] = [];
  upload = {
    addEventListener: (_: string, fn: (e: { lengthComputable: boolean; loaded: number; total: number }) => void) => this.uploadListeners.push(fn),
  };

  constructor() {
    FakeXHR.all.push(this);
  }
  open(method: string, url: string) {
    this.method = method;
    this.url = url;
  }
  setRequestHeader() {}
  getResponseHeader(name: string) {
    return this.headers[name] ?? null;
  }
  addEventListener(type: string, fn: () => void) {
    (this.listeners[type] ??= []).push(fn);
  }
  send(body: FormData) {
    this.body = body;
  }
  abort() {
    this.emit("abort");
  }
  emit(type: string) {
    this.listeners[type]?.forEach((fn) => fn());
  }
  progress(loaded: number, total: number) {
    this.uploadListeners.forEach((fn) => fn({ lengthComputable: true, loaded, total }));
  }
  respond(status: number, body: unknown, headers: Record<string, string> = {}) {
    this.status = status;
    this.responseText = body === undefined ? "" : JSON.stringify(body);
    this.headers = headers;
    this.emit("load");
  }
}

const file = new File(["%PDF-1.7"], "draft.pdf", { type: "application/pdf" });
const latest = () => FakeXHR.all.at(-1)!;
const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

beforeEach(() => {
  FakeXHR.all = [];
  Object.defineProperty(globalThis, "XMLHttpRequest", { configurable: true, value: FakeXHR });
});
afterEach(() => jest.resetAllMocks());

describe("uploadFile", () => {
  it("posts the file as multipart to /api/v1 with the cookies and reports progress", async () => {
    const progress = jest.fn();
    const result = uploadFile("/conversations/c1/attachments", file, { onProgress: progress });
    const xhr = latest();
    expect(xhr.method).toBe("POST");
    expect(xhr.url).toBe("/api/v1/conversations/c1/attachments");
    expect(xhr.withCredentials).toBe(true);
    expect(xhr.body?.get("file")).toBe(file);
    xhr.progress(50, 100);
    expect(progress).toHaveBeenLastCalledWith(0.5);
    xhr.respond(201, { id: "a1" });
    await expect(result).resolves.toEqual({ ok: true, data: { id: "a1" } });
  });

  it("keeps the API error, the status and Retry-After", async () => {
    const result = uploadFile("/x", file);
    latest().respond(429, { code: "rate_limited", message: "Slow down." }, { "Retry-After": "120" });
    await expect(result).resolves.toEqual({ ok: false, status: 429, code: "rate_limited", message: "Slow down.", retryAfter: 120 });
  });

  it("keeps the status of an answer that is not in the error shape", async () => {
    const result = uploadFile("/x", file);
    latest().status = 413;
    latest().responseText = "<html>Too large</html>";
    latest().emit("load");
    await expect(result).resolves.toMatchObject({ ok: false, status: 413, code: "internal_error" });
  });

  it("refreshes once on 401 and retries once", async () => {
    jest.mocked(refreshSession).mockResolvedValue("ok");
    const result = uploadFile("/x", file);
    latest().respond(401, { code: "unauthenticated", message: "No." });
    await flush();
    expect(FakeXHR.all).toHaveLength(2);
    latest().respond(401, { code: "unauthenticated", message: "No." });
    await expect(result).resolves.toMatchObject({ ok: false, status: 401 });
    expect(refreshSession).toHaveBeenCalledTimes(1);
    expect(FakeXHR.all).toHaveLength(2);
  });

  it("reports a lost session when the refresh fails, and does not retry", async () => {
    jest.mocked(refreshSession).mockResolvedValue("refused");
    const result = uploadFile("/x", file);
    latest().respond(401, { code: "unauthenticated", message: "No." });
    await expect(result).resolves.toMatchObject({ ok: false, status: 401 });
    expect(reportSessionLost).toHaveBeenCalledTimes(1);
    expect(FakeXHR.all).toHaveLength(1);
  });

  it("answers a network failure and a cancel", async () => {
    const failed = uploadFile("/x", file);
    latest().emit("error");
    await expect(failed).resolves.toMatchObject({ ok: false, status: 0, code: "network_error" });

    const controller = new AbortController();
    const cancelled = uploadFile("/x", file, { signal: controller.signal });
    controller.abort();
    await expect(cancelled).resolves.toMatchObject({ ok: false, status: 0, code: "aborted" });
  });
});
