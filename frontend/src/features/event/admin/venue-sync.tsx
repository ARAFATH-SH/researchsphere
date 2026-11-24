"use client";

import { useCallback, useEffect, useState } from "react";

import { SectionCard } from "@/components/section-layout";
import { rsButtonClass } from "@/components/ui/button";
import { chipClass } from "@/components/ui/chip";
import { Dialog } from "@/components/ui/dialog";
import { FormError } from "@/components/ui/field";

import { getVenueSyncStatus, startVenueSync, type VenueSyncRun, type VenueSyncSource, type VenueSyncStatus } from "../api";
import { reasonText } from "./import-venues-dialog";

export const pollEvery = 3000;

const when = new Intl.DateTimeFormat("en-GB", { day: "numeric", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit", hourCycle: "h23" });

const statusChip: Record<VenueSyncRun["status"], string> = {
  running: "bg-rs-violet-soft text-rs-violet",
  succeeded: "bg-rs-accent-soft text-rs-heading",
  failed: "bg-rs-danger-soft text-rs-danger-strong",
};
const statusLabel: Record<VenueSyncRun["status"], string> = { running: "Running", succeeded: "Succeeded", failed: "Failed" };

// counts is a run's numbers in one line.
export function runCounts(r: VenueSyncRun): string {
  return `${r.created} created, ${r.updated} updated, ${r.unchanged} unchanged, ${r.skipped} skipped, ${r.rejected} rejected`;
}

// freshDryRun says whether the source's latest dry run succeeded after its latest real run, so
// a real run may follow it.
export function freshDryRun(s: VenueSyncSource): VenueSyncRun | null {
  const dry = s.lastDryRun;
  if (!dry || dry.status !== "succeeded") return null;
  if (s.lastRun && new Date(s.lastRun.startedAt) >= new Date(dry.startedAt)) return null;
  return dry;
}

const running = (s: VenueSyncStatus) =>
  s.sources.some((x) => x.lastRun?.status === "running" || x.lastDryRun?.status === "running");

// VenueSync shows the automatic venue import per source (FR-14.5, docs/venues-sync.md): its
// licence, what it read last, the latest real run and dry run with their counts, errors and
// rejected entries, and the next scheduled run. A dry run can start at any time; a real run
// asks first and needs a successful dry run newer than the last real run. While a run is
// going the status is read again every 3 seconds.
export function VenueSync() {
  const [status, setStatus] = useState<VenueSyncStatus | null>(null);
  const [failure, setFailure] = useState("");
  const [tick, setTick] = useState(0);
  const [confirming, setConfirming] = useState<VenueSyncSource | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let current = true;
    void getVenueSyncStatus().then((res) => {
      if (!current) return;
      if (res.ok) setStatus(res.data);
      else setFailure(res.message);
    });
    return () => {
      current = false;
    };
  }, [tick]);

  useEffect(() => {
    if (!status || !running(status)) return;
    const t = setTimeout(() => setTick((n) => n + 1), pollEvery);
    return () => clearTimeout(t);
  }, [status]);

  const start = useCallback(async (sources: VenueSyncSource[], dryRun: boolean) => {
    setBusy(true);
    setFailure("");
    const res = await startVenueSync(
      sources.map((s) => s.source),
      dryRun,
    );
    setBusy(false);
    setConfirming(null);
    if (!res.ok) setFailure(res.message);
    setTick((n) => n + 1);
  }, []);

  const anyRunning = status ? running(status) : false;
  const aside = status ? (
    <div className="flex flex-wrap items-center gap-2">
      <span className="text-rs-caption text-rs-text-muted">Daily schedule {status.daily ? "on" : "off"}</span>
      <button
        type="button"
        disabled={busy || anyRunning}
        onClick={() => void start(status.sources, true)}
        className={rsButtonClass({ variant: "outline", size: "xs" })}
      >
        Dry run all sources
      </button>
    </div>
  ) : null;

  return (
    <SectionCard id="venue-sync" title="Automatic venue import" header aside={aside}>
      {failure && (
        <div className="px-4 pt-3">
          <FormError look="rs">{failure}</FormError>
        </div>
      )}
      {!status && !failure && (
        <p role="status" className="px-4 py-3 text-rs-body text-rs-text-muted">
          Loading the import status…
        </p>
      )}
      {status && (
        <ul className="flex flex-col">
          {status.sources.map((s) => {
            const sourceRunning = s.lastRun?.status === "running" || s.lastDryRun?.status === "running";
            return (
              <li key={s.source} className="flex flex-col gap-2 border-t border-rs-divider px-4 py-3.5 first:border-t-0">
                <div className="flex flex-wrap items-start justify-between gap-2">
                  <div className="flex min-w-0 flex-col">
                    <h3 className="text-rs-body font-medium text-rs-text">{s.label}</h3>
                    <span className="text-rs-caption break-all text-rs-text-muted">
                      {s.licence} ·{" "}
                      <a href={s.url} target="_blank" rel="noopener noreferrer" className="text-rs-accent-strong hover:underline">
                        {s.url}
                      </a>
                      {s.commit && <> · commit {s.commit.slice(0, 7)}</>}
                    </span>
                    <span className="text-rs-caption text-rs-text-muted">
                      Next scheduled run: {s.nextRunAt ? when.format(new Date(s.nextRunAt)) : "none (schedule off)"}
                    </span>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    <button
                      type="button"
                      disabled={busy || sourceRunning}
                      aria-label={`Dry run ${s.label}`}
                      onClick={() => void start([s], true)}
                      className={rsButtonClass({ variant: "secondary", size: "xs" })}
                    >
                      Dry run
                    </button>
                    <button
                      type="button"
                      disabled={busy || sourceRunning}
                      aria-label={`Run sync ${s.label}`}
                      onClick={() => setConfirming(s)}
                      className={rsButtonClass({ size: "xs" })}
                    >
                      Run sync
                    </button>
                  </div>
                </div>
                <RunLine title="Last run" run={s.lastRun} />
                <RunLine title="Last dry run" run={s.lastDryRun} />
              </li>
            );
          })}
        </ul>
      )}
      {confirming && (
        <Dialog title={`Run ${confirming.label}`} onClose={() => setConfirming(null)}>
          <ConfirmRun source={confirming} busy={busy} onStart={(dry) => void start([confirming], dry)} onCancel={() => setConfirming(null)} />
        </Dialog>
      )}
    </SectionCard>
  );
}

