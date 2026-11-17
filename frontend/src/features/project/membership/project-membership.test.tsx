import "@/test/dialog-polyfill";

import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { useSession } from "@/features/auth/session";
import type { Notification } from "@/features/notification/api";
import { people, ready } from "@/features/recommendation/test-recommendations";
import { type PersonSearchResult, searchPeople } from "@/features/search/api";

import {
  acceptInvitation,
  acceptJoinRequest,
  cancelInvitation,
  cancelJoinRequest,
  createInvitation,
  createJoinRequest,
  findMyInvitation,
  findMyPendingRequest,
  getProject,
  getRecommendedResearchers,
  leaveProject,
  listProjectInvitations,
  listProjectJoinRequests,
  listProjectMembers,
  rejectInvitation,
  rejectJoinRequest,
  removeProjectMember,
  transferProject,
  updateProjectMember,
} from "../api";
import { ProjectPage } from "../project-page";
import {
  invitation,
  joinRequest,
  member,
  myInvitation,
  myRequest,
  ownerPerson,
  page,
  person,
  project,
  projectID,
} from "../test-projects";
import { searchDelay } from "./invite-dialog";

jest.mock("../api", () => ({
  ...jest.requireActual("../api"),
  getProject: jest.fn(),
  getRecommendedResearchers: jest.fn(),
  listProjectMembers: jest.fn(),
  listProjectJoinRequests: jest.fn(),
  listProjectInvitations: jest.fn(),
  findMyPendingRequest: jest.fn(),
  findMyInvitation: jest.fn(),
  createJoinRequest: jest.fn(),
  cancelJoinRequest: jest.fn(),
  acceptJoinRequest: jest.fn(),
  rejectJoinRequest: jest.fn(),
  createInvitation: jest.fn(),
  cancelInvitation: jest.fn(),
  acceptInvitation: jest.fn(),
  rejectInvitation: jest.fn(),
  updateProjectMember: jest.fn(),
  removeProjectMember: jest.fn(),
  leaveProject: jest.fn(),
  transferProject: jest.fn(),
}));
jest.mock("@/features/search/api");
jest.mock("@/features/profile/api");
jest.mock("@/features/social/api");
jest.mock("@/features/notification/badges");
jest.mock("@/features/auth/session");
jest.mock("next/navigation", () => ({ useRouter: () => ({ push: jest.fn() }) }));
const mockRealtime: { handler: ((event: unknown) => void) | null; connected: (() => void) | null } = {
  handler: null,
  connected: null,
};
jest.mock("@/features/realtime/use-realtime", () => ({
  useRealtimeEvent: (handler: (event: unknown) => void) => {
    mockRealtime.handler = handler;
  },
  useOnConnected: (handler: () => void) => {
    mockRealtime.connected = handler;
  },
}));

const mockGet = jest.mocked(getProject);
const mockMembers = jest.mocked(listProjectMembers);
const mockRequests = jest.mocked(listProjectJoinRequests);
const mockInvitations = jest.mocked(listProjectInvitations);
const mockMyRequest = jest.mocked(findMyPendingRequest);
const mockMyInvitation = jest.mocked(findMyInvitation);
const refused = (code: string) => ({ ok: false as const, code, message: "x" });
const done = { ok: true as const, data: undefined };

let consoleError: jest.SpyInstance;

beforeEach(() => {
  consoleError = jest.spyOn(console, "error");
  jest.mocked(useSession).mockReturnValue({
    status: "signedIn",
    user: { id: ownerPerson.userId, email: "me@example.org", fullName: "Zinat Owner", role: "researcher" },
  });
  jest.mocked(getRecommendedResearchers).mockResolvedValue(ready(people(1, 2, 3, 4)));
  mockMembers.mockResolvedValue(page([member(ownerPerson, "owner")]));
  mockRequests.mockResolvedValue(page([]));
  mockInvitations.mockResolvedValue(page([]));
  mockMyRequest.mockResolvedValue({ ok: true, data: null });
  mockMyInvitation.mockResolvedValue({ ok: true, data: null });
});
afterEach(() => {
  // No act() or other React warnings.
  expect(consoleError).not.toHaveBeenCalled();
  consoleError.mockRestore();
  jest.useRealTimers();
  jest.resetAllMocks();
  mockRealtime.handler = null;
  mockRealtime.connected = null;
});

