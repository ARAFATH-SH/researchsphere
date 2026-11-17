import type { Notification } from "./api";
import { linkFor, projectIdOf, sentenceFor } from "./types";

const projectId = "4c2f7d3e-0000-4000-8000-0000000000b1";

function projectNotice(type: Notification["type"], withSubject = true): Notification {
  return {
    id: "n-1",
    type,
    createdAt: "2026-11-17T10:00:00Z",
    read: false,
    link: `/projects/${projectId}`,
    actor: { userId: "4c2f7d3e-0000-4000-8000-0000000000c1", fullName: "Nafisa Hasan", photoUrl: null },
    subject: withSubject ? { type: "project", id: projectId, title: "Flood maps" } : undefined,
  };
}

describe("project notification sentences (FR-9.2)", () => {
  it.each([
    ["project_join_request", "Nafisa Hasan asked to join Flood maps."],
    ["project_join_request_accepted", "Nafisa Hasan accepted your request to join Flood maps."],
    ["project_join_request_rejected", "Nafisa Hasan declined your request to join Flood maps."],
    ["project_invitation", "Nafisa Hasan invited you to join Flood maps."],
    ["project_invitation_accepted", "Nafisa Hasan accepted your invitation to Flood maps."],
    ["project_invitation_rejected", "Nafisa Hasan declined your invitation to Flood maps."],
    ["project_member_role_changed", "Nafisa Hasan changed your role in Flood maps."],
    ["project_member_removed", "Nafisa Hasan removed you from Flood maps."],
    ["project_ownership_transferred", "Nafisa Hasan made you the owner of Flood maps."],
  ] as const)("%s names the project", (type, sentence) => {
    expect(sentenceFor(projectNotice(type))).toBe(sentence);
  });

  it("says a project when the recipient cannot see it", () => {
    expect(sentenceFor(projectNotice("project_member_removed", false))).toBe("Nafisa Hasan removed you from a project.");
  });
});

describe("project notification links", () => {
  it("takes the owner to the Join requests card for a join request", () => {
    expect(linkFor(projectNotice("project_join_request"))).toBe(`/projects/${projectId}#join-requests`);
  });

  it.each([
    "project_join_request_accepted",
    "project_join_request_rejected",
    "project_invitation",
    "project_invitation_accepted",
    "project_invitation_rejected",
    "project_member_role_changed",
    "project_member_removed",
    "project_ownership_transferred",
  ] as const)("opens the project page for %s", (type) => {
    expect(linkFor(projectNotice(type))).toBe(`/projects/${projectId}`);
  });

  it("finds the project from the subject, or from the link when the subject is hidden", () => {
    expect(projectIdOf(projectNotice("project_invitation"))).toBe(projectId);
    expect(projectIdOf(projectNotice("project_member_removed", false))).toBe(projectId);
    expect(projectIdOf({ ...projectNotice("project_invitation"), type: "group_added" })).toBeNull();
  });
});
