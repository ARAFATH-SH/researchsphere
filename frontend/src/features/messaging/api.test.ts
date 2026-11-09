import { api } from "@/lib/api/client";

import { getConversation, listConversations, sendDirectMessage, sendMessage } from "./api";
import { conversation } from "./test-data";

jest.mock("@/lib/api/client", () => ({ api: { POST: jest.fn(), GET: jest.fn() } }));
const mockPost = jest.mocked(api.POST);
const mockGet = jest.mocked(api.GET);

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

// Groups (FR-16.6) have no screens until slice 14B.5: the client drops them where they arrive.
describe("groups", () => {
  const group = {
    ...conversation({ id: "g1" }),
    kind: "group",
    otherMember: null,
    group: { name: "Lab", memberCount: 3, avatars: [], myRole: "member", members: null },
  };
  type GetReply = Awaited<ReturnType<typeof api.GET>>;
  const ok = (body: object) => ({ data: body, response: { status: 200, ok: true, headers: new Map() } }) as unknown as GetReply;

  it("leaves groups out of a box", async () => {
    mockGet.mockResolvedValue(ok({ items: [group, conversation()], nextCursor: "next" }));
    const res = await listConversations("inbox");
    expect(res).toEqual({ ok: true, data: { items: [conversation()], nextCursor: "next" } });
  });

  it("answers a group as not found", async () => {
    mockGet.mockResolvedValue(ok(group));
    await expect(getConversation("g1")).resolves.toMatchObject({ ok: false, code: "not_found" });
    mockGet.mockResolvedValue(ok(conversation()));
    await expect(getConversation("c1")).resolves.toEqual({ ok: true, data: conversation() });
  });
});
