import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { setSearchDraftLength } from "@/components/app-shell/search-draft";
import { sendConnectionRequest } from "@/features/social/api";

import { type PeopleSearchPage, type PersonSearchResult, searchPeople } from "./api";
import { SearchPage } from "./search-page";

jest.mock("./api");
jest.mock("@/features/social/api");
jest.mock("@/features/social/pending-invitations");
jest.mock("@/features/notification/badges");
const mockSearchPeople = jest.mocked(searchPeople);
const mockPush = jest.fn();
let mockSearch = "";
jest.mock("next/navigation", () => ({
  useRouter: () => ({ push: mockPush }),
  useSearchParams: () => new URLSearchParams(mockSearch),
}));

const ada: PersonSearchResult = {
  userId: "0b8f4c1e-0000-4000-8000-000000000001",
  fullName: "Ada Rahman",
  headline: "PhD student · Medical imaging",
  institution: "BUET",
  photoUrl: null,
  interests: ["Machine learning", "Retinal analysis"],
  matchedOn: "name",
  connectionStatus: "none",
  connectionRequestId: null,
};

function resultPage(over: Partial<PeopleSearchPage> = {}): PeopleSearchPage {
  return {
    mode: "hybrid",
    items: [ada],
    total: 1,
    page: 1,
    pageSize: 20,
    fuzzy: false,
    correctedQuery: null,
    facets: {
      institutions: [
        { name: "BUET", count: 1 },
        { name: "University of Dhaka", count: 4 },
      ],
      interests: [
        { name: "Machine learning", count: 1 },
        { name: "NLP", count: 2 },
      ],
    },
    ...over,
  };
}

beforeEach(() => {
  mockPush.mockReset();
  mockSearchPeople.mockReset();
  mockSearchPeople.mockResolvedValue({ ok: true, data: resultPage() });
  act(() => setSearchDraftLength(0));
});

function renderAt(search: string) {
  mockSearch = search;
  return render(<SearchPage />);
}

