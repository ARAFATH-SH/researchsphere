import "@/test/dialog-polyfill";

import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { getVenue, listResearchFields, listVenues } from "./api";
import { EventsPage } from "./events-page";
import { fields, journal, now, summary, venue, venuePage } from "./test-venues";

jest.mock("./api");
const mockList = jest.mocked(listVenues);
const mockFields = jest.mocked(listResearchFields);
const mockGet = jest.mocked(getVenue);
const mockPush = jest.fn();
const mockReplace = jest.fn();
let mockSearch = "";
jest.mock("next/navigation", () => ({
  useRouter: () => ({ push: mockPush, replace: mockReplace }),
  useSearchParams: () => new URLSearchParams(mockSearch),
}));

const day = 86_400_000;
const at = (ms: number) => new Date(now.getTime() + ms).toISOString();

const soon = summary({
  id: "0192f0a0-0000-7000-8000-000000000002",
  name: "Workshop on Explainable AI",
  shortName: null,
  type: "workshop",
  attendance: "in_person",
  city: "Dhaka",
  country: "BD",
  startsOn: "2027-02-20",
  endsOn: null,
  timeZone: "Asia/Dhaka",
  submissionDeadline: { local: "2027-02-04T23:59:00", at: at(3 * day + 18 * 3_600_000 - 60_000) },
});
const later = summary(); // 15 Feb AoE, 15 days away

beforeEach(() => {
  mockPush.mockReset();
  mockReplace.mockReset();
  mockList.mockReset().mockResolvedValue({ ok: true, data: venuePage([soon, later]) });
  mockFields.mockResolvedValue({ ok: true, data: fields });
  mockGet.mockReset().mockImplementation(async (id) => ({ ok: true, data: venue({ id }) }));
});

async function renderAt(search: string) {
  mockSearch = search;
  render(<EventsPage now={now} userZone="Asia/Dhaka" />);
  await screen.findByRole("list", { name: /Venues|Journals/ });
  // The side panel loads its venue's scope.
  await screen.findByText(/Digital tools for health/);
}

// rows are the list's own items, not the field chips inside them.
const rows = () => Array.from(screen.getByRole("list", { name: /Venues|Journals/ }).children) as HTMLElement[];

