import { useCallback, useEffect, useRef, useState } from "react";

import type { RealtimeEvent } from "@/features/realtime/events";
import { useOnConnected, useRealtimeEvent } from "@/features/realtime/use-realtime";

import {
  type Attachment,
  type Conversation,
  type ConversationState,
  getConversation,
  listMessages,
  type Message,
  type MessagePage,
} from "./api";
import type { ThreadMessage } from "./format";

export type ThreadData = {
  conversation: Conversation;
  // messages are stored messages, oldest first.
  messages: ThreadMessage[];
  nextBefore: string | null;
  otherLastReadMessageId: string | null;
  loadingEarlier: boolean;
  earlierFailed: boolean;
};

export type ThreadState = { status: "loading" } | { status: "notFound" } | { status: "error" } | ({ status: "ok" } & ThreadData);

function shown(message: Message, key = message.id): ThreadMessage {
  return {
    key,
    id: message.id,
    // Only a group's system messages have no sender; the client shows no group until 14B.5b.
    senderId: message.senderId ?? "",
    body: message.body ?? "",
    createdAt: message.createdAt,
    editedAt: message.editedAt,
    deleted: message.deleted,
    attachments: message.attachments,
    delivery: "sent",
  };
}

// tombstone is a deleted message in its place: no text and no files (FR-16.5).
function tombstone(m: ThreadMessage): ThreadMessage {
  return { ...m, deleted: true, body: "", attachments: [] };
}

// withStatus sets one attachment's scan result wherever it is shown (FR-16.7).
function withStatus(messages: ThreadMessage[], attachmentId: string, status: Attachment["status"]): ThreadMessage[] {
  return messages.map((m) =>
    m.attachments.some((a) => a.id === attachmentId)
      ? { ...m, attachments: m.attachments.map((a) => (a.id === attachmentId ? { ...a, status } : a)) }
      : m,
  );
}

// conversationOf names the conversation an event is about, if it is about one the thread shows.
function conversationOf(event: RealtimeEvent): string | null {
  if (event.type === "message.created" || event.type === "message.updated") return event.data.message.conversationId;
  if (event.type === "message.deleted") return event.data.conversationId;
  // Before it is sent, a file's result is the composer's (useAttachments).
  if (event.type === "attachment.updated") return event.data.messageId ? event.data.conversationId : null;
  if (event.type === "conversation.read" || event.type === "conversation.updated") return event.data.conversationId;
  return null;
}

// byId orders stored messages oldest first: IDs are UUIDv7, so a later message has a greater ID.
function byId(a: ThreadMessage, b: ThreadMessage): number {
  return (a.id ?? "") < (b.id ?? "") ? -1 : 1;
}

// resynced folds the newest page into what is shown after a reconnect. A page that touches
// nothing shown leaves a gap, so it replaces the list and Load earlier fills in from there.
// Messages on both take the page's version, so an edit or deletion missed meanwhile shows.
function resynced(data: ThreadData, page: MessagePage): Partial<ThreadData> {
  const known = new Set(data.messages.map((m) => m.id));
  const fresh = page.items.map((m) => shown(m));
  const otherLastReadMessageId = page.otherLastReadMessageId;
  if (page.nextBefore !== null && !fresh.some((m) => known.has(m.id))) {
    return { messages: fresh.reverse(), nextBefore: page.nextBefore, otherLastReadMessageId };
  }
  const byFresh = new Map(fresh.map((m) => [m.id, m]));
  const kept = data.messages.map((m) => {
    const newer = byFresh.get(m.id);
    return newer ? { ...newer, key: m.key } : m;
  });
  return { messages: [...kept, ...fresh.filter((m) => !known.has(m.id))].sort(byId), otherLastReadMessageId };
}

export type Thread = {
  state: ThreadState;
  retry: () => void;
  loadEarlier: () => void;
  // setConversation stores a conversation an action returned; reload reads it again.
  setConversation: (conversation: Conversation) => void;
  reload: () => void;
  // addSent adds a message the server stored, under the key its row had while on its way.
  addSent: (message: Message, state: ConversationState, key: string) => void;
  // replace shows a message's new version in its place, as an edit returns it; markDeleted
  // turns one into a tombstone. A message that is not loaded is ignored.
  replace: (message: Message) => void;
  markDeleted: (messageId: string) => void;
};

