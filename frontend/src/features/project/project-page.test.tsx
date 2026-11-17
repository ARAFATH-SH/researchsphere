import "@/test/dialog-polyfill";

import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { useSession } from "@/features/auth/session";
import { suggestTags } from "@/features/profile/api";
import { processingRetryMs } from "@/features/recommendation/recommendations-provider";
import { failed as recsFailed, people, ready } from "@/features/recommendation/test-recommendations";

import {
  deleteProject,
  findMyInvitation,
  findMyPendingRequest,
  getProject,
  getRecommendedResearchers,
  listProjectInvitations,
  listProjectJoinRequests,
  listProjectMembers,
  reportProject,
  updateProject,
} from "./api";
import { mismatchMessage } from "./delete-project-dialog";
import { ProjectPage } from "./project-page";
import { failed, member, ownerPerson, page, project, projectID, researchersProcessing } from "./test-projects";

jest.mock("./api", () => ({
  ...jest.requireActual("./api"),
  getProject: jest.fn(),
  updateProject: jest.fn(),
  deleteProject: jest.fn(),
  reportProject: jest.fn(),
  getRecommendedResearchers: jest.fn(),
  listProjectMembers: jest.fn(),
  listProjectJoinRequests: jest.fn(),
  listProjectInvitations: jest.fn(),
  findMyPendingRequest: jest.fn(),
  findMyInvitation: jest.fn(),
}));
jest.mock("@/features/realtime/use-realtime", () => ({ useRealtimeEvent: jest.fn(), useOnConnected: jest.fn() }));
jest.mock("@/features/profile/api");
jest.mock("@/features/social/api");
jest.mock("@/features/notification/badges");
jest.mock("@/features/auth/session");
const mockPush = jest.fn();
jest.mock("next/navigation", () => ({ useRouter: () => ({ push: mockPush }) }));

const mockGet = jest.mocked(getProject);
const mockUpdate = jest.mocked(updateProject);
const mockDelete = jest.mocked(deleteProject);
const mockReport = jest.mocked(reportProject);
const mockResearchers = jest.mocked(getRecommendedResearchers);

let consoleError: jest.SpyInstance;

beforeEach(() => {
  consoleError = jest.spyOn(console, "error");
  jest.mocked(useSession).mockReturnValue({
    status: "signedIn",
    user: { id: "me", email: "me@example.org", fullName: "Me", role: "researcher" },
  });
  jest.mocked(suggestTags).mockResolvedValue({ ok: true, data: [] });
  mockResearchers.mockResolvedValue(ready(people(1, 2)));
  jest.mocked(listProjectMembers).mockResolvedValue(page([member(ownerPerson, "owner")]));
  jest.mocked(listProjectJoinRequests).mockResolvedValue(page([]));
  jest.mocked(listProjectInvitations).mockResolvedValue(page([]));
  jest.mocked(findMyPendingRequest).mockResolvedValue({ ok: true, data: null });
  jest.mocked(findMyInvitation).mockResolvedValue({ ok: true, data: null });
});
afterEach(() => {
  // No act() or other React warnings.
  expect(consoleError).not.toHaveBeenCalled();
  consoleError.mockRestore();
  jest.useRealTimers();
  jest.resetAllMocks();
});

async function renderProject(p = project()) {
  mockGet.mockResolvedValue({ ok: true, data: p });
  render(<ProjectPage projectId={p.id} />);
  await screen.findByRole("heading", { level: 1, name: p.title });
  // The membership parts load after the project.
  await screen.findByRole("list", { name: "Members", busy: false });
}

function summaryCard() {
  return screen.getByRole("region", { name: "Project summary" });
}

