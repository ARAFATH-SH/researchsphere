"use client";

import Link from "next/link";

import { Avatar } from "@/components/avatar";
import { rsButtonClass } from "@/components/ui/button";
import { homeCard } from "@/features/home/cards";
import { relativeTime } from "@/features/notification/format";

import {
  acceptInvitation,
  cancelJoinRequest,
  type MyInvitation,
  type MyInvitationPage,
  type MyJoinRequest,
  type MyJoinRequestPage,
  membershipErrorText,
  rejectInvitation,
} from "../api";
import { optimistic, type ResourceHandle as Res } from "./resource";

const pageHeader = "flex flex-wrap items-center justify-between gap-2 border-b border-rs-divider px-4 py-3.5";
const headingClass = "text-rs-lead leading-rs-snug font-medium text-rs-text";

// without removes item from a page, and with puts it back where it was.
function without<T, P extends { items: T[]; total: number }>(page: P, item: T): P {
  return { ...page, items: page.items.filter((i) => i !== item), total: page.total - 1 };
}
function withItem<T, P extends { items: T[]; total: number }>(page: P, item: T, before: T[]): P {
  const items = [...page.items];
  const at = before.indexOf(item);
  items.splice(at < 0 ? items.length : Math.min(at, items.length), 0, item);
  return { ...page, items, total: page.total + 1 };
}

// MyInvitationsCard is the design's "Project invitations" card on /projects (FR-11.3): who
// invited the caller to which project and as what, with Decline and Accept. An answered
// invitation leaves the list at once and comes back if the server refuses. It shows only when
// there is an invitation.
export function MyInvitationsCard({
  invitations,
  onJoined,
  showToast,
}: {
  invitations: Res<MyInvitationPage>;
  onJoined: () => void;
  showToast: (message: string) => void;
}) {
  if (invitations.value.status !== "ok" || invitations.value.data.items.length === 0) return null;
  const items = invitations.value.data.items;

  async function answer(inv: MyInvitation, accept: boolean) {
    const res = await optimistic<unknown>({
      apply: () => invitations.set((page) => without(page, inv)),
      rollback: () => invitations.set((page) => withItem(page, inv, items)),
      send: () => (accept ? acceptInvitation(inv.project.id, inv.id) : rejectInvitation(inv.project.id, inv.id)),
      onError: (code) => {
        showToast(membershipErrorText(code, "We couldn’t answer the invitation. Try again."));
        if (code === "invitation_not_pending" || code === "not_found") invitations.reload();
      },
    });
    if (!res.ok) return;
    if (accept) {
      showToast(`You joined ${inv.project.title}`);
      onJoined();
    } else showToast("Invitation declined");
  }

  return (
    <section aria-labelledby="my-invitations-h" className={homeCard}>
      <div className={pageHeader}>
        <h2 id="my-invitations-h" className={headingClass}>
          Project invitations
        </h2>
      </div>
      <ul aria-label="Project invitations">
        {items.map((inv) => {
          const owner = inv.project.owner;
          return (
            <li key={inv.id} className="flex flex-wrap items-center gap-3 border-b border-rs-divider px-4 py-3.5 last:border-b-0">
              <Avatar name={owner.fullName} src={owner.photoUrl} size="post" decorative />
              <p className="min-w-[220px] flex-1 text-rs-body text-rs-text">
                <strong>{owner.fullName}</strong> invited you to{" "}
                <Link href={`/projects/${inv.project.id}`} className="font-bold text-rs-text hover:underline">
                  {inv.project.title}
                </Link>{" "}
                as <strong>{inv.role}</strong>
              </p>
              <span className="flex gap-2">
                <button
                  type="button"
                  onClick={() => void answer(inv, false)}
                  aria-label={`Decline the invitation to ${inv.project.title}`}
                  className={rsButtonClass({ variant: "secondary", size: "xs" })}
                >
                  Decline
                </button>
                <button
                  type="button"
                  onClick={() => void answer(inv, true)}
                  aria-label={`Accept the invitation to ${inv.project.title}`}
                  className={rsButtonClass({ size: "xs" })}
                >
                  Accept
                </button>
              </span>
            </li>
          );
        })}
      </ul>
    </section>
  );
}

const statusLook: Record<MyJoinRequest["status"], { text: string; className: string }> = {
  pending: { text: "Pending", className: "bg-rs-accent-soft text-rs-heading" },
  accepted: { text: "Accepted", className: "bg-rs-accent-strong text-rs-surface" },
  rejected: { text: "Declined", className: "border border-rs-control text-rs-text" },
};

// MyRequestsCard lists the caller's own join requests, newest first, with their status and
// Cancel on a pending one (FR-11.1). It shows only when there is a request.
export function MyRequestsCard({
  requests,
  showToast,
}: {
  requests: Res<MyJoinRequestPage>;
  showToast: (message: string) => void;
}) {
  if (requests.value.status !== "ok" || requests.value.data.items.length === 0) return null;
  const items = requests.value.data.items;

  async function cancel(req: MyJoinRequest) {
    const res = await optimistic({
      apply: () => requests.set((page) => without(page, req)),
      rollback: () => requests.set((page) => withItem(page, req, items)),
      send: () => cancelJoinRequest(req.project.id, req.id),
      onError: (code) => {
        showToast(membershipErrorText(code, "We couldn’t cancel the request. Try again."));
        if (code === "not_found") requests.reload();
      },
    });
    if (res.ok) showToast("Request cancelled");
  }

  return (
    <section aria-labelledby="my-requests-h" className={homeCard}>
      <div className={pageHeader}>
        <h2 id="my-requests-h" className={headingClass}>
          My requests
        </h2>
      </div>
      <ul aria-label="My requests">
        {items.map((req) => {
          const look = statusLook[req.status];
          return (
            <li key={req.id} className="flex flex-wrap items-center gap-3 border-b border-rs-divider px-4 py-3.5 last:border-b-0">
              <span className="flex min-w-[220px] flex-1 flex-col gap-0.5">
                <Link href={`/projects/${req.project.id}`} className="text-rs-body font-bold text-rs-text no-underline hover:underline">
                  {req.project.title}
                </Link>
                <span className="text-rs-caption text-rs-text-muted">
                  Owner: {req.project.owner.fullName} · sent {lower(relativeTime(req.createdAt))}
                </span>
              </span>
              <span className={`rounded-rs-pill px-2.5 py-0.5 text-rs-caption font-medium ${look.className}`}>{look.text}</span>
              {req.status === "pending" && (
                <button
                  type="button"
                  onClick={() => void cancel(req)}
                  aria-label={`Cancel the request to join ${req.project.title}`}
                  className={rsButtonClass({ variant: "secondary", size: "xs" })}
                >
                  Cancel
                </button>
              )}
            </li>
          );
        })}
      </ul>
    </section>
  );
}

function lower(text: string): string {
  return text.charAt(0).toLowerCase() + text.slice(1);
}
