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
  type MessagePeople,
  type MessageReader,
} from "./api";
import type { ThreadMessage } from "./format";

export type ThreadData = {
  conversation: Conversation;
  // messages are stored messages, oldest first.
  messages: ThreadMessage[];
  // nextBefore and nextAfter page to older and newer messages; nextAfter is set only while the
  // thread was opened at a search result and has not reached the newest message (FR-16.8).
  nextBefore: string | null;
  nextAfter: string | null;
  otherLastReadMessageId: string | null;
  // readers are a group's other members' read markers, for "Seen by N" (FR-16.3).
  readers: MessageReader[] | null;
  // people names every sender and everyone a system message names, from every page loaded.
  people: MessagePeople;
  // anchorId is the message the thread was opened at, shown highlighted for a moment.
  anchorId: string | null;
  // anchorMissing says the message asked for is not there any more (deleted with its sender,
  // or the caller cannot see it); the thread opened at the newest messages instead.
  anchorMissing: boolean;
  loadingEarlier: boolean;
  earlierFailed: boolean;
  loadingNewer: boolean;
  newerFailed: boolean;
};

// lost: the caller left the group or was removed from it (FR-16.6), so every read of it now
// answers 404.
export type ThreadState =
  | { status: "loading" }
  | { status: "notFound" }
  | { status: "error" }
  | { status: "lost" }
  | ({ status: "ok" } & ThreadData);

