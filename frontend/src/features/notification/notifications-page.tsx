"use client";

import { useRouter } from "next/navigation";
import { type MouseEvent, type ReactNode, useState } from "react";

import { Avatar } from "@/components/avatar";
import { useSession } from "@/features/auth/session";
import { emptyCard, homeCard } from "@/features/home/cards";
import { acceptConnectionRequest, rejectConnectionRequest } from "@/features/social/api";
import { pillClass } from "@/features/social/connect-button";

import { markAllNotificationsRead, markNotificationRead, type Notification } from "./api";
import { refreshBadges } from "./badges";
import { relativeTime } from "./format";
import { type NotificationList, useNotificationList } from "./notification-list";
import { hasPendingRequest, sentenceFor } from "./types";
import { setUnreadNotifications, useUnreadNotifications } from "./unread-count";

const headingId = "notifications-heading";

// NotificationsPage lists the signed-in user's notifications, newest first (FR-9.5), as
// design/screens/Notifications.html and Notifications_m.html draw them: one card with the
// title and "Mark all as read" (FR-9.6), then a row per notification. Opening a row marks
// it read; a connection request that is still pending can be accepted or ignored in place.
// The design's All / Unread tabs and its explanatory side card are left out (the API has no
// unread filter), and so are types this client does not know yet.
export function NotificationsPage() {
  const session = useSession();
  const me = session.status === "signedIn" ? session.user.id : null;
  const list = useNotificationList(me ?? "");
  const unread = useUnreadNotifications();
  const [markingAll, setMarkingAll] = useState(false);
  const [markAllFailed, setMarkAllFailed] = useState(false);
  const { state } = list;

  const visible = state.status === "ok" ? state.items.filter((n) => sentenceFor(n) !== null) : [];
  const anyUnread = visible.some((n) => !n.read);
  const empty = state.status === "ok" && visible.length === 0 && !state.nextCursor;

  async function markAll() {
    if (!me) return;
    setMarkAllFailed(false);
    setMarkingAll(true);
    const res = await markAllNotificationsRead();
    setMarkingAll(false);
    if (!res.ok) {
      setMarkAllFailed(true);
      return;
    }
    list.updateAll({ read: true });
    setUnreadNotifications(me, 0);
    refreshBadges(me);
  }

  let body: ReactNode;
  if (state.status === "loading") {
    body = (
      <p role="status" className="px-4 py-3.5 text-rs-body text-rs-text-muted">
        Loading…
      </p>
    );
  } else if (state.status === "error") {
    body = (
      <div className="flex flex-wrap items-center gap-3 px-4 py-3.5">
        <p className="text-rs-body text-rs-text-muted">Couldn&apos;t load your notifications.</p>
        <button type="button" onClick={list.retry} className={pillClass("outline", "row")}>
          Try again
        </button>
      </div>
    );
  } else if (empty) {
    body = <p className="px-4 py-10 text-center text-rs-body text-rs-text-muted">No notifications yet.</p>;
  } else {
    body = <Rows list={list} items={visible} me={me} />;
  }

  return (
    <section aria-labelledby={headingId} className={empty ? emptyCard : homeCard}>
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-rs-divider px-4 py-3.5">
        <h1 id={headingId} className="text-rs-h3 leading-rs-tight font-bold text-rs-text">
          Notifications
        </h1>
        <button
          type="button"
          onClick={markAll}
          disabled={markingAll || (unread === 0 && !anyUnread)}
          className={pillClass("outline", "row")}
        >
          Mark all as read
        </button>
      </div>
      {markAllFailed && (
        <p role="alert" className="px-4 pt-2 text-rs-caption text-rs-danger">
          Couldn&apos;t mark your notifications as read. Try again.
        </p>
      )}
      {body}
    </section>
  );
}

function Rows({ list, items, me }: { list: NotificationList; items: Notification[]; me: string | null }) {
  const { state } = list;
  // connected holds the requests accepted here, which then read "You're now connected".
  const [connected, setConnected] = useState<ReadonlySet<string>>(new Set());
  if (state.status !== "ok") return null;

  // markRead shows n as read at once and tells the server; it goes back if that fails.
  function markRead(n: Notification) {
    if (n.read) return;
    list.update(n.id, { read: true });
    void markNotificationRead(n.id).then((res) => {
      if (!res.ok) list.update(n.id, { read: false });
      else if (me) refreshBadges(me);
    });
  }

  return (
    <>
      <ul aria-label="Notifications">
        {items.map((n) => (
          <Row
            key={n.id}
            n={n}
            connected={connected.has(n.id)}
            onRead={() => markRead(n)}
            onAccepted={() => {
              setConnected((prev) => new Set(prev).add(n.id));
              list.update(n.id, { requestPending: false });
              // Answering a request counts as reading it; markRead refreshes the badges.
              if (n.read) {
                if (me) refreshBadges(me);
              } else markRead(n);
            }}
            onIgnored={() => {
              list.remove(n.id);
              if (me) refreshBadges(me);
            }}
            onGone={() => list.update(n.id, { requestPending: false })}
          />
        ))}
      </ul>
      {state.moreFailed && (
        <p role="alert" className="px-4 pt-2 text-rs-caption text-rs-danger">
          Couldn&apos;t load more. Try again.
        </p>
      )}
      {state.nextCursor && (
        <button
          type="button"
          onClick={list.loadMore}
          disabled={state.loadingMore}
          className="min-h-11 w-full rounded-b-card text-rs-body font-medium text-rs-accent-strong hover:bg-rs-subtle focus-visible:shadow-focus focus-visible:outline-none disabled:opacity-60"
        >
          {state.loadingMore ? "Loading…" : "Show more"}
        </button>
      )}
    </>
  );
}

