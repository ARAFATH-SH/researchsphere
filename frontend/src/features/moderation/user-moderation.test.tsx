import "@/test/dialog-polyfill";

import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { getUserModeration, liftRestriction, restoreUser, warnUser } from "./api";
import { action, adaID, bobID, history } from "./test-data";
import { UserModeration } from "./user-moderation";

jest.mock("./api");
jest.mock("@/features/auth/session", () => ({
  useSession: () => ({ status: "signedIn", user: { id: "0b8f4c1e-0000-4000-8000-000000000001" } }),
}));
const mockGet = jest.mocked(getUserModeration);
beforeEach(() => jest.useFakeTimers({ now: new Date("2026-11-06T10:00:00Z"), doNotFake: ["nextTick", "queueMicrotask", "setTimeout", "clearTimeout"] }));
afterEach(() => {
  jest.useRealTimers();
  jest.resetAllMocks();
});

const reason = "Repeated spam in messages";
const active = action({ id: "r1", action: "restriction", restrictionId: "rest-1", endsAt: "2026-11-20T10:00:00Z", warningId: null });

async function open(userId = bobID) {
  render(<UserModeration userId={userId} />);
  await screen.findByRole("heading", { level: 1 });
}

const label = (name: string) => screen.queryByRole("button", { name });
const inDialog = (name: string) => within(screen.getByRole("dialog")).getByRole("button", { name });

describe("UserModeration", () => {
  it("names the account, also a suspended one, with the reason and the end", async () => {
    mockGet.mockResolvedValue({
      ok: true,
      data: history([], { suspended: true, suspensionReason: "Spam", suspendedUntil: "2026-12-03T14:05:00" }),
    });
    await open();
    expect(screen.getByRole("heading", { level: 1, name: "Bob Babbage" })).toBeTruthy();
    expect(screen.getByText("bob@example.org")).toBeTruthy();
    expect(screen.getByText("Suspended until 3 Dec 2026, 2:05 PM. Reason: Spam")).toBeTruthy();
  });

  it("says a suspension with no end is permanent, and offers Restore in place of Suspend", async () => {
    mockGet.mockResolvedValue({ ok: true, data: history([], { suspended: true }) });
    await open();
    expect(screen.getByText("Suspended permanently.")).toBeTruthy();
    expect(label("Restore")).toBeTruthy();
    expect(label("Suspend")).toBeNull();
  });

  it("offers Warn, Restrict and Suspend on an active account", async () => {
    mockGet.mockResolvedValue({ ok: true, data: history([]) });
    await open();
    for (const name of ["Warn", "Restrict messaging", "Suspend"]) expect(label(name)).toBeTruthy();
    expect(label("Restore")).toBeNull();
    expect(screen.getByText("No moderation actions.")).toBeTruthy();
  });

  it("shows the history with linked reports and internal notes", async () => {
    const linked = { id: "rep1", reason: "spam" as const, outcome: "action_taken" as const, createdAt: "2026-11-01T10:00:00Z" };
    mockGet.mockResolvedValue({
      ok: true,
      data: history([
        action({ note: "Second time", report: linked, reportId: linked.id }),
        action({ id: "s1", action: "suspension", warningId: null }),
        action({ id: "x1", action: "restriction", warningId: null, restrictionId: "old", endsAt: "2026-11-02T10:00:00Z", liftedAt: "2026-11-02T09:00:00" }),
      ]),
    });
    await open();
    expect(screen.getByText("Warning")).toBeTruthy();
    expect(screen.getByText("Internal note: Second time")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Report: Spam, Action taken" }).getAttribute("href")).toBe("/admin/reports/rep1");
    expect(screen.getByText("Permanent")).toBeTruthy();
    expect(screen.getByText("Lifted on 2 Nov 2026, 9:00 AM")).toBeTruthy();
    expect(screen.getByText("Not acknowledged yet")).toBeTruthy();
  });

  it("lifts an active restriction with a reason, and reads the history again", async () => {
    mockGet.mockResolvedValue({ ok: true, data: history([active]) });
    jest.mocked(liftRestriction).mockResolvedValue({ ok: true, data: undefined });
    await open();
    expect(label("Restrict messaging")).toBeNull();
    fireEvent.click(label("Lift")!);
    await act(async () => fireEvent.click(inDialog("Lift restriction")));
    expect(screen.getByText("Write at least 10 characters.")).toBeTruthy();
    expect(liftRestriction).not.toHaveBeenCalled();
    fireEvent.change(screen.getByRole("textbox", { name: "Reason shown to the user" }), { target: { value: reason } });
    mockGet.mockResolvedValue({ ok: true, data: history([{ ...active, liftedAt: "2026-11-06T10:00:00Z" }]) });
    await act(async () => fireEvent.click(inDialog("Lift restriction")));
    expect(liftRestriction).toHaveBeenCalledWith("rest-1", { reason });
    expect(mockGet).toHaveBeenCalledTimes(2);
    expect(label("Lift")).toBeNull();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("offers no Lift on a restriction that has ended", async () => {
    mockGet.mockResolvedValue({ ok: true, data: history([{ ...active, endsAt: "2026-11-05T10:00:00Z" }]) });
    await open();
    expect(label("Lift")).toBeNull();
    expect(label("Restrict messaging")).toBeTruthy();
  });

  it("restores with a reason and no report", async () => {
    mockGet.mockResolvedValue({ ok: true, data: history([], { suspended: true }) });
    jest.mocked(restoreUser).mockResolvedValue({ ok: true, data: action() });
    await open();
    fireEvent.click(label("Restore")!);
    fireEvent.change(screen.getByRole("textbox", { name: "Reason shown to the user" }), { target: { value: reason } });
    await act(async () => fireEvent.click(inDialog("Restore")));
    expect(restoreUser).toHaveBeenCalledWith(bobID, { reason });
  });

  it.each([
    ["myself", adaID, history([active], { id: adaID, role: "admin" })],
    ["another administrator", bobID, history([active], { role: "admin" })],
    ["my own account, whatever its role", adaID, history([active], { id: adaID })],
  ])("hides every action on %s", async (_, id, data) => {
    mockGet.mockResolvedValue({ ok: true, data });
    await open(id);
    for (const name of ["Warn", "Restrict messaging", "Suspend", "Restore", "Lift"]) expect(label(name)).toBeNull();
    expect(screen.getByText("Messaging restricted")).toBeTruthy();
  });

  it("handles the 403 anyway, and hides the buttons", async () => {
    mockGet.mockResolvedValue({ ok: true, data: history([]) });
    jest.mocked(warnUser).mockResolvedValue({ ok: false, code: "cannot_moderate_admin", message: "Administrators cannot be moderated." });
    await open();
    fireEvent.click(label("Warn")!);
    fireEvent.change(screen.getByRole("textbox", { name: "Reason shown to the user" }), { target: { value: reason } });
    await act(async () => fireEvent.click(inDialog("Warn")));
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByRole("alert").textContent).toBe("Administrators cannot be moderated.");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(label("Warn")).toBeNull();
  });

  it("says when the user does not exist", async () => {
    mockGet.mockResolvedValue({ ok: false, code: "not_found", message: "Not found." });
    render(<UserModeration userId="x" />);
    expect(await screen.findByText("This user doesn't exist.")).toBeTruthy();
  });

  it("pages through the history", async () => {
    mockGet.mockResolvedValue({ ok: true, data: { ...history([action()]), total: 21 } });
    await open();
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Next" })));
    expect(mockGet).toHaveBeenLastCalledWith(bobID, 2);
  });
});
