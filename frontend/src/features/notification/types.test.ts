import type { Notification } from "./api";
import { sentenceFor } from "./types";

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