// useThread loads a conversation and its newest messages, and earlier ones on request. After
// that the socket keeps it current, and after every reconnect it reads the newest page again.
export function useThread(conversationId: string): Thread {
  const [attempt, setAttempt] = useState(0);
  const [state, setState] = useState<ThreadState>({ status: "loading" });
  const stateRef = useRef(state);
  useEffect(() => {
    stateRef.current = state;
  });

  const update = useCallback((change: (data: ThreadData) => Partial<ThreadData>) => {
    setState((prev) => (prev.status === "ok" ? { ...prev, ...change(prev) } : prev));
  }, []);

  const replace = useCallback(
    (message: Message) => {
      update((data) => ({
        messages: data.messages.map((m) => (m.id === message.id ? { ...shown(message, m.key), delivery: m.delivery } : m)),
      }));
    },
    [update],
  );

  const markDeleted = useCallback(
    (messageId: string) => {
      update((data) => ({ messages: data.messages.map((m) => (m.id === messageId ? tombstone(m) : m)) }));
    },
    [update],
  );

  const reload = useCallback(() => {
    void getConversation(conversationId).then((res) => {
      if (res.ok) update(() => ({ conversation: res.data }));
    });
  }, [conversationId, update]);

  const apply = useCallback(
    (event: RealtimeEvent) => {
      if (event.type === "message.created") {
        const { message, conversationState, clientId } = event.data;
        update((data) => ({
          conversation: { ...data.conversation, state: conversationState },
          messages: data.messages.some((m) => m.id === message.id) ? data.messages : [...data.messages, shown(message, clientId)].sort(byId),
        }));
      } else if (event.type === "message.updated") {
        replace(event.data.message);
      } else if (event.type === "message.deleted") {
        markDeleted(event.data.messageId);
      } else if (event.type === "attachment.updated") {
        const { attachmentId, status } = event.data;
        update((data) => ({ messages: withStatus(data.messages, attachmentId, status) }));
      } else if (event.type === "conversation.read") {
        update((data) => {
          const marker = data.otherLastReadMessageId;
          return marker !== null && marker >= event.data.messageId ? {} : { otherLastReadMessageId: event.data.messageId };
        });
      } else if (event.type === "conversation.updated") {
        const { state: conversationState, canMessage } = event.data;
        update((data) => ({ conversation: { ...data.conversation, state: conversationState, canMessage } }));
        // The event never says why, so blockedByMe and available come from the conversation.
        reload();
      }
    },
    [reload, update, replace, markDeleted],
  );

  // Events for this conversation that arrive during the first load wait for it, as its answer
  // may or may not include them.
  const early = useRef<RealtimeEvent[]>([]);

  useRealtimeEvent((event) => {
    if (conversationOf(event) !== conversationId) return;
    if (stateRef.current.status === "ok") apply(event);
    else early.current.push(event);
  });

  useOnConnected(() => {
    if (stateRef.current.status !== "ok") return;
    void Promise.all([getConversation(conversationId), listMessages(conversationId)]).then(([conversation, page]) => {
      if (!conversation.ok || !page.ok) return;
      update((data) => ({ conversation: conversation.data, ...resynced(data, page.data) }));
    });
  });

  useEffect(() => {
    let current = true;
    void Promise.all([getConversation(conversationId), listMessages(conversationId)]).then(([conversation, page]) => {
      if (!current) return;
      if (!conversation.ok || !page.ok) {
        const failed = !conversation.ok ? conversation : page;
        setState({ status: !failed.ok && failed.code === "not_found" ? "notFound" : "error" });
        return;
      }
      setState({
        status: "ok",
        conversation: conversation.data,
        messages: page.data.items.map((m) => shown(m)).reverse(),
        nextBefore: page.data.nextBefore,
        otherLastReadMessageId: page.data.otherLastReadMessageId,
        loadingEarlier: false,
        earlierFailed: false,
      });
    });
    return () => {
      current = false;
      early.current = [];
      setState({ status: "loading" });
    };
  }, [conversationId, attempt]);

  useEffect(() => {
    if (state.status === "ok") early.current.splice(0).forEach(apply);
  }, [state.status, apply]);

  const loadEarlier = useCallback(() => {
    const now = stateRef.current;
    if (now.status !== "ok" || now.loadingEarlier || !now.nextBefore) return;
    stateRef.current = { ...now, loadingEarlier: true };
    update(() => ({ loadingEarlier: true, earlierFailed: false }));
    void listMessages(conversationId, now.nextBefore).then((res) => {
      if (!res.ok) {
        update(() => ({ loadingEarlier: false, earlierFailed: true }));
        return;
      }
      update((data) => {
        const seen = new Set(data.messages.map((m) => m.id));
        const earlier = res.data.items.filter((m) => !seen.has(m.id)).map((m) => shown(m)).reverse();
        return { messages: [...earlier, ...data.messages], nextBefore: res.data.nextBefore, loadingEarlier: false };
      });
    });
  }, [conversationId, update]);

  const setConversation = useCallback((conversation: Conversation) => update(() => ({ conversation })), [update]);

  const addSent = useCallback(
    (message: Message, conversationState: ConversationState, key: string) => {
      update((data) => ({
        conversation: { ...data.conversation, state: conversationState },
        messages: data.messages.some((m) => m.id === message.id) ? data.messages : [...data.messages, shown(message, key)],
      }));
    },
    [update],
  );

  return { state, retry: () => setAttempt((n) => n + 1), loadEarlier, setConversation, reload, addSent, replace, markDeleted };
}
