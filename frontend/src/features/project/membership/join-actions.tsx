"use client";

import { useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { relativeTime } from "@/features/notification/format";

import {
  acceptInvitation,
  cancelJoinRequest,
  membershipErrorText,
  type MyInvitation,
  type MyJoinRequest,
  type Project,
  rejectInvitation,
} from "../api";
import { JoinRequestDialog } from "./join-request-dialog";
import { optimistic } from "./resource";
import type { Membership } from "./use-membership";

const requestButtonId = "request-to-join";

// JoinActions is what a non-member can do about joining (FR-11.1, FR-11.3), in the project
// summary's action row: answer an invitation (Accept / Decline), see and cancel a pending
// request, or ask to join an open project. A closed or archived project is "Not accepting
// members", unless the caller is invited, as an invitation can be accepted whatever the status.
export function JoinActions({
  project,
  membership,
  refreshProject,
  showToast,
}: {
  project: Project;
  membership: Membership;
  refreshProject: () => void;
  showToast: (message: string) => void;
}) {
  const { myRequest, myInvitation } = membership;
  const [asking, setAsking] = useState(false);
  const [accepting, setAccepting] = useState(false);

  if (myRequest.value.status === "loading" || myInvitation.value.status === "loading")
    return (
      <p role="status" className="text-rs-body text-rs-text-muted">
        Checking your membership…
      </p>
    );

  const invitation = myInvitation.value.status === "ok" ? myInvitation.value.data : null;
  const request = myRequest.value.status === "ok" ? myRequest.value.data : null;

  if (invitation) {
    async function accept(inv: MyInvitation) {
      setAccepting(true);
      const res = await acceptInvitation(project.id, inv.id);
      setAccepting(false);
      if (res.ok) {
        showToast("You joined the project");
        refreshProject();
      } else {
        showToast(membershipErrorText(res.code, "We couldn’t accept the invitation. Try again."));
        myInvitation.reload();
      }
    }
    async function decline(inv: MyInvitation) {
      const res = await optimistic({
        apply: () => myInvitation.set(() => null),
        rollback: () => myInvitation.set(() => inv),
        send: () => rejectInvitation(project.id, inv.id),
        onError: (code) => showToast(membershipErrorText(code, "We couldn’t decline the invitation. Try again.")),
      });
      if (res.ok) showToast("Invitation declined");
    }
    return (
      <div className="flex flex-wrap items-center gap-2.5">
        <p className="min-w-[200px] flex-1 text-rs-body text-rs-text">
          <strong>{invitation.project.owner.fullName}</strong> invited you to join as <strong>{invitation.role}</strong>.
        </p>
        <button
          type="button"
          disabled={accepting}
          onClick={() => void decline(invitation)}
          className={rsButtonClass({ variant: "secondary", size: "md" })}
        >
          Decline
        </button>
        <button
          type="button"
          disabled={accepting}
          onClick={() => void accept(invitation)}
          className={rsButtonClass({ size: "md" })}
        >
          Accept
        </button>
      </div>
    );
  }

  if (request) {
    async function cancel(req: MyJoinRequest) {
      const res = await optimistic({
        apply: () => myRequest.set(() => null),
        rollback: () => myRequest.set(() => req),
        send: () => cancelJoinRequest(project.id, req.id),
        onError: (code) => showToast(membershipErrorText(code, "We couldn’t cancel your request. Try again.")),
      });
      if (res.ok) showToast("Request cancelled");
    }
    return (
      <div className="flex flex-wrap items-center gap-2.5">
        <span className="rounded-rs-pill bg-rs-accent-soft px-3 py-1 text-rs-caption font-medium text-rs-heading">
          Request pending
        </span>
        <span className="text-rs-caption text-rs-text-muted">Sent {lower(relativeTime(request.createdAt))}</span>
        <button
          type="button"
          onClick={() => void cancel(request)}
          className={rsButtonClass({ variant: "secondary", size: "xs" })}
        >
          Cancel request
        </button>
      </div>
    );
  }

  if (project.status !== "open")
    return <p className="text-rs-body font-medium text-rs-text-muted">Not accepting members</p>;

  return (
    <>
      <button id={requestButtonId} type="button" onClick={() => setAsking(true)} className={rsButtonClass({ size: "md" })}>
        Request to join
      </button>
      {asking && (
        <JoinRequestDialog
          projectId={project.id}
          ownerName={project.owner.fullName}
          returnFocusId={requestButtonId}
          onClose={() => setAsking(false)}
          onSent={(sent) => {
            setAsking(false);
            myRequest.set(() => sent);
            showToast("Request sent");
          }}
          onConflict={(code) => {
            if (code === "project_not_open" || code === "already_member") refreshProject();
            else {
              myRequest.reload();
              myInvitation.reload();
            }
          }}
        />
      )}
    </>
  );
}

function lower(text: string): string {
  return text.charAt(0).toLowerCase() + text.slice(1);
}
