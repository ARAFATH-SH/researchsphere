"use client";

import { type CSSProperties, useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";

import { type Compile, fetchPdf } from "./api";
import { seconds } from "./compile-status";
import { pageBox, type PageSize } from "./pdf/geometry";
import type { PdfDocument, Task } from "./pdf/load-pdf";
import { usePixelRatio } from "./pdf/use-pixel-ratio";

// pdf.js loads only when the workspace first shows a PDF.
const loadPdfModule = () => import("./pdf/load-pdf");

type Shown = { doc: PdfDocument; compileId: string | null; sizes: PageSize[] };
type Zoom = { fit: true } | { fit: false; scale: number };

const zoomSteps = [0.5, 0.75, 1, 1.25, 1.5, 2, 3];
// Pages within one panel height above or below the visible part are drawn; the others hold no
// canvas.
const nearMargin = "100% 0px";
// Text layers of pages not yet near are built one at a time in the background, so the browser's
// find covers the whole paper, up to this many pages.
const backgroundTextPages = 100;
const gap = 12;

// wantedPdf is the compile whose PDF the viewer should show next: a compile that made one.
export function wantedPdf(compile: Compile | null): string | null {
  return compile?.status === "succeeded" && compile.hasPdf ? compile.id : null;
}

// PdfPanel shows the project's latest PDF beside the editor (FR-13.1), drawn by pdf.js with
// nothing in the document run (pdf/load-pdf.ts), with page navigation, zoom, fit width and a
// download (FR-13.6). It shows the latest PDF on open, and keeps the shown PDF while a new
// compile runs until the new one has loaded, then swaps. Pages are one scrolling column; only
// those near the visible part are drawn, at the screen's pixel density, and drawn again when the
// zoom or the density changes.
export function PdfPanel({
  projectId,
  compile,
  onDownload,
  downloading,
}: {
  projectId: string;
  compile: Compile | null;
  onDownload: () => void;
  downloading: boolean;
}) {
  const [shown, setShown] = useState<Shown | null>(null);
  const [state, setState] = useState<"loading" | "none" | "error" | "ok">("loading");
  const [page, setPage] = useState(1);
  const [zoom, setZoom] = useState<Zoom>({ fit: true });
  const [fitScale, setFitScale] = useState(1);
  const [near, setNear] = useState<ReadonlySet<number>>(new Set());
  // draft is what the member is typing into the page box; null shows the page.
  const [draft, setDraft] = useState<string | null>(null);
  const ratio = usePixelRatio();
  const box = useRef<HTMLDivElement>(null);
  const pageEls = useRef<(HTMLDivElement | null)[]>([]);
  // anchor is where the view is, as a page and a fraction of it, kept across a zoom.
  const anchor = useRef({ page: 1, fraction: 0 });
  const textQueue = useRef<Promise<void>>(Promise.resolve());
  const shownRef = useRef<Shown | null>(null);
  const request = useRef(0);
  const want = wantedPdf(compile);

  const load = useCallback(async () => {
    const token = ++request.current;
    const res = await fetchPdf(projectId);
    if (token !== request.current) return;
    if (!res.ok) {
      // A failed read keeps the PDF on screen; with none shown, say why.
      if (!shownRef.current) setState(res.code === "no_pdf" ? "none" : "error");
      return;
    }
    try {
      const { openPdf, pageSizes } = await loadPdfModule();
      const doc = await openPdf(res.data.data);
      const sizes = await pageSizes(doc);
      if (token !== request.current) {
        void doc.loadingTask.destroy();
        return;
      }
      const old = shownRef.current;
      const next = { doc, compileId: res.data.compileId, sizes };
      shownRef.current = next;
      textQueue.current = Promise.resolve();
      setShown(next);
      setState("ok");
      setPage((p) => Math.min(Math.max(1, p), doc.numPages));
      if (old) void old.doc.loadingTask.destroy();
    } catch {
      if (token === request.current && !shownRef.current) setState("error");
    }
  }, [projectId]);

  // The latest PDF on open; then each new PDF a compile made.
  useEffect(() => {
    if (shownRef.current && want !== null && shownRef.current.compileId === want) return;
    if (shownRef.current && want === null) return;
    void load();
  }, [want, load]);

  useEffect(
    () => () => {
      request.current++;
      void shownRef.current?.doc.loadingTask.destroy();
      shownRef.current = null;
    },
    [],
  );

  // Fit width fits the widest page to the panel's width, and follows it.
  useEffect(() => {
    const el = box.current;
    if (!shown || !zoom.fit || !el) return;
    const widest = Math.max(...shown.sizes.map((s) => s.width));
    const measure = () => {
      const available = el.clientWidth - 32;
      if (widest > 0 && available > 0) setFitScale(available / widest);
    };
    measure();
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(measure);
    observer?.observe(el);
    return () => observer?.disconnect();
  }, [shown, zoom.fit]);

  const scale = zoom.fit ? fitScale : zoom.scale;
  const pages = shown?.doc.numPages ?? 0;

  // Which pages are near the visible part.
  const observes = typeof IntersectionObserver !== "undefined";
  useEffect(() => {
    const root = box.current;
    if (!shown || !root || !observes) return;
    const observer = new IntersectionObserver(
      (entries) =>
        setNear((prev) => {
          const next = new Set(prev);
          for (const e of entries) {
            const n = Number((e.target as HTMLElement).dataset.page);
            if (e.isIntersecting) next.add(n);
            else next.delete(n);
          }
          return next;
        }),
      { root, rootMargin: nearMargin },
    );
    for (const el of pageEls.current.slice(0, shown.doc.numPages)) if (el) observer.observe(el);
    return () => observer.disconnect();
  }, [shown, observes]);
  // Without IntersectionObserver (jsdom), the page shown and its neighbours.
  const isNear = (n: number) => (observes ? near.has(n) : Math.abs(n - page) <= 1);

  // The page shown is the one across the upper third of the panel, or the last at the bottom.
  const onScroll = () => {
    const el = box.current;
    if (!el || pages === 0) return;
    const line = el.scrollTop + el.clientHeight / 3;
    let n = 1;
    for (let i = 0; i < pages; i++) {
      const p = pageEls.current[i];
      if (p && p.offsetTop <= line) n = i + 1;
    }
    const top = pageEls.current[n - 1];
    if (top) anchor.current = { page: n, fraction: Math.max(0, el.scrollTop - top.offsetTop) / Math.max(1, top.offsetHeight) };
    if (el.scrollHeight > el.clientHeight && el.scrollTop + el.clientHeight >= el.scrollHeight - 1) n = pages;
    setPage(n);
  };

  // A zoom keeps the same place of the same page in view.
  useLayoutEffect(() => {
    const el = box.current;
    const p = pageEls.current[anchor.current.page - 1];
    if (el && p) el.scrollTop = p.offsetTop + anchor.current.fraction * p.offsetHeight;
  }, [scale]);

  const go = (n: number) => {
    setDraft(null);
    const next = Math.min(Math.max(1, n), Math.max(1, pages));
    setPage(next);
    anchor.current = { page: next, fraction: 0 };
    const el = box.current;
    const p = pageEls.current[next - 1];
    if (el && p) el.scrollTop = p.offsetTop;
  };
  const typed = () => go(Number(draft) || page);
  const zoomBy = (dir: 1 | -1) => {
    const now = scale;
    const next = dir > 0 ? (zoomSteps.find((s) => s > now + 0.001) ?? zoomSteps.at(-1)!) : ([...zoomSteps].reverse().find((s) => s < now - 0.001) ?? zoomSteps[0]);
    setZoom({ fit: false, scale: next });
  };
  const queueText = useCallback((job: () => Promise<void>) => {
    textQueue.current = textQueue.current.then(job, job);
  }, []);
  const made = compile && shown && compile.id === shown.compileId ? compile : null;

  return (
    <section aria-label="PDF preview" className="flex h-full min-h-0 min-w-0 flex-col gap-2.5 bg-rs-subtle p-4">
      <div className="flex flex-wrap items-center justify-between gap-2 text-rs-caption text-rs-text">
        <span data-testid="pdf-pages">{pages > 0 ? `PDF · page ${page} of ${pages}` : "PDF"}</span>
        {made?.durationMs != null && made.finishedAt && (
          <span className="text-rs-text-muted">
            Compiled {ago(made.finishedAt)} · {seconds(made.durationMs)}
          </span>
        )}
      </div>
      {shown && (
        <div role="toolbar" aria-label="PDF" className="flex flex-wrap items-center gap-1 text-rs-caption">
          <PdfButton label="Previous page" disabled={page <= 1} onClick={() => go(page - 1)}>
            ‹
          </PdfButton>
          <label className="flex items-center gap-1">
            <input
              value={draft ?? String(page)}
              inputMode="numeric"
              aria-label="Page number"
              onChange={(e) => setDraft(e.target.value)}
              onBlur={typed}
              onKeyDown={(e) => {
                if (e.key === "Enter") typed();
              }}
              className="h-8 w-12 rounded-control border border-rs-control bg-rs-surface px-1.5 text-center"
            />
            <span aria-hidden="true">/ {pages}</span>
          </label>
          <PdfButton label="Next page" disabled={page >= pages} onClick={() => go(page + 1)}>
            ›
          </PdfButton>
          <span className="mx-1 h-5 w-px bg-rs-border" aria-hidden="true" />
          <PdfButton label="Zoom out" disabled={scale <= zoomSteps[0] + 0.001} onClick={() => zoomBy(-1)}>
            −
          </PdfButton>
          <span data-testid="pdf-zoom" className="w-11 text-center">
            {Math.round(scale * 100)}%
          </span>
          <PdfButton label="Zoom in" disabled={scale >= zoomSteps.at(-1)! - 0.001} onClick={() => zoomBy(1)}>
            +
          </PdfButton>
          <button
            type="button"
            aria-pressed={zoom.fit}
            onClick={() => setZoom({ fit: true })}
            className={`h-8 rounded-control border px-2 focus-visible:shadow-focus focus-visible:outline-none ${
              zoom.fit ? "border-rs-accent-strong bg-rs-accent-soft text-rs-heading" : "border-rs-control bg-rs-surface text-rs-text"
            }`}
          >
            Fit width
          </button>
          <button type="button" onClick={onDownload} disabled={downloading} className={`${rsButtonClass({ variant: "outline", size: "xs" })} ml-auto`}>
            {downloading ? "Downloading…" : "Download PDF"}
          </button>
        </div>
      )}
      <div ref={box} onScroll={onScroll} data-testid="pdf-scroll" className="relative min-h-0 flex-1 overflow-auto">
        {shown ? (
          <div data-testid="pdf-document" data-compile={shown.compileId ?? ""} className="flex w-fit min-w-full flex-col items-center" style={{ gap }}>
            {shown.sizes.map((size, i) => (
              <PdfPage
                key={`${shown.compileId}:${i}`}
                ref={(el) => {
                  pageEls.current[i] = el;
                }}
                doc={shown.doc}
                pageNumber={i + 1}
                pages={pages}
                size={size}
                scale={scale}
                ratio={ratio}
                near={isNear(i + 1)}
                queueText={i < backgroundTextPages ? queueText : null}
              />
            ))}
          </div>
        ) : state === "loading" ? (
          <p role="status" className="text-rs-body text-rs-text-muted">
            Loading the PDF…
          </p>
        ) : state === "none" ? (
          <p className="text-rs-body text-rs-text-muted">No PDF yet. Compile the project to see it here.</p>
        ) : (
          <div role="alert" className="flex flex-col items-start gap-2 text-rs-body">
            <p>We couldn’t show the PDF.</p>
            <button type="button" onClick={() => void load()} className={rsButtonClass({ variant: "secondary", size: "xs" })}>
              Try again
            </button>
          </div>
        )}
      </div>
    </section>
  );
}

// PdfPage is one page: its box is always laid out at the current scale; its canvas exists only
// while the page is near the visible part, and its text layer is built once.
function PdfPage({
  ref,
  doc,
  pageNumber,
  pages,
  size,
  scale,
  ratio,
  near,
  queueText,
}: {
  ref: (el: HTMLDivElement | null) => void;
  doc: PdfDocument;
  pageNumber: number;
  pages: number;
  size: PageSize;
  scale: number;
  ratio: number;
  near: boolean;
  queueText: ((job: () => Promise<void>) => void) | null;
}) {
  const canvasHolder = useRef<HTMLDivElement>(null);
  const text = useRef<HTMLDivElement>(null);
  const textState = useRef<"none" | "queued" | "building" | "built">("none");
  const textTask = useRef<Task | null>(null);
  const box = pageBox(size, scale, ratio);

  // Draw at the current scale and density; the new canvas replaces the old one when it is done,
  // so the page is never shown stretched.
  useEffect(() => {
    if (!near) return;
    let task: Task | null = null;
    let current = true;
    void loadPdfModule().then(({ renderPage }) => {
      if (!current) return;
      const t = renderPage(doc, pageNumber, scale, ratio);
      task = t;
      t.done.then(
        () => {
          if (current) canvasHolder.current?.replaceChildren(t.canvas);
        },
        () => {},
      );
    });
    return () => {
      current = false;
      task?.cancel();
    };
  }, [doc, pageNumber, scale, ratio, near]);

  // A page far from view gives its canvas memory back.
  useEffect(() => {
    if (near) return;
    const holder = canvasHolder.current;
    for (const c of holder?.querySelectorAll("canvas") ?? []) {
      c.width = 0;
      c.height = 0;
    }
    holder?.replaceChildren();
  }, [near]);

  // The text layer is built once: at once when the page is near, else in the background queue.
  // An unmount (or React's development remount) cancels a layer being built and empties it, so
  // the next mount builds it again.
  const alive = useRef(false);
  // Each build has a number; a newer build or an unmount makes an older one stop.
  const generation = useRef(0);
  useEffect(() => {
    alive.current = true;
    const holder = text.current;
    const builds = generation;
    return () => {
      alive.current = false;
      if (textState.current === "built") return;
      builds.current++;
      textTask.current?.cancel();
      textState.current = "none";
      holder?.replaceChildren();
    };
  }, []);

  useEffect(() => {
    const build = async () => {
      if (textState.current === "building" || textState.current === "built") return;
      textState.current = "building";
      const mine = ++generation.current;
      const { renderText } = await loadPdfModule();
      if (generation.current !== mine) return;
      if (!alive.current || !text.current) {
        textState.current = "none";
        return;
      }
      const t = renderText(doc, pageNumber, text.current);
      textTask.current = t;
      try {
        await t.done;
        if (generation.current === mine) textState.current = "built";
      } catch {
        if (generation.current === mine) textState.current = "none";
      }
    };
    if (near) void build();
    else if (queueText && textState.current === "none") {
      textState.current = "queued";
      queueText(async () => {
        if (alive.current && textState.current === "queued") {
          textState.current = "none";
          await build();
        }
      });
    }
  }, [doc, pageNumber, near, queueText]);

  const style = {
    width: box.cssWidth,
    height: box.cssHeight,
    "--total-scale-factor": box.cssWidth / size.width,
    "--scale-round-x": "1px",
    "--scale-round-y": "1px",
  } as CSSProperties;
  return (
    <div
      ref={ref}
      data-page={pageNumber}
      data-testid={`pdf-page-${pageNumber}`}
      role="group"
      aria-label={`Page ${pageNumber} of ${pages}`}
      className="relative shrink-0 overflow-hidden rounded-rs-xs bg-rs-surface ring-1 ring-rs-border"
      style={style}
    >
      <div ref={canvasHolder} aria-hidden="true" className="absolute top-0 left-0" />
      <div ref={text} className="rs-pdf-text" />
    </div>
  );
}

function PdfButton({ label, disabled, onClick, children }: { label: string; disabled: boolean; onClick: () => void; children: string }) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={onClick}
      className="flex size-8 items-center justify-center rounded-control text-rs-lead text-rs-text hover:bg-rs-surface focus-visible:shadow-focus focus-visible:outline-none disabled:opacity-40"
    >
      <span aria-hidden="true">{children}</span>
    </button>
  );
}

// ago says how long ago a time was, as the design's "Compiled 2 min ago".
export function ago(iso: string, now = Date.now()): string {
  const min = Math.floor((now - Date.parse(iso)) / 60_000);
  if (min < 1) return "just now";
  if (min < 60) return `${min} min ago`;
  const h = Math.floor(min / 60);
  if (h < 24) return `${h} h ago`;
  return `on ${new Date(iso).toLocaleDateString()}`;
}
