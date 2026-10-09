import { render, screen } from "@testing-library/react";
import { StrictMode } from "react";

import { resendVerification, verifyEmail } from "./api";
import { VerifyEmail } from "./verify-email";

jest.mock("./api");
const mockVerify = jest.mocked(verifyEmail);
const mockResend = jest.mocked(resendVerification);

const token = "A".repeat(43);

describe("VerifyEmail", () => {
  beforeEach(() => window.history.replaceState(null, "", "/verify-email#token=" + token));
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
