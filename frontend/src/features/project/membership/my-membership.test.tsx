import { act, fireEvent, render, screen, within } from "@testing-library/react";

import type { Notification } from "@/features/notification/api";
import { suggestTags } from "@/features/profile/api";

import {
  acceptInvitation,
  cancelJoinRequest,
  listMyInvitations,
  listMyJoinRequests,
  listMyProjects,
  rejectInvitation,
} from "../api";
import { ProjectsPage } from "../projects-page";
import { myInvitation, myRequest, page, summary } from "../test-projects";

jest.mock("../api", () => ({
  ...jest.requireActual("../api"),
  listMyProjects: jest.fn(),
  listMyInvitations: jest.fn(),
  listMyJoinRequests: jest.fn(),
  acceptInvitation: jest.fn(),
  rejectInvitation: jest.fn(),
  cancelJoinRequest: jest.fn(),
}));
jest.mock("@/features/profile/api");
jest.mock("next/navigation", () => ({ useRouter: () => ({ push: jest.fn() }) }));
const mockRealtime: { handler: ((event: unknown) => void) | null } = { handler: null };
jest.mock("@/features/realtime/use-realtime", () => ({
  useRealtimeEvent: (handler: (event: unknown) => void) => {
    mockRealtime.handler = handler;
  },
  useOnConnected: jest.fn(),
}));

const mockProjects = jest.mocked(listMyProjects);
const mockInvitations = jest.mocked(listMyInvitations);
const mockRequests = jest.mocked(listMyJoinRequests);
const projectsPage = (items: ReturnType<typeof summary>[]) => ({
  ok: true as const,
  data: { items, page: 1, pageSize: 20, total: items.length },
});

let consoleError: jest.SpyInstance;

beforeEach(() => {
  consoleError = jest.spyOn(console, "error");
  jest.mocked(suggestTags).mockResolvedValue({ ok: true, data: [] });
  mockProjects.mockResolvedValue(projectsPage([summary(1), summary(2, { myRole: "viewer" })]));
  mockInvitations.mockResolvedValue(page([]));
  mockRequests.mockResolvedValue(page([]));
});
afterEach(() => {
  // No act() or other React warnings.
  expect(consoleError).not.toHaveBeenCalled();
  consoleError.mockRestore();
  jest.resetAllMocks();
  mockRealtime.handler = null;
});

async function renderPage() {
  render(<ProjectsPage />);
  await screen.findByRole("list", { name: "My projects" });
}

async function click(el: HTMLElement) {
  await act(async () => {
    fireEvent.click(el);
  });
}

