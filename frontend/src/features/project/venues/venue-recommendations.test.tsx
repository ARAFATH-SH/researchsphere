import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { now } from "@/features/event/test-venues";
import { processingRetryMs } from "@/features/recommendation/recommendations-provider";

import { getRecommendedVenues } from "../api";
import { failed, projectID, recommendedJournal, recommendedVenue, venueNotice, venueRecs } from "../test-projects";
import {
  emptyText,
  journalsHeading,
  needsAbstractHeading,
  needsAbstractText,
  processingText,
  VenueRecommendations,
  venuesHeading,
  venuesSubtitle,
} from "./venue-recommendations";

jest.mock("../api", () => ({ ...jest.requireActual("../api"), getRecommendedVenues: jest.fn() }));

const mockVenues = jest.mocked(getRecommendedVenues);
let consoleError: jest.SpyInstance;

beforeEach(() => {
  consoleError = jest.spyOn(console, "error");
});
afterEach(() => {
  // No act() or other React warnings.
  expect(consoleError).not.toHaveBeenCalled();
  consoleError.mockRestore();
  jest.useRealTimers();
  jest.resetAllMocks();
});

type CardProps = Partial<Parameters<typeof VenueRecommendations>[0]>;

function card(props: CardProps = {}) {
  return (
    <VenueRecommendations
      projectId={projectID}
      abstract="We collect passive screen-use signals."
      canEditAbstract
      onAddAbstract={() => {}}
      now={now}
      userZone="Asia/Dhaka"
      {...props}
    />
  );
}

async function renderCard(props: CardProps = {}) {
  const view = render(card(props));
  await screen.findByRole("region", { busy: false });
  return view;
}

function notice() {
  return screen.getByTestId("venue-notice");
}

