import "@/test/dialog-polyfill";

import { act, fireEvent, render, screen } from "@testing-library/react";

import { ActionDialog, type ActionKind } from "./action-dialog";
import { dismissMessageReport, liftRestriction, restoreUser, restrictUser, suspendUser, warnUser } from "./api";

jest.mock("./api");
afterEach(() => {
  jest.useRealTimers();
  jest.resetAllMocks();
});

const longReason = "Repeated spam in messages";
const ok = { ok: true as const, data: {} as never };

function open(kind: ActionKind, extra: Partial<Parameters<typeof ActionDialog>[0]> = {}) {
  const onDone = jest.fn();
  const onRefused = jest.fn();
  render(<ActionDialog kind={kind} name="Bob" userId="u" reportId="r" onClose={jest.fn()} onDone={onDone} onRefused={onRefused} {...extra} />);
  return { onDone, onRefused };
}

const reason = () => screen.getByRole("textbox", { name: "Reason shown to the user" });
const note = () => screen.getByRole("textbox", { name: "Internal note (optional)" });
const days = () => screen.getByRole("spinbutton", { name: /Days/ });
const submit = (name: string) => act(async () => fireEvent.click(screen.getByRole("button", { name })));

describe("ActionDialog", () => {
  describe.each<[ActionKind, string]>([
    ["warn", "Warn"],
    ["restrict", "Restrict messaging"],
    ["suspend", "Suspend"],
    ["lift", "Lift restriction"],
    ["restore", "Restore"],
  ])("%s", (kind, button) => {
    it("needs a reason of at least 10 characters", async () => {
      open(kind, { restrictionId: "x" });
      await submit(button);
      expect(screen.getByText("Write at least 10 characters.")).toBeTruthy();
      fireEvent.change(reason(), { target: { value: "too short" } });
      await submit(button);
      expect(screen.getByText("Write at least 10 characters.")).toBeTruthy();
      for (const fn of [warnUser, restrictUser, suspendUser, liftRestriction, restoreUser]) expect(fn).not.toHaveBeenCalled();
    });

    it("refuses more than 1000 characters and counts them", async () => {
      open(kind, { restrictionId: "x" });
      fireEvent.change(reason(), { target: { value: "x".repeat(1001) } });
      expect(screen.getByText("1001 / 1000")).toBeTruthy();
      await submit(button);
      for (const fn of [warnUser, restrictUser, suspendUser, liftRestriction, restoreUser]) expect(fn).not.toHaveBeenCalled();
    });

    it("previews what the user will see", () => {
      open(kind, { restrictionId: "x" });
      expect(screen.queryByText(/The user will see/)).toBeNull();
      fireEvent.change(reason(), { target: { value: `  ${longReason}  ` } });
      expect(screen.getByText(`The user will see: ${longReason}`)).toBeTruthy();
    });
  });

  it("warns with the reportId and the note", async () => {
    jest.mocked(warnUser).mockResolvedValue(ok);
    const { onDone } = open("warn");
    fireEvent.change(reason(), { target: { value: longReason } });
    fireEvent.change(note(), { target: { value: "second time" } });
    await submit("Warn");
    expect(warnUser).toHaveBeenCalledWith("u", { reason: longReason, note: "second time", reportId: "r" });
    expect(onDone).toHaveBeenCalled();
  });

  it("restricts for 1 to 90 days with the reportId", async () => {
    jest.mocked(restrictUser).mockResolvedValue(ok);
    open("restrict");
    fireEvent.change(reason(), { target: { value: longReason } });
    for (const bad of ["0", "91", "1.5", ""]) {
      fireEvent.change(days(), { target: { value: bad } });
      await submit("Restrict messaging");
      expect(screen.getByText("Enter a whole number from 1 to 90.")).toBeTruthy();
    }
    expect(restrictUser).not.toHaveBeenCalled();
    fireEvent.change(days(), { target: { value: "90" } });
    await submit("Restrict messaging");
    expect(restrictUser).toHaveBeenCalledWith("u", 90, { reason: longReason, reportId: "r" });
  });

  it("suspends for 1 to 365 days as a time from now", async () => {
    jest.useFakeTimers({ now: new Date("2026-11-06T10:00:00Z"), doNotFake: ["nextTick", "queueMicrotask"] });
    jest.mocked(suspendUser).mockResolvedValue(ok);
    open("suspend");
    fireEvent.change(reason(), { target: { value: longReason } });
    fireEvent.change(days(), { target: { value: "366" } });
    await submit("Suspend");
    expect(screen.getByText("Enter a whole number from 1 to 365.")).toBeTruthy();
    expect(suspendUser).not.toHaveBeenCalled();
    fireEvent.change(days(), { target: { value: "2" } });
    await submit("Suspend");
    expect(suspendUser).toHaveBeenCalledWith("u", "2026-11-08T10:00:00.000Z", { reason: longReason, reportId: "r" });
  });

  it("suspends permanently, with no days and no end", async () => {
    jest.mocked(suspendUser).mockResolvedValue(ok);
    open("suspend");
    fireEvent.change(reason(), { target: { value: longReason } });
    fireEvent.click(screen.getByRole("checkbox", { name: "Permanently" }));
    expect(screen.queryByRole("spinbutton")).toBeNull();
    await submit("Suspend");
    expect(suspendUser).toHaveBeenCalledWith("u", null, { reason: longReason, reportId: "r" });
  });

  it("lifts and restores with no report", async () => {
    jest.mocked(liftRestriction).mockResolvedValue({ ok: true, data: undefined });
    open("lift", { restrictionId: "rest-1" });
    fireEvent.change(reason(), { target: { value: longReason } });
    await submit("Lift restriction");
    expect(liftRestriction).toHaveBeenCalledWith("rest-1", { reason: longReason });
  });

  it("restores with no report", async () => {
    jest.mocked(restoreUser).mockResolvedValue(ok);
    open("restore");
    fireEvent.change(reason(), { target: { value: longReason } });
    await submit("Restore");
    expect(restoreUser).toHaveBeenCalledWith("u", { reason: longReason });
  });

  it("dismisses with an optional note only: no user sees it", async () => {
    jest.mocked(dismissMessageReport).mockResolvedValue(ok);
    const { onDone } = open("dismiss");
    expect(screen.queryByRole("textbox", { name: "Reason shown to the user" })).toBeNull();
    expect(screen.queryByText(/The user will see/)).toBeNull();
    fireEvent.change(note(), { target: { value: "No rule broken" } });
    await submit("Dismiss report");
    expect(dismissMessageReport).toHaveBeenCalledWith("r", "No rule broken");
    expect(onDone).toHaveBeenCalled();
  });

  it("dismisses with no note at all", async () => {
    jest.mocked(dismissMessageReport).mockResolvedValue(ok);
    open("dismiss");
    await submit("Dismiss report");
    expect(dismissMessageReport).toHaveBeenCalledWith("r", undefined);
  });

  it("shows a refusal and tells the page about an administrator (403)", async () => {
    jest.mocked(warnUser).mockResolvedValue({ ok: false, code: "cannot_moderate_admin", message: "Administrators cannot be moderated." });
    const { onDone, onRefused } = open("warn");
    fireEvent.change(reason(), { target: { value: longReason } });
    await submit("Warn");
    expect(screen.getByRole("alert").textContent).toBe("Administrators cannot be moderated.");
    expect(onRefused).toHaveBeenCalled();
    expect(onDone).not.toHaveBeenCalled();
  });

  it("shows a conflict", async () => {
    jest.mocked(restrictUser).mockResolvedValue({ ok: false, code: "already_restricted", message: "Already restricted." });
    const { onRefused } = open("restrict");
    fireEvent.change(reason(), { target: { value: longReason } });
    await submit("Restrict messaging");
    expect(screen.getByRole("alert").textContent).toBe("Already restricted.");
    expect(onRefused).not.toHaveBeenCalled();
  });
});
