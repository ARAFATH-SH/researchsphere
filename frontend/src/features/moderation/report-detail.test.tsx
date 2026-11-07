import "@/test/dialog-polyfill";

import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { dismissMessageReport, getMessageReport, type MessageReport, type ReportedMessage, restrictUser, suspendUser, warnUser } from "./api";
import { ReportDetail } from "./report-detail";
import { adaID, bob, bobID, cyd, report, reportID } from "./test-data";

jest.mock("./api");
jest.mock("@/features/auth/session", () => ({
  useSession: () => ({ status: "signedIn", user: { id: "0b8f4c1e-0000-4000-8000-000000000001" } }),
}));
const mockGet = jest.mocked(getMessageReport);
afterEach(() => jest.resetAllMocks());

const reason = "Repeated spam in messages";

function msg(n: number, senderId: string, body: string): ReportedMessage {
  return { id: `m${n}`, conversationId: "c", senderId, body, createdAt: "2026-11-05T08:00:00Z" };
}

function load(r: MessageReport, messages: ReportedMessage[] = [msg(2, bobID, "second"), msg(1, cyd.userId, "first")], nextBefore: string | null = null) {
  mockGet.mockResolvedValue({ ok: true, data: { report: r, messages, nextBefore } });
}

async function open() {
  render(<ReportDetail reportId={reportID} />);
  await screen.findByRole("heading", { name: "Report by Cyd Charisse" });
}

const actions = () => screen.queryByRole("region", { name: "Actions" });
const button = (name: string) => screen.getByRole("button", { name });
const inDialog = (name: string) => within(screen.getByRole("dialog")).getByRole("button", { name });

