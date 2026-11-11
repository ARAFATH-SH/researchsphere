import { api } from "@/lib/api/client";

import {
  addGroupMembers,
  createGroup,
  deleteMessage,
  editMessage,
  ensureSession,
  getConversation,
  leaveGroup,
  listConversations,
  listMessages,
  removeGroupMember,
  renameGroup,
  searchMessages,
  sendDirectMessage,
  sendMessage,
} from "./api";
import { conversation } from "./test-data";

jest.mock("@/lib/api/client", () => ({ api: { POST: jest.fn(), GET: jest.fn(), PATCH: jest.fn(), DELETE: jest.fn() } }));
const mockPost = jest.mocked(api.POST);
const mockGet = jest.mocked(api.GET);
const mockPatch = jest.mocked(api.PATCH);
const mockDelete = jest.mocked(api.DELETE);

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

  it("sends files with their IDs, and leaves out empty text (FR-16.7)", async () => {
    mockPost.mockResolvedValue(reply(201, { message: {}, conversationState: "active" }));
    await sendMessage("c1", "", "client", ["f1", "f2"]);
    expect(mockPost).toHaveBeenCalledWith("/conversations/{conversationId}/messages", {
      params: { path: { conversationId: "c1" } },
      body: { clientId: "client", attachmentIds: ["f1", "f2"] },
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

// Groups (FR-16.6) arrive as they are, and their operations keep what a refusal says.
describe("groups", () => {
  const group = {
    ...conversation({ id: "g1" }),
    kind: "group",
    otherMember: null,
    group: { name: "Lab", memberCount: 3, avatars: [], myRole: "member", members: null },
  };
  type GetReply = Awaited<ReturnType<typeof api.GET>>;
  const ok = (body: object) => ({ data: body, response: { status: 200, ok: true, headers: new Map() } }) as unknown as GetReply;

  it("keeps groups in a box and reads one", async () => {
    mockGet.mockResolvedValue(ok({ items: [group, conversation()], nextCursor: "next" }));
    await expect(listConversations("inbox")).resolves.toEqual({ ok: true, data: { items: [group, conversation()], nextCursor: "next" } });
    mockGet.mockResolvedValue(ok(group));
    await expect(getConversation("g1")).resolves.toEqual({ ok: true, data: group });
  });

  it("creates a group with its client ID and keeps the refused user IDs", async () => {
    mockPost.mockResolvedValue(reply(400, { code: "ineligible_members", message: "No.", userIds: ["u2"] }) as never);
    await expect(createGroup("Lab", ["u1", "u2"], "client")).resolves.toMatchObject({ ok: false, code: "ineligible_members", userIds: ["u2"] });
    expect(mockPost).toHaveBeenCalledWith("/conversations/groups", { body: { name: "Lab", memberIds: ["u1", "u2"], clientId: "client" } });
    mockPost.mockResolvedValue(reply(429, { code: "rate_limited", message: "Slow down." }, { "Retry-After": "30" }) as never);
    await expect(createGroup("Lab", ["u1"], "client")).resolves.toMatchObject({ ok: false, code: "rate_limited", retryAfter: 30 });
  });

  it("renames, adds, removes and leaves", async () => {
    mockPatch.mockResolvedValue(reply(200, group) as never);
    await renameGroup("g1", "Lab 2");
    expect(mockPatch).toHaveBeenCalledWith("/conversations/{conversationId}", { params: { path: { conversationId: "g1" } }, body: { name: "Lab 2" } });
    mockPost.mockResolvedValue(reply(409, { code: "group_full", message: "Full." }) as never);
    await expect(addGroupMembers("g1", ["u3"])).resolves.toMatchObject({ ok: false, code: "group_full" });
    expect(mockPost).toHaveBeenCalledWith("/conversations/{conversationId}/members", { params: { path: { conversationId: "g1" } }, body: { userIds: ["u3"] } });
    mockDelete.mockResolvedValue(reply(204, {}) as never);
    await expect(removeGroupMember("g1", "u3")).resolves.toMatchObject({ ok: true });
    mockPost.mockResolvedValue(reply(204, {}) as never);
    await expect(leaveGroup("g1")).resolves.toMatchObject({ ok: true });
    expect(mockPost).toHaveBeenLastCalledWith("/conversations/{conversationId}/leave", { params: { path: { conversationId: "g1" } } });
  });
});

describe("message pages and search (FR-16.8)", () => {
  it("asks for a page around a message, or after one", async () => {
    mockGet.mockResolvedValue(reply(200, { items: [] }) as never);
    await listMessages("c1", { around: "m5" });
    expect(mockGet).toHaveBeenLastCalledWith("/conversations/{conversationId}/messages", { params: { path: { conversationId: "c1" }, query: { around: "m5" } } });
    await listMessages("c1", { after: "m9" }, 1);
    expect(mockGet).toHaveBeenLastCalledWith("/conversations/{conversationId}/messages", { params: { path: { conversationId: "c1" }, query: { after: "m9", limit: 1 } } });
  });

  it("searches with only the filters given, and keeps Retry-After", async () => {
    mockGet.mockResolvedValue(reply(200, { items: [], nextCursor: null, people: {} }) as never);
    await searchMessages({ q: "paper", conversationId: "c1", after: "2026-11-01T00:00:00.000Z" });
    expect(mockGet).toHaveBeenLastCalledWith("/messages/search", { params: { query: { q: "paper", conversationId: "c1", after: "2026-11-01T00:00:00.000Z" } } });
    mockGet.mockResolvedValue(reply(429, { code: "rate_limited", message: "Slow down." }, { "Retry-After": "12" }) as never);
    await expect(searchMessages({ q: "paper" })).resolves.toMatchObject({ ok: false, code: "rate_limited", retryAfter: 12 });
  });
});

describe("editing and deleting (FR-16.5)", () => {
  it("edits with PATCH and answers the message", async () => {
    mockPatch.mockResolvedValue(reply(200, { id: "m1", body: "new" }) as never);
    await expect(editMessage("c1", "m1", "new")).resolves.toEqual({ ok: true, data: { id: "m1", body: "new" } });
    expect(mockPatch).toHaveBeenCalledWith("/conversations/{conversationId}/messages/{messageId}", {
      params: { path: { conversationId: "c1", messageId: "m1" } },
      body: { body: "new" },
    });
  });

  it("keeps the edit window refusal's code", async () => {
    mockPatch.mockResolvedValue(reply(409, { code: "edit_window_closed", message: "Too late." }) as never);
    await expect(editMessage("c1", "m1", "new")).resolves.toMatchObject({ ok: false, code: "edit_window_closed" });
  });

  it("deletes with DELETE", async () => {
    mockDelete.mockResolvedValue(reply(204, {}) as never);
    await expect(deleteMessage("c1", "m1")).resolves.toMatchObject({ ok: true });
    expect(mockDelete).toHaveBeenCalledWith("/conversations/{conversationId}/messages/{messageId}", {
      params: { path: { conversationId: "c1", messageId: "m1" } },
    });
  });
});

describe("ensureSession", () => {
  it("reads /auth/me and is false only when the session is over", async () => {
    mockGet.mockResolvedValue(reply(200, { id: "u" }) as never);
    await expect(ensureSession()).resolves.toBe(true);
    expect(mockGet).toHaveBeenCalledWith("/auth/me");
    mockGet.mockResolvedValue(reply(401, { code: "unauthenticated", message: "No." }) as never);
    await expect(ensureSession()).resolves.toBe(false);
    mockGet.mockRejectedValue(new Error("offline"));
    await expect(ensureSession()).resolves.toBe(true);
  });
});
