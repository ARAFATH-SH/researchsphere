import { act, render, screen } from "@testing-library/react";

import type { Session } from "@/features/auth/session";
import { refreshPendingInvitations } from "@/features/social/pending-invitations";

import { getUnreadNotificationCount } from "./api";
import { BadgePoller } from "./badge-poller";
import { pollInterval, refreshBadges } from "./badges";
import { resetUnreadNotifications, useUnreadNotifications } from "./unread-count";

jest.mock("./api");
jest.mock("@/features/social/pending-invitations");
const mockSession = jest.fn<Session, []>();
jest.mock("@/features/auth/session", () => ({ useSession: () => mockSession() }));

const adaID = "0b8f4c1e-0000-4000-8000-000000000001";
const signedIn: Session = {
  status: "signedIn",
  user: { id: adaID, email: "ada@example.org", fullName: "Ada Lovelace", role: "researcher" },
};

const mockCount = jest.mocked(getUnreadNotificationCount);
const mockPending = jest.mocked(refreshPendingInvitations);

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
  resetUnreadNotifications();
});
afterEach(() => {
  jest.useRealTimers();
  jest.resetAllMocks();
});

describe("badge poller", () => {
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
