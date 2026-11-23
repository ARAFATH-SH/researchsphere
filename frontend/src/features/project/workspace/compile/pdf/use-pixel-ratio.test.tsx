import { act, renderHook } from "@testing-library/react";

import { usePixelRatio } from "./use-pixel-ratio";

describe("usePixelRatio", () => {
  const dpr = window.devicePixelRatio;
  const queries: { media: string; listeners: Set<() => void> }[] = [];
  beforeEach(() => {
    queries.length = 0;
    window.matchMedia = jest.fn((media: string) => {
      const q = { media, listeners: new Set<() => void>() };
      queries.push(q);
      return {
        media,
        addEventListener: (_: string, l: () => void) => q.listeners.add(l),
        removeEventListener: (_: string, l: () => void) => q.listeners.delete(l),
      } as unknown as MediaQueryList;
    });
  });
  afterEach(() => {
    window.devicePixelRatio = dpr;
    delete (window as { matchMedia?: unknown }).matchMedia;
  });

  it("follows devicePixelRatio when the screen or the browser zoom changes it", () => {
    window.devicePixelRatio = 1;
    const { result, unmount } = renderHook(() => usePixelRatio());
    expect(result.current).toBe(1);
    expect(queries.at(-1)!.media).toBe("(resolution: 1dppx)");

    window.devicePixelRatio = 1.5;
    act(() => queries.at(-1)!.listeners.forEach((l) => l()));
    expect(result.current).toBe(1.5);
    expect(queries.at(-1)!.media).toBe("(resolution: 1.5dppx)");
    // Only the query for the current ratio is listened to.
    expect(queries.filter((q) => q.listeners.size > 0)).toHaveLength(1);

    window.devicePixelRatio = 2;
    act(() => {
      window.dispatchEvent(new Event("resize"));
    });
    expect(result.current).toBe(2);

    unmount();
    expect(queries.filter((q) => q.listeners.size > 0)).toHaveLength(0);
  });
});
