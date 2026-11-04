"use client";

import { useEffect, useRef, useState } from "react";

import { useSession } from "@/features/auth/session";
import { refreshBadges } from "@/features/notification/badges";

import {
  acceptConnectionRequest,
  cancelConnectionRequest,
  type ConnectionStatus,
  rejectConnectionRequest,
  sendConnectionRequest,
} from "./api";

// Link is the caller's connection with one user: its status and, while it is pending,
// the request's ID.
export type Link = { status: ConnectionStatus; requestId: string | null };

// The two sizes: profile is the 40px action row of design/screens/Profile.html, with a
// filled Connect; row is the 36px outline pill of a search or list row (Search.html).
export type ConnectButtonSize = "profile" | "row";

type ConnectButtonProps = {
  userId: string;
  name: string;
  status: ConnectionStatus;
  requestId: string | null;
  size: ConnectButtonSize;
  // onChange hears every change, the optimistic one first and a rollback after it.
  onChange?: (link: Link) => void;
};

export const rateLimitedMessage = "You've sent a lot of requests today. Try again tomorrow.";

const shape: Record<ConnectButtonSize, string> = {
  profile: "h-10 px-5",
  row: "h-9 px-4",
};
const pill =
  "inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-rs-pill border text-rs-body font-medium transition-colors focus-visible:shadow-focus focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60";
const look: Record<PillKind, string> = {
  filled: "border-rs-accent-strong bg-rs-accent-strong text-rs-surface hover:border-rs-accent-hover hover:bg-rs-accent-hover",
  outline: "border-rs-accent-strong bg-rs-surface text-rs-accent-strong hover:bg-rs-accent-soft",
  danger: "border-rs-danger bg-rs-surface text-rs-danger hover:bg-rs-danger-soft",
  quiet: "border-transparent bg-transparent text-rs-text-muted hover:bg-rs-subtle hover:text-rs-text",
};

// PillKind is a connection button's look; pillClass gives its classes, so other rows that
// answer a request (the notifications list) look the same.
export type PillKind = "filled" | "outline" | "danger" | "quiet";

export function pillClass(kind: PillKind, size: ConnectButtonSize): string {
  return `${pill} ${shape[size]} ${look[kind]}`;
}

type Message = { text: string; error: boolean };

