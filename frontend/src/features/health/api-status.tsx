"use client";

import { useEffect, useState } from "react";

import { type ApiState, checkHealth } from "./api";

const REFRESH_MS = 30_000;

const labels: Record<ApiState | "checking", { text: string; dot: string }> = {
  checking: { text: "checking", dot: "bg-text-muted" },
  up: { text: "up", dot: "bg-primary" },
  down: { text: "down", dot: "bg-danger" },
};

// ApiStatus shows whether the API server answers GET /api/v1/health.
// It checks once on mount and then every 30 seconds.
export function ApiStatus() {
  const [state, setState] = useState<ApiState | "checking">("checking");

  useEffect(() => {
    let active = true;
    const refresh = () => {
      void checkHealth().then((next) => {
        if (active) setState(next);
      });
    };
    refresh();
    const timer = setInterval(refresh, REFRESH_MS);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, []);

  const { text, dot } = labels[state];
  return (
    <p role="status" className="flex items-center gap-2 text-sm">
      <span aria-hidden="true" className={`inline-block h-2 w-2 rounded-full ${dot}`} />
      API: {text}
    </p>
  );
}
