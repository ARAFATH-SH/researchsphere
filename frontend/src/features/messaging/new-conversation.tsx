"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import { useSession } from "@/features/auth/session";
import { getProfile, type Profile } from "@/features/profile/api";

import { getConversation, sendDirectMessage, type SentMessage } from "./api";
import { Composer, RestrictedComposer } from "./composer";
import { MessageList } from "./message-list";
import { sendNotice, type SendNotice } from "./send-errors";
import { Note } from "./thread";
import { ThreadHeader } from "./thread-header";
import { refreshUnreadMessages } from "./unread-count";
import { useConversations } from "./use-conversations";
import { useMessagingRestriction } from "./use-messaging-restriction";
import { outgoingMessages, useOutbox } from "./use-outbox";

type State = { status: "loading" } | { status: "unavailable" } | { status: "error" } | { status: "ok"; profile: Profile };

// NewConversation is /messaging/new?to=<userId>: the first message to someone. It becomes a
// message request when the two are not connected. If they have a conversation already, it
// opens that one instead, so a link to this page is always safe.
export function NewConversation({ to }: { to: string | null }) {
  const session = useSession();
  const me = session.status === "signedIn" ? session.user.id : null;
  const router = useRouter();
  const [state, setState] = useState<State>({ status: "loading" });

  useEffect(() => {
    if (!to || !me) return;
    let current = true;
    void getProfile(to).then((res) => {
      if (!current) return;
      if (res.status !== "ok") {
        setState({ status: res.status === "notFound" ? "unavailable" : "error" });
      } else if (res.profile.conversation) {
        router.replace(`/messaging/${res.profile.conversation.id}`);
      } else if (to === me || res.profile.blockedByMe) {
        setState({ status: "unavailable" });
      } else {
        setState({ status: "ok", profile: res.profile });
      }
    });
    return () => {
      current = false;
    };
  }, [to, me, router]);

  if (!to) return <Note>Choose someone to message from their profile.</Note>;
  if (state.status === "unavailable") return <Note>You can&apos;t message this person.</Note>;
  if (state.status === "error") return <Note role="alert">Couldn&apos;t load this person. Try again.</Note>;
  if (state.status === "loading" || !me) return <Note role="status">Loading…</Note>;
  return <FirstMessage key={to} me={me} profile={state.profile} />;
}

function FirstMessage({ me, profile }: { me: string; profile: Profile }) {
  const router = useRouter();
  const list = useConversations();
  const [notice, setNotice] = useState<SendNotice | null>(null);
  const restriction = useMessagingRestriction();

  async function opened({ message }: SentMessage) {
    const res = await getConversation(message.conversationId);
    if (res.ok) list.put("inbox", res.data);
    void refreshUnreadMessages(me);
    router.replace(`/messaging/${message.conversationId}`);
  }

  const outbox = useOutbox({
    deliver: (body, clientId) => sendDirectMessage(profile.userId, body, clientId),
    onSent: (sent) => {
      setNotice(null);
      void opened(sent);
    },
    onFailed: (failure) => {
      // The restriction may be newer than the last read of /auth/me.
      if (failure.code === "messaging_restricted" && failure.until) {
        restriction.refused(failure.until);
        setNotice(null);
      } else {
        setNotice(sendNotice(failure));
      }
    },
  });

  return (
    <section aria-label={`New message to ${profile.fullName}`} className="flex h-full min-h-0 flex-col">
      <ThreadHeader subject={{ kind: "person", person: profile }} />
      <MessageList
        messages={outgoingMessages(outbox.outgoing, me)}
        me={me}
        personOf={(id) => (id === profile.userId ? { fullName: profile.fullName, photoUrl: profile.photoUrl } : undefined)}
        onRetry={outbox.retry}
      />
      {restriction.until ? <RestrictedComposer until={restriction.until} /> : <Composer notice={notice} onSend={outbox.send} />}
    </section>
  );
}
