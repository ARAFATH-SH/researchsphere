"use client";

import { useCallback, useEffect, useId, useRef, useState } from "react";

import { saveBlob } from "@/lib/download";
import type { Result } from "@/lib/api/call";

import { type Download, downloadArchive, downloadErrorText, downloadPdf } from "./api";

type Kind = "pdf" | "zip";

// useDownloads saves the latest PDF (FR-13.6) and the project's files as a ZIP (FR-13.7) for
// every member, one at a time each, with the reason when the API refuses.
export function useDownloads(projectId: string) {
  const [busy, setBusy] = useState<ReadonlySet<Kind>>(new Set());
  const [error, setError] = useState<string | null>(null);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const run = useCallback(
    async (kind: Kind, fetcher: (id: string) => Promise<Result<Download>>) => {
      setBusy((b) => new Set(b).add(kind));
      setError(null);
      const res = await fetcher(projectId);
      if (!mounted.current) return;
      setBusy((b) => {
        const next = new Set(b);
        next.delete(kind);
        return next;
      });
      if (res.ok) saveBlob(res.data.blob, res.data.name);
      else setError(downloadErrorText(res));
    },
    [projectId],
  );

  const pdf = useCallback(() => void run("pdf", downloadPdf), [run]);
  const zip = useCallback(() => void run("zip", downloadArchive), [run]);
  const dismiss = useCallback(() => setError(null), []);
  return { pdf, zip, busy, error, dismiss };
}

export type Downloads = ReturnType<typeof useDownloads>;

export const barButton =
  "h-[38px] rounded-rs-pill border border-rs-surface bg-transparent px-3.5 text-rs-body font-medium text-rs-surface hover:bg-rs-accent-deep focus-visible:shadow-[0_0_0_2px_var(--rs-surface)] focus-visible:outline-none disabled:opacity-60";

// DownloadButtons are the bar's "Download PDF" and "Download ZIP" (design/screens/Workspace.html)
// on a wide screen, and a "More" menu holding both below 1024 pixels.
export function DownloadButtons({ downloads }: { downloads: Downloads }) {
  const { pdf, zip, busy } = downloads;
  return (
    <>
      <div className="hidden items-center gap-2.5 min-[1024px]:flex">
        <button type="button" onClick={pdf} disabled={busy.has("pdf")} className={barButton}>
          {busy.has("pdf") ? "Downloading…" : "Download PDF"}
        </button>
        <button type="button" onClick={zip} disabled={busy.has("zip")} aria-label="Download all files (ZIP)" className={barButton}>
          {busy.has("zip") ? "Downloading…" : "Download ZIP"}
        </button>
      </div>
      <div className="min-[1024px]:hidden">
        <MoreMenu downloads={downloads} />
      </div>
    </>
  );
}

function MoreMenu({ downloads }: { downloads: Downloads }) {
  const [open, setOpen] = useState(false);
  const menuId = useId();
  const button = useRef<HTMLButtonElement>(null);
  const items = useRef<(HTMLButtonElement | null)[]>([]);
  const box = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    items.current[0]?.focus();
    // A press outside closes the menu.
    const onDown = (e: PointerEvent) => {
      if (!box.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("pointerdown", onDown);
    return () => document.removeEventListener("pointerdown", onDown);
  }, [open]);

  function close(focus = true) {
    setOpen(false);
    if (focus) button.current?.focus();
  }

  function choose(action: () => void) {
    close();
    action();
  }

  return (
    <div ref={box} className="relative">
      <button
        ref={button}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        onClick={() => setOpen((o) => !o)}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown") {
            e.preventDefault();
            setOpen(true);
          }
        }}
        className={barButton}
      >
        More
      </button>
      {open && (
        <div
          id={menuId}
          role="menu"
          aria-label="Project"
          onKeyDown={(e) => {
            const list = items.current.filter((x): x is HTMLButtonElement => x !== null);
            const at = list.indexOf(document.activeElement as HTMLButtonElement);
            if (e.key === "Escape") {
              e.preventDefault();
              close();
            } else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
              e.preventDefault();
              const step = e.key === "ArrowDown" ? 1 : -1;
              list[(at + step + list.length) % list.length]?.focus();
            } else if (e.key === "Home" || e.key === "End") {
              e.preventDefault();
              list[e.key === "Home" ? 0 : list.length - 1]?.focus();
            } else if (e.key === "Tab") {
              close(false);
            }
          }}
          className="absolute right-0 z-50 mt-1 flex min-w-[220px] flex-col rounded-control border border-rs-border bg-rs-surface py-1 text-rs-text shadow-menu"
        >
          {(
            [
              ["Download PDF", downloads.pdf],
              ["Download all files (ZIP)", downloads.zip],
            ] as const
          ).map(([label, action], i) => (
            <button
              key={label}
              ref={(el) => {
                items.current[i] = el;
              }}
              type="button"
              role="menuitem"
              tabIndex={-1}
              onClick={() => choose(action)}
              className="px-3 py-2 text-left text-rs-body hover:bg-rs-subtle focus-visible:bg-rs-subtle focus-visible:outline-none"
            >
              {label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

// DownloadError shows why a download did not work, under the bar.
export function DownloadError({ downloads }: { downloads: Downloads }) {
  if (!downloads.error) return null;
  return (
    <div role="alert" className="flex items-center gap-2 border-b border-rs-danger-line bg-rs-danger-soft px-4 py-2 text-rs-caption text-rs-danger-strong">
      <span>{downloads.error}</span>
      <button type="button" onClick={downloads.dismiss} className="ml-auto font-medium underline">
        Dismiss
      </button>
    </div>
  );
}
