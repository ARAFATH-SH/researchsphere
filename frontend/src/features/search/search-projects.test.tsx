import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { setSearchDraftLength } from "@/components/app-shell/search-draft";
import { type ProjectSearchPage, type ProjectSearchResult, searchProjects } from "@/features/project/api";

import { searchPeople } from "./api";
import { SearchPage } from "./search-page";

jest.mock("./api");
jest.mock("@/features/project/api", () => ({
  ...jest.requireActual("@/features/project/api"),
  searchProjects: jest.fn(),
}));
jest.mock("@/features/social/api");
jest.mock("@/features/social/pending-invitations");
jest.mock("@/features/notification/badges");
const mockSearchProjects = jest.mocked(searchProjects);
const mockPush = jest.fn();
let mockSearch = "";
jest.mock("next/navigation", () => ({
  useRouter: () => ({ push: mockPush }),
  useSearchParams: () => new URLSearchParams(mockSearch),
}));

const blink: ProjectSearchResult = {
  id: "7a1d2c3e-0000-4000-8000-000000000501",
  title: "Blink-rate sensing from webcams",
  description: "Measuring blink frequency during screen work.",
  status: "open",
  requiredSkills: ["OpenCV", "signal processing"],
  owner: { userId: "u1", fullName: "Nafisa Hasan", headline: null, photoUrl: null },
  myRole: null,
  createdAt: "2026-11-10T10:00:00Z",
  updatedAt: "2026-11-10T10:00:00Z",
  matchedOn: "title",
};

function resultPage(over: Partial<ProjectSearchPage> = {}): ProjectSearchPage {
  return { mode: "hybrid", items: [blink], total: 1, page: 1, pageSize: 20, fuzzy: false, correctedQuery: null, ...over };
}

let consoleError: jest.SpyInstance;

beforeEach(() => {
  consoleError = jest.spyOn(console, "error");
  mockPush.mockReset();
  mockSearchProjects.mockResolvedValue({ ok: true, data: resultPage() });
  act(() => setSearchDraftLength(0));
});
afterEach(() => {
  // No act() or other React warnings.
  expect(consoleError).not.toHaveBeenCalled();
  consoleError.mockRestore();
  jest.resetAllMocks();
});

function renderAt(search: string) {
  mockSearch = search;
  return render(<SearchPage />);
}

