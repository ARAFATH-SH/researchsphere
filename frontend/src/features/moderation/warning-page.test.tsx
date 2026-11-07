import { act, fireEvent, render, screen } from "@testing-library/react";

import { acknowledgeWarning, getMyWarning, listPendingWarnings } from "./api";
import { warning } from "./test-data";
import { WarningPage } from "./warning-page";

jest.mock("./api");
const mockGet = jest.mocked(getMyWarning);
const mockAck = jest.mocked(acknowledgeWarning);
beforeEach(() => jest.mocked(listPendingWarnings).mockResolvedValue({ ok: true, data: [] }));
afterEach(() => jest.resetAllMocks());

const understand = () => screen.queryByRole("button", { name: "I understand" });

describe("WarningPage", () => {
  it("shows the reason, the date and I understand while pending", async () => {
    mockGet.mockResolvedValue({ ok: true, data: warning() });
    render(<WarningPage warningId="w1" />);
    expect(await screen.findByText("Repeated spam in your messages")).toBeTruthy();
    expect(screen.getByText("2 Nov 2026")).toBeTruthy();
    expect(understand()).toBeTruthy();
    expect(screen.queryByText(/Acknowledged on/)).toBeNull();
  });

  it("acknowledges, shows the server's date and updates the banner's list", async () => {
    mockGet.mockResolvedValueOnce({ ok: true, data: warning() });
    mockAck.mockResolvedValue({ ok: true, data: undefined });
    render(<WarningPage warningId="w1" />);
    await screen.findByText("Repeated spam in your messages");
    mockGet.mockResolvedValueOnce({ ok: true, data: warning({ acknowledged: true, acknowledgedAt: "2026-11-04T12:00:00" }) });
    await act(async () => fireEvent.click(understand()!));
    expect(mockAck).toHaveBeenCalledWith("w1");
    expect(screen.getByText("Acknowledged on 4 Nov 2026")).toBeTruthy();
    expect(understand()).toBeNull();
    expect(listPendingWarnings).toHaveBeenCalled();
  });

  it("shows an acknowledged warning with its date and no button", async () => {
    mockGet.mockResolvedValue({ ok: true, data: warning({ acknowledged: true, acknowledgedAt: "2026-11-04T12:00:00" }) });
    render(<WarningPage warningId="w1" />);
    expect(await screen.findByText("Acknowledged on 4 Nov 2026")).toBeTruthy();
    expect(understand()).toBeNull();
  });

  it("keeps the button when acknowledging fails", async () => {
    mockGet.mockResolvedValue({ ok: true, data: warning() });
    mockAck.mockResolvedValue({ ok: false, code: "internal_error", message: "No." });
    render(<WarningPage warningId="w1" />);
    await screen.findByText("Repeated spam in your messages");
    await act(async () => fireEvent.click(understand()!));
    expect(screen.getByRole("alert").textContent).toContain("Try again");
    expect(understand()).toBeTruthy();
  });

  it("does not show someone else's warning", async () => {
    mockGet.mockResolvedValue({ ok: false, code: "not_found", message: "Not found." });
    render(<WarningPage warningId="other" />);
    expect(await screen.findByText("This warning isn't available.")).toBeTruthy();
  });
});
