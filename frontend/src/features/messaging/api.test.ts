import { api } from "@/lib/api/client";

import { sendDirectMessage, sendMessage } from "./api";

jest.mock("@/lib/api/client", () => ({ api: { POST: jest.fn() } }));
const mockPost = jest.mocked(api.POST);

type Reply = Awaited<ReturnType<typeof api.POST>>;

function reply(status: number, body: object, headers: Record<string, string> = {}): Reply {
  const response = { status, ok: status < 400, headers: new Map(Object.entries(headers)) };
  return (status < 400 ? { data: body, response } : { error: body, response }) as unknown as Reply;
}

afterEach(() => jest.resetAllMocks());

describe("sending", () => {
  it("keeps Retry-After from a 429", async () => {
    mockPost.mockResolvedValue(reply(429, { code: "rate_limited", message: "Too many." }, { "Retry-After": "42" }));
    await expect(sendMessage("c1", "hi", "client")).resolves.toEqual({
      ok: false,
      code: "rate_limited",
      message: "Too many.",
      retryAfter: 42,
    });
  });

  it("has no retryAfter on other refusals", async () => {
    mockPost.mockResolvedValue(reply(409, { code: "request_pending", message: "Wait." }));
    const res = await sendMessage("c1", "hi", "client");
    expect(res).toMatchObject({ ok: false, code: "request_pending" });
    expect(res).toHaveProperty("retryAfter", undefined);
  });

  it("sends the conversation, body and clientId", async () => {
    const sent = { message: {}, conversationState: "active" };
    mockPost.mockResolvedValue(reply(201, sent));
    await expect(sendMessage("c1", "hi", "client")).resolves.toEqual({ ok: true, data: sent });
    expect(mockPost).toHaveBeenCalledWith("/conversations/{conversationId}/messages", {
      params: { path: { conversationId: "c1" } },
      body: { body: "hi", clientId: "client" },
    });
  });

  it("sends a first message to the user", async () => {
    mockPost.mockResolvedValue(reply(201, { message: {}, conversationState: "request" }));
    await sendDirectMessage("bob", "hi", "client");
    expect(mockPost).toHaveBeenCalledWith("/messages/direct", { body: { userId: "bob", body: "hi", clientId: "client" } });
  });

  it("turns a lost connection into a failure", async () => {
    mockPost.mockRejectedValue(new Error("offline"));
    await expect(sendMessage("c1", "hi", "client")).resolves.toMatchObject({ ok: false, code: "network_error" });
  });
});
