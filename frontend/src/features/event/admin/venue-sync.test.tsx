import "@/test/dialog-polyfill";

import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { getVenueSyncStatus, startVenueSync, type VenueSyncRun, type VenueSyncSource } from "../api";
import { freshDryRun, VenueSync } from "./venue-sync";

jest.mock("../api");
const mockStatus = jest.mocked(getVenueSyncStatus);
const mockStart = jest.mocked(startVenueSync);

function run(over: Partial<VenueSyncRun> = {}): VenueSyncRun {
  return {
    id: "0192f0a0-0000-7000-8000-00000000a001",
    dryRun: false,
    trigger: "on_demand",
    status: "succeeded",
    startedAt: "2026-11-24T04:00:00Z",
    finishedAt: "2026-11-24T04:01:00Z",
    commit: "abcdef1234567",
    created: 12,
    updated: 3,
    unchanged: 40,
    skipped: 2,
    rejected: 1,
    errors: [],
    rejects: [{ sourceKey: "acl-2027", name: "ACL 2027", reasons: [{ field: "submissionDeadline", code: "deadline_tbd" }] }],
    ...over,
  };
}

function source(over: Partial<VenueSyncSource> = {}): VenueSyncSource {
  return {
    source: "ccfddl",
    label: "ccf-deadlines (conferences)",
    licence: "MIT",
    url: "https://github.com/ccfddl/ccf-deadlines (main)",
    commit: "abcdef1234567",
    lastRun: run(),
    lastDryRun: null,
    nextRunAt: null,
    ...over,
  };
}

beforeEach(() => {
  mockStatus.mockReset();
  mockStart.mockReset().mockResolvedValue({ ok: true, data: { dryRun: true, runs: [] } });
});

describe("freshDryRun", () => {
  it("needs a successful dry run after the last real run", () => {
    expect(freshDryRun(source())).toBeNull();
    expect(freshDryRun(source({ lastDryRun: run({ dryRun: true, startedAt: "2026-11-23T00:00:00Z" }) }))).toBeNull();
    expect(freshDryRun(source({ lastDryRun: run({ dryRun: true, status: "failed", startedAt: "2026-11-25T00:00:00Z" }) }))).toBeNull();
    expect(freshDryRun(source({ lastDryRun: run({ dryRun: true, startedAt: "2026-11-25T00:00:00Z" }) }))).not.toBeNull();
    expect(freshDryRun(source({ lastRun: null, lastDryRun: run({ dryRun: true }) }))).not.toBeNull();
  });
});

describe("VenueSync", () => {
  it("shows each source with its licence, commit, counts and rejected entries", async () => {
    mockStatus.mockResolvedValue({ ok: true, data: { daily: true, sources: [source()] } });
    render(<VenueSync />);
    const item = (await screen.findByRole("heading", { name: "ccf-deadlines (conferences)" })).closest("li")!;
    expect(item.textContent).toContain("MIT");
    expect(item.textContent).toContain("commit abcdef1");
    expect(item.textContent).toContain("12 created, 3 updated, 40 unchanged, 2 skipped, 1 rejected");
    expect(within(item).getByText("Last dry run: none yet")).toBeTruthy();
    expect(within(item).getByText("ACL 2027 (acl-2027): submissionDeadline has no deadline yet")).toBeTruthy();
    expect(screen.getByText("Daily schedule on")).toBeTruthy();
  });

  it("asks for a dry run before a real run", async () => {
    mockStatus.mockResolvedValue({ ok: true, data: { daily: false, sources: [source()] } });
    render(<VenueSync />);
    fireEvent.click(await screen.findByRole("button", { name: "Run sync ccf-deadlines (conferences)" }));
    const d = screen.getByRole("dialog", { name: "Run ccf-deadlines (conferences)" });
    expect(within(d).getByText(/Do a dry run first/)).toBeTruthy();
    expect(within(d).queryByRole("button", { name: "Run sync now" })).toBeNull();
    fireEvent.click(within(d).getByRole("button", { name: "Start dry run" }));
    await act(async () => {});
    expect(mockStart).toHaveBeenCalledWith(["ccfddl"], true);
  });

  it("runs for real after a fresh dry run, showing its counts first", async () => {
    const dry = run({ dryRun: true, startedAt: "2026-11-25T00:00:00Z", created: 5 });
    mockStatus.mockResolvedValue({ ok: true, data: { daily: false, sources: [source({ lastDryRun: dry })] } });
    render(<VenueSync />);
    fireEvent.click(await screen.findByRole("button", { name: "Run sync ccf-deadlines (conferences)" }));
    const d = screen.getByRole("dialog", { name: "Run ccf-deadlines (conferences)" });
    expect(within(d).getByText(/5 created, 3 updated/)).toBeTruthy();
    fireEvent.click(within(d).getByRole("button", { name: "Run sync now" }));
    await act(async () => {});
    expect(mockStart).toHaveBeenCalledWith(["ccfddl"], false);
  });

  it("shows a refusal such as a run too soon", async () => {
    const dry = run({ dryRun: true, startedAt: "2026-11-25T00:00:00Z" });
    mockStatus.mockResolvedValue({ ok: true, data: { daily: false, sources: [source({ lastDryRun: dry })] } });
    mockStart.mockResolvedValue({ ok: false, code: "venue_sync_too_soon", message: "The last real run was less than an hour ago." });
    render(<VenueSync />);
    fireEvent.click(await screen.findByRole("button", { name: "Run sync ccf-deadlines (conferences)" }));
    fireEvent.click(screen.getByRole("button", { name: "Run sync now" }));
    expect(await screen.findByText("The last real run was less than an hour ago.")).toBeTruthy();
  });

  it("reads the status again every 3 seconds while a run is going, then stops", async () => {
    jest.useFakeTimers();
    try {
      mockStatus
        .mockResolvedValueOnce({ ok: true, data: { daily: false, sources: [source({ lastRun: run({ status: "running", finishedAt: null }) })] } })
        .mockResolvedValue({ ok: true, data: { daily: false, sources: [source()] } });
      render(<VenueSync />);
      await act(async () => {});
      expect(screen.getByText("Running")).toBeTruthy();
      expect((screen.getByRole("button", { name: "Dry run ccf-deadlines (conferences)" }) as HTMLButtonElement).disabled).toBe(true);
      await act(async () => {
        jest.advanceTimersByTime(3000);
      });
      expect(mockStatus).toHaveBeenCalledTimes(2);
      expect(screen.getByText("Succeeded")).toBeTruthy();
      await act(async () => {
        jest.advanceTimersByTime(10_000);
      });
      expect(mockStatus).toHaveBeenCalledTimes(2);
    } finally {
      jest.useRealTimers();
    }
  });
});
