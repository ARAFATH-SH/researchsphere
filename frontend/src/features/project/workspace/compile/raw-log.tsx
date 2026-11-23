"use client";

import { useEffect, useId, useMemo, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";

import { getCompileLog } from "./api";

type Load = { status: "loading" } | { status: "ok"; text: string } | { status: "gone" } | { status: "error" };

// RawLog shows the pdfLaTeX log of a compile (FR-13.2), read only when the member opens it,
// with a search that keeps the lines containing the text. The API keeps the log of the newest
// finished compile only (SRS 7).
export function RawLog({ projectId, compileId }: { projectId: string; compileId: string }) {
  const [result, setResult] = useState<{ key: string; load: Load } | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [query, setQuery] = useState("");
  const searchId = useId();
  const key = `${compileId}#${attempt}`;

  useEffect(() => {
    let current = true;
    void getCompileLog(projectId, compileId).then((res) => {
      if (!current) return;
      const load: Load = res.ok ? { status: "ok", text: res.data } : res.code === "no_log" || res.code === "not_found" ? { status: "gone" } : { status: "error" };
      setResult({ key, load });
    });
    return () => {
      current = false;
    };
  }, [projectId, compileId, key]);

  const load: Load = result?.key === key ? result.load : { status: "loading" };
  const text = load.status === "ok" ? load.text : null;
  const lines = useMemo(() => (text === null ? [] : text.split(/\r?\n/)), [text]);
  const q = query.trim().toLowerCase();
  const shown = useMemo(
    () => (q ? lines.map((text, i) => ({ text, n: i + 1 })).filter((l) => l.text.toLowerCase().includes(q)) : lines.map((text, i) => ({ text, n: i + 1 }))),
    [lines, q],
  );

  switch (load.status) {
    case "loading":
      return (
        <p role="status" className="text-rs-caption text-rs-text-muted">
          Loading the log…
        </p>
      );
    case "gone":
      return <p className="text-rs-caption text-rs-text-muted">The log is kept only for the newest finished compile.</p>;
    case "error":
      return (
        <div role="alert" className="flex items-center gap-2 text-rs-caption">
          <span>We couldn’t load the log.</span>
          <button type="button" onClick={() => setAttempt((n) => n + 1)} className={rsButtonClass({ variant: "secondary", size: "xs" })}>
            Try again
          </button>
        </div>
      );
    case "ok":
      return (
        <div className="flex min-h-0 flex-col gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <label htmlFor={searchId} className="text-rs-caption">
              Search the log
            </label>
            <input
              id={searchId}
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="h-8 w-48 rounded-control border border-rs-control bg-rs-surface px-2 text-rs-caption"
            />
            <span role="status" className="text-rs-caption text-rs-text-muted">
              {q ? `${shown.length} matching line${shown.length === 1 ? "" : "s"}` : `${lines.length} lines`}
            </span>
          </div>
          <pre
            tabIndex={0}
            aria-label="Compile log"
            className="max-h-72 overflow-auto rounded-control bg-rs-subtle p-2 font-mono text-rs-caption whitespace-pre-wrap"
          >
            {shown.map((l) => (
              <span key={l.n} className="block">
                <span aria-hidden="true" className="mr-3 inline-block w-10 text-right text-rs-text-muted select-none">
                  {l.n}
                </span>
                {l.text}
              </span>
            ))}
          </pre>
        </div>
      );
  }
}
