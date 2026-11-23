"use client";

import { type ReactNode } from "react";

import { rsButtonClass } from "@/components/ui/button";

import { type Compile, failureText } from "./api";
import type { CompileNotice } from "./use-compile";

export const timeoutText = "Stopped after 60 seconds. Look for an endless loop or a very large figure.";

type Tone = "warning" | "danger" | "neutral";

const tones: Record<Tone, string> = {
  warning: "border-rs-warning-line bg-rs-warning-soft text-rs-warning",
  danger: "border-rs-danger-line bg-rs-danger-soft text-rs-danger-strong",
  neutral: "border-rs-border bg-rs-subtle text-rs-text",
};

function Card({ tone, title, children, role = "status" }: { tone: Tone; title: string; children?: ReactNode; role?: "status" | "alert" }) {
  return (
    <div role={role} className={`flex flex-wrap items-center gap-x-2 gap-y-1 rounded-card border px-3 py-2.5 text-rs-caption ${tones[tone]}`}>
      <strong>{title}</strong>
      {children}
    </div>
  );
}

// CompileMessages says what a member must know about the newest compile and their own
// request (FR-13.4, FR-13.5): already running, timed out, failed with the first error, the
// compile service unavailable, and the smaller notes on live text and skipped figures.
export function CompileMessages({
  compile,
  notice,
  nameOf,
  onRetry,
  onDismiss,
}: {
  compile: Compile | null;
  notice: CompileNotice;
  nameOf: (userId: string | null) => string;
  onRetry: () => void;
  onDismiss: () => void;
}) {
  const items: ReactNode[] = [];
  if (notice?.kind === "running") {
    items.push(
      <Card key="running" tone="warning" title={`A compile is already running (started by ${nameOf(notice.by)}).`}>
        <span>You’ll see the new PDF when it finishes.</span>
      </Card>,
    );
  } else if (notice?.kind === "unavailable") {
    items.push(
      <Card key="unavailable" tone="danger" role="alert" title="Compile service unavailable.">
        <span>Your files are safe; nothing was compiled.</span>
        <button type="button" onClick={onRetry} className={`${rsButtonClass({ variant: "secondary", size: "xs" })} ml-auto`}>
          Retry
        </button>
      </Card>,
    );
  } else if (notice?.kind === "error") {
    items.push(
      <Card key="error" tone="danger" role="alert" title={notice.text}>
        <button type="button" onClick={onDismiss} className="ml-auto font-medium underline">
          Dismiss
        </button>
      </Card>,
    );
  }

  if (compile?.status === "timed_out") {
    items.push(
      <Card key="timeout" tone="danger" title="Compile timed out.">
        <span>{timeoutText}</span>
      </Card>,
    );
  } else if (compile?.status === "failed") {
    const first = compile.errors.find((e) => e.severity === "error");
    const where = first?.file ? `${first.file}${first.line !== null ? `:${first.line}` : ""}: ` : "";
    items.push(
      <Card key="failed" tone="danger" title="Compile failed.">
        <span>{first ? `${where}${first.message}` : failureText(compile.failure)}</span>
      </Card>,
    );
  } else if (compile?.status === "cancelled") {
    items.push(
      <Card key="cancelled" tone="danger" title="Compile cancelled.">
        <span>{failureText(compile.failure)}</span>
      </Card>,
    );
  }

  if (compile && !compile.liveTextConfirmed && compile.status !== "queued") {
    items.push(
      <Card key="live" tone="neutral" title="Edits of the last few seconds may be missing.">
        <span>Live editing didn’t confirm in time, so this compile used the last saved text.</span>
      </Card>,
    );
  }
  if (compile && compile.skipped.length > 0) {
    items.push(
      <Card key="skipped" tone="neutral" title={`${compile.skipped.length} figure${compile.skipped.length === 1 ? " was" : "s were"} left out:`}>
        <span>{compile.skipped.map((s) => s.path).join(", ")} (not checked for malware yet, or not clean).</span>
      </Card>,
    );
  }

  if (items.length === 0) return null;
  return <div className="flex flex-col gap-2 border-b border-rs-border px-4 py-2">{items}</div>;
}