type RowProps = {
  n: Notification;
  connected: boolean;
  onRead: () => void;
  onAccepted: () => void;
  onIgnored: () => void;
  // onGone hears that the request is no longer pending (withdrawn or answered elsewhere).
  onGone: () => void;
};

// Row is one notification: the unread dot, the actor's photo, the sentence (a link that
// covers the row), the time, and the row's actions above that link. Unread rows are tinted
// and also say "Unread" to screen readers, so the state is not shown by color alone.
function Row({ n, connected, onRead, onAccepted, onIgnored, onGone }: RowProps) {
  const router = useRouter();
  const name = n.actor.fullName;

  function open(e: MouseEvent<HTMLAnchorElement>) {
    onRead();
    // A modified or middle click opens the link the browser's way.
    if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    router.push(n.link);
  }

  return (
    <li
      className={`relative flex items-start gap-3 border-b border-rs-divider px-4 py-3.5 last:border-b-0 max-[640px]:px-3 ${n.read ? "bg-rs-surface" : "bg-rs-accent-soft"}`}
    >
      <span aria-hidden="true" className={`mt-5 size-2 flex-none rounded-full ${n.read ? "" : "bg-rs-accent-strong"}`} />
      <Avatar name={name} src={n.actor.photoUrl} size="list" decorative />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <a
          href={n.link}
          onClick={open}
          className="text-rs-body text-rs-text no-underline after:absolute after:inset-0 hover:text-rs-text focus-visible:outline-none focus-visible:after:shadow-focus"
        >
          {!n.read && <span className="sr-only">Unread: </span>}
          {sentenceFor(n)}
        </a>
        <time dateTime={n.createdAt} className="text-rs-caption text-rs-text-muted">
          {relativeTime(n.createdAt)}
        </time>
        {connected ? (
          <p role="status" className="text-rs-caption font-medium text-rs-accent-strong">
            You&apos;re now connected
          </p>
        ) : (
          hasPendingRequest(n) && (
            <RequestActions requestId={n.requestId} name={name} onAccepted={onAccepted} onIgnored={onIgnored} onGone={onGone} />
          )
        )}
      </div>
      {!n.read && (
        <button
          type="button"
          onClick={onRead}
          aria-label={`Mark notification from ${name} as read`}
          className="relative z-10 h-8 flex-none rounded-rs-pill px-2.5 text-rs-caption font-medium text-rs-accent-strong hover:bg-rs-surface focus-visible:shadow-focus focus-visible:outline-none"
        >
          Mark as read
        </button>
      )}
    </li>
  );
}

type RequestActionsProps = {
  requestId: string;
  name: string;
  onAccepted: () => void;
  onIgnored: () => void;
  onGone: () => void;
};

// RequestActions answers a pending connection request from its notification (FR-7.3)
// with the same calls and buttons as My Network. The row changes once the server agrees.
function RequestActions({ requestId, name, onAccepted, onIgnored, onGone }: RequestActionsProps) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function answer(run: (id: string) => Promise<{ ok: boolean; code?: string }>, done: () => void, failed: string) {
    setError(null);
    setBusy(true);
    const res = await run(requestId);
    setBusy(false);
    if (res.ok) {
      done();
    } else if (res.code === "not_found") {
      onGone();
    } else {
      setError(failed);
    }
  }

  return (
    <div className="relative z-10 mt-1 flex flex-col items-start gap-1">
      <div className="flex flex-wrap items-center gap-2">
        <button
          type="button"
          aria-label={`Ignore request from ${name}`}
          onClick={() => answer(rejectConnectionRequest, onIgnored, "Couldn't ignore the request. Try again.")}
          disabled={busy}
          className={pillClass("outline", "row")}
        >
          Ignore
        </button>
        <button
          type="button"
          aria-label={`Accept request from ${name}`}
          onClick={() => answer(acceptConnectionRequest, onAccepted, "Couldn't accept the request. Try again.")}
          disabled={busy}
          className={pillClass("filled", "row")}
        >
          Accept
        </button>
      </div>
      {error && (
        <p role="alert" className="text-rs-caption text-rs-danger">
          {error}
        </p>
      )}
    </div>
  );
}
