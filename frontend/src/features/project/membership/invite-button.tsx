"use client";

import { useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { rsSelectClass } from "@/components/ui/field";
import { joinRequestsAnchor } from "@/features/notification/types";

import type { MemberRole, ProjectPerson } from "../api";
import type { InviteState } from "./use-membership";

export const memberRoles: MemberRole[] = ["editor", "viewer"];
export const memberRoleLabels: Record<MemberRole, string> = { editor: "Editor", viewer: "Viewer" };

// The compact role select of a row, 36px high as in the design.
export const rowSelectClass = `${rsSelectClass} h-9 w-auto text-rs-caption`;

const stateText: Record<Exclude<InviteState, null | "requested">, string> = {
  member: "Member",
  invited: "Invited",
};

// InviteControl is the owner's Invite in a researcher's row (FR-11.3): a role picker and
// Invite, or where the researcher stands when they cannot be invited: a member, invited, or
// asking to join (a link to the Join requests card, where the request is answered).
export function InviteControl({
  person,
  state,
  onInvite,
}: {
  person: ProjectPerson;
  state: InviteState;
  onInvite: (person: ProjectPerson, role: MemberRole) => Promise<boolean>;
}) {
  const [role, setRole] = useState<MemberRole>("editor");
  const [busy, setBusy] = useState(false);

  if (state === "requested")
    return (
      <a href={`#${joinRequestsAnchor}`} className="text-rs-caption font-medium text-rs-accent-strong hover:underline">
        Asked to join
      </a>
    );
  if (state)
    return (
      <span className="inline-block rounded-rs-pill bg-rs-accent-soft px-2.5 py-0.5 text-rs-caption font-medium text-rs-heading">
        {stateText[state]}
      </span>
    );

  async function invite() {
    setBusy(true);
    await onInvite(person, role);
    setBusy(false);
  }

  return (
    <span className="flex flex-wrap items-center gap-1.5">
      <select
        aria-label={`Role for ${person.fullName}`}
        value={role}
        disabled={busy}
        onChange={(e) => setRole(e.target.value as MemberRole)}
        className={rowSelectClass}
      >
        {memberRoles.map((r) => (
          <option key={r} value={r}>
            {memberRoleLabels[r]}
          </option>
        ))}
      </select>
      <button
        type="button"
        disabled={busy}
        onClick={() => void invite()}
        aria-label={`Invite ${person.fullName}`}
        className={rsButtonClass({ variant: "outline", size: "xs" })}
      >
        Invite
      </button>
    </span>
  );
}
