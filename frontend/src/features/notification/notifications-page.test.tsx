import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { FakeWebSocket } from "@/features/realtime/fake-websocket";
import { startSocket, stopSocket } from "@/features/realtime/socket";
import { acceptConnectionRequest, rejectConnectionRequest } from "@/features/social/api";
import { fakeDate } from "@/test/fake-date";

import {
  listNotifications,
  markAllNotificationsRead,
  markNotificationRead,
  type Notification,
  type NotificationPage,
} from "./api";
import { refreshBadges } from "./badges";
import { NotificationsPage } from "./notifications-page";
import { resetUnreadNotifications, setUnreadNotifications } from "./unread-count";

jest.mock("./api");
jest.mock("./badges");
jest.mock("@/features/social/api");
jest.mock("@/features/auth/session", () => ({
  useSession: () => ({
    status: "signedIn",
    user: { id: adaID, email: "ada@example.org", fullName: "Ada Lovelace", role: "researcher" },
  }),
}));
const mockPush = jest.fn();
jest.mock("next/navigation", () => ({ useRouter: () => ({ push: mockPush }) }));

const adaID = "0b8f4c1e-0000-4000-8000-000000000001";

const mockList = jest.mocked(listNotifications);
const mockRead = jest.mocked(markNotificationRead);
const mockReadAll = jest.mocked(markAllNotificationsRead);
const mockAccept = jest.mocked(acceptConnectionRequest);
const mockReject = jest.mocked(rejectConnectionRequest);
const mockRefresh = jest.mocked(refreshBadges);

function actor(n: number, name: string) {
  return { userId: `0b8f4c1e-0000-4000-8000-0000000001${String(n).padStart(2, "0")}`, fullName: name, photoUrl: null };
}

function received(id: string, name: string, overrides: Partial<Notification> = {}): Notification {
  return {
    id,
    type: "connection_request_received",
    createdAt: "2026-11-03T10:00:00Z",
    read: false,
    link: "/network",
    actor: actor(1, name),
    requestId: `7a1d2c3e-0000-4000-8000-0000000000${id.slice(-2)}`,
    requestPending: true,
    ...overrides,
  };
}

function accepted(id: string, name: string, overrides: Partial<Notification> = {}): Notification {
  const who = actor(2, name);
  return {
    id,
    type: "connection_request_accepted",
    createdAt: "2026-11-01T09:00:00Z",
    read: true,
    link: `/profile/${who.userId}`,
    actor: who,
    ...overrides,
  };
}

function page(items: Notification[], nextCursor: string | null = null) {
  return { ok: true as const, data: { items, nextCursor } satisfies NotificationPage };
}

const ok = { ok: true as const, data: undefined };
const list = () => screen.getByRole("list", { name: "Notifications" });
const rows = () => within(list()).getAllByRole("listitem");

async function renderPage() {
  render(<NotificationsPage />);
  await act(async () => {});
}

beforeEach(() => {
  fakeDate("2026-11-03T12:00:00Z");
  resetUnreadNotifications();
});
afterEach(() => {
  jest.useRealTimers();
  jest.resetAllMocks();
});

