import { act, fireEvent, render, screen } from "@testing-library/react";
import { StrictMode } from "react";

import { confirmPasswordReset } from "./api";
import { ResetPassword } from "./reset-password";

jest.mock("./api");
const mockConfirm = jest.mocked(confirmPasswordReset);

const token = "A".repeat(43);

function fill(password: string, confirmPassword = password) {
  fireEvent.change(screen.getByLabelText("New password"), { target: { value: password } });
  fireEvent.change(screen.getByLabelText("Confirm new password"), { target: { value: confirmPassword } });
}

const submit = () => fireEvent.click(screen.getByRole("button", { name: "Set new password" }));

// errorFor returns the message linked to a field through aria-describedby.
function errorFor(label: string): string {
  const ids = (screen.getByLabelText(label).getAttribute("aria-describedby") ?? "").split(" ");
  const id = ids.find((x) => x.endsWith("-error"));
  return id ? (document.getElementById(id)?.textContent ?? "") : "";
}

describe("ResetPassword", () => {
  beforeEach(() => window.history.replaceState(null, "", "/reset-password#token=" + token));
  afterEach(() => jest.resetAllMocks());

  it("makes no request until submit and shows the form, even in Strict Mode", async () => {
    window.history.replaceState(null, "", "/reset-password#token=abc");
    const fetchSpy = jest.fn();
    const realFetch = globalThis.fetch;
    globalThis.fetch = fetchSpy;
    try {
      mockConfirm.mockResolvedValue({ ok: true, data: undefined });
      render(
        <StrictMode>
          <ResetPassword />
        </StrictMode>,
      );
      expect(screen.getByRole("heading", { name: "Choose a new password" })).toBeTruthy();
      expect(screen.queryByRole("heading", { name: "No reset link" })).toBeNull();
      // Let pending effects and microtasks run: still nothing is sent.
      await act(async () => {});
      expect(screen.getByRole("heading", { name: "Choose a new password" })).toBeTruthy();
      expect(window.location.hash).toBe("#token=abc");
      expect(mockConfirm).not.toHaveBeenCalled();
      expect(fetchSpy).not.toHaveBeenCalled();

      fill("new password 1");
      submit();
      expect(await screen.findByRole("heading", { name: "Password changed" })).toBeTruthy();
      expect(mockConfirm).toHaveBeenCalledTimes(1);
      expect(mockConfirm).toHaveBeenCalledWith("abc", "new password 1");
    } finally {
      globalThis.fetch = realFetch;
    }
  });

  it("still finds the token when the page is mounted again", () => {
    const first = render(<ResetPassword />);
    first.unmount();
    render(<ResetPassword />);
    expect(screen.getByRole("heading", { name: "Choose a new password" })).toBeTruthy();
  });

  it("sets the password and then removes the token from the address bar", async () => {
    mockConfirm.mockResolvedValue({ ok: true, data: undefined });
    render(
      <StrictMode>
        <ResetPassword />
      </StrictMode>,
    );
    expect(window.location.hash).toBe("#token=" + token);
    fill("new password 1");
    submit();
    expect(await screen.findByRole("heading", { name: "Password changed" })).toBeTruthy();
    expect(window.location.hash).toBe("");
    expect(mockConfirm).toHaveBeenCalledTimes(1);
    expect(mockConfirm).toHaveBeenCalledWith(token, "new password 1");
    expect(screen.getByRole("link", { name: "Sign in" }).getAttribute("href")).toBe("/login");
  });

  it.each([
    { name: "a short password", password: "1234567", confirm: "1234567", field: "New password", want: "The password must be at least 8 characters long." },
    { name: "an empty password", password: "", confirm: "", field: "New password", want: "The password must be at least 8 characters long." },
    { name: "passwords that do not match", password: "long enough", confirm: "long enougH", field: "Confirm new password", want: "Passwords do not match." },
  ])("refuses $name without calling the API", ({ password, confirm, field, want }) => {
    render(<ResetPassword />);
    fill(password, confirm);
    submit();
    expect(errorFor(field)).toBe(want);
    expect(screen.getByLabelText(field).getAttribute("aria-invalid")).toBe("true");
    expect(mockConfirm).not.toHaveBeenCalled();
  });

  it.each([
    { code: "token_expired", heading: "Link expired" },
    { code: "token_used", heading: "Link already used" },
    { code: "token_invalid", heading: "Link not valid" },
  ])("shows $heading with a link to ask again for $code", async ({ code, heading }) => {
    mockConfirm.mockResolvedValue({ ok: false, code, message: "x" });
    render(<ResetPassword />);
    fill("new password 1");
    submit();
    expect(await screen.findByRole("heading", { name: heading })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Ask for a new link" }).getAttribute("href")).toBe("/forgot-password");
    expect(window.location.hash).toBe("");
  });

  it("shows the server's password message next to the field", async () => {
    mockConfirm.mockResolvedValue({ ok: false, code: "password_too_long", message: "The password must be at most 128 characters long." });
    render(<ResetPassword />);
    fill("new password 1");
    submit();
    expect(await screen.findByText("The password must be at most 128 characters long.")).toBeTruthy();
    expect(screen.getByLabelText("New password").getAttribute("aria-invalid")).toBe("true");
  });

  it("shows other errors above the form", async () => {
    mockConfirm.mockResolvedValue({ ok: false, code: "network_error", message: "Could not reach the server." });
    render(<ResetPassword />);
    fill("new password 1");
    submit();
    expect((await screen.findByRole("alert")).textContent).toBe("Could not reach the server.");
  });

  it("asks for a new link when the address has no token", () => {
    window.history.replaceState(null, "", "/reset-password");
    render(<ResetPassword />);
    expect(screen.getByRole("heading", { name: "No reset link" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Ask for a new link" }).getAttribute("href")).toBe("/forgot-password");
  });
});