describe("/projects: invitations and requests (FR-11.1, FR-11.3, FR-11.6)", () => {
  it("shows the role in each project and hides empty sections", async () => {
    await renderPage();
    const list = screen.getByRole("list", { name: "My projects" });
    expect(within(list).getByText("Owner")).toBeTruthy();
    expect(within(list).getByText("Viewer")).toBeTruthy();
    expect(screen.queryByRole("region", { name: "Project invitations" })).toBeNull();
    expect(screen.queryByRole("region", { name: "My requests" })).toBeNull();
  });

  it("lists invitations and accepts one, reloading My projects", async () => {
    mockInvitations.mockResolvedValue(page([myInvitation(1), myInvitation(2)]));
    await renderPage();
    const card = await screen.findByRole("region", { name: "Project invitations" });
    expect(card.textContent).toContain("Person 91 invited you to Invited project 1 as editor");
    jest.mocked(acceptInvitation).mockResolvedValue({
      ok: true,
      data: { user: myInvitation(1).project.owner, role: "editor", joinedAt: "2026-11-17T10:00:00Z" },
    });
    await click(within(card).getByRole("button", { name: "Accept the invitation to Invited project 1" }));
    expect(acceptInvitation).toHaveBeenCalledWith(myInvitation(1).project.id, myInvitation(1).id);
    expect(within(card).queryByText(/Invited project 1/)).toBeNull();
    expect(mockProjects).toHaveBeenCalledTimes(2);
    expect(screen.getByRole("status").textContent).toBe("You joined Invited project 1");
  });

  it("declines an invitation", async () => {
    mockInvitations.mockResolvedValue(page([myInvitation(1)]));
    await renderPage();
    jest.mocked(rejectInvitation).mockResolvedValue({ ok: true, data: undefined });
    await click(await screen.findByRole("button", { name: "Decline the invitation to Invited project 1" }));
    expect(rejectInvitation).toHaveBeenCalledWith(myInvitation(1).project.id, myInvitation(1).id);
    expect(screen.queryByRole("region", { name: "Project invitations" })).toBeNull();
    expect(mockProjects).toHaveBeenCalledTimes(1);
  });

  it("puts the invitation back when answering fails", async () => {
    mockInvitations.mockResolvedValue(page([myInvitation(1)]));
    await renderPage();
    jest.mocked(acceptInvitation).mockResolvedValue({ ok: false, code: "internal_error", message: "x" });
    await click(await screen.findByRole("button", { name: "Accept the invitation to Invited project 1" }));
    expect(screen.getByRole("region", { name: "Project invitations" }).textContent).toContain("Invited project 1");
    expect(screen.getByRole("status").textContent).toBe("We couldn’t answer the invitation. Try again.");
  });

  it("lists my requests with their status and cancels a pending one", async () => {
    mockRequests.mockResolvedValue(
      page([myRequest(1), myRequest(2, { status: "accepted" }), myRequest(3, { status: "rejected" })]),
    );
    await renderPage();
    const card = await screen.findByRole("region", { name: "My requests" });
    const rows = within(card).getAllByRole("listitem");
    expect(within(rows[0]).getByText("Pending")).toBeTruthy();
    expect(within(rows[1]).getByText("Accepted")).toBeTruthy();
    expect(within(rows[2]).getByText("Declined")).toBeTruthy();
    expect(within(card).getAllByRole("button", { name: /Cancel/ })).toHaveLength(1);
    expect(within(rows[0]).getByRole("link", { name: "Requested project 1" }).getAttribute("href")).toBe(
      `/projects/${myRequest(1).project.id}`,
    );

    jest.mocked(cancelJoinRequest).mockResolvedValue({ ok: true, data: undefined });
    await click(within(card).getByRole("button", { name: "Cancel the request to join Requested project 1" }));
    expect(cancelJoinRequest).toHaveBeenCalledWith(myRequest(1).project.id, myRequest(1).id);
    expect(within(card).getAllByRole("listitem")).toHaveLength(2);
  });

  it("puts the request back when cancelling fails", async () => {
    mockRequests.mockResolvedValue(page([myRequest(1)]));
    await renderPage();
    jest.mocked(cancelJoinRequest).mockResolvedValue({ ok: false, code: "network_error", message: "x" });
    await click(await screen.findByRole("button", { name: "Cancel the request to join Requested project 1" }));
    expect(within(screen.getByRole("region", { name: "My requests" })).getAllByRole("listitem")).toHaveLength(1);
    expect(screen.getByRole("status").textContent).toMatch(/Could not reach the server/);
  });

  it("refreshes what a project notification changes", async () => {
    await renderPage();
    const n = (type: Notification["type"]): Notification => ({
      id: "n",
      type,
      createdAt: "2026-11-17T10:00:00Z",
      read: false,
      link: `/projects/${myInvitation(3).project.id}`,
    });
    mockInvitations.mockResolvedValue(page([myInvitation(3)]));
    await act(async () => {
      mockRealtime.handler?.({ type: "notification.created", data: n("project_invitation") });
    });
    expect(await screen.findByRole("region", { name: "Project invitations" })).toBeTruthy();
    expect(mockProjects).toHaveBeenCalledTimes(1);

    mockRequests.mockResolvedValue(page([myRequest(1, { status: "accepted" })]));
    await act(async () => {
      mockRealtime.handler?.({ type: "notification.created", data: n("project_join_request_accepted") });
    });
    expect(await screen.findByText("Accepted")).toBeTruthy();
    expect(mockProjects).toHaveBeenCalledTimes(2);
  });
});
