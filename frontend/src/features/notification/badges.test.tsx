import { act, render, screen } from "@testing-library/react";

import type { Session } from "@/features/auth/session";
import { refreshUnreadMessages, resetUnreadMessages, setUnreadMessages } from "@/features/messaging/unread-count";
import { FakeWebSocket } from "@/features/realtime/fake-websocket";
import { startSocket, stopSocket } from "@/features/realtime/socket";
import { refreshPendingInvitations } from "@/features/social/pending-invitations";

import { getUnreadNotificationCount } from "./api";
import { BadgePoller } from "./badge-poller";
import { pollInterval, refreshBadges } from "./badges";
import { resetUnreadNotifications, useUnreadNotifications } from "./unread-count";

jest.mock("./api");
jest.mock("@/features/social/pending-invitations");
jest.mock("@/features/messaging/unread-count");
const mockSession = jest.fn<Session, []>();
jest.mock("@/features/auth/session", () => ({ useSession: () => mockSession() }));

const adaID = "0b8f4c1e-0000-4000-8000-000000000001";
const signedIn: Session = {
  status: "signedIn",
  user: { id: adaID, email: "ada@example.org", fullName: "Ada Lovelace", role: "researcher" },
};

const mockCount = jest.mocked(getUnreadNotificationCount);
const mockPending = jest.mocked(refreshPendingInvitations);
const mockMessages = jest.mocked(refreshUnreadMessages);

let visibility: DocumentVisibilityState = "visible";
Object.defineProperty(document, "visibilityState", { configurable: true, get: () => visibility });

async function setVisibility(next: DocumentVisibilityState) {
  visibility = next;
  await act(async () => {
    document.dispatchEvent(new Event("visibilitychange"));
  });
}

async function advance(ms: number) {
  await act(async () => {
    jest.advanceTimersByTime(ms);
  });
}

function Count() {
  return <p>count {useUnreadNotifications()}</p>;
}

beforeEach(() => {
  jest.useFakeTimers();
  visibility = "visible";
  mockSession.mockReturnValue(signedIn);
  mockCount.mockResolvedValue({ ok: true, data: 4 });
  mockPending.mockResolvedValue();
  mockMessages.mockResolvedValue();
  resetUnreadNotifications();
});
afterEach(() => {
  jest.useRealTimers();
  jest.resetAllMocks();
});

describe("badge poller", () => {
  it("refreshes the Messaging badge with the others, and forgets it on sign-out", async () => {
    const { unmount } = render(<BadgePoller />);
    await act(async () => {});
    expect(mockMessages).toHaveBeenCalledWith(adaID);
    await advance(pollInterval);
    expect(mockMessages).toHaveBeenCalledTimes(2);
    unmount();
    expect(resetUnreadMessages).toHaveBeenCalled();
  });

  it("fetches both counts on sign-in, then every 60 seconds", async () => {
    const { unmount } = render(
      <>
        <BadgePoller />
        <Count />
      </>,
    );
    await act(async () => {});
    expect(mockCount).toHaveBeenCalledTimes(1);
    expect(mockPending).toHaveBeenCalledWith(adaID);
    expect(screen.getByText("count 4")).toBeTruthy();

    await advance(pollInterval - 1);
    expect(mockCount).toHaveBeenCalledTimes(1);
    mockCount.mockResolvedValue({ ok: true, data: 5 });
    await advance(1);
    expect(mockCount).toHaveBeenCalledTimes(2);
    expect(mockPending).toHaveBeenCalledTimes(2);
    expect(screen.getByText("count 5")).toBeTruthy();
    unmount();
  });

  it("pauses while the tab is hidden and refreshes at once when it is shown", async () => {
    const { unmount } = render(<BadgePoller />);
    await act(async () => {});
    expect(mockCount).toHaveBeenCalledTimes(1);

    await setVisibility("hidden");
    await advance(3 * pollInterval);
    expect(mockCount).toHaveBeenCalledTimes(1);

    await setVisibility("visible");
    expect(mockCount).toHaveBeenCalledTimes(2);
    await advance(pollInterval);
    expect(mockCount).toHaveBeenCalledTimes(3);
    unmount();
  });

  it("does not start while the tab is hidden", async () => {
    visibility = "hidden";
    const { unmount } = render(<BadgePoller />);
    await advance(2 * pollInterval);
    expect(mockCount).not.toHaveBeenCalled();
    await setVisibility("visible");
    expect(mockCount).toHaveBeenCalledTimes(1);
    unmount();
  });

  it("refreshes both counts after an action", async () => {
    const { unmount } = render(<BadgePoller />);
    await act(async () => {});
    await act(async () => refreshBadges(adaID));
    expect(mockCount).toHaveBeenCalledTimes(2);
    expect(mockPending).toHaveBeenCalledTimes(2);
    unmount();
  });

  it("stops on sign-out and forgets the count", async () => {
    const view = render(
      <>
        <BadgePoller />
        <Count />
      </>,
    );
    await act(async () => {});
    expect(screen.getByText("count 4")).toBeTruthy();

    mockSession.mockReturnValue({ status: "signedOut" });
    view.rerender(
      <>
        <BadgePoller />
        <Count />
      </>,
    );
    expect(screen.getByText("count 0")).toBeTruthy();
    await advance(3 * pollInterval);
    await setVisibility("hidden");
    await setVisibility("visible");
    expect(mockCount).toHaveBeenCalledTimes(1);
    view.unmount();
  });

  it("makes no call while nobody is signed in", async () => {
    mockSession.mockReturnValue({ status: "signedOut" });
    const { unmount } = render(<BadgePoller />);
    await advance(2 * pollInterval);
    expect(mockCount).not.toHaveBeenCalled();
    expect(mockPending).not.toHaveBeenCalled();
    unmount();
  });

  it("runs one poll however many pollers are mounted", async () => {
    const view = render(
      <>
        <BadgePoller />
        <BadgePoller />
      </>,
    );
    await act(async () => {});
    expect(mockCount).toHaveBeenCalledTimes(1);
    await advance(pollInterval);
    expect(mockCount).toHaveBeenCalledTimes(2);

    // Unmounting one keeps the poll going for the other.
    view.rerender(<BadgePoller />);
    await advance(pollInterval);
    expect(mockCount).toHaveBeenCalledTimes(3);
    view.unmount();
    await advance(pollInterval);
    expect(mockCount).toHaveBeenCalledTimes(3);
  });
});

