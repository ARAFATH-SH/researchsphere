import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { AdminReports } from "./admin-reports";
import { listMessageReports, type MessageReportPage } from "./api";
import { report, reportID } from "./test-data";

jest.mock("./api");
const mockList = jest.mocked(listMessageReports);
afterEach(() => jest.resetAllMocks());

const page = (items: MessageReportPage["items"], extra: Partial<MessageReportPage> = {}) => ({
  ok: true as const,
  data: { items, page: 1, pageSize: 20, total: items.length, ...extra },
});

describe("AdminReports", () => {
  it("starts on the open reports and shows each row's facts", async () => {
    mockList.mockResolvedValue(page([report()]));
    render(<AdminReports />);
    const row = (await screen.findByRole("listitem")).textContent;
    expect(mockList).toHaveBeenCalledWith("open", 1);
    expect(row).toContain("Cyd Charisse reported a conversation with Bob Babbage");
    expect(row).toContain("Reason: Harassment");
    expect(screen.getByRole("tab", { name: "Open" }).getAttribute("aria-selected")).toBe("true");
    expect(screen.getByRole("heading", { name: "Reported conversations (1)" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Review the report by Cyd Charisse" }).getAttribute("href")).toBe(`/admin/reports/${reportID}`);
  });

  it("says when a report is of a whole group", async () => {
    mockList.mockResolvedValue(page([report({ reportedUser: null, conversationKind: "group", groupName: "Lab" })]));
    render(<AdminReports />);
    expect((await screen.findByRole("listitem")).textContent).toContain("Cyd Charisse reported a whole group");
  });

  it("moves to resolved reports, with their outcome", async () => {
    mockList.mockResolvedValueOnce(page([report()]));
    render(<AdminReports />);
    await screen.findByRole("listitem");
    mockList.mockResolvedValueOnce(
      page([
        report({ status: "resolved", outcome: "action_taken", id: "a" }),
        report({ status: "resolved", outcome: "dismissed", id: "b", messageId: "m" }),
      ]),
    );
    await act(async () => fireEvent.click(screen.getByRole("tab", { name: "Resolved" })));
    expect(mockList).toHaveBeenLastCalledWith("resolved", 1);
    const rows = screen.getAllByRole("listitem");
    expect(within(rows[0]).getByText("Action taken")).toBeTruthy();
    expect(within(rows[1]).getByText("Dismissed")).toBeTruthy();
    expect(rows[1].textContent).toContain("reported a message from");
    expect(screen.getByRole("tab", { name: "Resolved" }).getAttribute("aria-selected")).toBe("true");
  });

  it("pages through a tab", async () => {
    mockList.mockResolvedValueOnce(page([report()], { total: 21 }));
    render(<AdminReports />);
    await screen.findByText("Page 1 of 2");
    mockList.mockResolvedValueOnce(page([report()], { total: 21, page: 2 }));
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Next" })));
    expect(mockList).toHaveBeenLastCalledWith("open", 2);
    expect(screen.getByText("Page 2 of 2")).toBeTruthy();
  });

  it("says when a tab is empty, and shows a refusal", async () => {
    mockList.mockResolvedValueOnce(page([]));
    render(<AdminReports />);
    expect(await screen.findByText("No open reports.")).toBeTruthy();
    mockList.mockResolvedValueOnce({ ok: false, code: "forbidden", message: "You do not have permission." });
    await act(async () => fireEvent.click(screen.getByRole("tab", { name: "Resolved" })));
    expect(screen.getByRole("alert").textContent).toBe("You do not have permission.");
  });
});
