"use client";

import Link from "next/link";
import { useState } from "react";

import { Avatar } from "@/components/avatar";
import { rsButtonClass } from "@/components/ui/button";
import { useSession } from "@/features/auth/session";
import { homeCard } from "@/features/home/cards";

import {
  leaveProject,
  type MemberRole,
  membershipErrorText,
  type Project,
  type ProjectMember,
  removeProjectMember,
  updateProjectMember,
} from "../api";
import { RoleBadge } from "../badges";
import { ConfirmDialog } from "./confirm-dialog";
import { InviteDialog } from "./invite-dialog";
import { memberRoleLabels, memberRoles, rowSelectClass } from "./invite-button";
import { optimistic } from "./resource";
import type { Membership } from "./use-membership";

const inviteButtonId = "invite-member";
const leaveButtonId = "leave-project";

type Open = { kind: "invite" } | { kind: "leave" } | { kind: "remove"; member: ProjectMember } | null;

// MembersCard lists the members with their roles for everyone who can see the project (SRS
// 2.3): the owner first, then the others in the order they joined. The owner changes a role
// at once (FR-11.4), removes a member after asking, and invites from "+ Invite" while the
// project is open (FR-11.3). A member other than the owner may leave after asking (FR-11.5).
export function MembersCard({
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
  const session = useSession();
  const me = session.status === "signedIn" ? session.user.id : null;
  const { members } = membership;
  const [open, setOpen] = useState<Open>(null);
  const isOwner = project.myRole === "owner";
  const canLeave = project.myRole === "editor" || project.myRole === "viewer";

  const list: ProjectMember[] =
    members.value.status === "ok"
      ? members.value.data.items
      : [{ user: project.owner, role: "owner", joinedAt: project.createdAt }];

  const setRole = (userId: string, role: ProjectMember["role"]) =>
    members.set((page) => ({ ...page, items: page.items.map((m) => (m.user.userId === userId ? { ...m, role } : m)) }));

  async function changeRole(member: ProjectMember, role: MemberRole) {
    if (role === member.role) return;
    const before = member.role;
    const res = await optimistic({
      apply: () => setRole(member.user.userId, role),
      rollback: () => setRole(member.user.userId, before),
      send: () => updateProjectMember(project.id, member.user.userId, role),
      onError: (code) => {
        showToast(membershipErrorText(code, "We couldn’t change the role. Try again."));
        if (code === "not_found") members.reload();
      },
    });
    if (res.ok) showToast(`${member.user.fullName} is now ${role === "editor" ? "an editor" : "a viewer"}`);
  }

  return (
    <section aria-labelledby="members-h" className={homeCard}>
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-rs-divider px-4 py-3.5">
        <h2 id="members-h" className="text-rs-lead leading-rs-snug font-medium text-rs-text">
          Members ({project.memberCount})
        </h2>
        {isOwner && project.status === "open" && (
          <button
            id={inviteButtonId}
            type="button"
            onClick={() => setOpen({ kind: "invite" })}
            className={rsButtonClass({ size: "sm" })}
          >
            + Invite
          </button>
        )}
        {canLeave && (
          <button
            id={leaveButtonId}
            type="button"
            onClick={() => setOpen({ kind: "leave" })}
            className={rsButtonClass({ variant: "danger", size: "sm" })}
          >
            Leave project
          </button>
        )}
      </div>
      {members.value.status === "error" && (
        <p className="px-4 pt-3 text-rs-caption text-rs-text-muted">We couldn’t load every member.</p>
      )}
      <ul aria-label="Members" aria-busy={members.value.status === "loading"}>
        {list.map((m) => {
          const name = m.user.fullName;
          return (
            <li key={m.user.userId} className="flex flex-wrap items-center gap-3 border-b border-rs-divider px-4 py-3 last:border-b-0">
              <Avatar name={name} src={m.user.photoUrl} size="post" decorative />
              <span className="flex min-w-[180px] flex-1 flex-col">
                <Link href={`/profile/${m.user.userId}`} className="text-rs-body font-bold text-rs-text no-underline hover:underline">
                  {name}
                  {m.user.userId === me && " (you)"}
                </Link>
                {m.user.headline && <span className="text-rs-caption text-rs-text-muted">{m.user.headline}</span>}
              </span>
              {isOwner && m.role !== "owner" ? (
                <span className="flex items-center gap-1">
                  <select
                    aria-label={`Role of ${name}`}
                    value={m.role}
                    onChange={(e) => void changeRole(m, e.target.value as MemberRole)}
                    className={rowSelectClass}
                  >
                    {memberRoles.map((r) => (
                      <option key={r} value={r}>
                        {memberRoleLabels[r]}
                      </option>
                    ))}
                  </select>
                  <button
                    id={`remove-${m.user.userId}`}
                    type="button"
                    onClick={() => setOpen({ kind: "remove", member: m })}
                    aria-label={`Remove ${name}`}
                    className="h-[34px] rounded-rs-pill px-3 text-rs-caption font-medium text-rs-danger hover:bg-rs-danger-soft focus-visible:shadow-focus focus-visible:outline-none"
                  >
                    Remove
                  </button>
                </span>
              ) : (
                <RoleBadge role={m.role} />
              )}
            </li>
          );
        })}
      </ul>

      {open?.kind === "invite" && (
        <InviteDialog
          inviteState={membership.inviteState}
          onInvite={membership.invite}
          onClose={() => setOpen(null)}
          returnFocusId={inviteButtonId}
        />
      )}
      {open?.kind === "remove" && (
        <ConfirmDialog
          title={`Remove ${open.member.user.fullName}?`}
          action="Remove"
          destructive
          run={() => removeProjectMember(project.id, open.member.user.userId)}
          onDone={() => {
            const gone = open.member;
            setOpen(null);
            members.set((page) => ({
              ...page,
              items: page.items.filter((m) => m.user.userId !== gone.user.userId),
              total: page.total - 1,
            }));
            refreshProject();
            showToast(`${gone.user.fullName} was removed`);
          }}
          onClose={() => setOpen(null)}
          returnFocusId={inviteButtonId}
        >
          They lose access to the project and its workspace at once. You can invite them again later.
        </ConfirmDialog>
      )}
      {open?.kind === "leave" && (
        <ConfirmDialog
          title="Leave this project?"
          action="Leave project"
          destructive
          run={() => leaveProject(project.id)}
          onDone={() => {
            setOpen(null);
            showToast("You left the project");
            refreshProject();
          }}
          onClose={() => setOpen(null)}
          returnFocusId={leaveButtonId}
        >
          You lose access to the workspace and files. To come back you need to ask to join or be invited again.
        </ConfirmDialog>
      )}
    </section>
  );
}
