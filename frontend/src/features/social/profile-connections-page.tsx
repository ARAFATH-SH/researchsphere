"use client";

import Link from "next/link";

import { Avatar } from "@/components/avatar";
import { rsButtonClass } from "@/components/ui/button";
import { homeCard } from "@/features/home/cards";
import { connectionsLabel } from "@/features/profile/profile-header";

import { type Connection, listProfileConnections } from "./api";
import { ConnectButton } from "./connect-button";
import { connectedLine } from "./format";
import { usePagedList } from "./paged-list";

const connectionId = (c: Connection) => c.userId;

// ProfileConnectionsPage lists any user's connections (FR-7.4), 20 at a time with "Show
// more"; "N connections" on another user's profile links here. Each row has the viewer's
// own connect button, so it is empty for the viewer's own row and for people they are
// already connected with.
export function ProfileConnectionsPage({ userId }: { userId: string }) {
  const list = usePagedList(userId, (page) => listProfileConnections(userId, page), connectionId);
  const { state } = list;

  let body;
  if (state.status === "loading") {
    body = (
      <p role="status" className="px-5 pb-5 text-rs-body text-rs-text-muted">
        Loading connections…
      </p>
    );
  } else if (state.status === "notFound") {
    body = <p className="px-5 pb-5 text-rs-body text-rs-text-muted">This profile does not exist or is not available.</p>;
  } else if (state.status === "error") {
    body = (
      <div className="flex flex-col items-start gap-3 px-5 pb-5">
        <p className="text-rs-body text-rs-text-muted">We couldn&apos;t load the connections.</p>
        <button type="button" onClick={list.retry} className={rsButtonClass({ variant: "secondary", size: "sm" })}>
          Try again
        </button>
      </div>
    );
  } else if (state.items.length === 0) {
    body = <p className="px-5 pb-5 text-rs-body text-rs-text-muted">No connections yet.</p>;
  } else {
    body = (
      <>
        <ul aria-label="Connections" className="border-t border-rs-divider">
          {state.items.map((c) => (
            <li key={c.userId} className="flex flex-wrap items-center gap-3 border-b border-rs-divider px-5 py-3">
              <Avatar name={c.fullName} src={c.photoUrl} size="list" decorative />
              <div className="min-w-[160px] flex-1">
                <Link
                  href={`/profile/${c.userId}`}
                  className="rounded-rs-2xs font-bold text-rs-text no-underline hover:text-rs-accent-hover hover:underline focus-visible:shadow-focus focus-visible:outline-none"
                >
                  {c.fullName}
                </Link>
                {c.headline && <div className="text-rs-caption text-rs-text-muted">{c.headline}</div>}
                <div className="text-rs-caption text-rs-text-muted">{connectedLine(c.connectedAt)}</div>
              </div>
              <ConnectButton
                userId={c.userId}
                name={c.fullName}
                status={c.connectionStatus}
                requestId={c.connectionRequestId}
                size="row"
              />
            </li>
          ))}
        </ul>
        {state.moreFailed && (
          <p role="alert" className="px-5 pt-2 text-rs-caption text-rs-danger">
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
    <section aria-labelledby="profile-connections-heading" className={homeCard}>
      <div className="flex flex-col gap-3 p-5 pb-3">
        <Link
          href={`/profile/${userId}`}
          className="self-start rounded-rs-2xs text-rs-body font-medium text-rs-accent-strong hover:underline focus-visible:shadow-focus focus-visible:outline-none"
        >
          <span aria-hidden="true">←</span> Back to profile
        </Link>
        <h1 id="profile-connections-heading" className="text-rs-title leading-rs-snug font-medium text-rs-text">
          {state.status === "ok" ? connectionsLabel(state.total) : "Connections"}
        </h1>
      </div>
      {body}
    </section>
  );
}
