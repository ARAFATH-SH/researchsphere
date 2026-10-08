import { api } from "@/lib/api/client";

import { checkHealth } from "./api";

jest.mock("@/lib/api/client", () => ({ api: { GET: jest.fn() } }));
const mockGet = jest.mocked(api.GET);

type GetResult = Awaited<ReturnType<typeof api.GET>>;

function result(status: number, body: object): GetResult {
  // Only the parts checkHealth reads; jsdom has no Response class.
  const response = { status, ok: status < 400 };
  return (status === 200 ? { data: body, response } : { error: body, response }) as GetResult;
}

describe("checkHealth", () => {
  afterEach(() => jest.resetAllMocks());

  it.each([
    {
      name: "200 with status ok is up",
      reply: () => Promise.resolve(result(200, { status: "ok", database: "ok", redis: "ok" })),
      want: "up",
    },
    {
      name: "503 is down",
      reply: () => Promise.resolve(result(503, { code: "unavailable", message: "Redis does not respond." })),
      want: "down",
    },
    {
      name: "500 is down",
      reply: () => Promise.resolve(result(500, { code: "internal", message: "Unexpected error." })),
      want: "down",
    },
    { name: "a network error is down", reply: () => Promise.reject(new TypeError("Failed to fetch")), want: "down" },
  ])("$name", async ({ reply, want }) => {
    mockGet.mockImplementation(reply);
    await expect(checkHealth()).resolves.toBe(want);
  });
});