describe("SearchPage", () => {
  it.each(["", "q=a", "q=+b+"])("asks for 2 characters and does not search for %j", (search) => {
    renderAt(search);
    expect(screen.getByText("Type at least 2 characters to search.")).toBeTruthy();
    expect(mockSearchPeople).not.toHaveBeenCalled();
  });

  it("shows the skeleton while searching, then the results", async () => {
    renderAt("q=ada");
    expect(screen.getByRole("status").textContent).toBe("Searching…");
    const link = await screen.findByRole("link", { name: "Ada Rahman" });
    expect(link.getAttribute("href")).toBe(`/profile/${ada.userId}`);
    expect(screen.getByText("PhD student · Medical imaging")).toBeTruthy();
    expect(within(screen.getByRole("list", { name: "People" })).getByText("BUET")).toBeTruthy();
    expect(screen.getByText("Interests: Machine learning, Retinal analysis")).toBeTruthy();
    expect(screen.getByText("1 person · page 1 of 1")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Connect with Ada Rahman" })).toBeTruthy();
    expect(screen.queryByRole("status")).toBeNull();
    expect(mockSearchPeople).toHaveBeenCalledWith({
      q: "ada",
      institution: "",
      interests: [],
      page: 1,
      keywordOnly: false,
    });
  });

  it("gives each row the button for the viewer's link, from the search result itself", async () => {
    const row = (n: number, over: Partial<PersonSearchResult>): PersonSearchResult => ({
      ...ada,
      userId: `0b8f4c1e-0000-4000-8000-00000000001${n}`,
      fullName: `Person ${n}`,
      ...over,
    });
    mockSearchPeople.mockResolvedValue({
      ok: true,
      data: resultPage({
        total: 5,
        items: [
          row(1, { connectionStatus: "none" }),
          row(2, { connectionStatus: "pending_sent", connectionRequestId: "7a1d2c3e-0000-4000-8000-000000000002" }),
          row(3, { connectionStatus: "pending_received", connectionRequestId: "7a1d2c3e-0000-4000-8000-000000000003" }),
          row(4, { connectionStatus: "connected" }),
          row(5, { connectionStatus: "self" }),
        ],
      }),
    });
    jest.mocked(sendConnectionRequest).mockResolvedValue({
      ok: true,
      data: {
        requestId: "7a1d2c3e-0000-4000-8000-000000000001",
        createdAt: "2026-11-01T10:00:00Z",
        userId: row(1, {}).userId,
        fullName: "Person 1",
        headline: null,
        institution: null,
        photoUrl: null,
      },
    });
    renderAt("q=person");
    const rows = within(await screen.findByRole("list", { name: "People" })).getAllByRole("listitem");
    const buttons = (li: HTMLElement) => within(li).queryAllByRole("button").map((b) => b.textContent);
    expect(rows.map(buttons)).toEqual([["Connect"], ["Pending"], ["Accept", "Ignore"], [], []]);
    expect(within(rows[0]).getByRole("button", { name: "Connect with Person 1" }).className).toContain("h-9");

    await act(async () => fireEvent.click(within(rows[0]).getByRole("button", { name: "Connect with Person 1" })));
    expect(sendConnectionRequest).toHaveBeenCalledWith(row(1, {}).userId);
    expect(within(rows[0]).getByRole("button", { name: "Pending request to Person 1" })).toBeTruthy();
    // One search request for the whole page, none per row.
    expect(mockSearchPeople).toHaveBeenCalledTimes(1);
  });

  it("has only the People tab", async () => {
    renderAt("q=ada");
    await screen.findByRole("link", { name: "Ada Rahman" });
    expect(screen.getAllByRole("tab").map((t) => t.textContent)).toEqual(["People"]);
  });

  it("says which name the results are for when the search was fuzzy", async () => {
    mockSearchPeople.mockResolvedValue({
      ok: true,
      data: resultPage({ fuzzy: true, correctedQuery: "Ada Rahman" }),
    });
    renderAt("q=Ada+Rahmn");
    const notice = await screen.findByText(/Showing results for/);
    expect(notice.textContent).toBe("Showing results for Ada Rahman · searched for “Ada Rahmn”");
  });

  it("lists the facets with their counts", async () => {
    renderAt("q=ada");
    await screen.findByRole("link", { name: "Ada Rahman" });
    const select = screen.getByLabelText("Institution");
    expect(within(select).getAllByRole("option").map((o) => o.textContent)).toEqual([
      "Any institution",
      "BUET (1)",
      "University of Dhaka (4)",
    ]);
    expect(screen.getByRole("checkbox", { name: "NLP (2)" })).toBeTruthy();
  });

  it("puts a filter change in the URL and starts again at page 1", async () => {
    mockSearchPeople.mockResolvedValue({ ok: true, data: resultPage({ total: 60, page: 2 }) });
    renderAt("q=ada&page=2");
    await screen.findByRole("link", { name: "Ada Rahman" });

    fireEvent.change(screen.getByLabelText("Institution"), { target: { value: "University of Dhaka" } });
    expect(mockPush).toHaveBeenLastCalledWith("/search?q=ada&institution=University+of+Dhaka");

    fireEvent.click(screen.getByRole("checkbox", { name: "NLP (2)" }));
    expect(mockPush).toHaveBeenLastCalledWith("/search?q=ada&interest=NLP");
  });

  it("restores the search from the URL", async () => {
    mockSearchPeople.mockResolvedValue({
      ok: true,
      data: resultPage({
        total: 41,
        page: 2,
        facets: {
          institutions: [{ name: "BUET", count: 41 }],
          interests: [
            { name: "ml", count: 41 },
            { name: "nlp", count: 41 },
            { name: "vision", count: 3 },
          ],
        },
      }),
    });
    renderAt("q=ada&institution=BUET&interest=ml&interest=nlp&page=2");
    await screen.findByRole("link", { name: "Ada Rahman" });
    expect(mockSearchPeople).toHaveBeenCalledWith({
      q: "ada",
      institution: "BUET",
      interests: ["ml", "nlp"],
      page: 2,
      keywordOnly: false,
    });
    expect(screen.getByLabelText<HTMLSelectElement>("Institution").value).toBe("BUET");
    expect(screen.getByRole<HTMLInputElement>("checkbox", { name: "ml (41)" }).checked).toBe(true);
    expect(screen.getByRole<HTMLInputElement>("checkbox", { name: "nlp (41)" }).checked).toBe(true);
    expect(screen.getByRole<HTMLInputElement>("checkbox", { name: "vision (3)" }).checked).toBe(false);

    // Unticking one keeps the others.
    fireEvent.click(screen.getByRole("checkbox", { name: "ml (41)" }));
    expect(mockPush).toHaveBeenLastCalledWith("/search?q=ada&institution=BUET&interest=nlp");
  });

  it("resets the filters", async () => {
    renderAt("q=ada&institution=BUET&interest=NLP");
    await screen.findByRole("link", { name: "Ada Rahman" });
    fireEvent.click(screen.getByRole("button", { name: "Reset filters" }));
    expect(mockPush).toHaveBeenCalledWith("/search?q=ada");
  });

  it("cannot reset when no filter is on", async () => {
    renderAt("q=ada");
    await screen.findByRole("link", { name: "Ada Rahman" });
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "Reset filters" }).disabled).toBe(true);
  });

  it("links to the other pages", async () => {
    mockSearchPeople.mockResolvedValue({ ok: true, data: resultPage({ total: 45, page: 2 }) });
    renderAt("q=ada&interest=NLP&page=2");
    const nav = await screen.findByRole("navigation", { name: "Pages" });
    const links = within(nav)
      .getAllByRole("link")
      .map((l) => [l.textContent, l.getAttribute("href"), l.getAttribute("aria-current")]);
    expect(links).toEqual([
      ["Previous", "/search?q=ada&interest=NLP", null],
      ["1", "/search?q=ada&interest=NLP", null],
      ["2", "/search?q=ada&interest=NLP&page=2", "page"],
      ["3", "/search?q=ada&interest=NLP&page=3", null],
      ["Next", "/search?q=ada&interest=NLP&page=3", null],
    ]);
    expect(screen.getByText("45 people · page 2 of 3")).toBeTruthy();
  });

  it("has no page links for a single page", async () => {
    renderAt("q=ada");
    await screen.findByRole("link", { name: "Ada Rahman" });
    expect(screen.queryByRole("navigation", { name: "Pages" })).toBeNull();
  });

  it("shows the keyword empty state with a tip", async () => {
    mockSearchPeople.mockResolvedValue({ ok: true, data: resultPage({ mode: "keyword", items: [], total: 0 }) });
    renderAt("q=zzz");
    expect(await screen.findByText("No researchers match “zzz”.")).toBeTruthy();
    expect(screen.getByText("Try a different spelling, or search by institution or interest.")).toBeTruthy();
    expect(screen.queryByRole("link", { name: "Reset filters" })).toBeNull();
  });

  it("offers to reset the filters when nothing matches with filters on", async () => {
    mockSearchPeople.mockResolvedValue({ ok: true, data: resultPage({ items: [], total: 0 }) });
    renderAt("q=zzz&institution=BUET");
    const reset = await screen.findByRole("link", { name: "Reset filters" });
    expect(reset.getAttribute("href")).toBe("/search?q=zzz");
  });

  it("says to slow down when searching too fast", async () => {
    mockSearchPeople.mockResolvedValue({ ok: false, code: "rate_limited", message: "Too many." });
    renderAt("q=ada");
    expect((await screen.findByRole("alert")).textContent).toBe(
      "You're searching too fast. Try again in a moment.",
    );
  });

  it("offers to try again after another error", async () => {
    mockSearchPeople.mockResolvedValueOnce({ ok: false, code: "internal_error", message: "Oops." });
    renderAt("q=ada");
    expect((await screen.findByRole("alert")).textContent).toBe("We couldn't load the results.");
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("link", { name: "Ada Rahman" })).toBeTruthy();
    expect(mockSearchPeople).toHaveBeenCalledTimes(2);
  });

  describe("search by meaning (AI-5)", () => {
    const meaningRow: PersonSearchResult = {
      ...ada,
      userId: "0b8f4c1e-0000-4000-8000-000000000002",
      fullName: "Bina Sultana",
      matchedOn: "meaning",
    };
    const hybridLine = "Results include researchers whose work matches the meaning of your search.";
    const fallbackText = "Smart search is unavailable right now. Showing keyword matches.";

    it("says hybrid results include matches by meaning and tags only those rows", async () => {
      mockSearchPeople.mockResolvedValue({ ok: true, data: resultPage({ items: [ada, meaningRow], total: 2 }) });
      renderAt("q=robots+that+help+the+elderly");
      const rows = within(await screen.findByRole("list", { name: "People" })).getAllByRole("listitem");
      expect(screen.getByText(hybridLine)).toBeTruthy();
      expect(within(rows[0]).queryByText("Related by meaning")).toBeNull();
      expect(within(rows[1]).getByText("Related by meaning")).toBeTruthy();
      expect(screen.queryByText(fallbackText)).toBeNull();
    });

    it("shows the fallback banner, without a retry, when the AI service did not answer", async () => {
      mockSearchPeople.mockResolvedValue({ ok: true, data: resultPage({ mode: "keyword_fallback" }) });
      renderAt("q=robots");
      const banner = await screen.findByText(fallbackText);
      expect(banner.getAttribute("role")).toBe("status");
      expect(screen.queryByText(hybridLine)).toBeNull();
      expect(screen.queryByRole("button", { name: /try again|retry/i })).toBeNull();
      expect(mockSearchPeople).toHaveBeenCalledTimes(1);
    });

    it("shows neither the line nor the banner for a keyword search", async () => {
      mockSearchPeople.mockResolvedValue({ ok: true, data: resultPage({ mode: "keyword" }) });
      renderAt("q=robots&mode=keyword");
      await screen.findByRole("link", { name: "Ada Rahman" });
      expect(screen.queryByText(hybridLine)).toBeNull();
      expect(screen.queryByText(fallbackText)).toBeNull();
    });

    it("suggests other words when a hybrid search finds nobody", async () => {
      mockSearchPeople.mockResolvedValue({ ok: true, data: resultPage({ items: [], total: 0 }) });
      renderAt("q=quantum+gardening");
      expect(await screen.findByText("No researchers found.")).toBeTruthy();
      expect(screen.getByText("Try describing the research in different words.")).toBeTruthy();
      expect(screen.queryByText(hybridLine)).toBeNull();
    });

    it("keeps the fallback banner above an empty keyword result", async () => {
      mockSearchPeople.mockResolvedValue({
        ok: true,
        data: resultPage({ mode: "keyword_fallback", items: [], total: 0 }),
      });
      renderAt("q=zzz");
      expect(await screen.findByText(fallbackText)).toBeTruthy();
      expect(screen.getByText("No researchers match “zzz”.")).toBeTruthy();
    });

    it("searches by meaning by default", async () => {
      renderAt("q=ada");
      await screen.findByRole("link", { name: "Ada Rahman" });
      const meaning = screen.getByRole("radio", { name: "By meaning (AI)" });
      expect(meaning.getAttribute("aria-checked")).toBe("true");
      expect(screen.getByRole("radio", { name: "Keyword" }).getAttribute("aria-checked")).toBe("false");
      expect(mockSearchPeople).toHaveBeenLastCalledWith(expect.objectContaining({ keywordOnly: false }));
    });

    it("puts Keyword in the URL, keeps the filters and starts again at page 1", async () => {
      mockSearchPeople.mockResolvedValue({ ok: true, data: resultPage({ total: 60, page: 2 }) });
      renderAt("q=ada&institution=BUET&page=2");
      await screen.findByRole("link", { name: "Ada Rahman" });
      fireEvent.click(screen.getByRole("radio", { name: "Keyword" }));
      expect(mockPush).toHaveBeenLastCalledWith("/search?q=ada&institution=BUET&mode=keyword");
    });

    it("restores Keyword from the URL and goes back to meaning", async () => {
      mockSearchPeople.mockResolvedValue({ ok: true, data: resultPage({ mode: "keyword", total: 60, page: 2 }) });
      renderAt("q=ada&page=2&mode=keyword");
      await screen.findByRole("link", { name: "Ada Rahman" });
      expect(mockSearchPeople).toHaveBeenCalledWith(expect.objectContaining({ keywordOnly: true, page: 2 }));
      expect(screen.getByRole("radio", { name: "Keyword" }).getAttribute("aria-checked")).toBe("true");
      // Page links and reset keep the mode.
      const nav = screen.getByRole("navigation", { name: "Pages" });
      expect(within(nav).getByRole("link", { name: "Next" }).getAttribute("href")).toBe(
        "/search?q=ada&page=3&mode=keyword",
      );
      fireEvent.click(screen.getByRole("radio", { name: "By meaning (AI)" }));
      expect(mockPush).toHaveBeenLastCalledWith("/search?q=ada");
    });

    it("switches mode with the arrow keys", async () => {
      renderAt("q=ada");
      await screen.findByRole("link", { name: "Ada Rahman" });
      fireEvent.keyDown(screen.getByRole("radio", { name: "By meaning (AI)" }), { key: "ArrowLeft" });
      expect(mockPush).toHaveBeenLastCalledWith("/search?q=ada&mode=keyword");
    });

    it("does nothing when the selected mode is clicked again", async () => {
      renderAt("q=ada");
      await screen.findByRole("link", { name: "Ada Rahman" });
      fireEvent.click(screen.getByRole("radio", { name: "By meaning (AI)" }));
      expect(mockPush).not.toHaveBeenCalled();
    });

    it("searches a 300-character query by meaning but refuses 301", async () => {
      renderAt(`q=${"a".repeat(300)}`);
      await screen.findByRole("link", { name: "Ada Rahman" });
      expect(mockSearchPeople).toHaveBeenCalledTimes(1);

      mockSearchPeople.mockClear();
      renderAt(`q=${"a".repeat(301)}`);
      expect(screen.getByText("Searches take up to 300 characters. Shorten your search.")).toBeTruthy();
      expect(mockSearchPeople).not.toHaveBeenCalled();
    });

    it("does not send a keyword search over 100 characters", () => {
      renderAt(`q=${"a".repeat(101)}&mode=keyword`);
      expect(
        screen.getByText("Keyword search takes up to 100 characters. Shorten your search, or search by meaning."),
      ).toBeTruthy();
      expect(mockSearchPeople).not.toHaveBeenCalled();
    });

    it("counts the characters being typed near the limit", async () => {
      renderAt("q=ada");
      await screen.findByRole("link", { name: "Ada Rahman" });
      act(() => setSearchDraftLength(249));
      expect(screen.queryByText(/characters$/)).toBeNull();
      act(() => setSearchDraftLength(250));
      expect(screen.getByText("250 / 300 characters")).toBeTruthy();
      act(() => setSearchDraftLength(300));
      expect(screen.getByText("300 / 300 characters")).toBeTruthy();
    });

    it("counts against 100 from 80 characters in keyword mode", async () => {
      mockSearchPeople.mockResolvedValue({ ok: true, data: resultPage({ mode: "keyword" }) });
      renderAt("q=ada&mode=keyword");
      await screen.findByRole("link", { name: "Ada Rahman" });
      act(() => setSearchDraftLength(79));
      expect(screen.queryByText(/characters$/)).toBeNull();
      act(() => setSearchDraftLength(80));
      expect(screen.getByText("80 / 100 characters")).toBeTruthy();
    });
  });
});
