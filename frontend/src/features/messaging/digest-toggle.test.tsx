import { act, fireEvent, render, screen } from "@testing-library/react";

import { getMySettings, setUnreadMessageDigest } from "./api";
import { DigestToggle } from "./digest-toggle";

jest.mock("./api");
const mockGet = jest.mocked(getMySettings);
const mockSet = jest.mocked(setUnreadMessageDigest);
afterEach(() => jest.resetAllMocks());

const settings = (on: boolean) => ({ ok: true as const, data: { unreadMessageDigest: on } });

// renderedSwitch renders the switch once its setting (on) has loaded.
async function renderedSwitch() {
  render(<DigestToggle />);
  return screen.findByRole("switch", { name: "Email me about unread messages", checked: true });
}

describe("DigestToggle", () => {
  it("shows the saved setting with its hint", async () => {
    mockGet.mockResolvedValue(settings(false));
    render(<DigestToggle />);
    const toggle = await screen.findByRole("switch", { name: "Email me about unread messages", checked: false });
    expect(toggle.getAttribute("aria-describedby")).toBeTruthy();
    expect(screen.getByText("A daily digest of messages left unread for 24 hours.")).toBeTruthy();
  });

  it("turns the digest off and on", async () => {
    mockGet.mockResolvedValue(settings(true));
    mockSet.mockResolvedValueOnce(settings(false)).mockResolvedValueOnce(settings(true));
    const toggle = await renderedSwitch();
    expect(toggle.getAttribute("aria-checked")).toBe("true");

    await act(async () => fireEvent.click(toggle));
    expect(mockSet).toHaveBeenLastCalledWith(false);
    expect(toggle.getAttribute("aria-checked")).toBe("false");

    await act(async () => fireEvent.click(toggle));
    expect(mockSet).toHaveBeenLastCalledWith(true);
    expect(toggle.getAttribute("aria-checked")).toBe("true");
  });

  it("goes back and says so when saving fails", async () => {
    mockGet.mockResolvedValue(settings(true));
    mockSet.mockResolvedValue({ ok: false, code: "internal_error", message: "No." });
    const toggle = await renderedSwitch();
    await act(async () => fireEvent.click(toggle));
    expect(toggle.getAttribute("aria-checked")).toBe("true");
    expect(screen.getByRole("alert").textContent).toContain("Couldn't save");
  });

  it("offers to try again when loading fails", async () => {
    mockGet.mockResolvedValueOnce({ ok: false, code: "network_error", message: "No." }).mockResolvedValueOnce(settings(true));
    render(<DigestToggle />);
    expect((await screen.findByRole("alert")).textContent).toContain("Couldn't load");
    expect(screen.queryByRole("switch")).toBeNull();
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Try again" })));
    expect(screen.getByRole("switch", { checked: true })).toBeTruthy();
  });
});
