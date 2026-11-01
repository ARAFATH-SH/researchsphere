import { api } from "@/lib/api/client";

import { sendConnectionRequest } from "./api";

jest.mock("@/lib/api/client", () => ({ api: { POST: jest.fn() } }));
const mockPost = jest.mocked(api.POST);

type Reply = Awaited<ReturnType<typeof api.POST>>;

// reply builds only the parts the helper reads; jsdom has no Response class.
function reply(status: number, body: object | undefined): Reply {
  const response = { status, ok: status < 400 };
  return (status < 400 ? { data: body, response } : { error: body, response }) as Reply;
}

afterEach(() => jest.resetAllMocks());

describe("sendConnectionRequest", () => {
  it("keeps the other user's requestId from a 409 request_pending_from_them", async () => {
    const requestId = "7a1d2c3e-0000-4000-8000-0000000000bb";
    mockPost.mockResolvedValue(
      reply(409, { code: "request_pending_from_them", message: "They asked first.", requestId }),
    );
    await expect(sendConnectionRequest("bob")).resolves.toEqual({
      ok: false,
      code: "request_pending_from_them",
      message: "They asked first.",
      requestId,
    });
    expect(mockPost).toHaveBeenCalledWith("/connections/requests", { body: { userId: "bob" } });
  });

  it("passes on a 429 without a requestId", async () => {
    mockPost.mockResolvedValue(reply(429, { code: "rate_limited", message: "Too many." }));
    await expect(sendConnectionRequest("bob")).resolves.toEqual({
      ok: false,
      code: "rate_limited",
      message: "Too many.",
    });
  });

  it("turns a network failure into network_error", async () => {
    mockPost.mockRejectedValue(new TypeError("offline"));
    await expect(sendConnectionRequest("bob")).resolves.toMatchObject({ ok: false, code: "network_error" });
  });
});