// ConnectButton is the connection action for one user (FR-7.1 to FR-7.3): Connect when
// there is no link, Pending (which offers Withdraw request) after sending one, Accept and
// Ignore when they sent one, and nothing when connected or on the caller's own profile or
// row. Every action shows its result at once and goes back if the request fails.
export function ConnectButton({ userId, name, status, requestId, size, onChange }: ConnectButtonProps) {
  const session = useSession();
  const me = session.status === "signedIn" ? session.user.id : null;
  const [link, setLinkState] = useState<Link>({ status, requestId });
  // synced is the link the props last gave; a change there (a new profile or row) wins.
  const [synced, setSynced] = useState<Link>({ status, requestId });
  if (synced.status !== status || synced.requestId !== requestId) {
    setSynced({ status, requestId });
    setLinkState({ status, requestId });
  }
  const [busy, setBusy] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [message, setMessage] = useState<Message | null>(null);
  const pendingButton = useRef<HTMLButtonElement>(null);
  const withdrawButton = useRef<HTMLButtonElement>(null);
  // focusPending returns the focus to Pending once the confirmation has gone.
  const focusPending = useRef(false);

  useEffect(() => {
    if (confirming) withdrawButton.current?.focus();
    else if (focusPending.current) {
      focusPending.current = false;
      pendingButton.current?.focus();
    }
  }, [confirming]);

  function update(next: Link) {
    setLinkState(next);
    onChange?.(next);
  }

  function refreshBadge() {
    // Ignoring a request also deletes its notification, so both badges may change.
    if (me) refreshBadges(me);
  }

  async function connect() {
    const before = link;
    setMessage(null);
    setBusy(true);
    update({ status: "pending_sent", requestId: null });
    const res = await sendConnectionRequest(userId);
    setBusy(false);
    if (res.ok) {
      update({ status: "pending_sent", requestId: res.data.requestId });
      return;
    }
    switch (res.code) {
      case "request_pending_from_them":
        update({ status: "pending_received", requestId: res.requestId ?? null });
        setMessage({ text: `${name} has already sent you a request.`, error: false });
        refreshBadge();
        return;
      case "request_already_pending":
        update({ status: "pending_sent", requestId: null });
        return;
      case "already_connected":
        update({ status: "connected", requestId: null });
        return;
      case "rate_limited":
        update(before);
        setMessage({ text: rateLimitedMessage, error: true });
        return;
      default:
        update(before);
        setMessage({ text: "Couldn't send the request. Try again.", error: true });
    }
  }

  // answer runs an action on the pending request: it shows next at once and goes back to
  // the link before it, with failed as the message, if the request fails.
  async function answer(next: Link, run: (id: string) => Promise<{ ok: boolean }>, failed: string, badge: boolean) {
    const before = link;
    if (!before.requestId) return;
    setMessage(null);
    setConfirming(false);
    setBusy(true);
    update(next);
    const res = await run(before.requestId);
    setBusy(false);
    if (res.ok) {
      if (badge) refreshBadge();
      return;
    }
    update(before);
    setMessage({ text: failed, error: true });
  }

  function cancelConfirm() {
    focusPending.current = true;
    setConfirming(false);
  }

  const cls = (kind: PillKind) => pillClass(kind, size);
  let buttons = null;
  if (link.status === "none") {
    buttons = (
      <button type="button" aria-label={`Connect with ${name}`} onClick={connect} disabled={busy} className={cls(size === "profile" ? "filled" : "outline")}>
        <PlusIcon />
        Connect
      </button>
    );
  } else if (link.status === "pending_sent") {
    buttons = confirming ? (
      <>
        <button
          ref={withdrawButton}
          type="button"
          aria-label={`Withdraw request to ${name}`}
          onClick={() =>
            answer({ status: "none", requestId: null }, cancelConnectionRequest, "Couldn't withdraw the request. Try again.", false)
          }
          className={cls("danger")}
        >
          Withdraw request
        </button>
        <button type="button" onClick={cancelConfirm} className={cls("quiet")}>
          Cancel
        </button>
      </>
    ) : (
      <button
        ref={pendingButton}
        type="button"
        aria-label={`Pending request to ${name}`}
        onClick={() => setConfirming(true)}
        disabled={busy || !link.requestId}
        className={cls("outline")}
      >
        Pending
      </button>
    );
  } else if (link.status === "pending_received") {
    buttons = (
      <>
        <button
          type="button"
          aria-label={`Accept request from ${name}`}
          onClick={() =>
            answer({ status: "connected", requestId: null }, acceptConnectionRequest, "Couldn't accept the request. Try again.", true)
          }
          disabled={busy || !link.requestId}
          className={cls("filled")}
        >
          Accept
        </button>
        <button
          type="button"
          aria-label={`Ignore request from ${name}`}
          onClick={() =>
            answer({ status: "none", requestId: null }, rejectConnectionRequest, "Couldn't ignore the request. Try again.", true)
          }
          disabled={busy || !link.requestId}
          className={cls("outline")}
        >
          Ignore
        </button>
      </>
    );
  }

  if (!buttons && !message) return null;
  return (
    <div className={`flex flex-col gap-1 ${size === "row" ? "items-end" : "items-start"}`}>
      {buttons && <div className="flex flex-wrap items-center gap-2">{buttons}</div>}
      {message && (
        <p role={message.error ? "alert" : "status"} className={`text-rs-caption ${message.error ? "text-rs-danger" : "text-rs-text-muted"}`}>
          {message.text}
        </p>
      )}
    </div>
  );
}

function PlusIcon() {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2.2} strokeLinecap="round" className="size-4">
      <path d="M12 5v14M5 12h14" />
    </svg>
  );
}
