import "@/test/dialog-polyfill";

import { act, fireEvent, render, screen, within } from "@testing-library/react";

import {
  createVenue,
  getVenue,
  importVenues,
  listResearchFields,
  listVenues,
  removeVenue,
  updateVenue,
  type VenueImportResult,
} from "../api";
import { fields, summary, venue, venuePage } from "../test-venues";
import { AdminVenues } from "./admin-venues";

jest.mock("../api");
const mockList = jest.mocked(listVenues);
const mockGet = jest.mocked(getVenue);
const mockCreate = jest.mocked(createVenue);
const mockUpdate = jest.mocked(updateVenue);
const mockRemove = jest.mocked(removeVenue);
const mockImport = jest.mocked(importVenues);

const synced = summary({
  id: "0192f0a0-0000-7000-8000-000000000005",
  name: "ACL 2027",
  shortName: "ACL",
  source: "ccfddl",
  locked: true,
  fields: [fields[0]],
});

beforeEach(() => {
  jest.mocked(listResearchFields).mockResolvedValue({ ok: true, data: fields });
  mockList.mockReset().mockResolvedValue({ ok: true, data: venuePage([summary({ locked: false }), synced]) });
  mockGet.mockReset();
  mockCreate.mockReset();
  mockUpdate.mockReset();
  mockRemove.mockReset();
  mockImport.mockReset();
});

async function renderTable() {
  render(<AdminVenues />);
  return screen.findByRole("table", { name: /Venues/ });
}

const dialog = (name: string | RegExp) => screen.getByRole("dialog", { name });

