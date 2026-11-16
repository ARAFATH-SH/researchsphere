import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { suggestTags } from "@/features/profile/api";

import { createProject, listMyProjects, type ProjectPage } from "./api";
import { ProjectsPage } from "./projects-page";
import { failed, project, summary } from "./test-projects";
import { projectMessages } from "./validation";

jest.mock("./api", () => ({
  ...jest.requireActual("./api"),
  listMyProjects: jest.fn(),
  createProject: jest.fn(),
}));
jest.mock("@/features/profile/api");
const mockPush = jest.fn();
jest.mock("next/navigation", () => ({ useRouter: () => ({ push: mockPush }) }));

const mockList = jest.mocked(listMyProjects);
const mockCreate = jest.mocked(createProject);

let consoleError: jest.SpyInstance;

function page(items: ProjectPage["items"], extra: Partial<ProjectPage> = {}): { ok: true; data: ProjectPage } {
  return { ok: true, data: { items, page: 1, pageSize: 20, total: items.length, ...extra } };
}

beforeEach(() => {
  consoleError = jest.spyOn(console, "error");
  jest.mocked(suggestTags).mockResolvedValue({ ok: true, data: [] });
  mockList.mockResolvedValue(page([]));
});
afterEach(() => {
  // No act() or other React warnings.
  expect(consoleError).not.toHaveBeenCalled();
  consoleError.mockRestore();
  jest.resetAllMocks();
});

function createCard() {
  return screen.getByRole("region", { name: "Create a project" });
}

function field(name: string | RegExp) {
  return within(createCard()).getByLabelText(name) as HTMLInputElement | HTMLTextAreaElement;
}

async function submit() {
  await act(async () => {
    fireEvent.click(within(createCard()).getByRole("button", { name: "Create project" }));
  });
}

describe("ProjectsPage: My projects", () => {
  it("lists the caller's projects with role and status, each linking to its page", async () => {
    const own = summary(1);
    const other = summary(2, { myRole: "editor", status: "closed", owner: { userId: "u2", fullName: "Farhan T", headline: null, photoUrl: null } });
    const archived = summary(3, { myRole: "viewer", status: "archived" });
    mockList.mockResolvedValue(page([own, other, archived]));
    render(<ProjectsPage />);

    const list = await screen.findByRole("list", { name: "My projects" });
    expect(mockList).toHaveBeenCalledTimes(1);
    expect(mockList).toHaveBeenCalledWith(1);
    const rows = within(list).getAllByRole("link");
    expect(rows.map((r) => r.getAttribute("href"))).toEqual([own.id, other.id, archived.id].map((id) => `/projects/${id}`));
    expect(within(rows[0]).getByText("Owner")).toBeTruthy();
    expect(within(rows[0]).getByText("Open")).toBeTruthy();
    expect(within(rows[0]).getByText(/^edited /)).toBeTruthy();
    expect(within(rows[1]).getByText("Editor")).toBeTruthy();
    expect(within(rows[1]).getByText("Closed")).toBeTruthy();
    expect(within(rows[1]).getByText(/Owner: Farhan T · edited/)).toBeTruthy();
    expect(within(rows[2]).getByText("Viewer")).toBeTruthy();
    expect(within(rows[2]).getByText("Archived")).toBeTruthy();
  });

  it("says when there are no projects", async () => {
    render(<ProjectsPage />);
    expect(await screen.findByText(/You have no projects yet/)).toBeTruthy();
  });

  it("offers a retry after an error", async () => {
    mockList.mockResolvedValueOnce(failed).mockResolvedValueOnce(page([summary(1)]));
    render(<ProjectsPage />);
    fireEvent.click(await screen.findByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("link", { name: /Project 1/ })).toBeTruthy();
    expect(mockList).toHaveBeenCalledTimes(2);
  });

  it("pages through more than 20 projects", async () => {
    mockList.mockResolvedValue(page([summary(1)], { total: 25 }));
    render(<ProjectsPage />);
    await screen.findByRole("list", { name: "My projects" });
    mockList.mockResolvedValue(page([summary(21)], { total: 25, page: 2 }));
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(await screen.findByRole("link", { name: /Project 21/ })).toBeTruthy();
    expect(mockList).toHaveBeenLastCalledWith(2);
  });

  it("moves the focus to the Create card's title with New project", async () => {
    render(<ProjectsPage />);
    await screen.findByText(/You have no projects yet/);
    fireEvent.click(screen.getByRole("button", { name: "New project" }));
    expect(document.activeElement).toBe(field(/^Title/));
  });

  it("stacks the columns under 640px", async () => {
    render(<ProjectsPage />);
    await screen.findByText(/You have no projects yet/);
    expect(createCard().className).toContain("max-[640px]:max-w-none");
    expect(createCard().parentElement?.className).toContain("flex-wrap");
  });
});