describe("ProjectPage for the owner", () => {
  it("shows the header, skills, abstract, members and the owner's actions", async () => {
    await renderProject();
    expect(mockGet).toHaveBeenCalledTimes(1);
    expect(mockGet).toHaveBeenCalledWith(projectID);
    const card = summaryCard();
    expect(within(card).getByText("You are the owner")).toBeTruthy();
    expect(within(card).getByText(/Detect Computer Vision Syndrome/)).toBeTruthy();
    expect(within(card).getByRole("link", { name: "Zinat Owner" }).getAttribute("href")).toMatch(/^\/profile\//);
    expect(within(card).getByText("1 member")).toBeTruthy();
    expect(within(card).getByText("Open", { selector: "span" })).toBeTruthy();
    expect(
      within(within(card).getByRole("list", { name: "Required skills" })).getAllByRole("listitem").map((li) => li.textContent),
    ).toEqual(["Python", "SHAP"]);
    expect(within(card).getByText(/412 students/)).toBeTruthy();

    const members = screen.getByRole("region", { name: "Members (1)" });
    expect(within(members).getByText("Zinat Owner")).toBeTruthy();
    expect(within(members).getByText("Owner")).toBeTruthy();

    expect(screen.getByRole("button", { name: "Edit details" })).toBeTruthy();
    expect(screen.getByRole("combobox", { name: "Status" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Delete project" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Report project" })).toBeNull();
    expect(screen.getByRole("button", { name: "+ Invite" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Transfer" })).toHaveProperty("disabled", true);
    // No request or invitation yet; the workspace and venues come with their slices.
    expect(screen.queryByText(/Join requests/)).toBeNull();
    expect(screen.queryByRole("region", { name: "Invitations" })).toBeNull();
    expect(screen.queryByText(/Open workspace/)).toBeNull();
    expect(screen.queryByText(/Where to submit/)).toBeNull();
  });

  it("changes the status at once", async () => {
    await renderProject();
    mockUpdate.mockResolvedValue({ ok: true, data: project({ status: "archived" }) });
    const select = screen.getByRole("combobox", { name: "Status" }) as HTMLSelectElement;
    await act(async () => {
      fireEvent.change(select, { target: { value: "archived" } });
    });
    expect(mockUpdate).toHaveBeenCalledWith(projectID, { status: "archived" });
    expect(select.value).toBe("archived");
    expect(screen.getByText("Only members can see it.")).toBeTruthy();
    expect(within(summaryCard()).getByText("Archived", { selector: "span" })).toBeTruthy();
  });

  it("puts the old status back when the change fails", async () => {
    await renderProject();
    mockUpdate.mockResolvedValue(failed);
    const select = screen.getByRole("combobox", { name: "Status" }) as HTMLSelectElement;
    await act(async () => {
      fireEvent.change(select, { target: { value: "closed" } });
    });
    expect(select.value).toBe("open");
    expect(screen.getByRole("alert").textContent).toMatch(/couldn’t change the status/);
  });

  it("edits the details, sending only what changed and clearing the abstract with null", async () => {
    await renderProject();
    fireEvent.click(screen.getByRole("button", { name: "Edit details" }));
    const dialog = screen.getByRole("dialog", { name: "Edit project" });
    expect((within(dialog).getByLabelText(/^Title/) as HTMLInputElement).value).toBe("Explainable CVS detection");
    fireEvent.change(within(dialog).getByLabelText(/^Title/), { target: { value: "Explainable CVS detection v2" } });
    fireEvent.change(within(dialog).getByLabelText(/^Paper abstract/), { target: { value: "  " } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Remove SHAP" }));
    const updated = project({ title: "Explainable CVS detection v2", abstract: null, requiredSkills: [{ id: "s1", name: "Python" }] });
    mockUpdate.mockResolvedValue({ ok: true, data: updated });
    await act(async () => {
      fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    });
    expect(mockUpdate).toHaveBeenCalledWith(projectID, {
      title: "Explainable CVS detection v2",
      requiredSkills: ["Python"],
      abstract: null,
    });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByRole("heading", { level: 1, name: "Explainable CVS detection v2" })).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "Abstract" })).toBeNull();
    expect(screen.getByRole("status").textContent).toBe("Saved");
  });

  it("checks the edit before sending", async () => {
    await renderProject();
    fireEvent.click(screen.getByRole("button", { name: "Edit details" }));
    const dialog = screen.getByRole("dialog", { name: "Edit project" });
    fireEvent.change(within(dialog).getByLabelText(/^Description/), { target: { value: " " } });
    await act(async () => {
      fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    });
    expect(within(dialog).getByText("Describe the project.")).toBeTruthy();
    expect(mockUpdate).not.toHaveBeenCalled();
  });

  it("deletes only after the exact title is typed", async () => {
    await renderProject();
    fireEvent.click(screen.getByRole("button", { name: "Delete project" }));
    const dialog = screen.getByRole("dialog", { name: "Delete project" });
    const input = within(dialog).getByLabelText("Type the project title to confirm");
    const confirm = within(dialog).getByRole("button", { name: "Delete project" }) as HTMLButtonElement;
    expect(confirm.disabled).toBe(true);
    fireEvent.change(input, { target: { value: "explainable cvs detection" } });
    expect(confirm.disabled).toBe(true);
    fireEvent.change(input, { target: { value: " Explainable CVS detection " } });
    expect(confirm.disabled).toBe(false);

    mockDelete.mockResolvedValue({ ok: true, data: undefined });
    await act(async () => {
      fireEvent.click(confirm);
    });
    expect(mockDelete).toHaveBeenCalledWith(projectID, "Explainable CVS detection");
    expect(mockPush).toHaveBeenCalledWith("/projects");
  });

  it("shows the server's mismatch under the field", async () => {
    await renderProject();
    fireEvent.click(screen.getByRole("button", { name: "Delete project" }));
    const dialog = screen.getByRole("dialog", { name: "Delete project" });
    fireEvent.change(within(dialog).getByLabelText("Type the project title to confirm"), {
      target: { value: "Explainable CVS detection" },
    });
    mockDelete.mockResolvedValue({ ok: false, code: "confirmation_mismatch", message: "x", field: "confirm" });
    await act(async () => {
      fireEvent.click(within(dialog).getByRole("button", { name: "Delete project" }));
    });
    expect(within(dialog).getByText(mismatchMessage)).toBeTruthy();
    expect(mockPush).not.toHaveBeenCalled();
  });

  it("shows the researchers who fit the project, with Invite in place of Connect", async () => {
    await renderProject();
    const card = await screen.findByRole("region", { name: "Researchers who fit this project" });
    expect(mockResearchers).toHaveBeenCalledTimes(1);
    expect(mockResearchers).toHaveBeenCalledWith(projectID);
    const list = await within(card).findByRole("list", { name: "Recommended researchers" });
    expect(within(list).getByRole("link", { name: "Researcher 1" })).toBeTruthy();
    expect(within(list).getAllByText("99% match")).toHaveLength(1);
    expect(within(list).getByRole("button", { name: "Invite Researcher 1" })).toBeTruthy();
    expect(within(list).getByRole("combobox", { name: "Role for Researcher 1" })).toBeTruthy();
    expect(within(list).queryByRole("button", { name: /Connect/ })).toBeNull();
  });

  it("says when the project's matches are being prepared, and asks once more after a minute", async () => {
    jest.useFakeTimers();
    mockResearchers.mockResolvedValueOnce(researchersProcessing).mockResolvedValueOnce(ready(people(3)));
    mockGet.mockResolvedValue({ ok: true, data: project() });
    render(<ProjectPage projectId={projectID} />);
    expect(await screen.findByText(/We’re finding researchers for this project/)).toBeTruthy();
    await act(async () => {
      jest.advanceTimersByTime(processingRetryMs);
    });
    expect(await screen.findByRole("link", { name: "Researcher 3" })).toBeTruthy();
    expect(mockResearchers).toHaveBeenCalledTimes(2);
  });

  it("says when no researcher fits, and offers a retry after an error", async () => {
    mockResearchers.mockResolvedValueOnce(recsFailed).mockResolvedValueOnce(ready([]));
    await renderProject();
    fireEvent.click(await screen.findByRole("button", { name: "Try again" }));
    expect(await screen.findByText("No new recommendations right now.")).toBeTruthy();
  });

  it("stacks the recommendations under the details under 640px", async () => {
    await renderProject();
    const aside = screen.getByRole("complementary", { name: "Project recommendations" });
    expect(aside.className).toContain("max-[640px]:max-w-none");
    expect(aside.parentElement?.className).toContain("flex-wrap");
  });
});

describe("ProjectPage for others", () => {
  it("hides the owner's parts and the abstract from a non-member, and offers Report", async () => {
    await renderProject(project({ myRole: null, abstract: null, memberCount: 3 }));
    expect(screen.queryByText(/You are/)).toBeNull();
    expect(screen.queryByRole("button", { name: "Edit details" })).toBeNull();
    expect(screen.queryByRole("combobox", { name: "Status" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Delete project" })).toBeNull();
    expect(screen.queryByRole("heading", { name: "Abstract" })).toBeNull();
    expect(screen.queryByRole("complementary", { name: "Project recommendations" })).toBeNull();
    expect(screen.getByText("3 members")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Report project" })).toBeTruthy();
    expect(mockResearchers).not.toHaveBeenCalled();
  });

  it("shows an editor the abstract but no owner's parts and no Report", async () => {
    await renderProject(project({ myRole: "editor" }));
    expect(screen.getByText("You are an editor")).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Abstract" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Edit details" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Delete project" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Report project" })).toBeNull();
    expect(mockResearchers).not.toHaveBeenCalled();
  });

  it("reports the project with a reason", async () => {
    await renderProject(project({ myRole: null }));
    fireEvent.click(screen.getByRole("button", { name: "Report project" }));
    const dialog = screen.getByRole("dialog", { name: "Report project" });
    await act(async () => {
      fireEvent.click(within(dialog).getByRole("button", { name: "Send report" }));
    });
    expect(within(dialog).getByText("Choose a reason.")).toBeTruthy();
    expect(mockReport).not.toHaveBeenCalled();

    const reason = within(dialog).getByRole("combobox", { name: /Reason/ }) as HTMLSelectElement;
    expect([...reason.options].map((o) => o.textContent)).toEqual([
      "Choose a reason",
      "Spam",
      "Harassment",
      "Inappropriate",
      "Other",
    ]);
    fireEvent.change(reason, { target: { value: "spam" } });
    fireEvent.change(within(dialog).getByLabelText(/Details/), { target: { value: " Ads only. " } });
    mockReport.mockResolvedValue({ ok: true, data: undefined });
    await act(async () => {
      fireEvent.click(within(dialog).getByRole("button", { name: "Send report" }));
    });
    expect(mockReport).toHaveBeenCalledWith(projectID, { reason: "spam", details: "Ads only." });
    expect(within(dialog).getByText("Thanks. Our team will review it.")).toBeTruthy();
  });

  it.each([
    ["already_reported", /already reported this project/],
    ["cannot_report_own_project", /can’t report your own project/],
  ])("explains %s", async (code, text) => {
    await renderProject(project({ myRole: null }));
    fireEvent.click(screen.getByRole("button", { name: "Report project" }));
    const dialog = screen.getByRole("dialog", { name: "Report project" });
    fireEvent.change(within(dialog).getByRole("combobox", { name: /Reason/ }), { target: { value: "other" } });
    mockReport.mockResolvedValue({ ok: false, code, message: "x" });
    await act(async () => {
      fireEvent.click(within(dialog).getByRole("button", { name: "Send report" }));
    });
    expect(within(dialog).getByText(text)).toBeTruthy();
    expect(mockReport).toHaveBeenCalledWith(projectID, { reason: "other" });
  });

  it("shows Project not found for a hidden or missing project", async () => {
    mockGet.mockResolvedValue({ ok: false, code: "not_found", message: "x" });
    render(<ProjectPage projectId={projectID} />);
    expect(await screen.findByRole("heading", { level: 1, name: "Project not found" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Go to My projects" }).getAttribute("href")).toBe("/projects");
    expect(mockResearchers).not.toHaveBeenCalled();
  });

  it("offers a retry when the project could not be loaded", async () => {
    mockGet.mockResolvedValueOnce(failed).mockResolvedValueOnce({ ok: true, data: project({ myRole: null }) });
    render(<ProjectPage projectId={projectID} />);
    fireEvent.click(await screen.findByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("heading", { level: 1, name: "Explainable CVS detection" })).toBeTruthy();
  });
});