describe("AdminVenues table", () => {
  it("is a real table with headers, source and the Locked badge", async () => {
    const table = await renderTable();
    expect(within(table).getAllByRole("columnheader").map((h) => h.textContent)).toEqual([
      "Name",
      "Type",
      "Field",
      "Deadline",
      "Source",
      "Actions",
    ]);
    const [, first, second] = within(table).getAllByRole("row");
    expect(within(first).getByText("Manual")).toBeTruthy();
    expect(within(first).queryByText("Locked")).toBeNull();
    expect(within(second).getByText("ccf-deadlines")).toBeTruthy();
    expect(within(second).getByText("Locked")).toBeTruthy();
    expect(within(first).getByText("15 Feb 2027, 23:59 AoE")).toBeTruthy();
    // Admins see past venues too by default.
    expect(mockList).toHaveBeenCalledWith(expect.objectContaining({ includePast: true, rolling: false }));
  });

  it("filters by type, field and rolling journals", async () => {
    await renderTable();
    fireEvent.change(screen.getByRole("combobox", { name: "Type" }), { target: { value: "journal" } });
    await act(async () => {});
    await act(async () => {});
    expect(mockList).toHaveBeenLastCalledWith(expect.objectContaining({ types: ["journal"], page: 1 }));
    fireEvent.change(screen.getByRole("combobox", { name: "Field" }), { target: { value: "other" } });
    await act(async () => {});
    await act(async () => {});
    expect(mockList).toHaveBeenLastCalledWith(expect.objectContaining({ fields: ["other"] }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Rolling journals" }));
    await act(async () => {});
    await act(async () => {});
    expect(mockList).toHaveBeenLastCalledWith(expect.objectContaining({ rolling: true, includePast: false }));
  });

  it("searches once typing pauses", async () => {
    await renderTable();
    jest.useFakeTimers();
    try {
      fireEvent.change(screen.getByRole("searchbox", { name: "Search venues" }), { target: { value: "acl" } });
      act(() => jest.advanceTimersByTime(300));
    } finally {
      jest.useRealTimers();
    }
    await act(async () => {});
    await act(async () => {});
    expect(mockList).toHaveBeenLastCalledWith(expect.objectContaining({ q: "acl" }));
  });
});

describe("AdminVenues add", () => {
  it("shows every rule a field breaks and sends nothing", async () => {
    await renderTable();
    fireEvent.click(screen.getByRole("button", { name: "+ Add venue" }));
    const d = dialog("Add a venue");
    fireEvent.change(within(d).getByLabelText("Official link"), { target: { value: "http://x.example.org" } });
    fireEvent.click(within(d).getByRole("button", { name: "Add venue" }));
    expect(within(d).getByText("Enter the venue's name.")).toBeTruthy();
    expect(within(d).getByText("Choose at least one research field.")).toBeTruthy();
    expect(within(d).getByText("Choose how the event takes place.")).toBeTruthy();
    expect(within(d).getByText("Enter the submission deadline.")).toBeTruthy();
    expect(within(d).getByText("The link must start with https://.")).toBeTruthy();
    expect(within(d).getByText("Describe the venue's scope.")).toBeTruthy();
    expect(document.activeElement).toBe(within(d).getByRole("textbox", { name: "Name" }));
    expect(mockCreate).not.toHaveBeenCalled();
  });

  it("shows the place only in person or hybrid, and rolling only for a journal", async () => {
    await renderTable();
    fireEvent.click(screen.getByRole("button", { name: "+ Add venue" }));
    const d = dialog("Add a venue");
    expect(within(d).queryByLabelText("City")).toBeNull();
    fireEvent.change(within(d).getByLabelText("Attendance"), { target: { value: "hybrid" } });
    expect(within(d).getByLabelText("City")).toBeTruthy();
    fireEvent.change(within(d).getByLabelText("Country code"), { target: { value: "bd" } });
    expect(within(d).getByText("Bangladesh")).toBeTruthy();
    fireEvent.change(within(d).getByLabelText("Attendance"), { target: { value: "online" } });
    expect(within(d).queryByLabelText("City")).toBeNull();
    expect(within(d).queryByRole("checkbox", { name: /rolling/ })).toBeNull();
    fireEvent.change(within(d).getByLabelText("Type"), { target: { value: "journal" } });
    expect(within(d).queryByLabelText("Attendance")).toBeNull();
    fireEvent.click(within(d).getByRole("checkbox", { name: /rolling/ }));
    expect(within(d).queryByLabelText("Submission deadline")).toBeNull();
    expect(within(d).queryByLabelText("Time zone of the deadlines")).toBeNull();
  });

  it("allows 5 fields at most and counts the scope", async () => {
    await renderTable();
    fireEvent.click(screen.getByRole("button", { name: "+ Add venue" }));
    const d = dialog("Add a venue");
    expect(within(d).getByText("0 of at most 5 chosen.")).toBeTruthy();
    fireEvent.change(within(d).getByLabelText("Scope"), { target: { value: "Twelve chars" } });
    expect(within(d).getByText("12 / 10000")).toBeTruthy();
  });

  it("creates the venue and puts a server refusal on its field", async () => {
    mockCreate
      .mockResolvedValueOnce({ ok: false, code: "bad_request", message: "The time zone must be AoE, UTC or an IANA name such as Asia/Dhaka.", field: "timeZone" })
      .mockResolvedValueOnce({ ok: true, data: venue({ name: "Graph Workshop" }) });
    await renderTable();
    fireEvent.click(screen.getByRole("button", { name: "+ Add venue" }));
    const d = dialog("Add a venue");
    fireEvent.change(within(d).getByLabelText("Name"), { target: { value: "Graph Workshop" } });
    fireEvent.click(within(d).getByRole("checkbox", { name: "Other" }));
    fireEvent.change(within(d).getByLabelText("Attendance"), { target: { value: "online" } });
    fireEvent.change(within(d).getByLabelText("Time zone of the deadlines"), { target: { value: "Asia/Dhaka" } });
    fireEvent.change(within(d).getByLabelText("Submission deadline"), { target: { value: "2027-03-01T23:59:59" } });
    fireEvent.change(within(d).getByLabelText("Official link"), { target: { value: "https://g.example.org" } });
    fireEvent.change(within(d).getByLabelText("Scope"), { target: { value: "Graphs." } });
    fireEvent.click(within(d).getByRole("button", { name: "Add venue" }));
    expect(await within(d).findByText(/The time zone must be AoE/)).toBeTruthy();
    expect(mockCreate).toHaveBeenCalledWith(
      expect.objectContaining({
        name: "Graph Workshop",
        type: "conference",
        fields: ["other"],
        attendance: "online",
        city: null,
        timeZone: "Asia/Dhaka",
        submissionDeadline: "2027-03-01T23:59:59",
        officialUrl: "https://g.example.org",
        scope: "Graphs.",
      }),
    );
    fireEvent.click(within(d).getByRole("button", { name: "Add venue" }));
    expect(await screen.findByText("Added Graph Workshop.")).toBeTruthy();
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});

describe("AdminVenues edit", () => {
  it("warns that editing an imported venue locks it, and sends only the change", async () => {
    const full = venue({ ...synced, locked: false });
    mockGet.mockResolvedValue({ ok: true, data: full });
    mockUpdate.mockResolvedValue({ ok: true, data: { ...full, locked: true } });
    await renderTable();
    fireEvent.click(screen.getByRole("button", { name: "Edit ACL 2027" }));
    const d = await screen.findByRole("dialog", { name: "Edit ACL 2027" });
    expect(within(d).getByText(/Saving a change locks this venue/)).toBeTruthy();
    fireEvent.change(within(d).getByLabelText("Scope"), { target: { value: "Computational linguistics." } });
    fireEvent.click(within(d).getByRole("button", { name: "Save changes" }));
    expect(await screen.findByText("Saved ACL 2027.")).toBeTruthy();
    expect(mockUpdate).toHaveBeenCalledWith(synced.id, { scope: "Computational linguistics." });
  });

  it("shows the Locked badge on a venue already locked", async () => {
    mockGet.mockResolvedValue({ ok: true, data: venue({ ...synced }) });
    await renderTable();
    fireEvent.click(screen.getByRole("button", { name: "Edit ACL 2027" }));
    const d = await screen.findByRole("dialog", { name: "Edit ACL 2027" });
    expect(within(d).getByText("Locked")).toBeTruthy();
    expect(within(d).getByText(/no longer updates it/)).toBeTruthy();
  });
});

describe("AdminVenues remove", () => {
  it("needs a reason of 10 characters before it removes", async () => {
    mockRemove.mockResolvedValue({ ok: true, data: undefined });
    await renderTable();
    fireEvent.click(screen.getByRole("button", { name: "Remove ACL 2027" }));
    const d = dialog("Remove venue");
    const remove = within(d).getByRole("button", { name: "Remove venue" }) as HTMLButtonElement;
    expect(remove.disabled).toBe(true);
    fireEvent.change(within(d).getByLabelText("Reason"), { target: { value: "Dup" } });
    expect(within(d).getByText("Give a reason of at least 10 characters.")).toBeTruthy();
    expect(remove.disabled).toBe(true);
    fireEvent.change(within(d).getByLabelText("Reason"), { target: { value: "Duplicate of ACL 2027 main" } });
    expect(remove.disabled).toBe(false);
    fireEvent.click(remove);
    expect(await screen.findByText("Removed ACL 2027.")).toBeTruthy();
    expect(mockRemove).toHaveBeenCalledWith(synced.id, "Duplicate of ACL 2027 main");
  });
});

describe("AdminVenues import", () => {
  const preview: VenueImportResult = {
    dryRun: true,
    created: 1,
    updated: 1,
    unchanged: 0,
    rejected: 1,
    rows: [
      { row: 1, outcome: "created", sourceKey: "auto:icml:2027", reasons: [] },
      { row: 2, outcome: "updated", sourceKey: "acl-2027", reasons: [] },
      {
        row: 3,
        outcome: "rejected",
        sourceKey: "",
        reasons: [
          { field: "submissionDeadline", code: "required" },
          { field: "officialUrl", code: "not_https" },
        ],
      },
    ],
  };

  it("previews a dry run, then imports on confirm", async () => {
    mockImport
      .mockResolvedValueOnce({ ok: true, data: preview })
      .mockResolvedValueOnce({ ok: true, data: { ...preview, dryRun: false } });
    await renderTable();
    fireEvent.click(screen.getByRole("button", { name: "Import from file" }));
    const d = dialog("Import venues from a file");
    const file = new File(["name\nICML"], "venues.csv", { type: "text/csv" });
    fireEvent.change(within(d).getByLabelText("CSV or JSON file"), { target: { files: [file] } });
    fireEvent.click(within(d).getByRole("button", { name: "Check file (dry run)" }));
    expect(await within(d).findByText(/Dry run, nothing changed yet: 1 new venue, 1 updated, 0 unchanged, 1 rejected/)).toBeTruthy();
    expect(mockImport).toHaveBeenCalledWith(file, true);
    const table = within(d).getByRole("table", { name: "What each row of the file would do" });
    const rejected = within(table).getAllByRole("row")[3];
    expect(within(rejected).getByText("Rejected")).toBeTruthy();
    expect(within(rejected).getByText("submissionDeadline is required")).toBeTruthy();
    expect(within(rejected).getByText("officialUrl must start with https://")).toBeTruthy();
    fireEvent.click(within(d).getByRole("button", { name: "Confirm import (2)" }));
    expect(await screen.findByText("Import finished: 1 new venue, 1 updated, 0 unchanged, 1 rejected.")).toBeTruthy();
    expect(mockImport).toHaveBeenLastCalledWith(file, false);
  });

  it("says why a file cannot be read", async () => {
    mockImport.mockResolvedValue({ ok: false, code: "too_many_rows", message: "x" });
    await renderTable();
    fireEvent.click(screen.getByRole("button", { name: "Import from file" }));
    const d = dialog("Import venues from a file");
    fireEvent.change(within(d).getByLabelText("CSV or JSON file"), { target: { files: [new File(["x"], "v.json")] } });
    fireEvent.click(within(d).getByRole("button", { name: "Check file (dry run)" }));
    expect(await within(d).findByText("The file has more than 1,000 rows.")).toBeTruthy();
  });
});
