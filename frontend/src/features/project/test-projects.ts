import type {
  MemberRole,
  MyInvitation,
  MyJoinRequest,
  PeopleRecommendations,
  Project,
  ProjectInvitation,
  ProjectJoinRequest,
  ProjectMember,
  ProjectPerson,
  ProjectRecommendation,
  ProjectRecommendations,
  ProjectSummary,
  RecommendedVenue,
  VenueRecommendations,
} from "./api";

// Test data for the project pages.

export const projectID = "7a1d2c3e-0000-4000-8000-000000000001";
export const ownerID = "7a1d2c3e-0000-4000-8000-0000000000aa";

export function project(extra: Partial<Project> = {}): Project {
  return {
    id: projectID,
    title: "Explainable CVS detection",
    description: "Detect Computer Vision Syndrome early from screen-use signals.",
    abstract: "We collect passive screen-use signals from 412 students.",
    status: "open",
    requiredSkills: [
      { id: "s1", name: "Python" },
      { id: "s2", name: "SHAP" },
    ],
    owner: { userId: ownerID, fullName: "Zinat Owner", headline: "MSc student · Explainable ML", photoUrl: null },
    myRole: "owner",
    memberCount: 1,
    createdAt: "2026-11-10T10:00:00Z",
    updatedAt: "2026-11-16T10:00:00Z",
    ...extra,
  };
}

export function summary(n: number, extra: Partial<ProjectSummary> = {}): ProjectSummary {
  return {
    id: `7a1d2c3e-0000-4000-8000-0000000001${String(n).padStart(2, "0")}`,
    title: `Project ${n}`,
    description: `Description ${n}`,
    status: "open",
    requiredSkills: ["Python"],
    owner: { userId: ownerID, fullName: "Zinat Owner", headline: null, photoUrl: null },
    myRole: "owner",
    createdAt: "2026-11-10T10:00:00Z",
    updatedAt: new Date().toISOString(),
    ...extra,
  };
}

export function recommendedProject(n: number, extra: Partial<ProjectRecommendation> = {}): ProjectRecommendation {
  return {
    project: {
      id: `7a1d2c3e-0000-4000-8000-0000000002${String(n).padStart(2, "0")}`,
      title: `Suggested project ${n}`,
      description: `About project ${n}`,
      requiredSkills: ["OpenCV"],
      owner: { userId: `7a1d2c3e-0000-4000-8000-0000000003${String(n).padStart(2, "0")}`, fullName: `Owner ${n}`, headline: null, photoUrl: null },
      memberCount: n,
    },
    score: 90 - n,
    shared: [],
    ...extra,
  };
}

export function projectRecs(items: ProjectRecommendation[]): { ok: true; data: ProjectRecommendations } {
  return { ok: true, data: { state: "ready", missing: [], items } };
}

export const projectsNeedProfile: { ok: true; data: ProjectRecommendations } = {
  ok: true,
  data: { state: "needs_profile", missing: ["interests"], items: [] },
};

export const projectsProcessing: { ok: true; data: ProjectRecommendations } = {
  ok: true,
  data: { state: "processing", missing: [], items: [] },
};

export const researchersProcessing: { ok: true; data: PeopleRecommendations } = {
  ok: true,
  data: { state: "processing", missing: [], items: [] },
};

export const failed = { ok: false as const, code: "internal_error", message: "Something went wrong on our side." };

// Membership test data (slice 20).

export function person(n: number, extra: Partial<ProjectPerson> = {}): ProjectPerson {
  return {
    userId: `7a1d2c3e-0000-4000-8000-0000000004${String(n).padStart(2, "0")}`,
    fullName: `Person ${n}`,
    headline: `Headline ${n}`,
    photoUrl: null,
    ...extra,
  };
}

export const ownerPerson: ProjectPerson = {
  userId: ownerID,
  fullName: "Zinat Owner",
  headline: "MSc student · Explainable ML",
  photoUrl: null,
};

export function member(user: ProjectPerson, role: ProjectMember["role"]): ProjectMember {
  return { user, role, joinedAt: "2026-11-12T10:00:00Z" };
}

