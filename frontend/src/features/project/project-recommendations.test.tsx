import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { useSession } from "@/features/auth/session";
import { getPeopleRecommendations } from "@/features/recommendation/api";
import { RecommendationList } from "@/features/recommendation/recommendation-list";
import { processingRetryMs, RecommendationsProvider } from "@/features/recommendation/recommendations-provider";
import { needsProfile, people, ready } from "@/features/recommendation/test-recommendations";

import { getProjectRecommendations } from "./api";
import {
  allProjectRecommendationsHref,
  ProjectRecommendationList,
  ProjectRecommendationsProvider,
} from "./project-recommendations";
import {
  failed,
  projectRecs,
  projectsNeedProfile,
  projectsProcessing,
  recommendedProject,
} from "./test-projects";

jest.mock("./api", () => ({ ...jest.requireActual("./api"), getProjectRecommendations: jest.fn() }));
jest.mock("@/features/recommendation/api", () => ({
  ...jest.requireActual("@/features/recommendation/api"),
  getPeopleRecommendations: jest.fn(),
}));
jest.mock("@/features/social/api");
jest.mock("@/features/notification/badges");
jest.mock("@/features/auth/session");

const mockProjects = jest.mocked(getProjectRecommendations);
const mockPeople = jest.mocked(getPeopleRecommendations);

let consoleError: jest.SpyInstance;

beforeEach(() => {
  consoleError = jest.spyOn(console, "error");
  jest.mocked(useSession).mockReturnValue({
    status: "signedIn",
    user: { id: "me", email: "me@example.org", fullName: "Me", role: "researcher" },
  });
  mockPeople.mockResolvedValue(ready(people(1)));
});
afterEach(() => {
  // No act() or other React warnings.
  expect(consoleError).not.toHaveBeenCalled();
  consoleError.mockRestore();
  jest.useRealTimers();
  jest.resetAllMocks();
});

function renderHome() {
  return render(
    <RecommendationsProvider>
      <ProjectRecommendationsProvider>
        <RecommendationList limit={3} variant="home" />
        <ProjectRecommendationList limit={3} variant="home" />
      </ProjectRecommendationsProvider>
    </RecommendationsProvider>,
  );
}

function card() {
  return screen.getByRole("region", { name: /^Projects for you/ });
}

describe("ProjectRecommendationList on Home", () => {
  it("shows 3 rows, each a link to the project with owner, match and shared skills, and See all", async () => {
    const first = recommendedProject(1, { score: 88, shared: ["OpenCV", "Python"] });
    mockProjects.mockResolvedValue(projectRecs([first, recommendedProject(2), recommendedProject(3), recommendedProject(4)]));
    renderHome();

    const list = await within(card()).findByRole("list", { name: "Recommended projects" });
    expect(mockProjects).toHaveBeenCalledTimes(1);
    const rows = within(list).getAllByRole("link");
    expect(rows).toHaveLength(3);
    expect(rows[0].getAttribute("href")).toBe(`/projects/${first.project.id}`);
    expect(within(rows[0]).getByText("Suggested project 1")).toBeTruthy();
    expect(within(rows[0]).getByText("Owner: Owner 1 · 1 member")).toBeTruthy();
    expect(within(rows[0]).getByText("88% match")).toBeTruthy();
    expect(
      within(within(rows[0]).getByRole("list", { name: "Shared skills" })).getAllByRole("listitem").map((li) => li.textContent),
    ).toEqual(["OpenCV", "Python"]);
    expect(within(rows[1]).getByText("Owner: Owner 2 · 2 members")).toBeTruthy();
    // The whole row is the link: no View button (Home.html).
    expect(within(card()).queryByRole("button", { name: /View/ })).toBeNull();
    expect(within(card()).getByRole("link", { name: "See all 4" }).getAttribute("href")).toBe(allProjectRecommendationsHref);
  });

  it("leaves out See all with 3 or fewer", async () => {
    mockProjects.mockResolvedValue(projectRecs([recommendedProject(1)]));
    renderHome();
    await within(card()).findByRole("list", { name: "Recommended projects" });
    expect(within(card()).queryByRole("link", { name: /See all/ })).toBeNull();
  });

  it("says when no open project matches", async () => {
    mockProjects.mockResolvedValue(projectRecs([]));
    renderHome();
    expect(await within(card()).findByText("No open projects match your profile right now.")).toBeTruthy();
  });

  it("says while the profile is being prepared, and asks once more after a minute", async () => {
    jest.useFakeTimers();
    mockProjects.mockResolvedValueOnce(projectsProcessing).mockResolvedValueOnce(projectRecs([recommendedProject(5)]));
    renderHome();
    expect(await within(card()).findByText(/We’re finding projects for you/)).toBeTruthy();
    await act(async () => {
      jest.advanceTimersByTime(processingRetryMs);
    });
    expect(await within(card()).findByText("Suggested project 5")).toBeTruthy();
    expect(mockProjects).toHaveBeenCalledTimes(2);
  });

  it("offers a retry after an error", async () => {
    mockProjects.mockResolvedValueOnce(failed).mockResolvedValueOnce(projectRecs([recommendedProject(1)]));
    renderHome();
    fireEvent.click(await within(card()).findByRole("button", { name: "Try again" }));
    expect(await within(card()).findByText("Suggested project 1")).toBeTruthy();
  });

  it("shows one Get better recommendations card for both lists when the profile is empty", async () => {
    mockPeople.mockResolvedValue(needsProfile);
    mockProjects.mockResolvedValue(projectsNeedProfile);
    renderHome();
    const cards = await screen.findAllByRole("region", { name: "Get better recommendations" });
    expect(cards).toHaveLength(1);
    expect(within(cards[0]).getByText(/we’ll suggest collaborators and projects/)).toBeTruthy();
    expect(screen.queryByRole("region", { name: /^Projects for you/ })).toBeNull();
  });

  it("asks for a profile itself when the collaborators card shows a list", async () => {
    mockProjects.mockResolvedValue(projectsNeedProfile);
    renderHome();
    const asked = await screen.findByRole("region", { name: "Get better recommendations" });
    expect(within(asked).getByText(/we’ll suggest projects/)).toBeTruthy();
  });
});

describe("ProjectRecommendationList page", () => {
  it("shows every recommendation with its description, and no See all", async () => {
    const items = Array.from({ length: 10 }, (_, i) => recommendedProject(i + 1));
    mockProjects.mockResolvedValue(projectRecs(items));
    render(
      <ProjectRecommendationsProvider>
        <ProjectRecommendationList limit={10} variant="page" />
      </ProjectRecommendationsProvider>,
    );
    expect(await screen.findByRole("heading", { level: 1, name: "Projects for you" })).toBeTruthy();
    const list = await screen.findByRole("list", { name: "Recommended projects" });
    expect(within(list).getAllByRole("link")).toHaveLength(10);
    expect(within(list).getByText("About project 1")).toBeTruthy();
    expect(screen.queryByRole("link", { name: /See all/ })).toBeNull();
  });
});
