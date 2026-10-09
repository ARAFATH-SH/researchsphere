import { fireEvent, render, screen, waitFor } from "@testing-library/react";

import { register } from "./api";
import { RegisterForm } from "./register-form";

jest.mock("./api");
const mockRegister = jest.mocked(register);

const mockPush = jest.fn();
jest.mock("next/navigation", () => ({ useRouter: () => ({ push: mockPush }) }));

const labels = { fullName: "Full name", email: "Email", password: "Password", confirmPassword: "Confirm password" };

function fill(values: Partial<Record<keyof typeof labels, string>>) {
  for (const [field, value] of Object.entries(values)) {
    fireEvent.change(screen.getByLabelText(labels[field as keyof typeof labels]), { target: { value } });
  }
}

// fillValid fills every field with valid values, then applies overrides.
function fillValid(overrides: Partial<Record<keyof typeof labels, string>> = {}) {
  const password = overrides.password ?? "long enough";
  fill({ fullName: "Ada Lovelace", email: "ada@example.org", password, confirmPassword: password, ...overrides });
}

const submit = () => fireEvent.click(screen.getByRole("button", { name: "Create account" }));

// errorFor returns the message linked to a field through aria-describedby.
function errorFor(label: string): string {
  const input = screen.getByLabelText(label);
  const ids = (input.getAttribute("aria-describedby") ?? "").split(" ");
  const id = ids.find((x) => x.endsWith("-error"));
  return id ? (document.getElementById(id)?.textContent ?? "") : "";
}

describe("RegisterForm", () => {
  beforeEach(() => sessionStorage.clear());
  afterEach(() => jest.resetAllMocks());

  it("shows a message next to every empty field and does not call the API", () => {
    render(<RegisterForm />);
    submit();
    expect(errorFor("Full name")).toBe("Enter your full name.");
    expect(errorFor("Email")).toBe("Enter a valid email address, such as name@example.org.");
    expect(errorFor("Password")).toBe("The password must be at least 8 characters long.");
    expect(errorFor("Confirm password")).toBe("");
    expect(screen.getByLabelText("Password").getAttribute("aria-invalid")).toBe("true");
    expect(mockRegister).not.toHaveBeenCalled();
  });

  it.each([
    { name: "a name of spaces", values: { fullName: "   " }, field: "Full name", want: "Enter your full name." },
    { name: "a bad email address", values: { email: "ada.example.org" }, field: "Email", want: "Enter a valid email address, such as name@example.org." },
    { name: "an email address with a space", values: { email: "ada lovelace@example.org" }, field: "Email", want: "Enter a valid email address, such as name@example.org." },
    { name: "a short password", values: { password: "1234567" }, field: "Password", want: "The password must be at least 8 characters long." },
    { name: "a password of 7 accented characters", values: { password: "ääääääá" }, field: "Password", want: "The password must be at least 8 characters long." },
    { name: "a password of 129 characters", values: { password: "x".repeat(129) }, field: "Password", want: "The password must be at most 128 characters long." },
    { name: "passwords that do not match", values: { confirmPassword: "long enougH" }, field: "Confirm password", want: "Passwords do not match." },
  ])("refuses $name", ({ values, field, want }) => {
    render(<RegisterForm />);
    fillValid(values);
    submit();
    expect(errorFor(field)).toBe(want);
    expect(mockRegister).not.toHaveBeenCalled();
  });

  it.each([
    { code: "email_taken", message: "This email address is already registered.", field: "Email" },
    { code: "invalid_email", message: "Enter a valid email address.", field: "Email" },
    { code: "password_too_short", message: "The password must be at least 8 characters long.", field: "Password" },
    { code: "invalid_full_name", message: "Enter your full name.", field: "Full name" },
  ])("shows the server's $code next to $field", async ({ code, message, field }) => {
    mockRegister.mockResolvedValue({ ok: false, code, message });
    render(<RegisterForm />);
    fillValid();
    submit();
    await screen.findByText(message);
    expect(errorFor(field)).toBe(message);
    expect(screen.queryByRole("alert")).toBeNull();
    expect(mockPush).not.toHaveBeenCalled();
  });

  it("shows other server errors above the form", async () => {
    mockRegister.mockResolvedValue({ ok: false, code: "network_error", message: "Could not reach the server." });
    render(<RegisterForm />);
    fillValid();
    submit();
    expect((await screen.findByRole("alert")).textContent).toBe("Could not reach the server.");
  });

  it("sends trimmed values and goes to the check-email page", async () => {
    mockRegister.mockResolvedValue({ ok: true, data: undefined });
    render(<RegisterForm />);
    fill({ fullName: "  Ada Lovelace ", email: " ada@example.org ", password: " long enough ", confirmPassword: " long enough " });
    submit();
    await waitFor(() => expect(mockPush).toHaveBeenCalledWith("/check-email"));
    // The confirmation stays in the browser: the API gets only the three FR-1.1 fields.
    expect(mockRegister).toHaveBeenCalledWith({ fullName: "Ada Lovelace", email: "ada@example.org", password: " long enough " });
    expect(sessionStorage.getItem("researchsphere.pendingVerificationEmail")).toBe("ada@example.org");
  });
});
