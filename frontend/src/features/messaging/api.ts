import { call, type Result, unexpectedError } from "@/lib/api/call";
import { api, type components } from "@/lib/api/client";
import { uploadFile, type UploadOptions, type UploadResult } from "@/lib/api/upload";

export type ConversationMember = components["schemas"]["ConversationMember"];
type AnyConversation = components["schemas"]["Conversation"];

// Conversation is a direct conversation. Groups (FR-16.6) have no screens until slice 14B.5b,
// so the client drops them where conversations arrive: from the lists and from
// getConversation, which answers a group as not found. A message event for a group then finds
// no conversation and is ignored.
export type Conversation = AnyConversation & { kind: "direct"; otherMember: ConversationMember; group: null };
export type ConversationPage = Omit<components["schemas"]["ConversationPage"], "items"> & { items: Conversation[] };

export function isDirect(c: AnyConversation): c is Conversation {
  return c.kind === "direct" && c.otherMember !== null;
}

const groupNotShown = { code: "not_found", message: "This conversation does not exist." };
export type ConversationState = components["schemas"]["ConversationState"];
export type Message = components["schemas"]["Message"];
export type MessagePage = components["schemas"]["MessagePage"];
export type MessageUnreadCount = components["schemas"]["MessageUnreadCount"];
export type SentMessage = components["schemas"]["SentMessage"];
export type Attachment = components["schemas"]["Attachment"];
export type AttachmentStatus = components["schemas"]["AttachmentStatus"];
export type AttachmentUpload = components["schemas"]["AttachmentUpload"];
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
  return res.data ? { ok: true, data: { ...res.data, items: res.data.items.filter(isDirect) } } : { ok: false, ...unexpectedError };
}

// getConversation returns one of the caller's conversations; any other ID is not_found.
export async function getConversation(conversationId: string): Promise<Result<Conversation>> {
  const res = await call(() => api.GET("/conversations/{conversationId}", { params: { path: { conversationId } } }));
  if (!res.ok) return res;
  if (!res.data) return { ok: false, ...unexpectedError };
  return isDirect(res.data) ? { ok: true, data: res.data } : { ok: false, ...groupNotShown };
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
// new (FR-16.10 limits count only stored messages). With files (FR-16.7) the text may be empty,
// and is then left out.
export function sendMessage(conversationId: string, body: string, clientId: string, attachmentIds: string[] = []): Promise<SendResult> {
  return deliver(() =>
    api.POST("/conversations/{conversationId}/messages", {
      params: { path: { conversationId } },
      body: {
        clientId,
        ...(body ? { body } : {}),
        ...(attachmentIds.length > 0 ? { attachmentIds } : {}),
      },
    }),
  );
}

// editMessage changes the text of one of the caller's messages within the edit window
// (FR-16.5); the answer is the message with its new text and editedAt.
export function editMessage(conversationId: string, messageId: string, body: string): Promise<LimitedResult<Message>> {
  return deliver(() =>
    api.PATCH("/conversations/{conversationId}/messages/{messageId}", {
      params: { path: { conversationId, messageId } },
      body: { body },
    }),
  );
}

// deleteMessage deletes one of the caller's messages for everyone (FR-16.5).
export function deleteMessage(conversationId: string, messageId: string): Promise<Result<undefined>> {
  return call(() =>
    api.DELETE("/conversations/{conversationId}/messages/{messageId}", { params: { path: { conversationId, messageId } } }),
  );
}

// uploadAttachment uploads a file to send with a message later (FR-16.7), reporting progress.
export function uploadAttachment(conversationId: string, file: File, options: UploadOptions = {}): Promise<UploadResult<AttachmentUpload>> {
  return uploadFile(`/conversations/${encodeURIComponent(conversationId)}/attachments`, file, options);
}

// ensureSession reads /auth/me through the client, which renews an expired access token, so
// a plain link to the API that follows carries a valid one. It is false only when the session
// is over, which the client has already reported.
export async function ensureSession(): Promise<boolean> {
  const res = await call(() => api.GET("/auth/me"));
  return res.ok || res.code !== "unauthenticated";
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
  // Only a direct conversation can be a request.
  return res.data && isDirect(res.data) ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

// declineMessageRequest closes a request for good and returns the conversation (FR-16.2).
export async function declineMessageRequest(conversationId: string): Promise<Result<Conversation>> {
  const res = await call(() =>
    api.POST("/conversations/{conversationId}/decline", { params: { path: { conversationId } } }),
  );
  if (!res.ok) return res;
  // Only a direct conversation can be a request.
  return res.data && isDirect(res.data) ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

// getMessageUnreadCount counts unread messages and pending requests (FR-16.3).
export async function getMessageUnreadCount(): Promise<Result<MessageUnreadCount>> {
  const res = await call(() => api.GET("/messages/unread-count"));
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}
