"use client";

import Link from "next/link";
import { useState } from "react";

import { Avatar } from "@/components/avatar";
import { rsButtonClass } from "@/components/ui/button";
import { homeCard } from "@/features/home/cards";
import { joinRequestsAnchor } from "@/features/notification/types";

import {
  acceptJoinRequest,
  cancelInvitation,
  type MemberRole,
  membershipErrorText,
  type ProjectInvitation,
  type ProjectJoinRequest,
  type ProjectMember,
  rejectJoinRequest,
} from "../api";
import { memberRoleLabels, memberRoles, rowSelectClass } from "./invite-button";
import { optimistic } from "./resource";
import type { Membership } from "./use-membership";

const decidedText = { accepted: "Accepted", rejected: "Declined", pending: "Pending" } as const;

// JoinRequestsCard is the owner's Join requests card (FR-11.2, design/screens/ProjectDetail.html):
// pending requests first, each with the sender, their message, a role picker, Reject and Accept;
// decided ones after them with the outcome. Accepting adds the sender to the members at once.
// It is the anchor a join request notification leads to.
export function JoinRequestsCard({
  projectId,
  membership,
  refreshProject,
  showToast,
}: {
  projectId: string;
  membership: Membership;
  refreshProject: () => void;
  showToast: (message: string) => void;
}) {
  const { requests, members } = membership;
  if (requests.value.status !== "ok" || requests.value.data.items.length === 0) return null;
  const items = requests.value.data.items;
  const pending = items.filter((r) => r.status === "pending").length;

  const setStatus = (id: string, status: ProjectJoinRequest["status"]) =>
    requests.set((page) => ({ ...page, items: page.items.map((r) => (r.id === id ? { ...r, status } : r)) }));

  async function accept(request: ProjectJoinRequest, role: MemberRole) {
    const temp: ProjectMember = { user: request.user, role, joinedAt: new Date().toISOString() };
    const res = await optimistic({
      apply: () => {
        setStatus(request.id, "accepted");
        members.set((page) => ({ ...page, items: [...page.items, temp], total: page.total + 1 }));
      },
      rollback: () => {
        setStatus(request.id, "pending");
        members.set((page) => ({ ...page, items: page.items.filter((m) => m !== temp), total: page.total - 1 }));
      },
      send: () => acceptJoinRequest(projectId, request.id, role),
      onError: (code) => {
        showToast(membershipErrorText(code, "We couldn’t accept the request. Try again."));
        if (code === "request_not_pending" || code === "not_found") requests.reload();
      },
    });
    if (res.ok) {
      members.set((page) => ({ ...page, items: page.items.map((m) => (m === temp ? res.data : m)) }));
      refreshProject();
      showToast(`${request.user.fullName} joined as ${role}`);
    }
  }

  async function reject(request: ProjectJoinRequest) {
    const res = await optimistic({
      apply: () => setStatus(request.id, "rejected"),
      rollback: () => setStatus(request.id, "pending"),
      send: () => rejectJoinRequest(projectId, request.id),
      onError: (code) => {
        showToast(membershipErrorText(code, "We couldn’t reject the request. Try again."));
        if (code === "request_not_pending" || code === "not_found") requests.reload();
      },
    });
    if (res.ok) showToast("Request declined");
  }

  return (
    <section id={joinRequestsAnchor} aria-labelledby="join-requests-h" className={`${homeCard} scroll-mt-20`}>
      <div className="border-b border-rs-divider px-4 py-3.5">
        <h2 id="join-requests-h" className="text-rs-lead leading-rs-snug font-medium text-rs-text">
          Join requests ({pending})
        </h2>
      </div>
      <ul aria-label="Join requests">
        {items.map((r) => (
          <li key={r.id} className="border-b border-rs-divider last:border-b-0">
            <RequestRow request={r} onAccept={(role) => void accept(r, role)} onReject={() => void reject(r)} />
          </li>
        ))}
      </ul>
    </section>
  );
}