describe("VenueRecommendations", () => {
  it("lists up to 5 open venues with score tile, name, type, deadline and countdown, linking to each venue", async () => {
    mockVenues.mockResolvedValue(venueRecs("ready", [1, 2, 3, 4, 5].map((n) => recommendedVenue(n))));
    await renderCard();
    expect(mockVenues).toHaveBeenCalledWith(projectID);
    const region = screen.getByRole("region", { name: venuesHeading });
    expect(within(region).getByText(venuesSubtitle)).toBeTruthy();

    const links = within(within(region).getByRole("list", { name: "Recommended venues" })).getAllByRole("link");
    expect(links).toHaveLength(5);
    const first = links[0];
    expect(first.getAttribute("href")).toBe(`/events/${recommendedVenue(1).id}`);
    expect(first.textContent).toContain("Conference on Digital Health 1");
    expect(first.textContent).toContain("(CDH1)");
    expect(within(first).getByText("Conference")).toBeTruthy();
    // The deadline as the venue states it (AoE) and in the reader's zone, with the countdown.
    expect(within(first).getByText("15 Feb 2027, 23:59 AoE = 16 Feb, 17:59 Dhaka").tagName).toBe("TIME");
    const countdown = within(first).getByText("in 15 days");
    expect(countdown.getAttribute("data-tone")).toBe("normal");
    expect(countdown.className).toContain("text-rs-heading");
    // The 44px score tile: the number for the eye, a label for screen readers.
    const tile = within(first).getByTestId("venue-score");
    expect(tile.className).toContain("w-11");
    expect(tile.className).toContain("bg-rs-accent-soft");
    expect(within(tile).getByText("89%").getAttribute("aria-hidden")).toBe("true");
    expect(within(tile).getByText("89 percent match").className).toContain("sr-only");
    expect(screen.queryByText(/% match$/)).toBeNull();

    expect(screen.queryByRole("heading", { name: journalsHeading })).toBeNull();
    expect(notice().textContent).toBe(venueNotice);
  });

  it("shows a deadline under a week away in red, with its words", async () => {
    const soon = recommendedVenue(1, {
      submissionDeadline: { local: "2027-02-03T23:59:59", at: "2027-02-04T11:59:59Z" },
      daysLeft: 3,
    });
    mockVenues.mockResolvedValue(venueRecs("ready", [soon]));
    await renderCard();
    const countdown = screen.getByText("in 3 days");
    expect(countdown.getAttribute("data-tone")).toBe("urgent");
    expect(countdown.className).toContain("text-rs-danger");
  });

  it("lists the journals that fit under the venues, with Open submission in place of a deadline", async () => {
    mockVenues.mockResolvedValue(
      venueRecs("ready", [recommendedVenue(1)], [recommendedJournal(1), recommendedJournal(2), recommendedJournal(3)]),
    );
    await renderCard();
    expect(screen.getByRole("heading", { level: 3, name: journalsHeading })).toBeTruthy();
    const journals = within(screen.getByRole("list", { name: journalsHeading })).getAllByRole("link");
    expect(journals).toHaveLength(3);
    expect(journals[0].getAttribute("href")).toBe(`/events/${recommendedJournal(1).id}`);
    expect(within(journals[0]).getByText("Open submission")).toBeTruthy();
    expect(within(journals[0]).getByText("Journal")).toBeTruthy();
    expect(within(journals[0]).queryByText(/Deadline/)).toBeNull();
    expect(within(journals[0]).getByText("69 percent match")).toBeTruthy();
    expect(notice().textContent).toBe(venueNotice);
  });

  it("says when no upcoming venue matches, still listing journals and the notice", async () => {
    mockVenues.mockResolvedValue(venueRecs("ready", [], [recommendedJournal(1)]));
    await renderCard();
    expect(screen.getByText(emptyText)).toBeTruthy();
    expect(screen.queryByRole("list", { name: "Recommended venues" })).toBeNull();
    expect(screen.getByRole("list", { name: journalsHeading })).toBeTruthy();
    expect(notice().textContent).toBe(venueNotice);
  });

  it("asks an owner or editor for an abstract (AI-6.3)", async () => {
    mockVenues.mockResolvedValue(venueRecs("needs_abstract"));
    const onAdd = jest.fn();
    render(card({ abstract: null, onAddAbstract: onAdd, addAbstractId: "add-abstract" }));
    const region = await screen.findByRole("region", { name: needsAbstractHeading });
    expect(region.className).toContain("border-dashed");
    expect(within(region).getByText(needsAbstractText)).toBeTruthy();
    expect(screen.queryByRole("heading", { name: venuesHeading })).toBeNull();
    const button = within(region).getByRole("button", { name: "Add abstract" });
    expect(button.id).toBe("add-abstract");
    fireEvent.click(button);
    expect(onAdd).toHaveBeenCalledTimes(1);
    expect(within(region).getByTestId("venue-notice").textContent).toBe(venueNotice);
  });

  it("shows a viewer the request for an abstract without the button", async () => {
    mockVenues.mockResolvedValue(venueRecs("needs_abstract"));
    render(card({ abstract: null, canEditAbstract: false }));
    const region = await screen.findByRole("region", { name: needsAbstractHeading });
    expect(within(region).queryByRole("button")).toBeNull();
    expect(notice().textContent).toBe(venueNotice);
  });

  it("says venues are being found, and asks quietly once more after a minute, never again", async () => {
    jest.useFakeTimers();
    mockVenues.mockResolvedValue(venueRecs("processing"));
    render(card());
    expect((await screen.findByRole("status")).textContent).toBe(processingText);
    expect(notice().textContent).toBe(venueNotice);
    expect(mockVenues).toHaveBeenCalledTimes(1);
    await act(async () => {
      jest.advanceTimersByTime(processingRetryMs - 1);
    });
    expect(mockVenues).toHaveBeenCalledTimes(1);
    await act(async () => {
      jest.advanceTimersByTime(1);
    });
    expect(mockVenues).toHaveBeenCalledTimes(2);
    // Still processing: the text stays, with no loading flash, and no third request.
    expect(screen.getByRole("status").textContent).toBe(processingText);
    await act(async () => {
      jest.advanceTimersByTime(processingRetryMs * 3);
    });
    expect(mockVenues).toHaveBeenCalledTimes(2);
  });

  it("shows the venues the retry finds", async () => {
    jest.useFakeTimers();
    mockVenues.mockResolvedValueOnce(venueRecs("processing")).mockResolvedValueOnce(venueRecs("ready", [recommendedVenue(1)]));
    render(card());
    await screen.findByText(processingText);
    await act(async () => {
      jest.advanceTimersByTime(processingRetryMs);
    });
    expect(await screen.findByRole("link", { name: /Conference on Digital Health 1/ })).toBeTruthy();
  });

  it("offers a retry after an error", async () => {
    mockVenues.mockResolvedValueOnce(failed).mockResolvedValueOnce(venueRecs("ready", [recommendedVenue(1)]));
    render(card());
    expect((await screen.findByRole("alert")).textContent).toBe("Couldn’t load venue suggestions.");
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("link", { name: /Conference on Digital Health 1/ })).toBeTruthy();
    expect(notice().textContent).toBe(venueNotice);
  });

  it("shows nothing when the API refuses (not a member)", async () => {
    mockVenues.mockResolvedValue({ ok: false, code: "not_found", message: "Not found." });
    const { container } = render(card());
    await act(async () => {});
    expect(container.innerHTML).toBe("");
  });

  it("reads the venues again when the abstract changes", async () => {
    mockVenues.mockResolvedValueOnce(venueRecs("needs_abstract")).mockResolvedValueOnce(venueRecs("processing"));
    const { rerender } = render(card({ abstract: null }));
    await screen.findByRole("region", { name: needsAbstractHeading });
    rerender(card({ abstract: "A new abstract." }));
    expect((await screen.findByRole("status")).textContent).toBe(processingText);
    expect(mockVenues).toHaveBeenCalledTimes(2);
    // The same abstract again does not ask again.
    rerender(card({ abstract: "A new abstract." }));
    await act(async () => {});
    expect(mockVenues).toHaveBeenCalledTimes(2);
  });

  it("lets a row's text wrap on a narrow screen (_m design)", async () => {
    mockVenues.mockResolvedValue(venueRecs("ready", [recommendedVenue(1)]));
    await renderCard();
    const link = screen.getByRole("link", { name: /Conference on Digital Health 1/ });
    const text = link.lastElementChild as HTMLElement;
    expect(text.className).toContain("min-w-0");
    expect((text.lastElementChild as HTMLElement).className).toContain("flex-wrap");
    expect(screen.getByTestId("venue-score").className).toContain("flex-none");
  });

  it("drops the card's border in the workspace sheet", async () => {
    mockVenues.mockResolvedValue(venueRecs("ready", [recommendedVenue(1)]));
    await renderCard({ frame: "plain" });
    expect(screen.getByRole("region", { name: venuesHeading }).className).not.toContain("border");
  });
});