export function shown(message: Message, key = message.id): ThreadMessage {
  return {
    key,
    id: message.id,
    // A system message with no actor (an ownership hand-over) has no sender.
    senderId: message.senderId ?? "",
    event: message.kind === "system" ? message.event : null,
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
  if (event.type === "conversation.read" || event.type === "conversation.updated" || event.type === "conversation.members") {
    return event.data.conversationId;
  }
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
  const marks = { otherLastReadMessageId: page.otherLastReadMessageId, readers: page.readers, people: { ...data.people, ...page.people } };
  if (page.nextBefore !== null && !fresh.some((m) => known.has(m.id))) {
    return { messages: fresh.reverse(), nextBefore: page.nextBefore, ...marks };
  }
  const byFresh = new Map(fresh.map((m) => [m.id, m]));
  const kept = data.messages.map((m) => {
    const newer = byFresh.get(m.id);
    return newer ? { ...newer, key: m.key } : m;
  });
  return { messages: [...kept, ...fresh.filter((m) => !known.has(m.id))].sort(byId), ...marks };
}

// fromPage is the thread's first state from a conversation and a page of its messages.
function fromPage(conversation: Conversation, page: MessagePage, anchorId: string | null, anchorMissing: boolean): ThreadData {
  return {
    conversation,
    messages: page.items.map((m) => shown(m)).reverse(),
    nextBefore: page.nextBefore,
    nextAfter: page.nextAfter,
    otherLastReadMessageId: page.otherLastReadMessageId,
    readers: page.readers,
    people: page.people,
    anchorId,
    anchorMissing,
    loadingEarlier: false,
    earlierFailed: false,
    loadingNewer: false,
    newerFailed: false,
  };
}

export type Thread = {
  state: ThreadState;
  retry: () => void;
  loadEarlier: () => void;
  // loadNewer reads the messages after the newest one shown, while the thread was opened at a
  // search result and has not reached the end.
  loadNewer: () => void;
  // setConversation stores a conversation an action returned; reload reads it again.
  setConversation: (conversation: Conversation) => void;
  reload: () => void;
  // addSent adds a message the server stored, under the key its row had while on its way.
  addSent: (message: Message, state: ConversationState, key: string) => void;
  // replace shows a message's new version in its place, as an edit returns it; markDeleted
  // turns one into a tombstone. A message that is not loaded is ignored.
  replace: (message: Message) => void;
  markDeleted: (messageId: string) => void;
  // leave shows the thread as one the caller is no longer in.
  leave: () => void;
  // jumpToLatest leaves a search result's place for the newest messages, in place.
  jumpToLatest: () => void;
};

// useThread loads a conversation and its newest messages, or those around one message (a search
// result), and more on request. After that the socket keeps it current, and after every
// reconnect it reads the newest page again.
export function useThread(conversationId: string, around: string | null = null): Thread {
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

  const leave = useCallback(() => setState({ status: "lost" }), []);

  const reload = useCallback(() => {
    void getConversation(conversationId).then((res) => {
      if (res.ok) update(() => ({ conversation: res.data }));
      else if (res.code === "not_found" && stateRef.current.status === "ok" && stateRef.current.conversation.kind === "group") leave();
    });
  }, [conversationId, update, leave]);

  // A group's markers have no event of their own: conversation.read says one moved, and the
  // newest page carries them all (docs/realtime.md).
  const reloadReaders = useCallback(() => {
    void listMessages(conversationId, {}, 1).then((res) => {
      if (res.ok) update(() => ({ readers: res.data.readers }));
    });
  }, [conversationId, update]);

  const apply = useCallback(
    (event: RealtimeEvent) => {
      if (event.type === "message.created") {
        const { message, conversationState, clientId } = event.data;
        update((data) => ({
          conversation: { ...data.conversation, state: conversationState },
          // Opened at a search result, the thread does not show the newest messages yet: a new one
          // waits until the reader reaches the end.
          messages:
            data.nextAfter !== null || data.messages.some((m) => m.id === message.id)
              ? data.messages
              : [...data.messages, shown(message, clientId)].sort(byId),
        }));
      } else if (event.type === "message.updated") {
        replace(event.data.message);
      } else if (event.type === "message.deleted") {
        markDeleted(event.data.messageId);
      } else if (event.type === "attachment.updated") {
        const { attachmentId, status } = event.data;
        update((data) => ({ messages: withStatus(data.messages, attachmentId, status) }));
      } else if (event.type === "conversation.read") {
        const current = stateRef.current;
        if (current.status === "ok" && current.conversation.kind === "group") {
          reloadReaders();
          return;
        }
        update((data) => {
          const marker = data.otherLastReadMessageId;
          return marker !== null && marker >= event.data.messageId ? {} : { otherLastReadMessageId: event.data.messageId };
        });
      } else if (event.type === "conversation.updated") {
        if (event.data.accessLost) {
          leave();
          return;
        }
        const { state: conversationState, canMessage } = event.data;
        update((data) => ({ conversation: { ...data.conversation, state: conversationState, canMessage } }));
        // The event never says why, so blockedByMe and available come from the conversation.
        reload();
      } else if (event.type === "conversation.members") {
        // The name and the members' roles come with the conversation (docs/realtime.md).
        reload();
      }
    },
    [reload, reloadReaders, update, replace, markDeleted, leave],
  );

  // Events for this conversation that arrive during the first load wait for it, as its answer
  // may or may not include them.
  const early = useRef<RealtimeEvent[]>([]);

  useRealtimeEvent((event) => {
    if (conversationOf(event) !== conversationId) return;
    if (stateRef.current.status === "ok") apply(event);
    else if (stateRef.current.status === "loading") early.current.push(event);
  });

  useOnConnected(() => {
    const now = stateRef.current;
    if (now.status !== "ok") return;
    // Away from the newest messages, only the conversation is read again; Load newer catches up.
    const detached = now.nextAfter !== null;
    void Promise.all([getConversation(conversationId), detached ? null : listMessages(conversationId)]).then(([conversation, page]) => {
      if (!conversation.ok) {
        if (conversation.code === "not_found" && now.conversation.kind === "group") leave();
        return;
      }
      if (page && !page.ok) return;
      update((data) => ({ conversation: conversation.data, ...(page ? resynced(data, page.data) : {}) }));
    });
  });

  useEffect(() => {
    let current = true;
    void (async () => {
      const [conversation, first] = await Promise.all([
        getConversation(conversationId),
        listMessages(conversationId, around ? { around } : {}),
      ]);
      let page = first;
      // The message asked for is gone or hidden: open at the newest messages and say so.
      const missing = !!around && !page.ok && page.code === "not_found" && conversation.ok;
      if (missing) page = await listMessages(conversationId);
      if (!current) return;
      if (!conversation.ok || !page.ok) {
        const failed = !conversation.ok ? conversation : page;
        setState({ status: !failed.ok && failed.code === "not_found" ? "notFound" : "error" });
        return;
      }
      setState({ status: "ok", ...fromPage(conversation.data, page.data, missing ? null : around, missing) });
    })();
    return () => {
      current = false;
      early.current = [];
      setState({ status: "loading" });
    };
  }, [conversationId, around, attempt]);

  useEffect(() => {
    if (state.status === "ok") early.current.splice(0).forEach(apply);
  }, [state.status, apply]);

  const loadEarlier = useCallback(() => {
    const now = stateRef.current;
    if (now.status !== "ok" || now.loadingEarlier || !now.nextBefore) return;
    stateRef.current = { ...now, loadingEarlier: true };
    update(() => ({ loadingEarlier: true, earlierFailed: false }));
    void listMessages(conversationId, { before: now.nextBefore }).then((res) => {
      if (!res.ok) {
        update(() => ({ loadingEarlier: false, earlierFailed: true }));
        return;
      }
      update((data) => {
        const seen = new Set(data.messages.map((m) => m.id));
        const earlier = res.data.items.filter((m) => !seen.has(m.id)).map((m) => shown(m)).reverse();
        return {
          messages: [...earlier, ...data.messages],
          nextBefore: res.data.nextBefore,
          people: { ...data.people, ...res.data.people },
          loadingEarlier: false,
        };
      });
    });
  }, [conversationId, update]);

  const loadNewer = useCallback(() => {
    const now = stateRef.current;
    if (now.status !== "ok" || now.loadingNewer || !now.nextAfter) return;
    stateRef.current = { ...now, loadingNewer: true };
    update(() => ({ loadingNewer: true, newerFailed: false }));
    void listMessages(conversationId, { after: now.nextAfter }).then((res) => {
      if (!res.ok) {
        update(() => ({ loadingNewer: false, newerFailed: true }));
        return;
      }
      update((data) => {
        const seen = new Set(data.messages.map((m) => m.id));
        const newer = res.data.items.filter((m) => !seen.has(m.id)).map((m) => shown(m)).reverse();
        return {
          messages: [...data.messages, ...newer],
          nextAfter: res.data.nextAfter,
          people: { ...data.people, ...res.data.people },
          // The newest page has the current markers.
          ...(res.data.nextAfter === null ? { readers: res.data.readers, otherLastReadMessageId: res.data.otherLastReadMessageId } : {}),
          loadingNewer: false,
        };
      });
    });
  }, [conversationId, update]);

  const jumpToLatest = useCallback(() => {
    void listMessages(conversationId).then((res) => {
      if (!res.ok) {
        update(() => ({ newerFailed: true }));
        return;
      }
      update((data) => {
        const page = res.data.items.map((m) => shown(m)).reverse();
        const newest = page.at(-1)?.id ?? "";
        // A message sent meanwhile may be newer than the page.
        const later = data.messages.filter((m) => (m.id ?? "") > newest);
        return {
          messages: [...page, ...later],
          nextBefore: res.data.nextBefore,
          nextAfter: null,
          otherLastReadMessageId: res.data.otherLastReadMessageId,
          readers: res.data.readers,
          people: { ...data.people, ...res.data.people },
          anchorId: null,
          loadingNewer: false,
          newerFailed: false,
        };
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

  return {
    state,
    retry: () => setAttempt((n) => n + 1),
    loadEarlier,
    loadNewer,
    setConversation,
    reload,
    addSent,
    replace,
    markDeleted,
    leave,
    jumpToLatest,
  };
}