describe("ProjectsPage: Create a project", () => {
  it("checks the fields before sending", async () => {
    render(<ProjectsPage />);
    fireEvent.change(field(/^Title/), { target: { value: "ab" } });
    fireEvent.change(field(/^Paper abstract/), { target: { value: "a".repeat(3001) } });
    await submit();
    expect(within(createCard()).getByText(projectMessages.titleShort)).toBeTruthy();
    expect(within(createCard()).getByText(projectMessages.descriptionRequired)).toBeTruthy();
    expect(within(createCard()).getByText(projectMessages.abstractLong)).toBeTruthy();
    expect(mockCreate).not.toHaveBeenCalled();
  });

  it("creates the project with its skills, trimmed, and opens its page", async () => {
    const created = project({ id: "new-id" });
    mockCreate.mockResolvedValue({ ok: true, data: created });
    render(<ProjectsPage />);
    fireEvent.change(field(/^Title/), { target: { value: "  Graph models " } });
    fireEvent.change(field(/^Description/), { target: { value: "A study. " } });
    const skill = within(createCard()).getByRole("combobox", { name: "Add a skill" });
    for (const name of ["Python", "  graph   theory ", "python"]) {
      fireEvent.change(skill, { target: { value: name } });
      await act(async () => {
        fireEvent.keyDown(skill, { key: "Enter" });
      });
    }
    // Enter in the skill box adds a skill and never sends the form.
    expect(mockCreate).not.toHaveBeenCalled();
    const chips = within(within(createCard()).getByRole("list", { name: "Required skills" })).getAllByRole("listitem");
    expect(chips.map((c) => c.textContent)).toEqual(["Python", "graph theory"]);
    fireEvent.click(within(chips[1]).getByRole("button", { name: "Remove graph theory" }));

    await submit();
    expect(mockCreate).toHaveBeenCalledWith({ title: "Graph models", description: "A study.", requiredSkills: ["Python"] });
    expect(mockPush).toHaveBeenCalledWith("/projects/new-id");
  });

  it("sends an abstract when there is one", async () => {
    mockCreate.mockResolvedValue({ ok: true, data: project() });
    render(<ProjectsPage />);
    fireEvent.change(field(/^Title/), { target: { value: "Graph models" } });
    fireEvent.change(field(/^Description/), { target: { value: "A study." } });
    fireEvent.change(field(/^Paper abstract/), { target: { value: " We study graphs. " } });
    await submit();
    expect(mockCreate).toHaveBeenCalledWith({
      title: "Graph models",
      description: "A study.",
      requiredSkills: [],
      abstract: "We study graphs.",
    });
  });

  it("shows a field the server refuses under that field", async () => {
    mockCreate.mockResolvedValue({ ok: false, code: "bad_request", message: "x", field: "requiredSkills[0]" });
    render(<ProjectsPage />);
    fireEvent.change(field(/^Title/), { target: { value: "Graph models" } });
    fireEvent.change(field(/^Description/), { target: { value: "A study." } });
    await submit();
    expect(within(createCard()).getByText(projectMessages.skillInvalid)).toBeTruthy();
    expect(mockPush).not.toHaveBeenCalled();
  });

  it("says when the daily limit of 10 projects is reached", async () => {
    mockCreate.mockResolvedValue({ ok: false, code: "rate_limited", message: "x" });
    render(<ProjectsPage />);
    fireEvent.change(field(/^Title/), { target: { value: "Graph models" } });
    fireEvent.change(field(/^Description/), { target: { value: "A study." } });
    await submit();
    expect(within(createCard()).getByText(/most projects allowed for today/)).toBeTruthy();
  });
});