describe("with the socket", () => {
  const start = () => startSocket({ url: "ws://test", refresh: jest.fn().mockResolvedValue("ok"), onSessionLost: jest.fn() });
  const accept = () => act(async () => FakeWebSocket.latest().accept());
  const receive = (type: string, data: unknown) => act(async () => FakeWebSocket.latest().receive(type, data));

  beforeEach(() => {
    FakeWebSocket.install();
    jest.spyOn(Math, "random").mockReturnValue(1);
  });
  afterEach(() => {
    stopSocket();
    jest.restoreAllMocks();
  });

  async function connected() {
    render(<BadgePoller />);
    await act(async () => {});
    start();
    await accept();
    mockCount.mockClear();
    mockMessages.mockClear();
    mockPending.mockClear();
  }

  it("refetches every count after a connect", async () => {
    render(<BadgePoller />);
    await act(async () => {});
    mockCount.mockClear();
    mockMessages.mockClear();
    mockPending.mockClear();
    start();
    await accept();
    expect(mockCount).toHaveBeenCalledTimes(1);
    expect(mockMessages).toHaveBeenCalledTimes(1);
    expect(mockPending).toHaveBeenCalledTimes(1);
  });

  it("polls only My Network while connected, which has no event", async () => {
    await connected();
    await advance(pollInterval * 3);
    expect(mockCount).not.toHaveBeenCalled();
    expect(mockMessages).not.toHaveBeenCalled();
    expect(mockPending).toHaveBeenCalledTimes(3);
  });

  it("polls everything again while disconnected", async () => {
    await connected();
    await act(async () => stopSocket());
    await advance(pollInterval);
    expect(mockCount).toHaveBeenCalledTimes(1);
    expect(mockMessages).toHaveBeenCalledTimes(1);
  });

  it("polls during a reconnect, and stops once it is back", async () => {
    await connected();
    await act(async () => FakeWebSocket.latest().serverClose(1013));
    await advance(pollInterval);
    expect(mockCount).toHaveBeenCalledTimes(1);
    await accept();
    mockCount.mockClear();
    await advance(pollInterval);
    expect(mockCount).not.toHaveBeenCalled();
  });

  it("polls in a tab replaced by a newer socket", async () => {
    await connected();
    await act(async () => FakeWebSocket.latest().serverClose(4409));
    await advance(pollInterval);
    expect(mockCount).toHaveBeenCalledTimes(1);
  });

  it("does not poll the counts when a connected tab is shown again", async () => {
    await connected();
    await setVisibility("hidden");
    await setVisibility("visible");
    expect(mockCount).not.toHaveBeenCalled();
  });

  it("sets the bell from notifications.unread", async () => {
    render(<Count />);
    await connected();
    await receive("notifications.unread", { count: 7 });
    expect(screen.getByText("count 7")).toBeTruthy();
  });

  it("sets the Messaging badge from unread.changed", async () => {
    await connected();
    await receive("unread.changed", { total: 3, requests: 1 });
    expect(setUnreadMessages).toHaveBeenCalledWith(adaID, { total: 3, requests: 1 });
  });

  it("refreshes My Network on a connection notification, and only then", async () => {
    await connected();
    await receive("notification.created", { id: "n1", type: "connection_request_received" });
    expect(mockPending).toHaveBeenCalledTimes(1);
    await receive("notification.created", { id: "n2", type: "account_warning" });
    expect(mockPending).toHaveBeenCalledTimes(1);
  });

  it("hears nothing after signing out", async () => {
    render(<Count />);
    const view = render(<BadgePoller />);
    await act(async () => {});
    start();
    await accept();
    mockSession.mockReturnValue({ status: "signedOut" });
    view.rerender(<BadgePoller />);
    mockMessages.mockClear();
    await receive("unread.changed", { total: 9, requests: 0 });
    expect(setUnreadMessages).not.toHaveBeenCalled();
  });
});