describe("EventsPage list", () => {
  it("shows the venues in the order the API gives, nearest deadline first", async () => {
    await renderAt("");
    const names = rows().map((r) => within(r).getAllByRole("link")[0].textContent);
    expect(names).toEqual(["Workshop on Explainable AI", "International Conference on Digital Health · ICDH"]);
    expect(mockList).toHaveBeenCalledWith(
      expect.objectContaining({ page: 1, pageSize: 20, rolling: false, includePast: false }),
    );
    expect(screen.getByText(/Sorted by nearest submission deadline/)).toBeTruthy();
  });

  it("shows each deadline in the venue's zone and the reader's, with a countdown", async () => {
    await renderAt("");
    const [first, second] = rows();
    expect(second.textContent).toContain("Submission deadline 15 Feb 2027, 23:59 AoE = 16 Feb, 17:59 Dhaka · in 15 days");
    // Same zone as the reader: one time.
    expect(first.textContent).toContain("4 Feb 2027, 23:59 Dhaka · in 3 days");
    expect(second.textContent).toContain("Singapore, Singapore · hybrid · 12–14 Mar 2027");
    expect(first.textContent).toContain("Dhaka, Bangladesh · 20 Feb 2027");
    expect(within(first).getByText("Workshop")).toBeTruthy();
    expect(within(second).getByText("Health informatics")).toBeTruthy();
  });

  it("colours the countdown red under 7 days only", async () => {
    await renderAt("");
    const [first, second] = rows();
    const urgent = within(first).getByText("in 3 days");
    expect(urgent.getAttribute("data-tone")).toBe("urgent");
    expect(urgent.className).toContain("text-rs-danger");
    const calm = within(second).getByText("in 15 days");
    expect(calm.getAttribute("data-tone")).toBe("normal");
    expect(calm.className).not.toContain("danger");
  });

  it("opens the official link in a new tab without an opener", async () => {
    await renderAt("");
    const link = within(rows()[1]).getByRole("link", { name: /Call for papers/ });
    expect(link.getAttribute("href")).toBe("https://icdh.example.org/cfp");
    expect(link.getAttribute("target")).toBe("_blank");
    expect(link.getAttribute("rel")).toBe("noopener noreferrer");
  });

  it("previews the chosen venue in the side panel and links to its page", async () => {
    await renderAt("");
    expect(mockGet).toHaveBeenCalledWith(soon.id);
    fireEvent.click(screen.getByRole("button", { name: `Details of ${later.name}` }));
    const panel = await screen.findByRole("region", { name: later.name });
    await within(panel).findByText(/Digital tools for health/);
    expect(mockGet).toHaveBeenLastCalledWith(later.id);
    expect(within(panel).getByRole("link", { name: "Open the full page" }).getAttribute("href")).toBe(`/events/${later.id}`);
  });

  it("pages through 20 at a time with the page in the URL", async () => {
    mockList.mockResolvedValue({ ok: true, data: venuePage([soon, later], { total: 45 }) });
    await renderAt("type=workshop");
    expect(screen.getByText("Page 1 of 3")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(mockPush).toHaveBeenCalledWith("/events?type=workshop&page=2");
  });

  it("says when nothing matches and clears the filters", async () => {
    mockList.mockResolvedValue({ ok: true, data: venuePage([]) });
    mockSearch = "type=journal&q=zzz";
    render(<EventsPage now={now} userZone="Asia/Dhaka" />);
    expect(await screen.findByText("No venues match these filters.")).toBeTruthy();
    fireEvent.click(screen.getAllByRole("button", { name: "Clear filters" })[0]);
    expect(mockReplace).toHaveBeenCalledWith("/events", { scroll: false });
  });
});

describe("EventsPage past deadlines", () => {
  it("hides past venues until asked, then shows them after the upcoming ones", async () => {
    await renderAt("");
    fireEvent.click(screen.getByRole("checkbox", { name: "Show past deadlines" }));
    expect(mockReplace).toHaveBeenCalledWith("/events?past=1", { scroll: false });
  });

  it("asks the API for past venues and marks them closed", async () => {
    const closed = summary({
      id: "0192f0a0-0000-7000-8000-000000000003",
      name: "Past Symposium",
      past: true,
      submissionDeadline: { local: "2027-01-28T23:59:59", at: at(-3 * day) },
    });
    mockList.mockResolvedValue({ ok: true, data: venuePage([soon, closed]) });
    await renderAt("past=1");
    expect(mockList).toHaveBeenCalledWith(expect.objectContaining({ includePast: true }));
    expect((screen.getByRole("checkbox", { name: "Show past deadlines" }) as HTMLInputElement).checked).toBe(true);
    const past = within(rows()[1]).getByText("closed 3 days ago");
    expect(past.getAttribute("data-tone")).toBe("past");
  });
});

describe("EventsPage filters in the URL", () => {
  it("writes the type with its facet counts", async () => {
    await renderAt("");
    const type = screen.getByRole("combobox", { name: "Type" });
    expect(within(type).getByRole("option", { name: "Conference (12)" })).toBeTruthy();
    fireEvent.change(type, { target: { value: "workshop" } });
    expect(mockReplace).toHaveBeenCalledWith("/events?type=workshop", { scroll: false });
  });

  it("writes several fields, in the list's order, with counts", async () => {
    await renderAt("field=other");
    expect(mockList).toHaveBeenCalledWith(expect.objectContaining({ fields: ["other"] }));
    fireEvent.click(screen.getByRole("button", { name: "Field Other" }));
    const group = screen.getByRole("group", { name: "Research fields" });
    fireEvent.click(within(group).getByRole("checkbox", { name: "Natural language processing (1)" }));
    expect(mockReplace).toHaveBeenCalledWith("/events?field=bangla_nlp&field=other", { scroll: false });
  });

  it("writes the deadline range and sends it with the reader's zone", async () => {
    await renderAt("from=2027-02-01");
    expect(mockList).toHaveBeenCalledWith(expect.objectContaining({ deadlineFrom: "2027-02-01", tz: "Asia/Dhaka" }));
    fireEvent.change(screen.getByLabelText("Deadline to"), { target: { value: "2027-03-31" } });
    expect(mockReplace).toHaveBeenCalledWith("/events?from=2027-02-01&to=2027-03-31", { scroll: false });
  });

  it("refuses a range that ends before it starts, without asking the API", async () => {
    mockSearch = "from=2027-03-01&to=2027-02-01";
    render(<EventsPage now={now} userZone="Asia/Dhaka" />);
    expect(await screen.findByText("The last day is before the first day.")).toBeTruthy();
    await act(async () => {});
    expect(mockList).not.toHaveBeenCalled();
  });

  it("searches once typing pauses", async () => {
    await renderAt("");
    jest.useFakeTimers();
    try {
      fireEvent.change(screen.getByRole("searchbox", { name: "Search" }), { target: { value: "health" } });
      expect(mockReplace).not.toHaveBeenCalled();
      act(() => jest.advanceTimersByTime(300));
      expect(mockReplace).toHaveBeenCalledWith("/events?q=health", { scroll: false });
    } finally {
      jest.useRealTimers();
    }
  });

  it("restores every filter from the URL", async () => {
    await renderAt("type=conference&from=2027-02-01&q=icdh");
    expect((screen.getByRole("combobox", { name: "Type" }) as HTMLSelectElement).value).toBe("conference");
    expect((screen.getByLabelText("Deadline from") as HTMLInputElement).value).toBe("2027-02-01");
    expect((screen.getByRole("searchbox", { name: "Search" }) as HTMLInputElement).value).toBe("icdh");
    expect(mockList).toHaveBeenCalledWith(expect.objectContaining({ types: ["conference"], q: "icdh" }));
  });
});

describe("EventsPage journals tab", () => {
  it("switches to rolling journals, never mixed into deadline order", async () => {
    await renderAt("type=journal");
    fireEvent.click(screen.getByRole("tab", { name: "Journals (open submission)" }));
    expect(mockPush).toHaveBeenCalledWith("/events?tab=journals&type=journal");
  });

  it("lists rolling journals by name without deadline filters", async () => {
    mockList.mockResolvedValue({ ok: true, data: venuePage([journal]) });
    mockGet.mockResolvedValue({ ok: true, data: venue({ ...journal }) });
    await renderAt("tab=journals");
    expect(mockList).toHaveBeenCalledWith(expect.objectContaining({ rolling: true, includePast: false, deadlineFrom: undefined }));
    expect(screen.getByRole("tab", { name: "Journals (open submission)" }).getAttribute("aria-selected")).toBe("true");
    expect(screen.queryByRole("checkbox", { name: "Show past deadlines" })).toBeNull();
    expect(screen.queryByLabelText("Deadline from")).toBeNull();
    expect(rows()[0].textContent).toContain("Open submission: no deadline");
    expect(within(rows()[0]).getByRole("link", { name: /ISSN portal record/ })).toBeTruthy();
  });
});

describe("EventsPage on a phone", () => {
  it("opens the filters in a sheet that traps focus and applies at once", async () => {
    await renderAt("type=workshop");
    const open = screen.getByRole("button", { name: "Filters (1)" });
    fireEvent.click(open);
    const sheet = screen.getByRole("dialog", { name: "Filters" });
    expect((sheet as HTMLDialogElement).open).toBe(true);
    // A modal <dialog> makes the rest of the page inert; focus starts inside it.
    expect(sheet.contains(document.activeElement)).toBe(true);
    fireEvent.click(within(sheet).getByRole("checkbox", { name: "Health informatics (3)" }));
    expect(mockReplace).toHaveBeenCalledWith("/events?type=workshop&field=health_informatics", { scroll: false });
    fireEvent.click(within(sheet).getByRole("button", { name: "Show 2 venues" }));
    expect(screen.queryByRole("dialog", { name: "Filters" })).toBeNull();
    expect(document.activeElement).toBe(open);
  });
});
