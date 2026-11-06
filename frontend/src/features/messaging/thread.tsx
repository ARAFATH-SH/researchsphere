"use client";

import Link from "next/link";
import { useState } from "react";

import { useSession } from "@/features/auth/session";

import { acceptMessageRequest, type Conversation, declineMessageRequest, sendMessage, type SentMessage } from "./api";
import { Composer } from "./composer";
import { seenMessageId, threadStatus } from "./format";
import { MessageList } from "./message-list";
import { isStale, sendNotice, type SendNotice } from "./send-errors";
import { ThreadBanner } from "./thread-banner";
import { ThreadHeader } from "./thread-header";
import { refreshUnreadMessages } from "./unread-count";
import { useConversations } from "./use-conversations";
import { useMarkRead } from "./use-mark-read";
import { outgoingMessages, useOutbox } from "./use-outbox";
import { type Thread as ThreadHandle, useThread } from "./use-thread";

// Thread is the right column: one conversation (FR-16.1 to FR-16.3). Give it a key per
// conversation, so nothing carries over from the last one.
export function Thread({ conversationId }: { conversationId: string }) {
  const session = useSession();
  const me = session.status === "signedIn" ? session.user.id : null;
  const thread = useThread(conversationId);
  if (!me || thread.state.status === "loading") return <Note role="status">Loading…</Note>;
  if (thread.state.status === "notFound") {
    return (
      <Note>
        This conversation isn&apos;t available.{" "}
        <Link href="/messaging" className="font-medium text-rs-accent hover:underline">
          Back to Messaging
        </Link>
      </Note>
    );
  }
  if (thread.state.status === "error") {
    return (
      <Note role="alert">
        Couldn&apos;t load this conversation.{" "}
        <button type="button" onClick={thread.retry} className="font-medium text-rs-accent hover:underline">
          Try again
        </button>
      </Note>
    );
  }
  return <LoadedThread me={me} thread={thread} state={thread.state} />;
}

export function Note({ role, children }: { role?: "status" | "alert"; children: React.ReactNode }) {
  return (
    <p role={role} className="p-6 text-center text-rs-body text-rs-text-muted">
      {children}
    </p>
  );
}

type LoadedThreadProps = {
  me: string;
  thread: ThreadHandle;
  state: Extract<ThreadHandle["state"], { status: "ok" }>;
};

function LoadedThread({ me, thread, state }: LoadedThreadProps) {
  const list = useConversations();
  const { conversation, messages } = state;
  const [notice, setNotice] = useState<SendNotice | null>(null);
  const [answering, setAnswering] = useState(false);
  const [answerError, setAnswerError] = useState<string | null>(null);
  const status = threadStatus(conversation, me);

  const outbox = useOutbox({
    deliver: (body, clientId) => sendMessage(conversation.id, body, clientId),
    onSent: ({ message, conversationState }: SentMessage, clientId) => {
      thread.addSent(message, conversationState, clientId);
      list.put("inbox", withLastMessage(conversation, message, conversationState));
      setNotice(null);
      void refreshUnreadMessages(me);
    },
    onFailed: (failure) => {
      setNotice(sendNotice(failure));
      if (isStale(failure.code)) thread.reload();
    },
  });

  // A request is not read until it is accepted, and a declined one never is (FR-16.2); the
  // sender's own message has moved their marker already.
  const newest = messages.at(-1);
  useMarkRead(conversation.id, conversation.state === "active" && newest && newest.senderId !== me ? newest.id : null, () => {
    list.patch(conversation.id, { unreadCount: 0 });
    void refreshUnreadMessages(me);
  });

  async function answer(run: typeof acceptMessageRequest, failed: string, done: (next: Conversation) => void) {
    setAnswerError(null);
    setAnswering(true);
    const res = await run(conversation.id);
    setAnswering(false);
    if (!res.ok) {
      setAnswerError(res.code === "not_found" ? "This request can no longer be answered." : failed);
      if (res.code === "not_found") thread.reload();
      return;
    }
    thread.setConversation(res.data);
    done(res.data);
    void refreshUnreadMessages(me);
  }

  const accept = () => answer(acceptMessageRequest, "Couldn't accept the request. Try again.", (next) => list.put("inbox", next));
  const decline = () => answer(declineMessageRequest, "Couldn't decline the request. Try again.", (next) => list.remove(next.id));

  return (
    <section aria-label={`Conversation with ${conversation.otherMember.fullName}`} className="flex h-full min-h-0 flex-col">
      <ThreadHeader person={conversation.otherMember} />
      <MessageList
        messages={[...messages, ...outgoingMessages(outbox.outgoing, me)]}
        me={me}
        other={conversation.otherMember}
        seenId={seenMessageId(messages, me, state.otherLastReadMessageId, conversation)}
        hasEarlier={state.nextBefore !== null}
        loadingEarlier={state.loadingEarlier}
        earlierFailed={state.earlierFailed}
        onLoadEarlier={thread.loadEarlier}
        onRetry={outbox.retry}
      />
      {status !== "open" && (
        <ThreadBanner
          status={status}
          name={conversation.otherMember.fullName}
          busy={answering}
          error={answerError}
          onAccept={accept}
          onDecline={decline}
        />
      )}
      <Composer disabled={status !== "open"} notice={notice} onSend={outbox.send} />
    </section>
  );
}

function withLastMessage(conversation: Conversation, message: SentMessage["message"], state: Conversation["state"]): Conversation {
  return {
    ...conversation,
    state,
    lastMessageAt: message.createdAt,
    unreadCount: 0,
    lastMessage: { senderId: message.senderId, body: [...message.body].slice(0, 120).join(""), createdAt: message.createdAt },
  };
}
