"use client";

import { Avatar } from "@/components/avatar";
import { useSession } from "@/features/auth/session";
import { useMyProfile } from "@/features/profile/my-profile";

import { homeCard } from "./cards";

// maxPostLength is the longest post FR-8.1 allows.
const maxPostLength = 5000;

const pill =
  "min-h-9 rounded-rs-pill border border-rs-control bg-rs-surface px-3 text-rs-caption font-medium text-rs-text disabled:cursor-not-allowed disabled:opacity-60";

// Composer is the "Share an update" card at the top of the feed. Posting (FR-8.1) is built
// by the feed slice; until then every control is disabled and a note says so.
export function Composer() {
  const session = useSession();
  const result = useMyProfile();
  const profile = result?.status === "ok" ? result.profile : null;
  const name = profile?.fullName ?? (session.status === "signedIn" ? session.user.fullName : "");

  return (
    <section aria-labelledby="composer-heading" className={`${homeCard} flex flex-col gap-2.5 p-4`}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 id="composer-heading" className="text-rs-body font-bold">
          Share an update
        </h2>
        <p id="composer-note" className="text-rs-caption text-rs-text-muted">
          Posting comes soon
        </p>
      </div>
      <div className="flex gap-2.5">
        <Avatar name={name} src={profile?.photoUrl ?? null} size="post" decorative />
        <label className="min-w-0 flex-1">
          <span className="sr-only">Post text</span>
          <textarea
            rows={3}
            disabled
            aria-describedby="composer-note"
            className="w-full resize-y rounded-rs-md border border-rs-control px-3 py-2.5 text-rs-body leading-[1.4] disabled:cursor-not-allowed disabled:bg-rs-subtle"
          />
        </label>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <button type="button" disabled className={pill}>
          Link publication
        </button>
        <button type="button" disabled className={pill}>
          Link project
        </button>
        <span className="ml-auto text-rs-caption text-rs-text-muted">
          0 / {maxPostLength.toLocaleString("en-US")}
        </span>
        <button
          type="button"
          disabled
          aria-describedby="composer-note"
          className="h-10 rounded-rs-pill bg-rs-accent-strong px-5 text-rs-body font-medium text-rs-surface disabled:cursor-not-allowed disabled:opacity-60"
        >
          Post
        </button>
      </div>
    </section>
  );
}
