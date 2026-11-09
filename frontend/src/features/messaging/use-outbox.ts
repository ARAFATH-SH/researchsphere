import { useEffect, useRef, useState } from "react";

import type { Attachment, SendResult, SentMessage } from "./api";
import type { ThreadMessage } from "./format";

export type Outgoing = {
  clientId: string;
  body: string;
  // attachments are the uploaded files sent with it; a retry sends the same ones.
  attachments: Attachment[];
  createdAt: string;
  status: "sending" | "failed";
};

type Failure = Extract<SendResult, { ok: false }>;

type OutboxOptions = {
  deliver: (body: string, clientId: string, attachmentIds: string[]) => Promise<SendResult>;
  onSent: (sent: SentMessage, clientId: string) => void;
  onFailed: (failure: Failure) => void;
};

export type Outbox = {
  outgoing: Outgoing[];
  send: (body: string, attachments?: Attachment[]) => void;
  retry: (clientId: string) => void;
  // confirm drops a message whose echo arrived on the socket, whether before the send's answer
  // or instead of it.
  confirm: (clientId: string) => void;
};

// useOutbox holds messages on their way. Each gets a clientId when written and keeps it on
// every retry, so a retry after a lost answer stores nothing new, and the socket's echo of a
// message still listed here carries the same ID.
export function useOutbox(options: OutboxOptions): Outbox {
  const [outgoing, setOutgoing] = useState<Outgoing[]>([]);
  const latest = useRef(options);
  const confirmed = useRef(new Set<string>());
  useEffect(() => {
    latest.current = options;
  });

  async function run({ clientId, body, attachments }: Outgoing) {
    const res = await latest.current.deliver(
      body,
      clientId,
      attachments.map((a) => a.id),
    );
    if (res.ok) {
      latest.current.onSent(res.data, clientId);
      setOutgoing((prev) => prev.filter((o) => o.clientId !== clientId));
      return;
    }
    if (confirmed.current.has(clientId)) return;
    setOutgoing((prev) => prev.map((o) => (o.clientId === clientId ? { ...o, status: "failed" } : o)));
    latest.current.onFailed(res);
  }

  function send(body: string, attachments: Attachment[] = []) {
    const item: Outgoing = { clientId: crypto.randomUUID(), body, attachments, createdAt: new Date().toISOString(), status: "sending" };
    setOutgoing((prev) => [...prev, item]);
    void run(item);
  }

  function retry(clientId: string) {
    const item = outgoing.find((o) => o.clientId === clientId);
    if (!item || item.status !== "failed") return;
    setOutgoing((prev) => prev.map((o) => (o.clientId === clientId ? { ...o, status: "sending" } : o)));
    void run(item);
  }

  function confirm(clientId: string) {
    confirmed.current.add(clientId);
    setOutgoing((prev) => prev.filter((o) => o.clientId !== clientId));
  }

  return { outgoing, send, retry, confirm };
}

// outgoingMessages shows the outbox in a thread.
export function outgoingMessages(outgoing: Outgoing[], me: string): ThreadMessage[] {
  return outgoing.map((o) => ({
    key: o.clientId,
    id: null,
    senderId: me,
    event: null,
    body: o.body,
    createdAt: o.createdAt,
    editedAt: null,
    deleted: false,
    attachments: o.attachments,
    delivery: o.status,
  }));
}
