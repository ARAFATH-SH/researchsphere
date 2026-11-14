import { call, type Result, unexpectedError } from "@/lib/api/call";
import { api, type components } from "@/lib/api/client";
import { uploadFile, type UploadOptions, type UploadResult } from "@/lib/api/upload";

export type ConversationMember = components["schemas"]["ConversationMember"];
// Conversation is a direct conversation (otherMember set) or a group (group set; FR-16.6).
export type Conversation = components["schemas"]["Conversation"];
export type ConversationPage = components["schemas"]["ConversationPage"];
export type ConversationGroup = components["schemas"]["ConversationGroup"];
export type GroupMember = components["schemas"]["GroupMember"];
export type GroupAvatar = components["schemas"]["GroupAvatar"];
export type SystemEvent = components["schemas"]["SystemEvent"];
export type SystemEventType = components["schemas"]["SystemEventType"];
export type MessagePeople = components["schemas"]["MessagePeople"];
export type MessagePerson = components["schemas"]["MessagePerson"];
export type MessageReader = components["schemas"]["MessageReader"];
export type MessageSearchPage = components["schemas"]["MessageSearchPage"];
export type MessageSearchResult = components["schemas"]["MessageSearchResult"];
export type TextRange = components["schemas"]["TextRange"];

// DirectConversation is a conversation with one other person.
export type DirectConversation = Conversation & { kind: "direct"; otherMember: ConversationMember };
// GroupConversation is a group (FR-16.6).
export type GroupConversation = Conversation & { kind: "group"; group: ConversationGroup; otherMember: null };


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
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

// getConversation returns one of the caller's conversations, a group with its members; any
// other ID is not_found.
export async function getConversation(conversationId: string): Promise<Result<Conversation>> {
  const res = await call(() => api.GET("/conversations/{conversationId}", { params: { path: { conversationId } } }));
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

// PageCursor picks a page of messages: older than before, newer than after, or centred on
// around (a search result, FR-16.8). None means the newest.
export type PageCursor = { before: string } | { after: string } | { around: string } | Record<string, never>;

// listMessages returns one page of a conversation, newest first. Leave the cursor out for the
// newest messages; pass the previous page's nextBefore as before for earlier ones, its
// nextAfter as after for newer ones, or a message ID as around to open the thread there.
export async function listMessages(conversationId: string, cursor: PageCursor | string = {}, limit?: number): Promise<Result<MessagePage>> {
  const query = { ...(typeof cursor === "string" ? { before: cursor } : cursor), ...(limit ? { limit } : {}) };
  const res = await call(() =>
    api.GET("/conversations/{conversationId}/messages", {
      params: { path: { conversationId }, query },
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

// createGroup creates a group of the caller's connections (FR-16.6). A retry sends the same
// clientId and gets the first group back. A refusal keeps userIds (ineligible_members) and
// field; a 429 its Retry-After.
export function createGroup(name: string, memberIds: string[], clientId: string): Promise<LimitedResult<Conversation>> {
  return deliver(() => api.POST("/conversations/groups", { body: { name, memberIds, clientId } }));
}

// renameGroup renames a group; only its owner may.
export function renameGroup(conversationId: string, name: string): Promise<Result<Conversation>> {
  return deliver(() => api.PATCH("/conversations/{conversationId}", { params: { path: { conversationId } }, body: { name } }));
}

// addGroupMembers adds the owner's connections to a group; current members are skipped.
export function addGroupMembers(conversationId: string, userIds: string[]): Promise<LimitedResult<Conversation>> {
  return deliver(() =>
    api.POST("/conversations/{conversationId}/members", { params: { path: { conversationId } }, body: { userIds } }),
  );
}

// removeGroupMember removes a member; only the owner may, and not themselves.
export function removeGroupMember(conversationId: string, userId: string): Promise<Result<undefined>> {
  return call(() =>
    api.DELETE("/conversations/{conversationId}/members/{userId}", { params: { path: { conversationId, userId } } }),
  );
}

// leaveGroup takes the caller out of a group; when they own it, the member who joined
// earliest becomes the owner.
export function leaveGroup(conversationId: string): Promise<Result<undefined>> {
  return call(() => api.POST("/conversations/{conversationId}/leave", { params: { path: { conversationId } } }));
}

// MessageSearch is what the search box asks (FR-16.8): q of 2 to 100 characters, optionally one
// conversation, one sender and a start time (RFC 3339), and the next page's cursor.
export type MessageSearch = { q: string; conversationId?: string; from?: string; after?: string; cursor?: string };

// searchMessages searches the caller's own conversations; a 429 keeps its Retry-After.
export function searchMessages(search: MessageSearch): Promise<LimitedResult<MessageSearchPage>> {
  const query: MessageSearch = { q: search.q };
  if (search.conversationId) query.conversationId = search.conversationId;
  if (search.from) query.from = search.from;
  if (search.after) query.after = search.after;
  if (search.cursor) query.cursor = search.cursor;
  return deliver(() => api.GET("/messages/search", { params: { query } }));
}

// getMessageUnreadCount counts unread messages and pending requests (FR-16.3).
export async function getMessageUnreadCount(): Promise<Result<MessageUnreadCount>> {
  const res = await call(() => api.GET("/messages/unread-count"));
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

export type UserSettings = components["schemas"]["UserSettings"];

// getMySettings returns the caller's settings (FR-16.11); a user who never changed one gets the
// defaults.
export async function getMySettings(): Promise<Result<UserSettings>> {
  const res = await call(() => api.GET("/me/settings"));
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

// setUnreadMessageDigest turns the unread message digest on or off (FR-16.11).
export async function setUnreadMessageDigest(on: boolean): Promise<Result<UserSettings>> {
  const res = await call(() => api.PATCH("/me/settings", { body: { unreadMessageDigest: on } }));
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

// unsubscribeDigest turns the digest off with the token from its email; it needs no session and
// never turns the digest on (FR-16.11).
export async function unsubscribeDigest(token: string): Promise<Result<undefined>> {
  const res = await call(() => api.POST("/unsubscribe", { params: { query: { token } } }));
  return res.ok ? { ok: true, data: undefined } : res;
}
