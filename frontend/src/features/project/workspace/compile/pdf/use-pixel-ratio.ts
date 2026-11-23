"use client";

import { useEffect, useState } from "react";

const current = () => (typeof window === "undefined" ? 1 : window.devicePixelRatio || 1);

// usePixelRatio is window.devicePixelRatio, kept current when it changes: the window moves to a
// screen of another density, or the browser zooms. A media query on the current resolution fires
// when it stops matching; it is then set up again for the new ratio.
export function usePixelRatio(): number {
  const [ratio, setRatio] = useState(current);
  useEffect(() => {
    let query: MediaQueryList | null = null;
    const update = () => {
      const r = current();
      setRatio(r);
      query?.removeEventListener("change", update);
      query = typeof window.matchMedia === "function" ? window.matchMedia(`(resolution: ${r}dppx)`) : null;
      query?.addEventListener("change", update);
    };
    update();
    // A zoom also resizes the window; this covers a browser whose media query does not fire.
    window.addEventListener("resize", update);
    return () => {
      query?.removeEventListener("change", update);
      window.removeEventListener("resize", update);
    };
  }, []);
  return ratio;
}
