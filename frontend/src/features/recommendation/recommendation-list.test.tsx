import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { useSession } from "@/features/auth/session";
import { type ConnectionRequest, sendConnectionRequest } from "@/features/social/api";

import { getPeopleRecommendations } from "./api";
import { RecommendationList, type RecommendationVariant, scoreInfoText } from "./recommendation-list";
import { processingRetryMs, RecommendationsProvider } from "./recommendations-provider";
import { failed, needsProfile, people, processing, ready, recommended } from "./test-recommendations";

jest.mock("./api", () => ({ ...jest.requireActual("./api"), getPeopleRecommendations: jest.fn() }));
jest.mock("@/features/social/api");
jest.mock("@/features/notification/badges");
jest.mock("@/features/auth/session");

const mockGet = jest.mocked(getPeopleRecommendations);
const mockSend = jest.mocked(sendConnectionRequest);
const adaID = "0b8f4c1e-0000-4000-8000-000000000001";

let consoleError: jest.SpyInstance;

beforeEach(() => {
  consoleError = jest.spyOn(console, "error");
  jest.mocked(useSession).mockReturnValue({
    status: "signedIn",
    user: { id: adaID, email: "ada@example.org", fullName: "Ada Lovelace", role: "researcher" },
  });
});
afterEach(() => {
  // No act() or other React warnings.
  expect(consoleError).not.toHaveBeenCalled();
  consoleError.mockRestore();
  jest.useRealTimers();
  jest.resetAllMocks();
});

function renderList(variant: RecommendationVariant = "home", limit = 3) {
  return render(
    <RecommendationsProvider>
      <RecommendationList limit={limit} variant={variant} />
    </RecommendationsProvider>,
  );
}

function rows() {
  return within(screen.getByRole("list", { name: "Recommended researchers" }))
    .getAllByRole("listitem")
    .filter((li) => li.parentElement?.getAttribute("aria-label") === "Recommended researchers");
}

