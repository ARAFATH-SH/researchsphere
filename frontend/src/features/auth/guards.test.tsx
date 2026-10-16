import { render, screen } from "@testing-library/react";

import { RequireSession } from "./require-session";
import { type Session, useSession } from "./session";

jest.mock("./session");
const mockUseSession = jest.mocked(useSession);

const mockReplace = jest.fn();
jest.mock("next/navigation", () => ({ useRouter: () => ({ replace: mockReplace }) }));

const signedIn: Session = {
  status: "signedIn",
  user: { id: "0b8f4c1e-0000-4000-8000-000000000001", email: "ada@example.org", fullName: "Ada Lovelace", role: "researcher" as const },
};

describe("RequireSession", () => {
  afterEach(() => jest.resetAllMocks());

  it("sends a signed-out user to /login and hides the page", () => {
    mockUseSession.mockReturnValue({ status: "signedOut" });
    render(<RequireSession>secret page</RequireSession>);
    expect(mockReplace).toHaveBeenCalledWith("/login");
    expect(screen.queryByText("secret page")).toBeNull();
  });

  it("hides the page while the session is loading, without redirecting", () => {
    mockUseSession.mockReturnValue({ status: "loading" });
    render(<RequireSession>secret page</RequireSession>);
    expect(screen.queryByText("secret page")).toBeNull();
    expect(mockReplace).not.toHaveBeenCalled();
  });

  it("shows the page to a signed-in user", () => {
    mockUseSession.mockReturnValue(signedIn);
    render(<RequireSession>secret page</RequireSession>);
    expect(screen.getByText("secret page")).toBeTruthy();
    expect(mockReplace).not.toHaveBeenCalled();
  });
});