function ConfirmRun({
  source,
  busy,
  onStart,
  onCancel,
}: {
  source: VenueSyncSource;
  busy: boolean;
  onStart: (dryRun: boolean) => void;
  onCancel: () => void;
}) {
  const dry = freshDryRun(source);
  return (
    <div className="flex flex-col gap-4">
      {dry ? (
        <p className="text-rs-body text-rs-text">
          The dry run of {when.format(new Date(dry.startedAt))} found: {runCounts(dry)}. A real run writes these changes now.
        </p>
      ) : (
        <p className="text-rs-body text-rs-text">
          Do a dry run first to see what this run would change. A real run is allowed once a dry run newer than the last real run has
          succeeded.
        </p>
      )}
      <div className="flex flex-wrap justify-end gap-2">
        <button type="button" onClick={onCancel} className={rsButtonClass({ variant: "secondary", size: "form" })}>
          Cancel
        </button>
        {dry ? (
          <button type="button" disabled={busy} onClick={() => onStart(false)} className={rsButtonClass({ size: "form" })}>
            Run sync now
          </button>
        ) : (
          <button type="button" disabled={busy} onClick={() => onStart(true)} className={rsButtonClass({ size: "form" })}>
            Start dry run
          </button>
        )}
      </div>
    </div>
  );
}

function RunLine({ title, run }: { title: string; run: VenueSyncRun | null }) {
  if (!run) {
    return (
      <p className="text-rs-caption text-rs-text-muted">
        {title}: none yet
      </p>
    );
  }
  return (
    <div className="flex flex-col gap-1 text-rs-caption">
      <p className="flex flex-wrap items-center gap-1.5 text-rs-text">
        <span className="font-medium">{title}:</span>
        <span className={`${chipClass} ${statusChip[run.status]}`}>{statusLabel[run.status]}</span>
        <time dateTime={run.startedAt}>{when.format(new Date(run.startedAt))}</time>
        {run.status !== "running" && <span className="text-rs-text-muted">· {runCounts(run)}</span>}
      </p>
      {(run.errors.length > 0 || run.rejects.length > 0) && (
        <details className="text-rs-text">
          <summary className="cursor-pointer text-rs-accent-strong">
            {run.errors.length} {run.errors.length === 1 ? "error" : "errors"}, {run.rejects.length} rejected{" "}
            {run.rejects.length === 1 ? "entry" : "entries"}
          </summary>
          <ul className="mt-1 list-disc pl-5">
            {run.errors.map((e, i) => (
              <li key={`e${i}`} className="text-rs-danger">
                {e}
              </li>
            ))}
            {run.rejects.map((r) => (
              <li key={r.sourceKey}>
                {r.name} ({r.sourceKey}): {r.reasons.map(reasonText).join("; ")}
              </li>
            ))}
          </ul>
        </details>
      )}
    </div>
  );
}
