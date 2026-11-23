"use client";

import { type KeyboardEvent, useRef, useState } from "react";

import type { Compile, CompileError } from "./api";
import { RawLog } from "./raw-log";

// JumpTarget is where a member asked to go: a project path and a line (FR-13.3).
export type JumpTarget = { file: string; line: number | null };

// ErrorsPanel lists the newest compile's errors under the editor (FR-13.2), errors first and
// warnings folded away, each with its severity, file and line. Selecting one opens its file at
// that line (FR-13.3); a file that no longer exists is said. The arrow keys move through the
// list. "View full log" reads the log only when asked.
export function ErrorsPanel({
  projectId,
  compile,
  missing,
  onJump,
}: {
  projectId: string;
  compile: Compile | null;
  // missing is the path of the last selected item's file when it no longer exists.
  missing: string | null;
  onJump: (target: JumpTarget) => void;
}) {
  const [logFor, setLogFor] = useState<string | null>(null);
  const list = useRef<HTMLDivElement>(null);
  const [focusAt, setFocusAt] = useState(0);
  const errors = compile?.errors.filter((e) => e.severity === "error") ?? [];
  const warnings = compile?.errors.filter((e) => e.severity === "warning") ?? [];
  const showLog = compile !== null && logFor === compile.id;
  const finished = compile !== null && compile.status !== "queued" && compile.status !== "running";

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    const buttons = [...(list.current?.querySelectorAll<HTMLButtonElement>("button[data-error-item]") ?? [])].filter((b) => !b.closest("details:not([open])"));
    const at = buttons.indexOf(document.activeElement as HTMLButtonElement);
    if (at < 0) return;
    let next = at;
    if (e.key === "ArrowDown") next = Math.min(at + 1, buttons.length - 1);
    else if (e.key === "ArrowUp") next = Math.max(at - 1, 0);
    else if (e.key === "Home") next = 0;
    else if (e.key === "End") next = buttons.length - 1;
    else return;
    e.preventDefault();
    buttons[next]?.focus();
    setFocusAt(Number(buttons[next]?.dataset.errorItem ?? 0));
  }

  let index = 0;
  const item = (err: CompileError) => {
    const i = index++;
    return <ErrorItem key={i} index={i} error={err} tabbable={i === focusAt} onFocus={() => setFocusAt(i)} onJump={onJump} />;
  };

  return (
    <section aria-labelledby="compile-errors-h" className="flex max-h-[40vh] min-h-0 flex-col gap-2 overflow-auto px-4 py-3">
      <div className="flex flex-wrap items-center gap-2">
        <h2 id="compile-errors-h" className="m-0 text-rs-lead leading-rs-snug font-medium">
          Compile errors
          {compile && (
            <span
              aria-label={`${errors.length} error${errors.length === 1 ? "" : "s"}`}
              className={`ml-2 inline-flex min-w-6 items-center justify-center rounded-rs-pill px-1.5 text-rs-caption font-bold ${
                errors.length > 0 ? "bg-rs-danger-soft text-rs-danger-strong" : "bg-rs-subtle text-rs-text-muted"
              }`}
            >
              {errors.length}
            </span>
          )}
        </h2>
        {compile?.hasLog && (
          <button
            type="button"
            aria-expanded={showLog}
            onClick={() => setLogFor(showLog ? null : compile.id)}
            className="h-7 text-rs-caption font-medium text-rs-accent-strong hover:underline focus-visible:shadow-focus focus-visible:outline-none"
          >
            {showLog ? "Hide full log" : "View full log"}
          </button>
        )}
      </div>
      {missing && (
        <p role="alert" className="text-rs-caption text-rs-danger-strong">
          {missing} no longer exists in this project.
        </p>
      )}
      {!compile ? (
        <p className="text-rs-caption text-rs-text-muted">Not compiled yet.</p>
      ) : !finished ? (
        <p className="text-rs-caption text-rs-text-muted">Errors appear here when the compile finishes.</p>
      ) : errors.length === 0 && warnings.length === 0 ? (
        <p className="text-rs-caption text-rs-text-muted">No errors or warnings.</p>
      ) : (
        <div ref={list} role="list" aria-label="Errors and warnings" onKeyDown={onKeyDown} className="flex flex-col gap-1.5">
          {errors.map(item)}
          {warnings.length > 0 && (
            <details className="flex flex-col gap-1.5" role="listitem">
              <summary className="cursor-pointer text-rs-caption font-medium text-rs-warning">
                {warnings.length} warning{warnings.length === 1 ? "" : "s"}
              </summary>
              <div role="list" className="mt-1.5 flex flex-col gap-1.5">
                {warnings.map(item)}
              </div>
            </details>
          )}
          <span className="text-rs-caption text-rs-text-muted">Select an error to jump to its file and line.</span>
        </div>
      )}
      {showLog && <RawLog projectId={projectId} compileId={compile.id} />}
    </section>
  );
}

function ErrorItem({
  index,
  error,
  tabbable,
  onFocus,
  onJump,
}: {
  index: number;
  error: CompileError;
  tabbable: boolean;
  onFocus: () => void;
  onJump: (target: JumpTarget) => void;
}) {
  const isError = error.severity === "error";
  const where = error.file ? `${error.file}${error.line !== null ? `:${error.line}` : ""}` : null;
  const box = isError ? "border-rs-danger-line bg-rs-danger-soft" : "border-rs-border bg-rs-surface";
  const content = (
    <>
      <span className={`flex-none font-mono text-rs-caption font-medium ${isError ? "text-rs-danger-strong" : "text-rs-warning"}`}>
        <span className="sr-only">{isError ? "Error" : "Warning"}</span>
        {where ?? (isError ? "Error" : "Warning")}
      </span>
      <span className="text-rs-caption break-words">{error.message}</span>
    </>
  );
  // Every item is a button so the arrow keys reach it; one without a project file goes nowhere.
  return (
    <div role="listitem">
      <button
        type="button"
        data-error-item={index}
        tabIndex={tabbable ? 0 : -1}
        aria-disabled={error.file ? undefined : true}
        onFocus={onFocus}
        onClick={() => {
          if (error.file) onJump({ file: error.file, line: error.line });
        }}
        className={`flex w-full items-start gap-2 rounded-control border px-2.5 py-2 text-left text-rs-text focus-visible:shadow-focus focus-visible:outline-none ${box} ${
          error.file ? "hover:border-rs-accent-line" : "cursor-default"
        }`}
      >
        {content}
      </button>
    </div>
  );
}
