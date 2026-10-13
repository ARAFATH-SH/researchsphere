import { act, render, screen } from "@testing-library/react";
import { StrictMode } from "react";

import { resendVerification, verifyEmail } from "./api";
import { VerifyEmail } from "./verify-email";

jest.mock("./api");
const mockVerify = jest.mocked(verifyEmail);
const mockResend = jest.mocked(resendVerification);

// The page remembers the check of each token for the whole page load, so every test uses
// a token of its own.
let n = 0;
let token = "";

describe("VerifyEmail", () => {
  beforeEach(() => {
    n += 1;
    token = String(n).padStart(43, "A");
    window.history.replaceState(null, "", "/verify-email#token=" + token);
  });
  afterEach(() => jest.resetAllMocks());

  it("verifies the token once, even in Strict Mode, and removes it from the address bar", async () => {
    mockVerify.mockResolvedValue({ ok: true, data: undefined });
    render(
      <StrictMode>
        <VerifyEmail />
      </StrictMode>,
    );
    expect(await screen.findByRole("heading", { name: "Email verified" })).toBeTruthy();
    expect(mockVerify).toHaveBeenCalledTimes(1);
    expect(mockVerify).toHaveBeenCalledWith(token);
    expect(window.location.hash).toBe("");
    expect(screen.getByRole("link", { name: "Sign in" }).getAttribute("href")).toBe("/login");
  });

  it("sends one request when the page is mounted again during the check", async () => {
    let answer: (value: Awaited<ReturnType<typeof verifyEmail>>) => void = () => {};
    mockVerify.mockReturnValue(new Promise((resolve) => (answer = resolve)));
    const first = render(
      <StrictMode>
        <VerifyEmail />
      </StrictMode>,
    );
    first.unmount();
    render(
      <StrictMode>
        <VerifyEmail />
      </StrictMode>,
    );
    expect(window.location.hash).toBe("#token=" + token);
    await act(async () => answer({ ok: true, data: undefined }));
    expect(await screen.findByRole("heading", { name: "Email verified" })).toBeTruthy();
    expect(mockVerify).toHaveBeenCalledTimes(1);
    expect(window.location.hash).toBe("");
  });

  it("keeps the token after an error, so reloading tries again", async () => {
    mockVerify.mockResolvedValue({ ok: false, code: "network_error", message: "x" });
    render(<VerifyEmail />);
    expect(await screen.findByRole("heading", { name: "Could not verify" })).toBeTruthy();
    expect(window.location.hash).toBe("#token=" + token);
  });

  it.each([
    { code: "token_used", heading: "Link already used", resend: false },
    { code: "token_expired", heading: "Link expired", resend: true },
    { code: "token_invalid", heading: "Link not valid", resend: true },
    { code: "internal_error", heading: "Could not verify", resend: false },
  ])("shows $heading for $code", async ({ code, heading, resend }) => {
    mockVerify.mockResolvedValue({ ok: false, code, message: "x" });
    render(<VerifyEmail />);
    expect(await screen.findByRole("heading", { name: heading })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Send a new link" }) !== null).toBe(resend);
    expect(screen.getByRole("link", { name: "Sign in" }).getAttribute("href")).toBe("/login");
  });

  it("asks for an address when the link has no token", async () => {
    window.history.replaceState(null, "", "/verify-email");
    render(<VerifyEmail />);
    expect(await screen.findByRole("heading", { name: "No verification link" })).toBeTruthy();
    expect(mockVerify).not.toHaveBeenCalled();
    expect(screen.getByLabelText("Email")).toBeTruthy();
    expect(mockResend).not.toHaveBeenCalled();
  });
});