describe("NotificationsPage", () => {
  it("shows a system notification with the ResearchSphere mark and its own sentence", async () => {
    const warning: Notification = {
      id: "n-09",
      type: "account_warning",
      createdAt: "2026-11-03T10:00:00Z",
      read: false,
      link: "/warnings/7a1d2c3e-0000-4000-8000-000000000009",
      actor: null,
    };
    mockList.mockResolvedValue(page([warning]));
    await renderPage();

    const [row] = rows();
    const link = within(row).getByRole("link", {
      name: "Unread: You received a warning from ResearchSphere. Tap to read it.",
    });
    expect(link.getAttribute("href")).toBe(warning.link);
    expect(within(row).getByText("RS")).toBeTruthy();
    expect(within(row).getByRole("button", { name: "Mark notification from ResearchSphere as read" })).toBeTruthy();
    expect(within(row).queryByRole("button", { name: /Accept/ })).toBeNull();
  });

  it("says a sent file was blocked, from ResearchSphere, linking to the conversation (FR-16.7)", async () => {
    const blocked: Notification = {
      id: "n-11",
      type: "attachment_blocked",
      createdAt: "2026-11-03T10:00:00Z",
      read: false,
      link: "/messaging/4c2f7d3e-0000-4000-8000-0000000000c1",
      actor: null,
    };
    mockList.mockResolvedValue(page([blocked]));
    await renderPage();

    const [row] = rows();
    const link = within(row).getByRole("link", {
      name: "Unread: A file you sent was removed because it failed a security check.",
    });
    expect(link.getAttribute("href")).toBe(blocked.link);
    expect(within(row).getByText("RS")).toBeTruthy();
  });

  it("also shows a system notification when it has no actor field at all", async () => {
    mockList.mockResolvedValue(
      page([{ id: "n-10", type: "account_warning", createdAt: "2026-11-03T10:00:00Z", read: true, link: "/warnings/x" }]),
    );
    await renderPage();

    expect(within(rows()[0]).getByRole("link", { name: /You received a warning/ })).toBeTruthy();
  });

  it("lists each type with its sentence, photo initials and time, newest first", async () => {
    mockList.mockResolvedValue(page([received("n-01", "Bob Babbage"), accepted("n-02", "Grace Hopper")]));
    await renderPage();

    expect(screen.getByRole("heading", { level: 1, name: "Notifications" })).toBeTruthy();
    const [first, second] = rows();
    expect(within(first).getByRole("link", { name: "Unread: Bob Babbage sent you a connection request." })).toBeTruthy();
    expect(within(first).getByText("2 h ago")).toBeTruthy();
    expect(within(first).getByText("BB")).toBeTruthy();
    expect(first.className).toContain("bg-rs-accent-soft");

    const link = within(second).getByRole("link", { name: "Grace Hopper accepted your connection request." });
    expect(link.getAttribute("href")).toBe(accepted("n-02", "Grace Hopper").link);
    expect(within(second).getByText("2 days ago")).toBeTruthy();
    expect(within(second).queryByRole("button", { name: /Mark notification/ })).toBeNull();
    expect(second.className).not.toContain("bg-rs-accent-soft");
    expect(mockList).toHaveBeenCalledWith();
  });

  it("skips a type it does not know", async () => {
    const unknown = { ...accepted("n-09", "Ada Byron"), type: "post_liked" } as unknown as Notification;
    mockList.mockResolvedValue(page([unknown, accepted("n-02", "Grace Hopper")]));
    await renderPage();
    expect(rows()).toHaveLength(1);
    expect(screen.queryByText(/Ada Byron/)).toBeNull();
  });

  it("marks a row read and goes to its link when clicked", async () => {
    mockList.mockResolvedValue(page([accepted("n-02", "Grace Hopper", { read: false })]));
    mockRead.mockResolvedValue(ok);
    await renderPage();

    await act(async () => {
      fireEvent.click(screen.getByRole("link", { name: /Grace Hopper accepted/ }));
    });
    expect(mockRead).toHaveBeenCalledWith("n-02");
    expect(mockPush).toHaveBeenCalledWith(accepted("n-02", "Grace Hopper").link);
    expect(mockRefresh).toHaveBeenCalledWith(adaID);
    expect(screen.getByRole("link", { name: "Grace Hopper accepted your connection request." })).toBeTruthy();
  });

  it("goes to a read row's link without marking it again", async () => {
    mockList.mockResolvedValue(page([accepted("n-02", "Grace Hopper")]));
    await renderPage();
    await act(async () => {
      fireEvent.click(screen.getByRole("link", { name: /Grace Hopper accepted/ }));
    });
    expect(mockRead).not.toHaveBeenCalled();
    expect(mockPush).toHaveBeenCalled();
  });

  it("marks one row read with its button and goes back if that fails", async () => {
    mockList.mockResolvedValue(page([accepted("n-02", "Grace Hopper", { read: false })]));
    mockRead.mockResolvedValue({ ok: false, code: "internal_error", message: "x" });
    await renderPage();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Mark notification from Grace Hopper as read" }));
    });
    expect(mockRead).toHaveBeenCalledWith("n-02");
    expect(mockPush).not.toHaveBeenCalled();
    expect(screen.getByRole("link", { name: /^Unread: Grace Hopper/ })).toBeTruthy();

    mockRead.mockResolvedValue(ok);
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Mark notification from Grace Hopper as read" }));
    });
    expect(screen.getByRole("link", { name: /^Grace Hopper/ })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Mark notification/ })).toBeNull();
  });

  it("accepts a pending request in place", async () => {
    mockList.mockResolvedValue(page([received("n-01", "Bob Babbage")]));
    mockAccept.mockResolvedValue({
      ok: true,
      data: {
        connectedAt: "2026-11-03T12:00:00Z",
        userId: actor(1, "Bob Babbage").userId,
        fullName: "Bob Babbage",
        headline: null,
        institution: null,
        photoUrl: null,
        connectionStatus: "connected",
        connectionRequestId: null,
      },
    });
    mockRead.mockResolvedValue(ok);
    await renderPage();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Accept request from Bob Babbage" }));
    });
    expect(mockAccept).toHaveBeenCalledWith("7a1d2c3e-0000-4000-8000-000000000001");
    expect(screen.getByRole("status").textContent).toBe("You're now connected");
    expect(screen.queryByRole("button", { name: /Accept|Ignore/ })).toBeNull();
    expect(mockRead).toHaveBeenCalledWith("n-01");
    expect(mockRefresh).toHaveBeenCalledWith(adaID);
  });

  it("removes the row when a request is ignored", async () => {
    mockList.mockResolvedValue(page([received("n-01", "Bob Babbage"), accepted("n-02", "Grace Hopper")]));
    mockReject.mockResolvedValue(ok);
    await renderPage();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Ignore request from Bob Babbage" }));
    });
    expect(mockReject).toHaveBeenCalledWith("7a1d2c3e-0000-4000-8000-000000000001");
    expect(rows()).toHaveLength(1);
    expect(screen.queryByText(/Bob Babbage/)).toBeNull();
    expect(mockRefresh).toHaveBeenCalledWith(adaID);
  });

  it("says so when accepting fails, and drops the buttons when the request is gone", async () => {
    mockList.mockResolvedValue(page([received("n-01", "Bob Babbage")]));
    mockAccept.mockResolvedValue({ ok: false, code: "internal_error", message: "x" });
    await renderPage();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Accept request from Bob Babbage" }));
    });
    expect(screen.getByRole("alert").textContent).toBe("Couldn't accept the request. Try again.");

    mockAccept.mockResolvedValue({ ok: false, code: "not_found", message: "x" });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Accept request from Bob Babbage" }));
    });
    expect(screen.queryByRole("button", { name: /Accept|Ignore/ })).toBeNull();
  });

  it("offers no buttons once a request is no longer pending", async () => {
    mockList.mockResolvedValue(page([received("n-01", "Bob Babbage", { requestPending: false, read: true })]));
    await renderPage();
    expect(screen.getByRole("link", { name: /Bob Babbage sent you/ })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Accept|Ignore/ })).toBeNull();
  });

  it("marks all as read", async () => {
    mockList.mockResolvedValue(page([received("n-01", "Bob Babbage"), accepted("n-02", "Grace Hopper", { read: false })]));
    mockReadAll.mockResolvedValue({ ok: true, data: 2 });
    await renderPage();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Mark all as read" }));
    });
    expect(mockReadAll).toHaveBeenCalled();
    expect(screen.queryByText("Unread:")).toBeNull();
    expect(screen.queryByRole("button", { name: /Mark notification/ })).toBeNull();
    expect((screen.getByRole("button", { name: "Mark all as read" }) as HTMLButtonElement).disabled).toBe(true);
    expect(mockRefresh).toHaveBeenCalledWith(adaID);
  });

  it("disables Mark all as read when nothing is unread, and enables it for unread on later pages", async () => {
    mockList.mockResolvedValue(page([accepted("n-02", "Grace Hopper")]));
    await renderPage();
    const button = () => screen.getByRole("button", { name: "Mark all as read" }) as HTMLButtonElement;
    expect(button().disabled).toBe(true);

    act(() => setUnreadNotifications(adaID, 2));
    expect(button().disabled).toBe(false);
  });

  it("shows more by the cursor", async () => {
    mockList.mockResolvedValueOnce(page([received("n-01", "Bob Babbage")], "cursor-1"));
    mockList.mockResolvedValueOnce(page([received("n-01", "Bob Babbage"), accepted("n-02", "Grace Hopper")]));
    await renderPage();
    expect(rows()).toHaveLength(1);

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Show more" }));
    });
    expect(mockList).toHaveBeenLastCalledWith("cursor-1");
    expect(rows()).toHaveLength(2);
    expect(screen.queryByRole("button", { name: "Show more" })).toBeNull();
  });

  it("shows the empty state", async () => {
    mockList.mockResolvedValue(page([]));
    await renderPage();
    expect(screen.getByText("No notifications yet.")).toBeTruthy();
    expect(screen.queryByRole("list", { name: "Notifications" })).toBeNull();
    expect((screen.getByRole("button", { name: "Mark all as read" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("offers to try again when the list does not load", async () => {
    mockList.mockResolvedValueOnce({ ok: false, code: "network_error", message: "x" });
    mockList.mockResolvedValueOnce(page([accepted("n-02", "Grace Hopper")]));
    await renderPage();
    expect(screen.getByText("Couldn't load your notifications.")).toBeTruthy();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    });
    expect(rows()).toHaveLength(1);
  });
});

describe("a notification on the socket", () => {
  beforeEach(() => {
    FakeWebSocket.install();
    startSocket({ url: "ws://test", refresh: jest.fn(), onSessionLost: jest.fn() });
    FakeWebSocket.latest().accept();
  });
  afterEach(stopSocket);

  it("goes on top of the open page, once", async () => {
    mockList.mockResolvedValue(page([accepted("n-old", "Old Friend")]));
    render(<NotificationsPage />);
    await screen.findByRole("list", { name: "Notifications" });
    const fresh = accepted("n-new", "New Friend", { read: false });
    await act(async () => FakeWebSocket.latest().receive("notification.created", fresh));
    await act(async () => FakeWebSocket.latest().receive("notification.created", fresh));
    expect(rows()).toHaveLength(2);
    expect(rows()[0].textContent).toContain("New Friend");
  });
});
