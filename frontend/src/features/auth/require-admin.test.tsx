import { act, render, screen } from "@testing-library/react";

import AdminPage from "@/app/(app)/admin/page";

import { AccessNotice, noAccessMessage } from "./access-notice";
import { listUsers } from "./api";
import { RequireAdmin } from "./require-admin";
import { type Session, useSession } from "./session";

jest.mock("./session");
const mockUseSession = jest.mocked(useSession);

jest.mock("./api");
const mockListUsers = jest.mocked(listUsers);

const mockReplace = jest.fn();
jest.mock("next/navigation", () => ({ useRouter: () => ({ replace: mockReplace }) }));

const signedInAs = (role: "researcher" | "admin"): Session => ({
  status: "signedIn",
  user: { id: "0b8f4c1e-0000-4000-8000-000000000001", email: "ada@example.org", fullName: "Ada Lovelace", role },
});

describe("RequireAdmin", () => {
  afterEach(() => jest.resetAllMocks());

  it("sends a researcher to /home with a notice and never shows the page", () => {
    mockUseSession.mockReturnValue(signedInAs("researcher"));
    const { unmount } = render(<RequireAdmin>admin data</RequireAdmin>);
    expect(mockReplace).toHaveBeenCalledWith("/home");
    expect(screen.queryByText("admin data")).toBeNull();
    unmount();

    // The page the user lands on shows the notice once.
    const first = render(<AccessNotice />);
    expect(screen.getByRole("status").textContent).toBe(noAccessMessage);
    first.unmount();
    render(<AccessNotice />);
    expect(screen.queryByText(noAccessMessage)).toBeNull();
  });

  it("hides the page while the session is loading, without redirecting", () => {
    mockUseSession.mockReturnValue({ status: "loading" });
    render(<RequireAdmin>admin data</RequireAdmin>);
    expect(screen.queryByText("admin data")).toBeNull();
    expect(mockReplace).not.toHaveBeenCalled();
  });

  it("shows the page to an administrator", () => {
    mockUseSession.mockReturnValue(signedInAs("admin"));
    render(<RequireAdmin>admin data</RequireAdmin>);
    expect(screen.getByText("admin data")).toBeTruthy();
    expect(mockReplace).not.toHaveBeenCalled();
  });
});

describe("/admin", () => {
  afterEach(() => jest.resetAllMocks());

  it("redirects a researcher without asking the API for users", async () => {
    mockUseSession.mockReturnValue(signedInAs("researcher"));
    render(<AdminPage />);
    await act(async () => {});
    expect(mockReplace).toHaveBeenCalledWith("/home");
    expect(mockListUsers).not.toHaveBeenCalled();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("loads the users for an administrator", async () => {
    mockUseSession.mockReturnValue(signedInAs("admin"));
    mockListUsers.mockResolvedValue({ ok: true, data: { items: [], page: 1, pageSize: 20, total: 0 } });
    render(<AdminPage />);
    expect(await screen.findByRole("table")).toBeTruthy();
    expect(mockListUsers).toHaveBeenCalledWith(1, 20);
    expect(mockReplace).not.toHaveBeenCalled();
  });
});