describe("RecommendationList", () => {
  it("shows the ready rows: photo, name to the profile, headline, score, shared chips and Connect", async () => {
    const first = recommended(1, { score: 97, shared: ["eye tracking", "HCI", "Python"], photoUrl: "/api/v1/media/p1" });
    mockGet.mockResolvedValue(ready([first, recommended(2, { headline: null, institution: "NSU" }), recommended(3)]));
    renderList();

    await screen.findByRole("link", { name: "Researcher 1" });
    expect(screen.getByRole("heading", { level: 2, name: "Recommended collaborators" })).toBeTruthy();
    const [row1, row2] = rows();
    expect(within(row1).getByRole("link", { name: "Researcher 1" }).getAttribute("href")).toBe(`/profile/${first.userId}`);
    expect(row1.querySelector('img[src="/api/v1/media/p1"]')).toBeTruthy();
    expect(within(row1).getByText("Headline 1")).toBeTruthy();
    // The badge reads as words; the "97% match" text is for sight only.
    expect(within(row1).getByText("97 percent match")).toBeTruthy();
    expect(within(row1).getByText("97% match").getAttribute("aria-hidden")).toBe("true");
    // The shared interests and skills are a list.
    const chips = within(row1).getByRole("list", { name: "Shared interests and skills" });
    expect(within(chips).getAllByRole("listitem").map((li) => li.textContent)).toEqual(["eye tracking", "HCI", "Python"]);
    expect(within(row1).getByText("Shared:")).toBeTruthy();
    expect(within(row1).getByRole("button", { name: "Connect with Researcher 1" })).toBeTruthy();
    // No headline: the institution instead; no shared items: no chips.
    expect(within(row2).getByText("NSU")).toBeTruthy();
    expect(within(row2).queryByRole("list", { name: "Shared interests and skills" })).toBeNull();
    expect(screen.getByText("AI")).toBeTruthy();
  });

  it("shows only limit rows and links to all of them with the real count", async () => {
    mockGet.mockResolvedValue(ready(people(1, 2, 3, 4, 5, 6, 7)));
    renderList();

    await screen.findByRole("link", { name: "Researcher 1" });
    expect(rows()).toHaveLength(3);
    const link = screen.getByRole("link", { name: "See all 7 recommendations" });
    expect(link.getAttribute("href")).toBe("/network/recommendations");
  });

  it("hides the link when there is nothing more to see", async () => {
    mockGet.mockResolvedValue(ready(people(1, 2, 3)));
    renderList();

    await screen.findByRole("link", { name: "Researcher 3" });
    expect(screen.queryByRole("link", { name: /See all/ })).toBeNull();
  });

  it("says so when everyone is left out", async () => {
    mockGet.mockResolvedValue(ready([]));
    renderList();

    expect(await screen.findByText("No new recommendations right now.")).toBeTruthy();
    expect(screen.queryByRole("list", { name: "Recommended researchers" })).toBeNull();
    expect(screen.queryByRole("link", { name: /See all/ })).toBeNull();
  });

  it("asks for the missing profile fields in the dashed card", async () => {
    mockGet.mockResolvedValue(needsProfile);
    renderList();

    expect(await screen.findByRole("heading", { name: "Get better recommendations" })).toBeTruthy();
    expect(screen.getByText(/Add research interests, skills or a short bio/)).toBeTruthy();
    expect(screen.getByRole("link", { name: "Add interests" }).getAttribute("href")).toBe("/profile?edit=interests");
    expect(screen.queryByRole("heading", { name: "Recommended collaborators" })).toBeNull();
  });

  it("names only what is missing and links to its editor", async () => {
    mockGet.mockResolvedValue({ ok: true, data: { state: "needs_profile", missing: ["bio"], items: [] } });
    renderList();

    expect(await screen.findByText(/Add a short bio and/)).toBeTruthy();
    expect(screen.getByRole("link", { name: "Add a bio" }).getAttribute("href")).toBe("/profile?edit=about");
  });

  it("says the list is being prepared and asks once more, quietly, after a minute", async () => {
    jest.useFakeTimers();
    mockGet.mockResolvedValue(processing);
    renderList();

    expect(await screen.findByText("We’re preparing your recommendations. Check back in a minute.")).toBeTruthy();
    expect(mockGet).toHaveBeenCalledTimes(1);

    await act(async () => jest.advanceTimersByTime(processingRetryMs - 1000));
    expect(mockGet).toHaveBeenCalledTimes(1);

    // Still processing after the retry: the message stays, and there is no third request.
    await act(async () => jest.advanceTimersByTime(1000));
    expect(mockGet).toHaveBeenCalledTimes(2);
    expect(screen.queryByText("Loading recommendations…")).toBeNull();
    await act(async () => jest.advanceTimersByTime(processingRetryMs * 3));
    expect(mockGet).toHaveBeenCalledTimes(2);
    expect(screen.getByRole("status").textContent).toMatch(/preparing your recommendations/);
  });

  it("shows the list when the retry finds it ready", async () => {
    jest.useFakeTimers();
    mockGet.mockResolvedValueOnce(processing).mockResolvedValueOnce(ready(people(1)));
    renderList();

    await screen.findByRole("status");
    await act(async () => jest.advanceTimersByTime(processingRetryMs));
    expect(screen.getByRole("link", { name: "Researcher 1" })).toBeTruthy();
  });

  it("offers a retry after an error", async () => {
    mockGet.mockResolvedValueOnce(failed).mockResolvedValueOnce(ready(people(1)));
    renderList();

    expect((await screen.findByRole("alert")).textContent).toBe("Couldn’t load recommendations.");
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("link", { name: "Researcher 1" })).toBeTruthy();
    expect(mockGet).toHaveBeenCalledTimes(2);
  });

  it("explains the score behind the info button", async () => {
    mockGet.mockResolvedValue(ready(people(1)));
    renderList();

    const info = await screen.findByRole("button", { name: "About the match score" });
    const text = screen.getByText(scoreInfoText);
    expect(info.getAttribute("aria-expanded")).toBe("false");
    expect(text.hidden).toBe(true);

    fireEvent.click(info);
    expect(info.getAttribute("aria-expanded")).toBe("true");
    expect(text.hidden).toBe(false);
    expect(text.id).toBe(info.getAttribute("aria-controls"));
    expect(scoreInfoText).toBe(
      "How closely your research profile matches theirs, based on your interests, skills, bio and publications. Not based on popularity.",
    );

    fireEvent.click(info);
    expect(text.hidden).toBe(true);
  });

  it("keeps a row it connected with in place, showing Pending, and fills the rest from a refetch", async () => {
    const [p1, p2, p3, p4, p5] = people(1, 2, 3, 4, 5);
    mockGet
      .mockResolvedValueOnce(ready([p1, p2, p3, p4]))
      // The API leaves out the pending request; a row shown already is not shown twice.
      .mockResolvedValueOnce(ready([p1, p3, p4, p5]));
    mockSend.mockResolvedValue({
      ok: true,
      data: { requestId: "7a1d2c3e-0000-4000-8000-000000000999", createdAt: "2026-11-15T10:00:00Z" } as ConnectionRequest,
    });
    renderList();

    fireEvent.click(await screen.findByRole("button", { name: "Connect with Researcher 2" }));
    expect(await screen.findByRole("button", { name: "Pending request to Researcher 2" })).toBeTruthy();
    expect(mockSend).toHaveBeenCalledWith(p2.userId);
    expect(mockGet).toHaveBeenCalledTimes(2);

    // Researcher 2 stays second with Pending; the others come from the refetch.
    await screen.findByRole("link", { name: "See all 5 recommendations" });
    expect(rows().map((r) => within(r).getAllByRole("link")[0].textContent)).toEqual([
      "Researcher 1",
      "Researcher 2",
      "Researcher 3",
    ]);
    expect(within(rows()[1]).getByRole("button", { name: "Pending request to Researcher 2" })).toBeTruthy();
    expect(screen.getAllByRole("link", { name: "Researcher 1" })).toHaveLength(1);
  });

  it("puts the Connect button under the text on a phone in the wide rows", async () => {
    mockGet.mockResolvedValue(ready(people(1)));
    renderList("network");

    await screen.findByRole("link", { name: "Researcher 1" });
    const connect = screen.getByTestId("recommendation-connect");
    for (const c of ["max-[640px]:basis-full", "max-[640px]:pl-[60px]"]) expect(connect.className).toContain(c);
    // The score bar of the wide rows is hidden from screen readers.
    expect(screen.getByText("99 percent match")).toBeTruthy();
    expect(screen.getByRole("heading", { level: 3, name: "Researchers to work with" })).toBeTruthy();
    expect(screen.getByText(/never by citations, papers or connection counts/)).toBeTruthy();
  });

  it("asks once for every card on the page", async () => {
    mockGet.mockResolvedValue(ready(people(1, 2, 3, 4)));
    render(
      <RecommendationsProvider>
        <RecommendationList limit={3} variant="home" />
        <RecommendationList limit={3} variant="profile" />
      </RecommendationsProvider>,
    );

    expect(await screen.findByRole("heading", { name: "Connect with researchers" })).toBeTruthy();
    expect(screen.getAllByRole("link", { name: "Researcher 1" })).toHaveLength(2);
    expect(mockGet).toHaveBeenCalledTimes(1);
  });

  it.each([
    ["needs a profile", needsProfile],
    ["is processing", processing],
    ["has no one", ready([])],
    ["fails", failed],
  ])("leaves the profile card out when the viewer %s", async (_, answer) => {
    mockGet.mockResolvedValue(answer);
    const { container } = renderList("profile");

    await act(async () => {});
    expect(mockGet).toHaveBeenCalledTimes(1);
    expect(container.textContent).toBe("");
  });

  it("renders nothing outside a provider", () => {
    const { container } = render(<RecommendationList limit={3} variant="home" />);
    expect(container.textContent).toBe("");
    expect(mockGet).not.toHaveBeenCalled();
  });
});
