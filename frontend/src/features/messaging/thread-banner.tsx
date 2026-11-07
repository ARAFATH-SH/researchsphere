import { pillClass } from "@/features/social/connect-button";

import type { ThreadStatus } from "./format";

type ThreadBannerProps = {
  status: Exclude<ThreadStatus, "open">;
  name: string;
  busy: boolean;
  error: string | null;
  onAccept: () => void;
  onDecline: () => void;
  onUnblock: () => void;
};

// ThreadBanner says why the composer is off. readOnly never says that they blocked the caller.
export function ThreadBanner({ status, name, busy, error, onAccept, onDecline, onUnblock }: ThreadBannerProps) {
  if (status === "blocked") {
    return (
      <div role="status" className="flex flex-wrap items-center justify-between gap-2 border-t border-rs-divider px-4 py-2.5">
        <p className="text-rs-body text-rs-text">You blocked {name}.</p>
        <button type="button" onClick={onUnblock} className={pillClass("outline", "row")}>
          Unblock
        </button>
      </div>
    );
  }
  if (status === "received") {
    return (
      <div role="status" className="flex flex-col gap-2 border-t border-rs-divider px-4 py-2.5">
        <p className="text-rs-body text-rs-text">
          <strong>{name}</strong> wants to message you.
        </p>
        {error && (
          <p role="alert" className="text-rs-caption text-rs-danger">
            {error}
          </p>
        )}
        <div className="flex gap-2">
          <button type="button" onClick={onAccept} disabled={busy} className={pillClass("filled", "row")}>
            Accept
          </button>
          <button type="button" onClick={onDecline} disabled={busy} className={pillClass("outline", "row")}>
            Decline
          </button>
        </div>
      </div>
    );
  }
  const text = {
    waiting: `Waiting for ${name} to accept your message request.`,
    closed: "This conversation is closed.",
    readOnly: "You can't reply to this conversation.",
  }[status];
  return (
    <p role="status" className="border-t border-rs-divider px-4 py-2.5 text-rs-body text-rs-text-muted">
      {text}
    </p>
  );
}
