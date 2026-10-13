import { fireEvent, render, screen } from "@testing-library/react";

import { requestPasswordReset } from "./api";
import { ForgotPasswordForm, RESET_SENT_MESSAGE } from "./forgot-password-form";

jest.mock("./api");
const mockRequest = jest.mocked(requestPasswordReset);

function send(email: string) {
  fireEvent.change(screen.getByLabelText("Email"), { target: { value: email } });
  fireEvent.click(screen.getByRole("button", { name: "Send reset link" }));
}

describe("ForgotPasswordForm", () => {
  afterEach(() => jest.resetAllMocks());

  it("shows the same message whatever the server knows about the address", async () => {
    mockRequest.mockResolvedValue({ ok: true, data: undefined });
    render(<ForgotPasswordForm />);
    send("  ada@example.org ");
    expect(await screen.findByText(RESET_SENT_MESSAGE)).toBeTruthy();
    expect(mockRequest).toHaveBeenCalledWith("ada@example.org");
  });

  it.each([
    { name: "an empty address", email: "", want: "Enter your email address." },
    { name: "a malformed address", email: "ada.example.org", want: "Enter a valid email address, such as name@example.org." },
  ])("refuses $name without calling the API", ({ email, want }) => {
    render(<ForgotPasswordForm />);
    send(email);
    expect(screen.getByText(want)).toBeTruthy();
    expect(screen.queryByText(RESET_SENT_MESSAGE)).toBeNull();
    expect(mockRequest).not.toHaveBeenCalled();
  });

  it("shows the server's error and no sent message", async () => {
    mockRequest.mockResolvedValue({ ok: false, code: "network_error", message: "Could not reach the server." });
    render(<ForgotPasswordForm />);
    send("ada@example.org");
    expect(await screen.findByText("Could not reach the server.")).toBeTruthy();
    expect(screen.queryByText(RESET_SENT_MESSAGE)).toBeNull();
  });
});
