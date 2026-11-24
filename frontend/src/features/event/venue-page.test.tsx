import { fireEvent, render, screen } from "@testing-library/react";

import { getVenue } from "./api";
import { now, venue } from "./test-venues";
import { VenuePage } from "./venue-page";

jest.mock("./api");
const mockGet = jest.mocked(getVenue);

beforeEach(() => mockGet.mockReset());

function renderPage(id = venue().id) {
  render(<VenuePage venueId={id} now={now} userZone="Asia/Dhaka" />);
}

describe("VenuePage", () => {
  it("shows every date in both zones, the fields, the place and the link", async () => {
    mockGet.mockResolvedValue({
      ok: true,
      data: venue({
        abstractDeadline: { local: "2027-02-08T23:59:59", at: "2027-02-09T11:59:59Z" },
        notificationAt: { local: "2027-03-01T12:00:00", at: "2027-03-02T00:00:00Z" },
        source: "ccfddl",
      }),
    });
    renderPage();
    expect(await screen.findByRole("heading", { level: 1, name: "International Conference on Digital Health (ICDH)" })).toBeTruthy();
    const text = document.body.textContent ?? "";
    expect(text).toContain("Abstract8 Feb 2027, 23:59 AoE = 9 Feb, 17:59 Dhaka · in 8 days");
    expect(text).toContain("Deadline15 Feb 2027, 23:59 AoE = 16 Feb, 17:59 Dhaka · in 15 days");
    expect(text).toContain("Notification1 Mar 2027, 12:00 AoE = 2 Mar, 06:00 Dhaka");
    expect(text).toContain("AttendanceHybrid");
    expect(text).toContain("LocationSingapore, Singapore · hybrid");
    expect(text).toContain("Dates12–14 Mar 2027");
    expect(text).toContain("SourceImported from ccf-deadlines");
    expect(screen.getByText("Health informatics")).toBeTruthy();
    const link = screen.getByRole("link", { name: /Call for papers/ });
    expect(link.getAttribute("rel")).toBe("noopener noreferrer");
    expect(link.getAttribute("target")).toBe("_blank");
    expect(screen.queryByText(/Deadline passed/)).toBeNull();
  });

  it("shows the scope as text with its line breaks, never as HTML", async () => {
    mockGet.mockResolvedValue({ ok: true, data: venue() });
    renderPage();
    const scope = await screen.findByText(/Digital tools for health/);
    expect(scope.textContent).toBe("Digital tools for health.\n\nTopics include <b>sensing</b>.");
    expect(scope.querySelector("b")).toBeNull();
    expect(scope.className).toContain("whitespace-pre-wrap");
  });

  it("says when the deadline has passed", async () => {
    mockGet.mockResolvedValue({ ok: true, data: venue({ past: true }) });
    renderPage();
    expect(await screen.findByText("Deadline passed: submissions to this venue have closed.")).toBeTruthy();
  });

  it("downloads the deadline as an .ics file in UTC", async () => {
    mockGet.mockResolvedValue({ ok: true, data: venue() });
    const blobs: Blob[] = [];
    URL.createObjectURL = jest.fn((b: Blob) => {
      blobs.push(b);
      return "blob:venue";
    });
    URL.revokeObjectURL = jest.fn();
    const click = jest.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "Add to calendar" }));
    expect(click).toHaveBeenCalled();
    expect(blobs[0].type).toBe("text/calendar;charset=utf-8");
    const text = await new Promise<string>((resolve) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result));
      reader.readAsText(blobs[0]);
    });
    expect(text).toContain("DTSTART:20270216T115959Z");
    expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:venue");
    expect(click.mock.contexts[0]).toMatchObject({ download: "icdh-deadline.ics", href: "blob:venue" });
    click.mockRestore();
  });

  it("offers no calendar for a rolling journal", async () => {
    mockGet.mockResolvedValue({
      ok: true,
      data: venue({ rolling: true, type: "journal", submissionDeadline: null, timeZone: null, attendance: null }),
    });
    renderPage();
    expect(await screen.findByText("Open submission: no deadline")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Add to calendar" })).toBeNull();
  });

  it("is not found when the venue is removed or unknown", async () => {
    mockGet.mockResolvedValue({ ok: false, code: "not_found", message: "This venue does not exist." });
    renderPage("0192f0a0-0000-7000-8000-00000000dead");
    expect(await screen.findByRole("heading", { name: "Venue not found" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Go to Events" }).getAttribute("href")).toBe("/events");
  });

  it("offers a retry when loading fails", async () => {
    mockGet.mockResolvedValueOnce({ ok: false, code: "internal_error", message: "x" }).mockResolvedValue({ ok: true, data: venue() });
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("heading", { level: 1, name: /Digital Health/ })).toBeTruthy();
  });
});
