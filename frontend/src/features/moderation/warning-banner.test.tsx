import { act, render, screen, waitFor } from "@testing-library/react";

import type { Session } from "@/features/auth/session";

import { listPendingWarnings } from "./api";
import { clearPendingWarnings, refreshPendingWarnings } from "./pending-warnings";
import { warning } from "./test-data";
import { WarningBanner } from "./warning-banner";

jest.mock("./api");
const mockPending = jest.mocked(listPendingWarnings);
const mockSession = jest.fn<Session, []>();
jest.mock("@/features/auth/session", () => ({ useSession: () => mockSession() }));
let connected: () => void = () => {};
let event: (e: unknown) => void = () => {};
jest.mock("@/features/realtime/use-realtime", () => ({
  useOnConnected: (handler: () => void) => {
    connected = handler;
  },
  useRealtimeEvent: (handler: (e: unknown) => void) => {
    event = handler;
  },
}));

const signedIn: Session = {
  status: "signedIn",
  user: { id: "u1", email: "ada@example.org", fullName: "Ada", role: "researcher" },
};

beforeEach(() => {
  mockSession.mockReturnValue(signedIn);
  mockPending.mockResolvedValue({ ok: true, data: [] });
});
afterEach(() => {
  act(() => clearPendingWarnings());
  jest.resetAllMocks();
});

describe("WarningBanner", () => {
  it("shows while a warning is pending and links to the newest", async () => {
    mockPending.mockResolvedValue({ ok: true, data: [warning({ id: "new" }), warning({ id: "old" })] });
    render(<WarningBanner />);
    expect((await screen.findByRole("status")).textContent).toContain("You received a warning from ResearchSphere.");
    expect(screen.getByRole("link", { name: "Read it" }).getAttribute("href")).toBe("/warnings/new");
  });

  it("shows nothing with no warnings", async () => {
    render(<WarningBanner />);
    await waitFor(() => expect(mockPending).toHaveBeenCalled());
    await act(async () => {});
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("asks nothing while signed out", async () => {
    mockSession.mockReturnValue({ status: "signedOut" });
    render(<WarningBanner />);
    await act(async () => {});
    expect(mockPending).not.toHaveBeenCalled();
  });

  it("goes away once the list is empty", async () => {
    mockPending.mockResolvedValue({ ok: true, data: [warning()] });
    render(<WarningBanner />);
    await screen.findByRole("status");
    mockPending.mockResolvedValue({ ok: true, data: [] });
    await act(() => refreshPendingWarnings());
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("appears when a warning notification arrives, and after a reconnect", async () => {
    render(<WarningBanner />);
    await waitFor(() => expect(mockPending).toHaveBeenCalledTimes(1));
    await act(async () => {
      event({ type: "notification.created", data: { type: "connection_request_received" } });
    });
    expect(mockPending).toHaveBeenCalledTimes(1);
    mockPending.mockResolvedValue({ ok: true, data: [warning()] });
    await act(async () => {
      event({ type: "notification.created", data: { type: "account_warning" } });
    });
    expect(screen.getByRole("status")).toBeTruthy();
    await act(async () => connected());
    expect(mockPending).toHaveBeenCalledTimes(3);
  });

  it("keeps what it shows when a read fails", async () => {
    mockPending.mockResolvedValue({ ok: true, data: [warning()] });
    render(<WarningBanner />);
    await screen.findByRole("status");
    mockPending.mockResolvedValue({ ok: false, code: "network_error", message: "No." });
    await act(() => refreshPendingWarnings());
    expect(screen.getByRole("status")).toBeTruthy();
  });
});
