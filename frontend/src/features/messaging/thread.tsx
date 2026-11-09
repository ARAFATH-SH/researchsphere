"use client";

import Link from "next/link";
import { useState } from "react";

import { useSession } from "@/features/auth/session";
import { BlockDialog } from "@/features/social/block-dialog";
import { useRealtimeEvent } from "@/features/realtime/use-realtime";

import { acceptMessageRequest, type Conversation, declineMessageRequest, sendMessage, type SentMessage } from "./api";
import { Composer, RestrictedComposer } from "./composer";
import { seenMessageId, threadStatus } from "./format";
import { MessageList } from "./message-list";
import { ReportDialog } from "./report-dialog";
import { isStale, sendNotice, type SendNotice } from "./send-errors";
import { ThreadActions } from "./thread-actions";
import { ThreadBanner } from "./thread-banner";
import { ThreadHeader } from "./thread-header";
import { refreshUnreadMessages } from "./unread-count";
import { useIsTyping } from "./typing";
import { useConversations } from "./use-conversations";
import { useMarkRead } from "./use-mark-read";
import { useMessagingRestriction } from "./use-messaging-restriction";
import { outgoingMessages, useOutbox } from "./use-outbox";
import { type Thread as ThreadHandle, useThread } from "./use-thread";
import { useTypingSender } from "./use-typing-sender";

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

// ThreadDialog is the open dialog: a report of the conversation or of one message, or asking
// before a block or an unblock.
type ThreadDialog = { kind: "report"; messageId?: string } | { kind: "block" } | { kind: "unblock" };

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
  const [dialog, setDialog] = useState<ThreadDialog | null>(null);
  const restriction = useMessagingRestriction();
  const status = threadStatus(conversation, me);
  const person = conversation.otherMember;

  const outbox = useOutbox({
    deliver: (body, clientId) => sendMessage(conversation.id, body, clientId),
    onSent: ({ message, conversationState }: SentMessage, clientId) => {
      thread.addSent(message, conversationState, clientId);
      list.put("inbox", withLastMessage(conversation, message, conversationState));
      setNotice(null);
      void refreshUnreadMessages(me);
    },
    onFailed: (failure) => {
      // The restriction may be newer than the last read of /auth/me.
      if (failure.code === "messaging_restricted" && failure.until) {
        restriction.refused(failure.until);
        setNotice(null);
        return;
      }
      setNotice(sendNotice(failure));
      if (isStale(failure.code)) thread.reload();
    },
  });

  const typing = useTypingSender(conversation.id, status === "open");
  const otherTyping = useIsTyping(conversation.id);

  // The echo of a message this tab sent can arrive before the send's answer. useThread stores
  // it under the clientId, so its row stays the same one.
  useRealtimeEvent((event) => {
    if (event.type === "message.created" && event.data.clientId) outbox.confirm(event.data.clientId);
  });

  function send(body: string) {
    typing.stop();
    outbox.send(body);
  }

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

  // A block declines a pending request, which takes it out of both boxes.
  function blocked() {
    setDialog(null);
    if (conversation.state === "request") list.remove(conversation.id);
    else list.patch(conversation.id, { blockedByMe: true, canMessage: false });
    thread.reload();
  }

  function unblocked() {
    setDialog(null);
    list.patch(conversation.id, { blockedByMe: false });
    thread.reload();
  }

  return (
    <section aria-label={`Conversation with ${person.fullName}`} className="flex h-full min-h-0 flex-col">
      <ThreadHeader
        person={person}
        typing={otherTyping}
        actions={
          <ThreadActions
            name={person.fullName}
            available={person.available}
            blockedByMe={conversation.blockedByMe}
            onBlock={() => setDialog({ kind: "block" })}
            onUnblock={() => setDialog({ kind: "unblock" })}
            onReport={() => setDialog({ kind: "report" })}
          />
        }
      />
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
        onReportMessage={(messageId) => setDialog({ kind: "report", messageId })}
      />
      {status !== "open" && (
        <ThreadBanner
          status={status}
          name={conversation.otherMember.fullName}
          busy={answering}
          error={answerError}
          onAccept={accept}
          onDecline={decline}
          onUnblock={() => setDialog({ kind: "unblock" })}
        />
      )}
      {restriction.until ? (
        <RestrictedComposer until={restriction.until} />
      ) : (
        <Composer disabled={status !== "open"} notice={notice} onSend={send} onInput={typing.input} onBlur={typing.stop} />
      )}
      {dialog?.kind === "report" && (
        <ReportDialog
          conversationId={conversation.id}
          messageId={dialog.messageId}
          name={person.fullName}
          canBlock={person.available && !conversation.blockedByMe}
          onClose={() => setDialog(null)}
          onBlock={() => setDialog({ kind: "block" })}
        />
      )}
      {(dialog?.kind === "block" || dialog?.kind === "unblock") && (
        <BlockDialog
          action={dialog.kind}
          userId={person.userId}
          name={person.fullName}
          onClose={() => setDialog(null)}
          onDone={dialog.kind === "block" ? blocked : unblocked}
        />
      )}
    </section>
  );
}

function withLastMessage(conversation: Conversation, message: SentMessage["message"], state: Conversation["state"]): Conversation {
  return {
    ...conversation,
    state,
    lastMessageAt: message.createdAt,
    unreadCount: 0,
    lastMessage: {
      senderId: message.senderId,
      kind: "text",
      event: null,
      body: [...(message.body ?? "")].slice(0, 120).join(""),
      deleted: message.deleted,
      // Files arrive with slice 14B.5's UI; a message's own count keeps the preview honest.
      attachmentCount: message.deleted ? 0 : message.attachments.length,
      createdAt: message.createdAt,
    },
  };
}
