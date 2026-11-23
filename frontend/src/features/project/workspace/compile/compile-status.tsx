"use client";

import { useSyncExternalStore } from "react";

import { active, type Compile } from "./api";

const names: Record<Compile["status"], string> = {
  queued: "Queued",
  running: "Compiling",
  succeeded: "Done",
  failed: "Failed",
  timed_out: "Timed out",
  cancelled: "Cancelled",
};

// statusName is the compile's state in a word or two, as announced (FR-13.1, FR-13.5).
export function statusName(compile: Compile | null, starting: boolean): string | null {
  if (starting && (!compile || !active(compile))) return "Starting";
  return compile ? names[compile.status] : null;
}

// seconds formats a duration as the design does: 7.8 s, 42 s.
export function seconds(ms: number): string {
  return ms < 10_000 ? `${(ms / 1000).toFixed(1)} s` : `${Math.round(ms / 1000)} s`;
}

// useNow is the time in whole seconds, read when rendering and ticking every second while on,
// for the elapsed time of a running compile.
function subscribeClock(listener: () => void): () => void {
  const t = setInterval(listener, 1000);
  return () => clearInterval(t);
}
const noClock = () => () => {};
const readClock = () => Math.floor(Date.now() / 1000) * 1000;

function useNow(on: boolean): number {
  return useSyncExternalStore(on ? subscribeClock : noClock, readClock, readClock);
}

// CompileStatus shows the newest compile's state in the workspace bar for every member. Only
// the state is in the live region, so a change is announced once and not every second.
export function CompileStatus({ compile, starting }: { compile: Compile | null; starting: boolean }) {
  const running = compile?.status === "running";
  const now = useNow(running);
  const name = statusName(compile, starting);
  let detail: string | null = null;
  if (compile?.status === "running" && compile.startedAt) {
    detail = `${Math.max(0, Math.floor((now - Date.parse(compile.startedAt)) / 1000))} s`;
  } else if (compile && !active(compile) && compile.durationMs !== null && !starting) {
    detail = seconds(compile.durationMs);
  }
  const trouble = !starting && (compile?.status === "failed" || compile?.status === "timed_out" || compile?.status === "cancelled");
  return (
    <span className={`flex items-center gap-1.5 text-rs-caption ${trouble ? "rounded-rs-pill bg-rs-surface px-2 py-0.5 font-medium text-rs-danger-strong" : ""}`}>
      {(starting || (compile && active(compile))) && (
        <span aria-hidden="true" className="size-3 animate-spin rounded-full border-2 border-current border-t-transparent" />
      )}
      <span aria-live="polite" aria-atomic="true" data-testid="compile-status">
        {name ? `Compile: ${name}` : ""}
      </span>
      {detail && (
        <span aria-hidden="true" data-testid="compile-elapsed">
          · {detail}
        </span>
      )}
    </span>
  );
}

// CompileButton is the bar's Compile action (editors and the owner, FR-13.1); Ctrl/Cmd+Enter
// does the same.
export function CompileButton({ onClick, busy }: { onClick: () => void; busy: boolean }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-keyshortcuts="Control+Enter Meta+Enter"
      title="Compile (Ctrl+Enter)"
      aria-busy={busy}
      className="flex h-[38px] items-center gap-1.5 rounded-rs-pill bg-rs-surface px-4 text-rs-body font-medium text-rs-accent-strong hover:bg-rs-accent-soft focus-visible:shadow-[0_0_0_2px_var(--rs-accent-strong),0_0_0_4px_var(--rs-surface)] focus-visible:outline-none"
    >
      <svg aria-hidden="true" viewBox="0 0 24 24" className="size-3.5 fill-current">
        <path d="M7 4l13 8-13 8z" />
      </svg>
      Compile
    </button>
  );
}
