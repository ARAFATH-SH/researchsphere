import { useCallback, useEffect, useRef, useState } from "react";

import { type Conversation, type ConversationState, getConversation, listMessages, type Message } from "./api";
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
    senderId: message.senderId,
    body: message.body,
    createdAt: message.createdAt,
    delivery: "sent",
  };
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
};

// useThread loads a conversation and its newest messages, and earlier ones on request. It
// reads each once: 14A.5 adds what arrives afterwards.
export function useThread(conversationId: string): Thread {
  const [attempt, setAttempt] = useState(0);
  const [state, setState] = useState<ThreadState>({ status: "loading" });
  const stateRef = useRef(state);
  useEffect(() => {
    stateRef.current = state;
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
      setState({ status: "loading" });
    };
  }, [conversationId, attempt]);

  const update = useCallback((change: (data: ThreadData) => Partial<ThreadData>) => {
    setState((prev) => (prev.status === "ok" ? { ...prev, ...change(prev) } : prev));
  }, []);

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

  const reload = useCallback(() => {
    void getConversation(conversationId).then((res) => {
      if (res.ok) setConversation(res.data);
    });
  }, [conversationId, setConversation]);

  const addSent = useCallback(
    (message: Message, conversationState: ConversationState, key: string) => {
      update((data) => ({
        conversation: { ...data.conversation, state: conversationState },
        messages: data.messages.some((m) => m.id === message.id) ? data.messages : [...data.messages, shown(message, key)],
      }));
    },
    [update],
  );

  return { state, retry: () => setAttempt((n) => n + 1), loadEarlier, setConversation, reload, addSent };
}
