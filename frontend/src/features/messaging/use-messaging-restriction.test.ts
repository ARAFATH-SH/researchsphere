import { act, renderHook } from "@testing-library/react";

import { getCurrentUser } from "@/features/auth/api";

import { useMessagingRestriction } from "./use-messaging-restriction";

jest.mock("@/features/auth/api");
jest.mock("@/features/realtime/use-realtime", () => ({ useOnConnected: jest.fn() }));

const user = { id: "u", email: "ada@example.org", fullName: "Ada", role: "researcher" as const };

beforeEach(() => jest.useFakeTimers({ now: new Date("2026-11-06T10:00:00Z") }));
afterEach(() => {
  jest.useRealTimers();
  jest.resetAllMocks();
});

describe("useMessagingRestriction", () => {
  it("ends by itself at its time, with no read", async () => {
    jest.mocked(getCurrentUser).mockResolvedValue({ ok: true, data: { ...user, messagingRestrictedUntil: "2026-11-06T10:01:00Z" } });
    const { result } = renderHook(() => useMessagingRestriction());
    await act(async () => {});
    expect(result.current.until).toBe("2026-11-06T10:01:00Z");
    act(() => jest.advanceTimersByTime(59_000));
    expect(result.current.until).toBe("2026-11-06T10:01:00Z");
    act(() => jest.advanceTimersByTime(1_000));
    expect(result.current.until).toBeNull();
    expect(getCurrentUser).toHaveBeenCalledTimes(1);
  });

  it("takes the end time of a refused send", async () => {
    jest.mocked(getCurrentUser).mockResolvedValue({ ok: true, data: { ...user, messagingRestrictedUntil: null } });
    const { result } = renderHook(() => useMessagingRestriction());
    await act(async () => {});
    expect(result.current.until).toBeNull();
    act(() => result.current.refused("2026-11-07T10:00:00Z"));
    expect(result.current.until).toBe("2026-11-07T10:00:00Z");
  });
});
