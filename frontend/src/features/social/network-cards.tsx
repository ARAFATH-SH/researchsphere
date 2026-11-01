"use client";

import Link from "next/link";
import { type ReactNode, useState } from "react";

import { Avatar } from "@/components/avatar";
import { homeCard } from "@/features/home/cards";
import { MoreMenu } from "@/features/profile/more-menu";
import { connectionsLabel } from "@/features/profile/profile-header";
import type { Result } from "@/lib/api/call";

import {
  acceptConnectionRequest,
  cancelConnectionRequest,
  type Connection,
  type ConnectionRequest,
  rejectConnectionRequest,
} from "./api";
import { connectedLine, sentLine } from "./format";
import type { ListState, PagedList } from "./paged-list";

// The cards of the My Network page's left column (design/screens/Network.html): received
// invitations, sent invitations and connections (FR-7.3 to FR-7.5). Each pages with "Show
// more" and has its own empty state.

export const invitationsHeadingId = "invitations-heading";
export const sentHeadingId = "sent-heading";
export const connectionsHeadingId = "connections-heading";

const rowClass = "flex flex-wrap items-center gap-3 border-b border-rs-divider px-4 py-3";
const actionBase =
  "h-9 rounded-rs-pill border text-rs-body font-medium transition-colors focus-visible:shadow-focus focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60";
const quietButton = `${actionBase} border-transparent px-3 text-rs-text hover:bg-rs-subtle`;
const filledButton = `${actionBase} border-rs-accent-strong bg-rs-accent-strong px-4 text-rs-surface hover:border-rs-accent-hover hover:bg-rs-accent-hover`;
const secondaryButton = `${actionBase} border-rs-control bg-rs-surface px-4 text-rs-text hover:bg-rs-subtle`;

function countOf<T>(state: ListState<T>): number | null {
  return state.status === "ok" ? state.total : null;
}

