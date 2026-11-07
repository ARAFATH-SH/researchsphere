import { call, type Result, unexpectedError } from "@/lib/api/call";
import { api, type components } from "@/lib/api/client";

export type Conversation = components["schemas"]["Conversation"];
export type ConversationMember = components["schemas"]["ConversationMember"];
export type ConversationPage = components["schemas"]["ConversationPage"];
export type ConversationState = components["schemas"]["ConversationState"];
export type Message = components["schemas"]["Message"];
export type MessagePage = components["schemas"]["MessagePage"];
export type MessageUnreadCount = components["schemas"]["MessageUnreadCount"];
export type SentMessage = components["schemas"]["SentMessage"];
export type ReportReason = components["schemas"]["ReportReason"];
export type ReportConversationRequest = components["schemas"]["ReportConversationRequest"];
export type ConversationReportReceipt = components["schemas"]["ConversationReportReceipt"];

// Box is one of the two lists of conversations (FR-16.2).
export type Box = "inbox" | "requests";

// listConversations returns one page of a box, newest activity first. Leave cursor out for
// the first page.
export async function listConversations(box: Box, cursor?: string): Promise<Result<ConversationPage>> {
  const res = await call(() => api.GET("/conversations", { params: { query: cursor ? { box, cursor } : { box } } }));
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

// getConversation returns one of the caller's conversations; any other ID is not_found.
export async function getConversation(conversationId: string): Promise<Result<Conversation>> {
  const res = await call(() => api.GET("/conversations/{conversationId}", { params: { path: { conversationId } } }));
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

// listMessages returns one page of a conversation, newest first. Leave before out for the
// newest messages; pass the previous page's nextBefore for earlier ones.
export async function listMessages(conversationId: string, before?: string): Promise<Result<MessagePage>> {
  const res = await call(() =>
    api.GET("/conversations/{conversationId}/messages", {
      params: { path: { conversationId }, query: before ? { before } : {} },
    }),
  );
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

// LimitedResult adds, to a refused request, how many seconds the rate limit lasts.
export type LimitedResult<T> = Result<T> & { retryAfter?: number };

export type SendResult = LimitedResult<SentMessage>;

// deliver reads Retry-After itself, since call() keeps only code, message, reason and until.
async function deliver<T>(request: Parameters<typeof call<T>>[0]): Promise<LimitedResult<T>> {
  let retryAfter: number | undefined;
  const res = await call(async () => {
    const raw = await request();
    const header = Number(raw.response.headers?.get("Retry-After"));
    if (header > 0) retryAfter = header;
    return raw;
  });
  if (!res.ok) return { ...res, retryAfter };
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

// sendMessage sends in a conversation. A retry sends the same clientId, which stores nothing
// new (FR-16.10 limits count only stored messages).
export function sendMessage(conversationId: string, body: string, clientId: string): Promise<SendResult> {
  return deliver(() =>
    api.POST("/conversations/{conversationId}/messages", {
      params: { path: { conversationId } },
      body: { body, clientId },
    }),
  );
}

// sendDirectMessage sends a first message to a user, finding or creating their conversation.
export function sendDirectMessage(userId: string, body: string, clientId: string): Promise<SendResult> {
  return deliver(() => api.POST("/messages/direct", { body: { userId, body, clientId } }));
}

// reportConversation reports a conversation, or one message of it, to the administrators
// (FR-16.9). Reporting blocks nobody; the receipt says whether the caller has blocked them.
export function reportConversation(conversationId: string, body: ReportConversationRequest): Promise<LimitedResult<ConversationReportReceipt>> {
  return deliver(() =>
    api.POST("/conversations/{conversationId}/reports", { params: { path: { conversationId } }, body }),
  );
}

// markConversationRead moves the caller's read marker forward (FR-16.3).
export function markConversationRead(conversationId: string, messageId: string): Promise<Result<undefined>> {
  return call(() =>
    api.POST("/conversations/{conversationId}/read", {
      params: { path: { conversationId } },
      body: { messageId },
    }),
  );
}

// acceptMessageRequest makes a request active and returns the conversation (FR-16.2).
export async function acceptMessageRequest(conversationId: string): Promise<Result<Conversation>> {
  const res = await call(() =>
    api.POST("/conversations/{conversationId}/accept", { params: { path: { conversationId } } }),
  );
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

// declineMessageRequest closes a request for good and returns the conversation (FR-16.2).
export async function declineMessageRequest(conversationId: string): Promise<Result<Conversation>> {
  const res = await call(() =>
    api.POST("/conversations/{conversationId}/decline", { params: { path: { conversationId } } }),
  );
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

// getMessageUnreadCount counts unread messages and pending requests (FR-16.3).
export async function getMessageUnreadCount(): Promise<Result<MessageUnreadCount>> {
  const res = await call(() => api.GET("/messages/unread-count"));
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}
