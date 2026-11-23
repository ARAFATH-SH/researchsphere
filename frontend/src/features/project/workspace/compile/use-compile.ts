"use client";

import { useCallback, useEffect, useRef, useState } from "react";

import { useOnConnected, useRealtimeEvent } from "@/features/realtime/use-realtime";

import { active, type Compile, compileErrorText, getCompile, getLatestCompile, startCompile } from "./api";

// CompileNotice is what the workspace says about a compile request beside the status:
// - running: one was already running (FR-13.4); by is the member who started it.
// - unavailable: no compile service answers (503 compile_unavailable), with a retry.
// - error: any other refusal, as text.
export type CompileNotice = { kind: "running"; by: string | null } | { kind: "unavailable" } | { kind: "error"; text: string } | null;

const rank: Record<Compile["status"], number> = { queued: 0, running: 1, succeeded: 2, failed: 2, timed_out: 2, cancelled: 2 };

// newer says whether next should replace current: a later job, or a later state of the same
// job. Events can arrive out of order with a REST read, so an older state never wins.
export function newer(current: Compile | null, next: Compile): boolean {
  if (!current) return true;
  if (next.id === current.id) return rank[next.status] >= rank[current.status];
  return Date.parse(next.createdAt) >= Date.parse(current.createdAt);
}

// useCompile follows the project's newest compile for every member (FR-13.1): read when the
// workspace opens and after every real-time reconnect (docs/realtime.md), then kept current by
// compile.updated events. start() first waits until this tab's edits have reached a server
// (flush), then asks for a compile; when one is already running it follows that one instead.
export function useCompile(projectId: string, flush: () => Promise<void>) {
  const [compile, setCompile] = useState<Compile | null>(null);
  const [notice, setNotice] = useState<CompileNotice>(null);
  const [starting, setStarting] = useState(false);
  const latest = useRef<Compile | null>(null);
  const busy = useRef(false);
  const mounted = useRef(true);

  const accept = useCallback((next: Compile) => {
    if (!mounted.current || !newer(latest.current, next)) return;
    latest.current = next;
    setCompile(next);
    // The compile someone else started has ended: its notice has done its work.
    if (!active(next)) setNotice((n) => (n?.kind === "running" ? null : n));
  }, []);

  const readLatest = useCallback(async () => {
    const res = await getLatestCompile(projectId);
    if (res.ok) accept(res.data);
  }, [projectId, accept]);

  useEffect(() => {
    mounted.current = true;
    void getLatestCompile(projectId).then((res) => {
      if (res.ok) accept(res.data);
    });
    return () => {
      mounted.current = false;
    };
  }, [projectId, accept]);

  useOnConnected(() => void readLatest());
  useRealtimeEvent((event) => {
    if (event.type === "compile.updated" && event.data.projectId === projectId) accept(event.data.compile);
  });

  const start = useCallback(async () => {
    if (busy.current) return;
    const current = latest.current;
    if (current && active(current)) {
      // One compile at a time (FR-13.4): say who started it rather than asking again.
      setNotice({ kind: "running", by: current.requestedBy });
      return;
    }
    busy.current = true;
    setStarting(true);
    setNotice(null);
    try {
      await flush();
      const res = await startCompile(projectId);
      if (!mounted.current) return;
      if (res.ok) {
        accept(res.data);
        return;
      }
      if (res.code === "compile_running" && res.compileId) {
        const running = await getCompile(projectId, res.compileId);
        if (!mounted.current) return;
        if (running.ok) accept(running.data);
        setNotice({ kind: "running", by: running.ok ? running.data.requestedBy : null });
        return;
      }
      setNotice(res.code === "compile_unavailable" ? { kind: "unavailable" } : { kind: "error", text: compileErrorText(res) });
    } finally {
      busy.current = false;
      if (mounted.current) setStarting(false);
    }
  }, [projectId, flush, accept]);

  const dismiss = useCallback(() => setNotice(null), []);

  return { compile, notice, starting, start, dismiss };
}