describe("SearchPage, Projects tab", () => {
  it("searches open projects by meaning and shows each with status, skills and owner", async () => {
    renderAt("q=blink&type=projects");
    const link = await screen.findByRole("link", { name: "Blink-rate sensing from webcams" });
    expect(link.getAttribute("href")).toBe(`/projects/${blink.id}`);
    expect(mockSearchProjects).toHaveBeenCalledTimes(1);
    expect(mockSearchProjects).toHaveBeenCalledWith({ q: "blink", page: 1, keywordOnly: false });
    expect(searchPeople).not.toHaveBeenCalled();

    const row = screen.getByRole("list", { name: "Projects" }).querySelector("li")!;
    expect(within(row).getByText("Open")).toBeTruthy();
    expect(within(row).getByText("Measuring blink frequency during screen work.")).toBeTruthy();
    expect(within(row).getByText("Needs: OpenCV · signal processing · Owner: Nafisa Hasan")).toBeTruthy();
    expect(screen.getByText("1 open project · page 1 of 1")).toBeTruthy();
    expect(screen.getByText(/Results include projects whose work matches the meaning/)).toBeTruthy();

    const tabs = screen.getAllByRole("tab");
    expect(tabs.map((t) => t.getAttribute("aria-selected"))).toEqual(["false", "true"]);
    // The people filters belong to the People tab.
    expect(screen.queryByRole("group", { name: /Institution/ })).toBeNull();
    expect(screen.queryByText("Institution")).toBeNull();
  });

  it("switches tabs keeping the text and the mode, from page 1", async () => {
    renderAt("q=blink&type=projects&page=2&mode=keyword");
    await screen.findByRole("link", { name: "Blink-rate sensing from webcams" });
    fireEvent.click(screen.getByRole("tab", { name: "People" }));
    expect(mockPush).toHaveBeenCalledWith("/search?q=blink&mode=keyword");
  });

  it("opens the Projects tab from People", async () => {
    jest.mocked(searchPeople).mockResolvedValue({
      ok: true,
      data: {
        mode: "hybrid",
        items: [],
        total: 0,
        page: 1,
        pageSize: 20,
        fuzzy: false,
        correctedQuery: null,
        facets: { institutions: [], interests: [] },
      },
    });
    renderAt("q=blink");
    await screen.findByText(/No researchers found/);
    fireEvent.click(screen.getByRole("tab", { name: "Projects" }));
    expect(mockPush).toHaveBeenCalledWith("/search?q=blink&type=projects");
    fireEvent.keyDown(screen.getByRole("tablist", { name: "Result type" }), { key: "ArrowRight" });
    expect(mockPush).toHaveBeenLastCalledWith("/search?q=blink&type=projects");
  });

  it("searches by keyword only in keyword mode, and the mode switch keeps the tab", async () => {
    mockSearchProjects.mockResolvedValue({ ok: true, data: resultPage({ mode: "keyword" }) });
    renderAt("q=blink&type=projects&mode=keyword");
    await screen.findByRole("link", { name: "Blink-rate sensing from webcams" });
    expect(mockSearchProjects).toHaveBeenCalledWith({ q: "blink", page: 1, keywordOnly: true });
    expect(screen.queryByText(/Results include projects/)).toBeNull();
    fireEvent.click(screen.getByRole("radio", { name: /By meaning/ }));
    expect(mockPush).toHaveBeenCalledWith("/search?q=blink&type=projects");
  });

  it("tags a project found only by meaning", async () => {
    mockSearchProjects.mockResolvedValue({ ok: true, data: resultPage({ items: [{ ...blink, matchedOn: "meaning" }] }) });
    renderAt("q=eye+strain&type=projects");
    await screen.findByRole("link", { name: "Blink-rate sensing from webcams" });
    expect(screen.getByText("Related by meaning")).toBeTruthy();
  });

  it("says when smart search fell back to keywords", async () => {
    mockSearchProjects.mockResolvedValue({ ok: true, data: resultPage({ mode: "keyword_fallback" }) });
    renderAt("q=blink&type=projects");
    await screen.findByRole("link", { name: "Blink-rate sensing from webcams" });
    expect(screen.getByRole("status").textContent).toBe("Smart search is unavailable right now. Showing keyword matches.");
    expect(screen.queryByText(/Results include projects/)).toBeNull();
  });

  it("says which title the results are for when the search was fuzzy", async () => {
    mockSearchProjects.mockResolvedValue({
      ok: true,
      data: resultPage({ mode: "keyword", fuzzy: true, correctedQuery: "Blink-rate sensing from webcams" }),
    });
    renderAt("q=blnk&type=projects&mode=keyword");
    const notice = await screen.findByText(/Showing results for/);
    expect(notice.textContent).toBe("Showing results for Blink-rate sensing from webcams · searched for “blnk”");
  });

  it("links the pages, keeping the tab", async () => {
    mockSearchProjects.mockResolvedValue({ ok: true, data: resultPage({ total: 45, page: 2 }) });
    renderAt("q=blink&type=projects&page=2");
    await screen.findByRole("link", { name: "Blink-rate sensing from webcams" });
    expect(mockSearchProjects).toHaveBeenCalledWith({ q: "blink", page: 2, keywordOnly: false });
    const pages = screen.getByRole("navigation", { name: "Pages" });
    expect(within(pages).getByRole("link", { name: "Next" }).getAttribute("href")).toBe("/search?q=blink&type=projects&page=3");
    expect(within(pages).getByRole("link", { name: "Previous" }).getAttribute("href")).toBe("/search?q=blink&type=projects");
    expect(within(pages).getByRole("link", { name: "2" }).getAttribute("aria-current")).toBe("page");
  });

  it.each([
    ["hybrid", "No open projects found."],
    ["keyword", "No open projects match “zzz”."],
  ] as const)("shows the %s empty state", async (mode, text) => {
    mockSearchProjects.mockResolvedValue({ ok: true, data: resultPage({ mode, items: [], total: 0 }) });
    renderAt(`q=zzz&type=projects${mode === "keyword" ? "&mode=keyword" : ""}`);
    expect(await screen.findByRole("heading", { name: text })).toBeTruthy();
  });

  it("says when searching too fast", async () => {
    mockSearchProjects.mockResolvedValue({ ok: false, code: "rate_limited", message: "x" });
    renderAt("q=blink&type=projects");
    expect(await screen.findByText(/searching too fast/)).toBeTruthy();
  });
});
