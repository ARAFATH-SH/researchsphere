import { fireEvent, render, screen } from "@testing-library/react";

import { changePassword } from "./api";
import { ChangePasswordForm } from "./change-password-form";
import { messages } from "./validation";

jest.mock("./api");
const mockChange = jest.mocked(changePassword);

function fill(current: string, next: string, confirm = next) {
  fireEvent.change(screen.getByLabelText("Current password"), {
    target: { value: current },
  });
  fireEvent.change(screen.getByLabelText("New password"), {
    target: { value: next },
  });
  fireEvent.change(screen.getByLabelText("Confirm new password"), {
    target: { value: confirm },
  });
}

const submit = () => fireEvent.click(screen.getByRole("button", { name: "Change password" }));

// errorFor returns the message linked to a field through aria-describedby.
function errorFor(label: string): string {
  const ids = (screen.getByLabelText(label).getAttribute("aria-describedby") ?? "").split(" ");
  const id = ids.find((x) => x.endsWith("-error"));
  return id ? (document.getElementById(id)?.textContent ?? "") : "";
}

describe("ChangePasswordForm", () => {
  afterEach(() => jest.resetAllMocks());

  it("refuses a confirmation that does not match, without calling the API", () => {
    render(<ChangePasswordForm />);
    fill("password1", "new password 1", "new password 2");
    submit();
    expect(errorFor("Confirm new password")).toBe(messages.passwordMismatch);
    expect(screen.getByLabelText("Confirm new password").getAttribute("aria-invalid")).toBe("true");
    expect(mockChange).not.toHaveBeenCalled();
  });

  it("refuses a new password under 8 characters, without calling the API", () => {
    render(<ChangePasswordForm />);
    fill("password1", "1234567");
    submit();
    expect(errorFor("New password")).toBe(messages.passwordTooShort);
    expect(mockChange).not.toHaveBeenCalled();
  });

  it("counts characters, not UTF-16 units, as the server does", () => {
    render(<ChangePasswordForm />);
    fill("password1", "😀😀😀😀"); // 4 characters, 8 UTF-16 units
    submit();
    expect(errorFor("New password")).toBe(messages.passwordTooShort);
    expect(mockChange).not.toHaveBeenCalled();
  });

  it("asks for the current password", () => {
    render(<ChangePasswordForm />);
    fill("", "new password 1");
    submit();
    expect(errorFor("Current password")).toBe(messages.currentPasswordRequired);
    expect(mockChange).not.toHaveBeenCalled();
  });

  it("changes the password, clears the fields and says other devices were signed out", async () => {
    mockChange.mockResolvedValue({ ok: true, data: undefined });
    render(<ChangePasswordForm />);
    fill("password1", "new password 1");
    submit();
    const status = await screen.findByRole("status");
    expect(status.textContent).toMatch(/password has been changed/i);
    expect(status.textContent).toMatch(/signed out on your other devices/i);
    expect(mockChange).toHaveBeenCalledWith({
      currentPassword: "password1",
      newPassword: "new password 1",
    });
    for (const label of ["Current password", "New password", "Confirm new password"]) {
      expect((screen.getByLabelText(label) as HTMLInputElement).value).toBe("");
    }
  });

  it.each([
    ["wrong_current_password", "Current password", "The current password is wrong."],
    ["same_password", "New password", "Choose a password different from the current one."],
    ["password_too_short", "New password", "The password must be at least 8 characters long."],
  ])("shows the server's %s under the field", async (code, label, message) => {
    mockChange.mockResolvedValue({ ok: false, code, message });
    render(<ChangePasswordForm />);
    fill("password1", "new password 1");
    submit();
    expect(await screen.findByText(message)).toBeTruthy();
    expect(errorFor(label)).toBe(message);
    expect(screen.queryByRole("status")).toBeNull();
    expect((screen.getByLabelText("Current password") as HTMLInputElement).value).toBe("password1");
  });

  it("shows a locked account in a banner", async () => {
    const message = "This account is locked after too many failed login attempts. Try again in 15 minutes.";
    mockChange.mockResolvedValue({
      ok: false,
      code: "account_locked",
      message,
    });
    render(<ChangePasswordForm />);
    fill("wrong-password", "new password 1");
    submit();
    expect((await screen.findByRole("alert")).textContent).toBe(message);
  });

  it("drops the success message when the user edits the form again", async () => {
    mockChange.mockResolvedValue({ ok: true, data: undefined });
    render(<ChangePasswordForm />);
    fill("password1", "new password 1");
    submit();
    await screen.findByRole("status");
    fireEvent.change(screen.getByLabelText("Current password"), {
      target: { value: "n" },
    });
    expect(screen.queryByRole("status")).toBeNull();
  });
});
