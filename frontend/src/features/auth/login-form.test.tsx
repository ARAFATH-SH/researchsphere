import { fireEvent, render, screen, waitFor } from "@testing-library/react";

import { login, resendVerification } from "./api";
import { INVALID_CREDENTIALS_MESSAGE, LoginForm } from "./login-form";
import { SENT_MESSAGE } from "./resend-form";
import { setSignedIn } from "./session";

jest.mock("./api");
jest.mock("./session");
const mockLogin = jest.mocked(login);
const mockResend = jest.mocked(resendVerification);
const mockSetSignedIn = jest.mocked(setSignedIn);

const mockReplace = jest.fn();
jest.mock("next/navigation", () => ({ useRouter: () => ({ replace: mockReplace }) }));

const user = { id: "0b8f4c1e-0000-4000-8000-000000000001", email: "ada@example.org", fullName: "Ada Lovelace" };

function fill(email: string, password: string) {
  fireEvent.change(screen.getByLabelText("Email"), { target: { value: email } });
  fireEvent.change(screen.getByLabelText("Password"), { target: { value: password } });
}

const submit = () => fireEvent.click(screen.getByRole("button", { name: "Sign in" }));

// errorFor returns the message linked to a field through aria-describedby.
function errorFor(label: string): string {
  const ids = (screen.getByLabelText(label).getAttribute("aria-describedby") ?? "").split(" ");
  const id = ids.find((x) => x.endsWith("-error"));
  return id ? (document.getElementById(id)?.textContent ?? "") : "";
}

describe("LoginForm", () => {
  afterEach(() => jest.resetAllMocks());

  it("asks for both fields when they are empty and does not call the API", () => {
    render(<LoginForm />);
    submit();
    expect(errorFor("Email")).toBe("Enter your email address.");
    expect(errorFor("Password")).toBe("Enter your password.");
    expect(screen.getByLabelText("Email").getAttribute("aria-invalid")).toBe("true");
    expect(mockLogin).not.toHaveBeenCalled();
  });

  it.each(["ada.example.org", "ada lovelace@example.org", "@example.org"])("refuses the email address %j", (email) => {
    render(<LoginForm />);
    fill(email, "any password");
    submit();
    expect(errorFor("Email")).toBe("Enter a valid email address, such as name@example.org.");
    expect(errorFor("Password")).toBe("");
    expect(mockLogin).not.toHaveBeenCalled();
  });

  it("does not check the password length: that is the server's answer", async () => {
    mockLogin.mockResolvedValue({ ok: true, data: user });
    render(<LoginForm />);
    fill("ada@example.org", "short");
    submit();
    await waitFor(() => expect(mockLogin).toHaveBeenCalledWith({ email: "ada@example.org", password: "short" }));
  });

  it("shows one generic message for a wrong email address or password (401)", async () => {
    mockLogin.mockResolvedValue({ ok: false, code: "invalid_credentials", message: "Wrong password for this account." });
    render(<LoginForm />);
    fill("ada@example.org", "wrong password");
    submit();
    expect((await screen.findByRole("alert")).textContent).toBe(INVALID_CREDENTIALS_MESSAGE);
    expect(screen.queryByText("Wrong password for this account.")).toBeNull();
    expect(errorFor("Email")).toBe("");
    expect(errorFor("Password")).toBe("");
    expect(mockReplace).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "Resend verification" })).toBeNull();
  });

  it("offers a new verification link when the address is not verified (FR-1.4)", async () => {
    mockLogin.mockResolvedValue({ ok: false, code: "email_not_verified", message: "Verify your email address first." });
    mockResend.mockResolvedValue({ ok: true, data: undefined });
    render(<LoginForm />);
    fill(" ada@example.org ", "right password");
    submit();
    expect((await screen.findByRole("alert")).textContent).toBe("Verify your email address first.");
    fireEvent.click(screen.getByRole("button", { name: "Resend verification" }));
    await screen.findByText(SENT_MESSAGE);
    expect(mockResend).toHaveBeenCalledWith("ada@example.org");
    expect(mockReplace).not.toHaveBeenCalled();
  });

  it("shows other errors above the form", async () => {
    mockLogin.mockResolvedValue({ ok: false, code: "network_error", message: "Could not reach the server." });
    render(<LoginForm />);
    fill("ada@example.org", "a password");
    submit();
    expect((await screen.findByRole("alert")).textContent).toBe("Could not reach the server.");
  });

  it("stores the user and goes to /home", async () => {
    mockLogin.mockResolvedValue({ ok: true, data: user });
    render(<LoginForm />);
    fill(" ada@example.org ", " pass word ");
    submit();
    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith("/home"));
    expect(mockLogin).toHaveBeenCalledWith({ email: "ada@example.org", password: " pass word " });
    expect(mockSetSignedIn).toHaveBeenCalledWith(user);
  });

  it("links to the password reset page", () => {
    render(<LoginForm />);
    expect(screen.getByRole("link", { name: "Forgot password?" }).getAttribute("href")).toBe("/forgot-password");
  });
});