export function joinRequest(n: number, extra: Partial<ProjectJoinRequest> = {}): ProjectJoinRequest {
  return {
    id: `7a1d2c3e-0000-4000-8000-0000000005${String(n).padStart(2, "0")}`,
    status: "pending",
    message: `I can help, says ${n}.`,
    user: person(n),
    createdAt: "2026-11-16T10:00:00Z",
    decidedAt: null,
    ...extra,
  };
}

export function invitation(n: number, role: MemberRole = "editor", extra: Partial<ProjectInvitation> = {}): ProjectInvitation {
  return {
    id: `7a1d2c3e-0000-4000-8000-0000000006${String(n).padStart(2, "0")}`,
    status: "pending",
    role,
    user: person(n),
    createdAt: "2026-11-16T10:00:00Z",
    decidedAt: null,
    ...extra,
  };
}

export function myInvitation(n: number, extra: Partial<MyInvitation> = {}): MyInvitation {
  return {
    id: `7a1d2c3e-0000-4000-8000-0000000007${String(n).padStart(2, "0")}`,
    role: "editor",
    project: { id: n === 0 ? projectID : `7a1d2c3e-0000-4000-8000-0000000008${String(n).padStart(2, "0")}`, title: `Invited project ${n}`, status: "open", owner: person(90 + n) },
    createdAt: "2026-11-16T10:00:00Z",
    ...extra,
  };
}

export function myRequest(n: number, extra: Partial<MyJoinRequest> = {}): MyJoinRequest {
  return {
    id: `7a1d2c3e-0000-4000-8000-0000000009${String(n).padStart(2, "0")}`,
    status: "pending",
    message: null,
    project: { id: n === 0 ? projectID : `7a1d2c3e-0000-4000-8000-0000000010${String(n).padStart(2, "0")}`, title: `Requested project ${n}`, status: "open", owner: person(80 + n) },
    createdAt: "2026-11-16T10:00:00Z",
    decidedAt: null,
    ...extra,
  };
}

// page wraps items as one page of a membership list.
export function page<T>(items: T[]) {
  return { ok: true as const, data: { items, page: 1, pageSize: 50, total: items.length } };
}

// venueNotice is the AI-6.4 sentence the API sends with every venue answer.
export const venueNotice = "Matched on topic only. This says nothing about a venue’s quality or ranking.";

// recommendedVenue is a conference whose deadline is 15 Feb 2027, 23:59:59 AoE (16 Feb 2027,
// 11:59:59 UTC), as in the events page's test data.
export function recommendedVenue(n: number, extra: Partial<RecommendedVenue> = {}): RecommendedVenue {
  return {
    id: `0192f0a0-0000-7000-8000-0000000003${String(n).padStart(2, "0")}`,
    name: `Conference on Digital Health ${n}`,
    shortName: `CDH${n}`,
    type: "conference",
    fields: [{ key: "health_informatics", label: "Health informatics" }],
    timeZone: "AoE",
    submissionDeadline: { local: "2027-02-15T23:59:59", at: "2027-02-16T11:59:59Z" },
    daysLeft: 15,
    officialUrl: `https://cdh${n}.example.org/cfp`,
    linkKind: "official",
    score: 90 - n,
    ...extra,
  };
}

// recommendedJournal is a rolling journal: no zone, deadline or days left.
export function recommendedJournal(n: number, extra: Partial<RecommendedVenue> = {}): RecommendedVenue {
  return recommendedVenue(n, {
    id: `0192f0a0-0000-7000-8000-0000000004${String(n).padStart(2, "0")}`,
    name: `Journal of Screen Health ${n}`,
    shortName: null,
    type: "journal",
    timeZone: null,
    submissionDeadline: null,
    daysLeft: null,
    linkKind: "issn_portal",
    score: 70 - n,
    ...extra,
  });
}

export function venueRecs(
  state: VenueRecommendations["state"],
  venues: RecommendedVenue[] = [],
  journals: RecommendedVenue[] = [],
): { ok: true; data: VenueRecommendations } {
  return { ok: true, data: { state, venues, journals, notice: venueNotice } };
}
