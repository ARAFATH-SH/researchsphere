import { act, fireEvent, render, screen } from "@testing-library/react";

import { unsubscribeDigest } from "./api";
import { SETTINGS_HREF, Unsubscribe } from "./unsubscribe";

jest.mock("./api");
const mockUnsubscribe = jest.mocked(unsubscribeDigest);
afterEach(() => {
  jest.resetAllMocks();
  window.history.replaceState(null, "", "/");
});

function openLink(search: string) {
  window.history.replaceState(null, "", "/unsubscribe" + search);
}

describe("Unsubscribe", () => {
  it("turns the digest off once, without a sign-in, and links to Settings", async () => {
    openLink("?token=abc.def");
    mockUnsubscribe.mockResolvedValue({ ok: true, data: undefined });
    render(<Unsubscribe />);
    expect(screen.getByRole("status").textContent).toContain("Turning off");
    expect(await screen.findByText("You won't get unread message emails any more.")).toBeTruthy();
    expect(mockUnsubscribe).toHaveBeenCalledTimes(1);
    expect(mockUnsubscribe).toHaveBeenCalledWith("abc.def");
    expect(screen.getByRole("link", { name: "Turn it back on in Settings" }).getAttribute("href")).toBe(SETTINGS_HREF);
    // The page never turns the digest back on itself.
    expect(screen.queryByRole("button", { name: /turn .* on/i })).toBeNull();
  });

  it("says when the link is not valid", async () => {
    openLink("?token=forged");
    mockUnsubscribe.mockResolvedValue({ ok: false, code: "invalid_token", message: "No.", field: "token" });
    render(<Unsubscribe />);
    expect(await screen.findByRole("heading", { name: "Link not valid" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Open Settings" }).getAttribute("href")).toBe(SETTINGS_HREF);
  });

  it("asks for nothing without a token", async () => {
    openLink("");
    render(<Unsubscribe />);
    expect(await screen.findByText("This page needs the link from an unread message email.")).toBeTruthy();
    expect(mockUnsubscribe).not.toHaveBeenCalled();
  });

  it("offers to try again after an error", async () => {
    openLink("?token=abc.def");
    mockUnsubscribe
      .mockResolvedValueOnce({ ok: false, code: "network_error", message: "No." })
      .mockResolvedValueOnce({ ok: true, data: undefined });
    render(<Unsubscribe />);
    expect((await screen.findByRole("alert")).textContent).toContain("not turned off yet");
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Try again" })));
    expect(screen.getByText("You won't get unread message emails any more.")).toBeTruthy();
    expect(mockUnsubscribe).toHaveBeenCalledTimes(2);
  });
});
