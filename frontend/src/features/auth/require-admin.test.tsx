import { act, render, screen } from "@testing-library/react";

import AdminPage from "@/app/(app)/admin/page";
import AdminReportPage from "@/app/(app)/admin/reports/[reportId]/page";
import AdminUserPage from "@/app/(app)/admin/users/[userId]/page";
import { getMessageReport, getUserModeration, listMessageReports } from "@/features/moderation/api";

import { AccessNotice, noAccessMessage } from "./access-notice";
import { listUsers } from "./api";
import { RequireAdmin } from "./require-admin";
import { type Session, useSession } from "./session";

jest.mock("./session");
const mockUseSession = jest.mocked(useSession);

jest.mock("./api");
const mockListUsers = jest.mocked(listUsers);
jest.mock("@/features/moderation/api");
const mockListReports = jest.mocked(listMessageReports);

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
    expect(screen.queryByRole("navigation", { name: "Admin sections" })).toBeNull();
  });

  it("loads the users for an administrator", async () => {
    mockUseSession.mockReturnValue(signedInAs("admin"));
    mockListUsers.mockResolvedValue({ ok: true, data: { items: [], page: 1, pageSize: 20, total: 0 } });
    mockListReports.mockResolvedValue({ ok: true, data: { items: [], page: 1, pageSize: 20, total: 0 } });
    render(<AdminPage />);
    expect(await screen.findByRole("table")).toBeTruthy();
    await screen.findByText("No open reports.");
    expect(mockListUsers).toHaveBeenCalledWith(1, 20);
    expect(mockReplace).not.toHaveBeenCalled();
  });

  it.each([
    ["the admin page", () => Promise.resolve(AdminPage())],
    ["a report", () => AdminReportPage({ params: Promise.resolve({ reportId: "r" }) } as never)],
    ["a user's moderation page", () => AdminUserPage({ params: Promise.resolve({ userId: "u" }) } as never)],
  ])("refuses a non-admin on %s, before any admin data is asked for", async (_, page) => {
    mockUseSession.mockReturnValue(signedInAs("researcher"));
    const element = await page();
    await act(async () => {
      render(element);
    });
    expect(mockReplace).toHaveBeenCalledWith("/home");
    expect(getMessageReport).not.toHaveBeenCalled();
    expect(getUserModeration).not.toHaveBeenCalled();
    expect(listMessageReports).not.toHaveBeenCalled();
    expect(mockListUsers).not.toHaveBeenCalled();
  });
});