// ListCard is one card: a heading row, then the rows, the empty state, or the loading or
// failed line, and "Show more" while the server has more.
function ListCard<T>({
  headingId,
  title,
  label,
  list,
  empty,
  failed,
  children,
}: {
  headingId: string;
  title: string;
  label: string;
  list: PagedList<T>;
  empty: ReactNode;
  failed: string;
  children: (items: T[]) => ReactNode;
}) {
  const { state } = list;
  let body: ReactNode;
  if (state.status === "loading") {
    body = (
      <p role="status" className="px-4 py-3 text-rs-body text-rs-text-muted">
        Loading…
      </p>
    );
  } else if (state.status !== "ok") {
    body = (
      <div className="flex flex-wrap items-center gap-3 px-4 py-3">
        <p className="text-rs-body text-rs-text-muted">{failed}</p>
        <button type="button" onClick={list.retry} className={secondaryButton}>
          Try again
        </button>
      </div>
    );
  } else if (state.items.length === 0) {
    body = <div className="px-4 py-3 text-rs-body text-rs-text-muted">{empty}</div>;
  } else {
    body = (
      <>
        <ul aria-label={label}>{children(state.items)}</ul>
        {state.moreFailed && (
          <p role="alert" className="px-4 pt-2 text-rs-caption text-rs-danger">
            Couldn&apos;t load more. Try again.
          </p>
        )}
        {state.items.length < state.total && (
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
  return (
    <section aria-labelledby={headingId} className={homeCard}>
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-rs-divider px-4 py-3">
        <h3 id={headingId} className="text-rs-lead leading-rs-snug font-medium text-rs-text">
          {title}
        </h3>
      </div>
      {body}
    </section>
  );
}

// Person is the avatar, the linked name and up to two muted lines of a row.
function Person({
  userId,
  name,
  photoUrl,
  lines,
}: {
  userId: string;
  name: string;
  photoUrl: string | null;
  lines: (string | null)[];
}) {
  return (
    <>
      <Avatar name={name} src={photoUrl} size="list" decorative />
      <div className="min-w-[160px] flex-1">
        <Link
          href={`/profile/${userId}`}
          className="rounded-rs-2xs font-bold text-rs-text no-underline hover:text-rs-accent-hover hover:underline focus-visible:shadow-focus focus-visible:outline-none"
        >
          {name}
        </Link>
        {lines.filter(Boolean).map((line) => (
          <div key={line} className="text-rs-caption text-rs-text-muted">
            {line}
          </div>
        ))}
      </div>
    </>
  );
}

// useRowAction runs one action per row at a time and keeps each row's failure message.
function useRowAction() {
  const [busy, setBusy] = useState<string | null>(null);
  const [errors, setErrors] = useState<Record<string, string>>({});
  async function run<T>(id: string, action: () => Promise<Result<T>>, failed: string, done: (data: T) => void) {
    setBusy(id);
    setErrors((prev) => {
      const next = { ...prev };
      delete next[id];
      return next;
    });
    const res = await action();
    setBusy(null);
    if (res.ok) done(res.data);
    else setErrors((prev) => ({ ...prev, [id]: failed }));
  }
  return { busy, errors, run };
}

function RowError({ text }: { text?: string }) {
  if (!text) return null;
  return (
    <p role="alert" className="w-full text-right text-rs-caption text-rs-danger">
      {text}
    </p>
  );
}

// InvitationsCard lists the requests sent to the user with Ignore and Accept. Accepting
// moves the person into the connections card through onAccepted.
export function InvitationsCard({
  list,
  onAccepted,
}: {
  list: PagedList<ConnectionRequest>;
  onAccepted: (connection: Connection) => void;
}) {
  const { busy, errors, run } = useRowAction();
  const count = countOf(list.state);
  return (
    <ListCard
      headingId={invitationsHeadingId}
      title={count === null ? "Invitations received" : `Invitations received (${count})`}
      label="Invitations received"
      list={list}
      empty="No pending invitations. When someone asks to connect, it shows up here."
      failed="We couldn't load your invitations."
    >
      {(items) =>
        items.map((r) => (
          <li key={r.requestId} className={rowClass}>
            <Person userId={r.userId} name={r.fullName} photoUrl={r.photoUrl} lines={[r.headline]} />
            <button
              type="button"
              aria-label={`Ignore request from ${r.fullName}`}
              disabled={busy === r.requestId}
              onClick={() =>
                run(r.requestId, () => rejectConnectionRequest(r.requestId), "Couldn't ignore the request. Try again.", () =>
                  list.remove(r.requestId),
                )
              }
              className={quietButton}
            >
              Ignore
            </button>
            <button
              type="button"
              aria-label={`Accept request from ${r.fullName}`}
              disabled={busy === r.requestId}
              onClick={() =>
                run(r.requestId, () => acceptConnectionRequest(r.requestId), "Couldn't accept the request. Try again.", (connection) => {
                  list.remove(r.requestId);
                  onAccepted(connection);
                })
              }
              className={filledButton}
            >
              Accept
            </button>
            <RowError text={errors[r.requestId]} />
          </li>
        ))
      }
    </ListCard>
  );
}

// SentCard lists the user's pending requests with Withdraw. A rejected request simply
// leaves this list: the sender is never told (FR-7.6).
export function SentCard({ list }: { list: PagedList<ConnectionRequest> }) {
  const { busy, errors, run } = useRowAction();
  const count = countOf(list.state);
  return (
    <ListCard
      headingId={sentHeadingId}
      title={count === null ? "Invitations sent" : `Invitations sent (${count})`}
      label="Invitations sent"
      list={list}
      empty="No invitations waiting for an answer."
      failed="We couldn't load your sent invitations."
    >
      {(items) =>
        items.map((r) => (
          <li key={r.requestId} className={rowClass}>
            <Person userId={r.userId} name={r.fullName} photoUrl={r.photoUrl} lines={[sentLine(r.createdAt)]} />
            <button
              type="button"
              aria-label={`Withdraw request to ${r.fullName}`}
              disabled={busy === r.requestId}
              onClick={() =>
                run(r.requestId, () => cancelConnectionRequest(r.requestId), "Couldn't withdraw the request. Try again.", () =>
                  list.remove(r.requestId),
                )
              }
              className={secondaryButton}
            >
              Withdraw
            </button>
            <RowError text={errors[r.requestId]} />
          </li>
        ))
      }
    </ListCard>
  );
}

// ConnectionsCard lists the user's connections, newest first, each with a "…" menu that
// offers Remove connection (asked again inside the menu) and Copy profile link.
export function ConnectionsCard({ list }: { list: PagedList<Connection> }) {
  const count = countOf(list.state);
  return (
    <ListCard
      headingId={connectionsHeadingId}
      title={count === null ? "Connections" : connectionsLabel(count)}
      label="Connections"
      list={list}
      empty={
        <>
          No connections yet.{" "}
          <Link
            href="/search"
            className="rounded-rs-2xs font-medium text-rs-accent-strong hover:text-rs-accent-hover hover:underline focus-visible:shadow-focus focus-visible:outline-none"
          >
            Search for researchers
          </Link>{" "}
          to connect with.
        </>
      }
      failed="We couldn't load your connections."
    >
      {(items) =>
        items.map((c) => (
          <li key={c.userId} className={rowClass}>
            <Person
              userId={c.userId}
              name={c.fullName}
              photoUrl={c.photoUrl}
              lines={[c.headline, connectedLine(c.connectedAt)]}
            />
            <MoreMenu
              userId={c.userId}
              variant="icon"
              label={`More actions for ${c.fullName}`}
              connected={{ name: c.fullName, onRemoved: () => list.remove(c.userId) }}
            />
          </li>
        ))
      }
    </ListCard>
  );
}