function RequestRow({
  request,
  onAccept,
  onReject,
}: {
  request: ProjectJoinRequest;
  onAccept: (role: MemberRole) => void;
  onReject: () => void;
}) {
  const [role, setRole] = useState<MemberRole>("editor");
  const { user } = request;
  return (
    <div className="flex flex-wrap items-start gap-3 px-4 py-3.5">
      <Avatar name={user.fullName} src={user.photoUrl} size="list" decorative />
      <div className="flex min-w-[220px] flex-1 flex-col gap-0.5">
        <Link href={`/profile/${user.userId}`} className="text-rs-body font-bold text-rs-text no-underline hover:underline">
          {user.fullName}
        </Link>
        {user.headline && <span className="text-rs-caption text-rs-text-muted">{user.headline}</span>}
        {request.message && (
          <q className="mt-1 rounded-rs-xs bg-rs-subtle px-2.5 py-1.5 text-rs-body text-rs-text">{request.message}</q>
        )}
      </div>
      {request.status === "pending" ? (
        <div className="flex flex-wrap items-center gap-1.5">
          <label className="flex items-center gap-1.5 text-rs-caption font-medium text-rs-text">
            Role
            <select value={role} onChange={(e) => setRole(e.target.value as MemberRole)} className={rowSelectClass}>
              {memberRoles.map((r) => (
                <option key={r} value={r}>
                  {memberRoleLabels[r]}
                </option>
              ))}
            </select>
          </label>
          <button
            type="button"
            onClick={onReject}
            aria-label={`Reject ${user.fullName}`}
            className={rsButtonClass({ variant: "secondary", size: "xs" })}
          >
            Reject
          </button>
          <button
            type="button"
            onClick={() => onAccept(role)}
            aria-label={`Accept ${user.fullName}`}
            className={rsButtonClass({ size: "xs" })}
          >
            Accept
          </button>
        </div>
      ) : (
        <span className="text-rs-caption font-medium text-rs-text-muted">{decidedText[request.status]}</span>
      )}
    </div>
  );
}

// InvitationsCard is the owner's list of invitations (FR-11.3), pending first, with Cancel on
// the pending ones. It shows only when there is one.
export function InvitationsCard({
  projectId,
  membership,
  showToast,
}: {
  projectId: string;
  membership: Membership;
  showToast: (message: string) => void;
}) {
  const { invitations } = membership;
  if (invitations.value.status !== "ok" || invitations.value.data.items.length === 0) return null;
  const items = invitations.value.data.items;

  async function cancel(inv: ProjectInvitation) {
    const res = await optimistic({
      apply: () =>
        invitations.set((page) => ({ ...page, items: page.items.filter((i) => i.id !== inv.id), total: page.total - 1 })),
      rollback: () =>
        invitations.set((page) => ({ ...page, items: restore(page.items, items, inv), total: page.total + 1 })),
      send: () => cancelInvitation(projectId, inv.id),
      onError: (code) => {
        showToast(membershipErrorText(code, "We couldn’t cancel the invitation. Try again."));
        if (code === "invitation_not_pending" || code === "not_found") invitations.reload();
      },
    });
    if (res.ok) showToast("Invitation cancelled");
  }

  return (
    <section aria-labelledby="invitations-h" className={homeCard}>
      <div className="border-b border-rs-divider px-4 py-3.5">
        <h2 id="invitations-h" className="text-rs-lead leading-rs-snug font-medium text-rs-text">
          Invitations
        </h2>
      </div>
      <ul aria-label="Invitations">
        {items.map((inv) => (
          <li key={inv.id} className="flex flex-wrap items-center gap-3 border-b border-rs-divider px-4 py-3 last:border-b-0">
            <Avatar name={inv.user.fullName} src={inv.user.photoUrl} size="post" decorative />
            <span className="flex min-w-[180px] flex-1 flex-col">
              <Link
                href={`/profile/${inv.user.userId}`}
                className="text-rs-body font-bold text-rs-text no-underline hover:underline"
              >
                {inv.user.fullName}
              </Link>
              <span className="text-rs-caption text-rs-text-muted">
                As {inv.role} · {decidedText[inv.status]}
              </span>
            </span>
            {inv.status === "pending" && !inv.id.startsWith("new-") && (
              <button
                type="button"
                onClick={() => void cancel(inv)}
                aria-label={`Cancel the invitation to ${inv.user.fullName}`}
                className={rsButtonClass({ variant: "secondary", size: "xs" })}
              >
                Cancel
              </button>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}

// restore puts item back in its old place among items, by the order of before.
function restore<T>(items: T[], before: T[], item: T): T[] {
  const index = before.indexOf(item);
  const out = [...items];
  out.splice(index < 0 ? out.length : Math.min(index, out.length), 0, item);
  return out;
}