describe("ReportDetail", () => {
  it("shows the report and links to both people", async () => {
    load(report());
    await open();
    expect(screen.getByText("Harassment")).toBeTruthy();
    expect(screen.getByText("Rude messages")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Bob Babbage" }).getAttribute("href")).toBe(`/profile/${bobID}`);
    expect(screen.getByRole("link", { name: "Cyd Charisse" }).getAttribute("href")).toBe(`/profile/${cyd.userId}`);
    expect(screen.getByRole("link", { name: "Moderation history of Bob Babbage" }).getAttribute("href")).toBe(`/admin/users/${bobID}`);
    expect(screen.getByRole("link", { name: "Moderation history of Cyd Charisse" }).getAttribute("href")).toBe(`/admin/users/${cyd.userId}`);
  });

  it("says that messages after the report are not shown", async () => {
    load(report());
    await open();
    expect(screen.getByRole("note").textContent).toBe("Messages after the report are not shown.");
  });

  it("shows the messages oldest first and read-only", async () => {
    load(report());
    await open();
    const list = screen.getByRole("list", { name: "Messages up to the report" });
    const items = within(list).getAllByRole("listitem");
    expect(items[0].textContent).toContain("first");
    expect(items[0].textContent).toContain("Cyd Charisse");
    expect(items[1].textContent).toContain("second");
    expect(items[1].textContent).toContain("Bob Babbage");
    expect(within(list).queryByRole("textbox")).toBeNull();
    expect(screen.queryByRole("textbox")).toBeNull();
  });

  it("highlights the reported message", async () => {
    load(report({ messageId: "m2" }));
    await open();
    const item = screen.getByText("second").closest("li")!;
    expect(item.getAttribute("data-reported")).toBe("true");
    expect(item.textContent).toContain("Reported message");
    expect(screen.getByText("first").closest("li")!.getAttribute("data-reported")).toBeNull();
  });

  it("loads earlier messages above the others", async () => {
    load(report(), [msg(2, bobID, "second")], "m2");
    await open();
    mockGet.mockResolvedValueOnce({ ok: true, data: { report: report(), messages: [msg(1, cyd.userId, "first")], nextBefore: null } });
    await act(async () => fireEvent.click(button("Load earlier")));
    expect(mockGet).toHaveBeenLastCalledWith(reportID, "m2");
    const items = within(screen.getByRole("list", { name: "Messages up to the report" })).getAllByRole("listitem");
    expect(items.map((i) => i.textContent).join()).toMatch(/first.*second/);
    expect(screen.queryByRole("button", { name: "Load earlier" })).toBeNull();
  });

  it("offers every action on an open report", async () => {
    load(report());
    await open();
    expect(within(actions()!).getAllByRole("button").map((b) => b.textContent)).toEqual([
      "Dismiss report",
      "Warn",
      "Restrict messaging",
      "Suspend",
    ]);
  });

  it("offers no actions on a resolved report", async () => {
    load(report({ status: "resolved", outcome: "dismissed", resolvedAt: "2026-11-06T09:00:00Z", note: "No rule broken" }));
    await open();
    expect(actions()).toBeNull();
    expect(screen.getByText("Dismissed")).toBeTruthy();
    expect(screen.getByText("No rule broken")).toBeTruthy();
  });

  it.each([
    ["another administrator", report({ reportedUser: { ...bob, role: "admin" } })],
    ["me", report({ reportedUser: { ...bob, userId: adaID } })],
  ])("hides warn, restrict and suspend when the reported user is %s", async (_, r) => {
    load(r);
    await open();
    expect(within(actions()!).getAllByRole("button").map((b) => b.textContent)).toEqual(["Dismiss report"]);
    expect(actions()!.textContent).toContain("Administrators can't be warned, restricted or suspended.");
  });

  it("dismisses with no reason and marks the report without reading it again", async () => {
    load(report());
    jest.mocked(dismissMessageReport).mockResolvedValue({ ok: true, data: report({ status: "resolved", outcome: "dismissed" }) });
    await open();
    fireEvent.click(button("Dismiss report"));
    await act(async () => fireEvent.click(inDialog("Dismiss report")));
    expect(dismissMessageReport).toHaveBeenCalledWith(reportID, undefined);
    expect(screen.getByText("Dismissed")).toBeTruthy();
    expect(actions()).toBeNull();
    expect(mockGet).toHaveBeenCalledTimes(1);
  });

  it.each<[string, () => void]>([
    ["Warn", () => expect(warnUser).toHaveBeenCalledWith(bobID, expect.objectContaining({ reportId: reportID, reason }))],
    ["Restrict messaging", () => expect(restrictUser).toHaveBeenCalledWith(bobID, 7, expect.objectContaining({ reportId: reportID, reason }))],
    ["Suspend", () => expect(suspendUser).toHaveBeenCalledWith(bobID, expect.any(String), expect.objectContaining({ reportId: reportID, reason }))],
  ])("%s passes the reportId and resolves the report", async (name, check) => {
    load(report());
    for (const fn of [warnUser, restrictUser, suspendUser]) jest.mocked(fn).mockResolvedValue({ ok: true, data: {} as never });
    await open();
    fireEvent.click(button(name));
    fireEvent.change(screen.getByRole("textbox", { name: "Reason shown to the user" }), { target: { value: reason } });
    await act(async () => fireEvent.click(inDialog(name)));
    check();
    expect(screen.getByText("Action taken")).toBeTruthy();
    expect(actions()).toBeNull();
  });

  it("hides the actions on a 403 for an administrator", async () => {
    load(report());
    jest.mocked(warnUser).mockResolvedValue({ ok: false, code: "cannot_moderate_admin", message: "Administrators cannot be moderated." });
    await open();
    fireEvent.click(button("Warn"));
    fireEvent.change(screen.getByRole("textbox", { name: "Reason shown to the user" }), { target: { value: reason } });
    await act(async () => fireEvent.click(inDialog("Warn")));
    expect(within(screen.getByRole("dialog")).getByRole("alert").textContent).toBe("Administrators cannot be moderated.");
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Cancel" }));
    expect(within(actions()!).getAllByRole("button").map((b) => b.textContent)).toEqual(["Dismiss report"]);
  });

  it("says when the report does not exist", async () => {
    mockGet.mockResolvedValue({ ok: false, code: "not_found", message: "Not found." });
    render(<ReportDetail reportId="x" />);
    expect(await screen.findByText("This report isn't available.")).toBeTruthy();
  });
});