async function renderProject(p = project()) {
  mockGet.mockResolvedValue({ ok: true, data: p });
  render(<ProjectPage projectId={p.id} />);
  await screen.findByRole("heading", { level: 1, name: p.title });
  await screen.findByRole("list", { name: "Members", busy: false });
}

const guest = (extra = {}) => project({ myRole: null, abstract: null, ...extra });

function toast() {
  return screen.getAllByRole("status").map((s) => s.textContent);
}

async function click(el: HTMLElement) {
  await act(async () => {
    fireEvent.click(el);
  });
}

describe("a non-member asks to join (FR-11.1)", () => {
  it("sends a request with a message, then shows it pending", async () => {
    await renderProject(guest());
    fireEvent.click(await screen.findByRole("button", { name: "Request to join" }));
    const dialog = screen.getByRole("dialog", { name: "Request to join" });
    const box = within(dialog).getByLabelText(/Message to Zinat Owner/);
    fireEvent.change(box, { target: { value: "  I can help with the sensors. " } });
    expect(within(dialog).getByText("28 / 500")).toBeTruthy();

    jest.mocked(createJoinRequest).mockResolvedValue({ ok: true, data: myRequest(0) });
    await click(within(dialog).getByRole("button", { name: "Send request" }));
    expect(createJoinRequest).toHaveBeenCalledWith(projectID, "  I can help with the sensors. ");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByText("Request pending")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Request to join" })).toBeNull();
    expect(toast()).toContain("Request sent");
  });

  it("refuses a message over 500 characters", async () => {
    await renderProject(guest());
    fireEvent.click(await screen.findByRole("button", { name: "Request to join" }));
    const dialog = screen.getByRole("dialog", { name: "Request to join" });
    fireEvent.change(within(dialog).getByLabelText(/Message to/), { target: { value: "a".repeat(501) } });
    expect(within(dialog).getByText("501 / 500")).toBeTruthy();
    expect(within(dialog).getByText("Keep the message to 500 characters.")).toBeTruthy();
    expect(within(dialog).getByRole("button", { name: "Send request" })).toHaveProperty("disabled", true);
  });

  it("reads again when a request is already pending", async () => {
    await renderProject(guest());
    fireEvent.click(await screen.findByRole("button", { name: "Request to join" }));
    const dialog = screen.getByRole("dialog", { name: "Request to join" });
    jest.mocked(createJoinRequest).mockResolvedValue(refused("request_pending"));
    mockMyRequest.mockResolvedValue({ ok: true, data: myRequest(0) });
    await click(within(dialog).getByRole("button", { name: "Send request" }));
    expect(within(dialog).getByText("There is already a pending join request.")).toBeTruthy();
    expect(mockMyRequest).toHaveBeenCalledTimes(2);
  });

  it("shows a pending request and cancels it", async () => {
    mockMyRequest.mockResolvedValue({ ok: true, data: myRequest(0) });
    await renderProject(guest());
    expect(await screen.findByText("Request pending")).toBeTruthy();
    jest.mocked(cancelJoinRequest).mockResolvedValue(done);
    await click(screen.getByRole("button", { name: "Cancel request" }));
    expect(cancelJoinRequest).toHaveBeenCalledWith(projectID, myRequest(0).id);
    expect(screen.getByRole("button", { name: "Request to join" })).toBeTruthy();
    expect(toast()).toContain("Request cancelled");
  });

  it("puts the pending request back when cancelling fails", async () => {
    mockMyRequest.mockResolvedValue({ ok: true, data: myRequest(0) });
    await renderProject(guest());
    jest.mocked(cancelJoinRequest).mockResolvedValue(refused("internal_error"));
    await click(await screen.findByRole("button", { name: "Cancel request" }));
    expect(screen.getByText("Request pending")).toBeTruthy();
    expect(toast()).toContain("We couldn’t cancel your request. Try again.");
  });

  it.each(["closed", "archived"] as const)("says a %s project is not accepting members", async (status) => {
    await renderProject(guest({ status }));
    expect(await screen.findByText("Not accepting members")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Request to join" })).toBeNull();
    expect(screen.getByRole("button", { name: "Report project" })).toBeTruthy();
  });
});

describe("a non-member answers an invitation (FR-11.3)", () => {
  it("accepts and becomes a member", async () => {
    mockMyInvitation.mockResolvedValue({ ok: true, data: myInvitation(0) });
    await renderProject(guest({ status: "closed" }));
    expect(await screen.findByText(/invited you to join as/)).toBeTruthy();
    expect(screen.queryByText("Not accepting members")).toBeNull();
    jest.mocked(acceptInvitation).mockResolvedValue({ ok: true, data: member(ownerPerson, "editor") });
    mockGet.mockResolvedValue({ ok: true, data: project({ myRole: "editor", status: "closed" }) });
    await click(screen.getByRole("button", { name: "Accept" }));
    expect(acceptInvitation).toHaveBeenCalledWith(projectID, myInvitation(0).id);
    expect(await screen.findByText("You are an editor")).toBeTruthy();
    expect(toast()).toContain("You joined the project");
  });

  it("declines, and can then ask to join", async () => {
    mockMyInvitation.mockResolvedValue({ ok: true, data: myInvitation(0) });
    await renderProject(guest());
    jest.mocked(rejectInvitation).mockResolvedValue(done);
    await click(await screen.findByRole("button", { name: "Decline" }));
    expect(rejectInvitation).toHaveBeenCalledWith(projectID, myInvitation(0).id);
    expect(screen.queryByText(/invited you/)).toBeNull();
    expect(screen.getByRole("button", { name: "Request to join" })).toBeTruthy();
  });

  it("puts the invitation back when declining fails", async () => {
    mockMyInvitation.mockResolvedValue({ ok: true, data: myInvitation(0) });
    await renderProject(guest());
    jest.mocked(rejectInvitation).mockResolvedValue(refused("network_error"));
    await click(await screen.findByRole("button", { name: "Decline" }));
    expect(screen.getByText(/invited you to join as/)).toBeTruthy();
    expect(toast().join(" ")).toMatch(/Could not reach the server/);
  });
});

describe("the owner decides join requests (FR-11.2)", () => {
  it("lists pending requests with their message and accepts one as viewer", async () => {
    mockRequests.mockResolvedValue(page([joinRequest(1), joinRequest(2, { status: "rejected" })]));
    await renderProject();
    const card = await screen.findByRole("region", { name: "Join requests (1)" });
    expect(card.id).toBe("join-requests");
    expect(within(card).getByText("I can help, says 1.")).toBeTruthy();
    expect(within(card).getByText("Declined")).toBeTruthy();

    fireEvent.change(within(card).getByRole("combobox", { name: "Role" }), { target: { value: "viewer" } });
    jest.mocked(acceptJoinRequest).mockResolvedValue({ ok: true, data: member(person(1), "viewer") });
    mockGet.mockResolvedValue({ ok: true, data: project({ memberCount: 2 }) });
    await click(within(card).getByRole("button", { name: "Accept Person 1" }));
    expect(acceptJoinRequest).toHaveBeenCalledWith(projectID, joinRequest(1).id, "viewer");
    expect(await screen.findByRole("region", { name: "Members (2)" })).toBeTruthy();
    expect(screen.getByRole("combobox", { name: "Role of Person 1" })).toHaveProperty("value", "viewer");
    expect(screen.getByRole("region", { name: "Join requests (0)" })).toBeTruthy();
    expect(toast()).toContain("Person 1 joined as viewer");
  });

  it("rolls the accept back when the server refuses", async () => {
    mockRequests.mockResolvedValue(page([joinRequest(1)]));
    await renderProject();
    jest.mocked(acceptJoinRequest).mockResolvedValue(refused("internal_error"));
    await click(await screen.findByRole("button", { name: "Accept Person 1" }));
    expect(screen.queryByRole("combobox", { name: "Role of Person 1" })).toBeNull();
    expect(screen.getByRole("button", { name: "Accept Person 1" })).toBeTruthy();
    expect(toast()).toContain("We couldn’t accept the request. Try again.");
  });

  it("rejects a request", async () => {
    mockRequests.mockResolvedValue(page([joinRequest(1)]));
    await renderProject();
    jest.mocked(rejectJoinRequest).mockResolvedValue(done);
    await click(await screen.findByRole("button", { name: "Reject Person 1" }));
    expect(rejectJoinRequest).toHaveBeenCalledWith(projectID, joinRequest(1).id);
    const card = screen.getByRole("region", { name: "Join requests (0)" });
    expect(within(card).getByText("Declined")).toBeTruthy();
    expect(toast()).toContain("Request declined");
  });
});

describe("the owner invites (FR-11.3)", () => {
  it("invites a recommended researcher with a role", async () => {
    await renderProject();
    const card = await screen.findByRole("region", { name: "Researchers who fit this project" });
    const list = await within(card).findByRole("list", { name: "Recommended researchers" });
    fireEvent.change(within(list).getByRole("combobox", { name: "Role for Researcher 1" }), { target: { value: "viewer" } });
    const rec = people(1)[0];
    jest.mocked(createInvitation).mockResolvedValue({
      ok: true,
      data: invitation(1, "viewer", { user: { userId: rec.userId, fullName: rec.fullName, headline: rec.headline, photoUrl: null } }),
    });
    await click(within(list).getByRole("button", { name: "Invite Researcher 1" }));
    expect(createInvitation).toHaveBeenCalledWith(projectID, rec.userId, "viewer");
    expect(within(list).getByText("Invited")).toBeTruthy();
    const invitations = screen.getByRole("region", { name: "Invitations" });
    expect(within(invitations).getByText("As viewer · Pending")).toBeTruthy();
    expect(toast()).toContain("Invited Researcher 1 as viewer");
  });

  it("takes the invitation back when the server refuses", async () => {
    await renderProject();
    const list = await screen.findByRole("list", { name: "Recommended researchers" });
    jest.mocked(createInvitation).mockResolvedValue(refused("already_invited"));
    await click(within(list).getByRole("button", { name: "Invite Researcher 1" }));
    expect(within(list).getByRole("button", { name: "Invite Researcher 1" })).toBeTruthy();
    expect(screen.queryByRole("region", { name: "Invitations" })).toBeNull();
    expect(toast()).toContain("They are already invited.");
  });

  it("shows invited and requesting researchers among the recommendations", async () => {
    const [r1, r2] = people(1, 2);
    mockInvitations.mockResolvedValue(page([invitation(1, "editor", { user: { ...person(1), userId: r1.userId } })]));
    mockRequests.mockResolvedValue(page([joinRequest(2, { user: { ...person(2), userId: r2.userId } })]));
    await renderProject();
    const list = await screen.findByRole("list", { name: "Recommended researchers" });
    expect(within(list).getByText("Invited")).toBeTruthy();
    expect(within(list).getByRole("link", { name: "Asked to join" }).getAttribute("href")).toBe("#join-requests");
    expect(within(list).getByRole("button", { name: "Invite Researcher 3" })).toBeTruthy();
  });

  it("searches people from + Invite, once the typing stops, and shows who cannot be invited", async () => {
    const free: PersonSearchResult = {
      userId: person(7).userId,
      fullName: "Free Person",
      headline: null,
      institution: "BUET",
      photoUrl: null,
      interests: [],
      matchedOn: "name",
      connectionStatus: "none",
      connectionRequestId: null,
    };
    const owner = { ...free, userId: ownerPerson.userId, fullName: "Zinat Owner" };
    const invited = { ...free, userId: person(8).userId, fullName: "Invited Person" };
    mockInvitations.mockResolvedValue(page([invitation(8, "editor", { user: person(8) })]));
    jest.mocked(searchPeople).mockResolvedValue({
      ok: true,
      data: {
        mode: "keyword",
        items: [owner, invited, free],
        total: 3,
        page: 1,
        pageSize: 20,
        fuzzy: false,
        correctedQuery: null,
        facets: { institutions: [], interests: [] },
      },
    });
    await renderProject();
    await screen.findByRole("list", { name: "Recommended researchers" });
    jest.useFakeTimers();
    fireEvent.click(screen.getByRole("button", { name: "+ Invite" }));
    const dialog = screen.getByRole("dialog", { name: "Invite to this project" });
    expect(within(dialog).getAllByRole("listitem")).toHaveLength(3);
    const box = within(dialog).getByLabelText("Search researchers");
    fireEvent.change(box, { target: { value: "Pe" } });
    fireEvent.change(box, { target: { value: "Per" } });
    await act(async () => {
      jest.advanceTimersByTime(searchDelay);
    });
    expect(searchPeople).toHaveBeenCalledTimes(1);
    expect(searchPeople).toHaveBeenCalledWith({ q: "Per", institution: "", interests: [], page: 1, keywordOnly: true });
    const results = within(dialog).getByRole("list", { name: "Search results" });
    expect(within(results).getByText("Member")).toBeTruthy();
    expect(within(results).getByText("Invited")).toBeTruthy();
    expect(within(results).getByRole("button", { name: "Invite Free Person" })).toBeTruthy();

    jest.mocked(createInvitation).mockResolvedValue({ ok: true, data: invitation(7, "editor", { user: person(7) }) });
    await click(within(results).getByRole("button", { name: "Invite Free Person" }));
    expect(createInvitation).toHaveBeenCalledWith(projectID, free.userId, "editor");
    expect(within(results).getAllByText("Invited")).toHaveLength(2);
  });

  it("cancels a pending invitation", async () => {
    mockInvitations.mockResolvedValue(page([invitation(1)]));
    await renderProject();
    jest.mocked(cancelInvitation).mockResolvedValue(done);
    await click(await screen.findByRole("button", { name: "Cancel the invitation to Person 1" }));
    expect(cancelInvitation).toHaveBeenCalledWith(projectID, invitation(1).id);
    expect(screen.queryByRole("region", { name: "Invitations" })).toBeNull();
  });

  it("puts a cancelled invitation back when the server refuses", async () => {
    mockInvitations.mockResolvedValue(page([invitation(1)]));
    await renderProject();
    jest.mocked(cancelInvitation).mockResolvedValue(refused("internal_error"));
    await click(await screen.findByRole("button", { name: "Cancel the invitation to Person 1" }));
    expect(screen.getByRole("button", { name: "Cancel the invitation to Person 1" })).toBeTruthy();
    expect(toast()).toContain("We couldn’t cancel the invitation. Try again.");
  });
});

describe("members (FR-11.4, FR-11.5, FR-10.5)", () => {
  const team = () => page([member(ownerPerson, "owner"), member(person(1), "editor")]);

  it("changes a role at once", async () => {
    mockMembers.mockResolvedValue(team());
    await renderProject(project({ memberCount: 2 }));
    jest.mocked(updateProjectMember).mockResolvedValue({ ok: true, data: member(person(1), "viewer") });
    const select = screen.getByRole("combobox", { name: "Role of Person 1" });
    await act(async () => {
      fireEvent.change(select, { target: { value: "viewer" } });
    });
    expect(updateProjectMember).toHaveBeenCalledWith(projectID, person(1).userId, "viewer");
    expect(select).toHaveProperty("value", "viewer");
    expect(toast()).toContain("Person 1 is now a viewer");
  });

  it("puts the role back when the change fails", async () => {
    mockMembers.mockResolvedValue(team());
    await renderProject(project({ memberCount: 2 }));
    jest.mocked(updateProjectMember).mockResolvedValue(refused("internal_error"));
    const select = screen.getByRole("combobox", { name: "Role of Person 1" });
    await act(async () => {
      fireEvent.change(select, { target: { value: "viewer" } });
    });
    expect(select).toHaveProperty("value", "editor");
    expect(toast()).toContain("We couldn’t change the role. Try again.");
  });

  it("removes a member after asking", async () => {
    mockMembers.mockResolvedValue(team());
    await renderProject(project({ memberCount: 2 }));
    fireEvent.click(screen.getByRole("button", { name: "Remove Person 1" }));
    const dialog = screen.getByRole("dialog", { name: "Remove Person 1?" });
    jest.mocked(removeProjectMember).mockResolvedValue(done);
    mockGet.mockResolvedValue({ ok: true, data: project({ memberCount: 1 }) });
    await click(within(dialog).getByRole("button", { name: "Remove" }));
    expect(removeProjectMember).toHaveBeenCalledWith(projectID, person(1).userId);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.queryByText("Person 1")).toBeNull();
    expect(await screen.findByRole("region", { name: "Members (1)" })).toBeTruthy();
  });

  it("keeps the member and says why when removing fails", async () => {
    mockMembers.mockResolvedValue(team());
    await renderProject(project({ memberCount: 2 }));
    fireEvent.click(screen.getByRole("button", { name: "Remove Person 1" }));
    const dialog = screen.getByRole("dialog", { name: "Remove Person 1?" });
    jest.mocked(removeProjectMember).mockResolvedValue(refused("cannot_remove_owner"));
    await click(within(dialog).getByRole("button", { name: "Remove" }));
    expect(within(dialog).getByText("The owner cannot be removed.")).toBeTruthy();
  });

  it("transfers ownership after a dialog that says the owner becomes an editor", async () => {
    mockMembers.mockResolvedValue(team());
    await renderProject(project({ memberCount: 2 }));
    const card = screen.getByRole("region", { name: "Ownership & deletion" });
    expect(within(card).getByRole("combobox", { name: "Transfer ownership to" })).toHaveProperty("value", person(1).userId);
    fireEvent.click(within(card).getByRole("button", { name: "Transfer" }));
    const dialog = screen.getByRole("dialog", { name: "Make Person 1 the owner?" });
    expect(within(dialog).getByText(/You become an editor/)).toBeTruthy();
    jest.mocked(transferProject).mockResolvedValue({
      ok: true,
      data: project({ myRole: "editor", owner: person(1), memberCount: 2 }),
    });
    mockMembers.mockResolvedValue(page([member(person(1), "owner"), member(ownerPerson, "editor")]));
    await click(within(dialog).getByRole("button", { name: "Transfer ownership" }));
    expect(transferProject).toHaveBeenCalledWith(projectID, person(1).userId);
    expect(await screen.findByText("You are an editor")).toBeTruthy();
    expect(screen.queryByRole("region", { name: "Ownership & deletion" })).toBeNull();
    expect(screen.getByRole("button", { name: "Leave project" })).toBeTruthy();
    expect(toast()).toContain("Person 1 is now the owner");
  });

  it("lets a member other than the owner leave after asking", async () => {
    jest.mocked(useSession).mockReturnValue({
      status: "signedIn",
      user: { id: person(1).userId, email: "p1@example.org", fullName: "Person 1", role: "researcher" },
    });
    mockMembers.mockResolvedValue(team());
    await renderProject(project({ myRole: "editor", memberCount: 2 }));
    expect(screen.getByText("Person 1 (you)")).toBeTruthy();
    expect(screen.queryByRole("combobox", { name: /Role of/ })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Leave project" }));
    const dialog = screen.getByRole("dialog", { name: "Leave this project?" });
    jest.mocked(leaveProject).mockResolvedValue(done);
    mockGet.mockResolvedValue({ ok: true, data: guest({ memberCount: 1 }) });
    await click(within(dialog).getByRole("button", { name: "Leave project" }));
    expect(leaveProject).toHaveBeenCalledWith(projectID);
    expect(await screen.findByRole("button", { name: "Request to join" })).toBeTruthy();
    expect(toast()).toContain("You left the project");
  });
});

describe("live updates", () => {
  function notice(type: Notification["type"], id = projectID): Notification {
    return {
      id: "n-1",
      type,
      createdAt: "2026-11-17T10:00:00Z",
      read: false,
      link: `/projects/${id}`,
      actor: { userId: person(1).userId, fullName: "Person 1", photoUrl: null },
      subject: { type: "project", id, title: "Explainable CVS detection" },
    };
  }

  async function emit(n: Notification) {
    await act(async () => {
      mockRealtime.handler?.({ type: "notification.created", data: n });
    });
  }

  it("reloads only the join requests when a request comes in", async () => {
    await renderProject();
    mockRequests.mockResolvedValue(page([joinRequest(1)]));
    await emit(notice("project_join_request"));
    expect(await screen.findByRole("region", { name: "Join requests (1)" })).toBeTruthy();
    expect(mockRequests).toHaveBeenCalledTimes(2);
    expect(mockMembers).toHaveBeenCalledTimes(1);
    expect(mockInvitations).toHaveBeenCalledTimes(1);
    expect(mockGet).toHaveBeenCalledTimes(1);
  });

  it("ignores another project's notification", async () => {
    await renderProject();
    await emit(notice("project_join_request", "7a1d2c3e-0000-4000-8000-0000000000ff"));
    expect(mockRequests).toHaveBeenCalledTimes(1);
  });

  it("turns the requester's page into a member's when the request is accepted", async () => {
    mockMyRequest.mockResolvedValue({ ok: true, data: myRequest(0) });
    await renderProject(guest());
    mockGet.mockResolvedValue({ ok: true, data: project({ myRole: "viewer" }) });
    await emit(notice("project_join_request_accepted"));
    expect(await screen.findByText("You are a viewer")).toBeTruthy();
    expect(mockMembers).toHaveBeenCalledTimes(2);
  });

  it("shows a new invitation", async () => {
    await renderProject(guest());
    mockMyInvitation.mockResolvedValue({ ok: true, data: myInvitation(0) });
    await emit(notice("project_invitation"));
    expect(await screen.findByText(/invited you to join as/)).toBeTruthy();
    expect(mockMyRequest).toHaveBeenCalledTimes(1);
  });
});
