import { useCallback } from "react";

import { projectIdOf } from "@/features/notification/types";
import { useOnConnected, useRealtimeEvent } from "@/features/realtime/use-realtime";

import {
  createInvitation,
  findMyInvitation,
  findMyPendingRequest,
  listProjectInvitations,
  listProjectJoinRequests,
  listProjectMembers,
  type MemberRole,
  membershipErrorText,
  type Project,
  type ProjectInvitation,
  type ProjectPerson,
} from "../api";
import { optimistic, useResource } from "./resource";

// InviteState is where a researcher stands with the project, for the owner's Invite controls:
// a member, invited already, asking to join, or none of these.
export type InviteState = "member" | "invited" | "requested" | null;

// useMembership holds the membership parts of a project page: the members (everyone who can
// see the page), the join requests and invitations (the owner), and the caller's own pending
// request and invitation (a non-member). Each part loads and reloads on its own, so a change
// refetches only what it touches. A project notification for this project reloads the parts
// it changes, as does every (re)connect of the socket, since events are not stored.
export function useMembership({
  project,
  refreshProject,
  showToast,
}: {
  project: Project;
  // refreshProject reads the project again quietly (role, member count, status).
  refreshProject: () => void;
  showToast: (message: string) => void;
}) {
  const id = project.id;
  const isOwner = project.myRole === "owner";
  const isMember = project.myRole !== null;

  const members = useResource(id, () => listProjectMembers(id));
  const requests = useResource(isOwner ? `${id}:owner` : null, () => listProjectJoinRequests(id));
  const invitations = useResource(isOwner ? `${id}:owner` : null, () => listProjectInvitations(id));
  const myRequest = useResource(isMember ? null : `${id}:guest`, () => findMyPendingRequest(id));
  const myInvitation = useResource(isMember ? null : `${id}:guest`, () => findMyInvitation(id));

  const { reload: reloadMembers } = members;
  const { reload: reloadRequests } = requests;
  const { reload: reloadInvitations } = invitations;
  const { reload: reloadMyRequest } = myRequest;
  const { reload: reloadMyInvitation } = myInvitation;

  useRealtimeEvent((event) => {
    if (event.type !== "notification.created" || projectIdOf(event.data) !== id) return;
    switch (event.data.type) {
      case "project_join_request":
        reloadRequests();
        break;
      case "project_invitation_accepted":
      case "project_invitation_rejected":
        reloadInvitations();
        reloadMembers();
        refreshProject();
        break;
      case "project_join_request_rejected":
        reloadMyRequest();
        break;
      case "project_invitation":
        reloadMyInvitation();
        break;
      case "project_join_request_accepted":
      case "project_member_role_changed":
      case "project_member_removed":
      case "project_ownership_transferred":
        reloadMembers();
        refreshProject();
        break;
    }
  });

  useOnConnected(() => {
    refreshProject();
    reloadMembers();
    reloadRequests();
    reloadInvitations();
    reloadMyRequest();
    reloadMyInvitation();
  });

  const inviteState = useCallback(
    (userId: string): InviteState => {
      if (members.value.status === "ok" && members.value.data.items.some((m) => m.user.userId === userId)) return "member";
      if (
        invitations.value.status === "ok" &&
        invitations.value.data.items.some((i) => i.user.userId === userId && i.status === "pending")
      )
        return "invited";
      if (
        requests.value.status === "ok" &&
        requests.value.data.items.some((r) => r.user.userId === userId && r.status === "pending")
      )
        return "requested";
      return null;
    },
    [members.value, invitations.value, requests.value],
  );

  const { set: setInvitations } = invitations;

  // invite invites a researcher, showing the invitation at once and taking it back when the
  // server refuses.
  const invite = useCallback(
    async (user: ProjectPerson, role: MemberRole) => {
      const temp: ProjectInvitation = {
        id: `new-${user.userId}`,
        status: "pending",
        role,
        user,
        createdAt: new Date().toISOString(),
        decidedAt: null,
      };
      const res = await optimistic({
        apply: () => setInvitations((page) => ({ ...page, items: [temp, ...page.items], total: page.total + 1 })),
        rollback: () =>
          setInvitations((page) => ({ ...page, items: page.items.filter((i) => i !== temp), total: page.total - 1 })),
        send: () => createInvitation(id, user.userId, role),
        onError: (code) => showToast(membershipErrorText(code, "We couldn’t send the invitation. Try again.")),
      });
      if (res.ok) {
        setInvitations((page) => ({ ...page, items: page.items.map((i) => (i === temp ? res.data : i)) }));
        showToast(`Invited ${user.fullName} as ${role}`);
      }
      return res.ok;
    },
    [id, setInvitations, showToast],
  );

  return { members, requests, invitations, myRequest, myInvitation, inviteState, invite };
}

export type Membership = ReturnType<typeof useMembership>;
