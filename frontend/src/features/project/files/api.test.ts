import { api } from "@/lib/api/client";
import { postToStorage } from "@/lib/api/upload";

import { fileErrorText, formatBytes, uploadFigure } from "./api";
import { figure } from "./test-files";

jest.mock("@/lib/api/client", () => ({ api: { POST: jest.fn() } }));
jest.mock("@/lib/api/upload", () => ({
  ...jest.requireActual("@/lib/api/upload"),
  postToStorage: jest.fn(),
}));
const mockPost = jest.mocked(api.POST);
const mockStorage = jest.mocked(postToStorage);

type Reply = Awaited<ReturnType<typeof api.POST>>;

function reply(status: number, body: object): Reply {
  const response = { status, ok: status < 400 };
  return (status < 400 ? { data: body, response } : { error: body, response }) as unknown as Reply;
}

const ticket = {
  id: "u1",
  url: "http://localhost:9000/researchsphere",
  fields: { key: "projects/p1/uploads/x", policy: "signed" },
  expiresAt: "2026-11-18T10:15:00Z",
};
const file = new File(["png bytes"], "plot.png", { type: "image/png" });

afterEach(() => jest.resetAllMocks());

// FR-12.2 (docs/decisions.md 2026-11-18): the API signs a POST of the file's size, the file
// goes straight to storage, then the API records it.
describe("uploadFigure", () => {
  it("starts, sends the file to storage with progress, and completes", async () => {
    const stored = figure("f1", "figures/plot.png", { scanStatus: "pending" });
    mockPost.mockResolvedValueOnce(reply(201, ticket)).mockResolvedValueOnce(reply(201, stored));
    mockStorage.mockResolvedValue({ ok: true });
    const onProgress = jest.fn();

    await expect(uploadFigure("p1", "figures/plot.png", file, { onProgress })).resolves.toEqual({ ok: true, data: stored });
    expect(mockPost).toHaveBeenNthCalledWith(1, "/projects/{projectId}/files/uploads", {
      params: { path: { projectId: "p1" } },
      body: { path: "figures/plot.png", size: file.size },
    });
    expect(mockStorage).toHaveBeenCalledWith(ticket.url, ticket.fields, file, { onProgress, signal: undefined });
    expect(mockPost).toHaveBeenNthCalledWith(2, "/projects/{projectId}/files/uploads/{uploadId}/complete", {
      params: { path: { projectId: "p1", uploadId: "u1" } },
    });
  });

  it("stops at the first step that fails, with its reason", async () => {
    const full = { code: "storage_limit_reached", message: "This project uses 480 MB of 500 MB.", usedBytes: 1, limitBytes: 2 };
    mockPost.mockResolvedValueOnce(reply(413, full));
    await expect(uploadFigure("p1", "a.png", file)).resolves.toEqual({ ok: false, step: "start", code: full.code, message: full.message });
    expect(mockStorage).not.toHaveBeenCalled();

    mockPost.mockResolvedValueOnce(reply(201, ticket));
    mockStorage.mockResolvedValue({ ok: false, status: 403, code: "storage_failed", message: "Storage said no." });
    await expect(uploadFigure("p1", "a.png", file)).resolves.toEqual({
      ok: false,
      step: "storage",
      status: 403,
      code: "storage_failed",
      message: "Storage said no.",
    });
    expect(mockPost).toHaveBeenCalledTimes(2); // never completed

    mockPost
      .mockResolvedValueOnce(reply(201, ticket))
      .mockResolvedValueOnce(reply(400, { code: "type_mismatch", message: "Not a PNG." }));
    mockStorage.mockResolvedValue({ ok: true });
    await expect(uploadFigure("p1", "a.png", file)).resolves.toMatchObject({ ok: false, step: "complete", code: "type_mismatch" });
  });

  it("logs the cause to the console in development, naming CORS for a storage failure without status", async () => {
    const env = process.env as Record<string, string | undefined>;
    const before = env.NODE_ENV;
    const warn = jest.spyOn(console, "warn").mockImplementation(() => {});
    try {
      env.NODE_ENV = "development";
      mockPost.mockResolvedValueOnce(reply(201, ticket));
      mockStorage.mockResolvedValue({ ok: false, status: 0, code: "network_error", message: "Could not reach." });
      await uploadFigure("p1", "a.png", file);
      expect(warn).toHaveBeenCalledWith(
        "Figure upload failed at the storage step (network error, or storage refused this origin: CORS):",
        expect.objectContaining({ step: "storage", status: 0 }),
      );
      env.NODE_ENV = "production";
      warn.mockClear();
      mockPost.mockResolvedValueOnce(reply(500, { code: "internal_error", message: "x" }));
      await uploadFigure("p1", "a.png", file);
      expect(warn).not.toHaveBeenCalled();
    } finally {
      env.NODE_ENV = before;
      warn.mockRestore();
    }
  });

  it("does not send the file once cancelled", async () => {
    const controller = new AbortController();
    mockPost.mockImplementationOnce(async () => {
      controller.abort();
      return reply(201, ticket);
    });
    await expect(uploadFigure("p1", "a.png", file, { signal: controller.signal })).resolves.toMatchObject({ code: "aborted" });
    expect(mockStorage).not.toHaveBeenCalled();
  });
});

describe("formatBytes", () => {
  it("writes sizes in MB as the server does, rounded down", () => {
    expect(formatBytes(0)).toBe("0 MB");
    expect(formatBytes(500 * 1024)).toBe("500 KB");
    expect(formatBytes(120 << 20)).toBe("120 MB");
    expect(formatBytes(12.5 * 1024 * 1024)).toBe("12.5 MB");
    expect(formatBytes((500 << 20) - 1)).toBe("499.9 MB");
  });
});

describe("fileErrorText", () => {
  it("keeps the server's words where they carry the limits", () => {
    expect(fileErrorText({ code: "file_too_large", message: "A figure may be at most 50 MB." })).toBe("A figure may be at most 50 MB.");
    expect(fileErrorText({ code: "storage_limit_reached", message: "This project uses 480 MB of 500 MB." })).toBe(
      "This project uses 480 MB of 500 MB.",
    );
    expect(fileErrorText({ code: "path_taken", message: "x" })).toContain("already there");
  });
});
