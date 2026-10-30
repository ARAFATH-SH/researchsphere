import { api } from "@/lib/api/client";

import { lookupDOI, removePublication } from "./publications-api";

jest.mock("@/lib/api/client", () => ({
  api: { GET: jest.fn(), DELETE: jest.fn() },
}));
const mockGet = jest.mocked(api.GET);
const mockDelete = jest.mocked(api.DELETE);

type Reply = Awaited<ReturnType<typeof api.GET>>;

// reply builds only the parts the helper reads; jsdom has no Response class.
function reply(status: number, body: object | undefined, headers: Record<string, string> = {}): Reply {
  const response = {
    status,
    ok: status < 400,
    headers: { get: (name: string) => headers[name] ?? null },
  };
  return (status < 400 ? { data: body, response } : { error: body, response }) as Reply;
}

afterEach(() => jest.resetAllMocks());

describe("publication requests", () => {
  it("passes on the error and the Retry-After seconds", async () => {
    mockGet.mockResolvedValue(
      reply(429, { code: "rate_limited", message: "Too many." }, { "Retry-After": "120" }),
    );
    await expect(lookupDOI("10.1/x")).resolves.toEqual({
      ok: false,
      status: 429,
      code: "rate_limited",
      message: "Too many.",
      retryAfter: 120,
    });
  });

  it("turns a reply outside the error shape into the unexpected error", async () => {
    mockGet.mockResolvedValue(reply(502, undefined));
    const res = await lookupDOI("10.1/x");
    expect(res).toMatchObject({ ok: false, status: 502, code: "internal_error" });
  });

  it("turns a network failure into network_error", async () => {
    mockGet.mockRejectedValue(new TypeError("Failed to fetch"));
    await expect(lookupDOI("10.1/x")).resolves.toMatchObject({ ok: false, status: 0, code: "network_error" });
  });

  it("treats 204 with no body as success for remove", async () => {
    mockDelete.mockResolvedValue(reply(204, undefined));
    await expect(removePublication("p1")).resolves.toEqual({ ok: true, data: null });
  });
});
