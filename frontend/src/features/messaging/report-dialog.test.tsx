import "@/test/dialog-polyfill";

import { act, fireEvent, render, screen } from "@testing-library/react";

import { reportConversation } from "./api";
import { ReportDialog } from "./report-dialog";

jest.mock("./api");
const mockReport = jest.mocked(reportConversation);
afterEach(() => jest.resetAllMocks());

const receipt = (blockedByMe = false) => ({
  ok: true as const,
  data: { id: "r", createdAt: "2026-11-05T09:00:00Z", reportedUserId: "b", blockedByMe },
});

function open(props: Partial<Parameters<typeof ReportDialog>[0]> = {}) {
  const onBlock = jest.fn();
  const onClose = jest.fn();
  render(<ReportDialog conversationId="c" name="Bob" canBlock onClose={onClose} onBlock={onBlock} {...props} />);
  return { onBlock, onClose };
}

const reason = () => screen.getByRole("combobox", { name: "Reason" }) as HTMLSelectElement;
const details = () => screen.getByRole("textbox", { name: "Details (optional)" });
const send = () => screen.getByRole("button", { name: "Send report" });

describe("ReportDialog", () => {
  it("offers the four reasons", () => {
    open();
    expect([...reason().options].map((o) => o.text)).toEqual(["Choose a reason", "Spam", "Harassment", "Inappropriate", "Other"]);
  });

  it("needs a reason", async () => {
    open();
    await act(async () => fireEvent.click(send()));
    expect(screen.getByText("Choose a reason.")).toBeTruthy();
    expect(mockReport).not.toHaveBeenCalled();
  });

  it("counts the details and refuses too many", () => {
    open();
    expect(screen.getByText("0 / 1000")).toBeTruthy();
    fireEvent.change(details(), { target: { value: "x".repeat(1001) } });
    expect(screen.getByText("1001 / 1000")).toBeTruthy();
    expect((send() as HTMLButtonElement).disabled).toBe(true);
  });

  it("reports the conversation, and offers to block because reporting does not", async () => {
    mockReport.mockResolvedValue(receipt());
    const { onBlock, onClose } = open();
    fireEvent.change(reason(), { target: { value: "harassment" } });
    fireEvent.change(details(), { target: { value: "  rude  " } });
    await act(async () => fireEvent.click(send()));
    expect(mockReport).toHaveBeenCalledWith("c", { reason: "harassment", details: "rude" });
    expect(screen.getByRole("status").textContent).toBe("Thanks. Our team will review it.");
    fireEvent.click(screen.getByRole("button", { name: "Block Bob" }));
    expect(onBlock).toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(onClose).toHaveBeenCalled();
  });

  it("reports one message, without empty details", async () => {
    mockReport.mockResolvedValue(receipt());
    open({ messageId: "m" });
    expect(screen.getByRole("dialog", { name: "Report message" })).toBeTruthy();
    fireEvent.change(reason(), { target: { value: "spam" } });
    await act(async () => fireEvent.click(send()));
    expect(mockReport).toHaveBeenCalledWith("c", { reason: "spam", messageId: "m" });
  });

  it.each([
    ["already blocked", {}, true],
    ["unavailable", { canBlock: false }, false],
  ])("offers no block when they are %s", async (_, props, blockedByMe) => {
    mockReport.mockResolvedValue(receipt(blockedByMe));
    open(props);
    fireEvent.change(reason(), { target: { value: "spam" } });
    await act(async () => fireEvent.click(send()));
    expect(screen.getByRole("status")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Block Bob" })).toBeNull();
  });

  it("says when reports are accepted again after the limit (429)", async () => {
    mockReport.mockResolvedValue({ ok: false, code: "rate_limited", message: "Limit.", retryAfter: 7200 });
    open();
    fireEvent.change(reason(), { target: { value: "spam" } });
    await act(async () => fireEvent.click(send()));
    expect(screen.getByRole("alert").textContent).toBe("You've sent the most reports allowed for today. Try again in 2 hours.");
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("shows any other refusal", async () => {
    mockReport.mockResolvedValue({ ok: false, code: "cannot_report_own_message", message: "That is your own message." });
    open();
    fireEvent.change(reason(), { target: { value: "spam" } });
    await act(async () => fireEvent.click(send()));
    expect(screen.getByRole("alert").textContent).toBe("That is your own message.");
  });
});
