import { AnnotationMode, getDocument, GlobalWorkerOptions, type PDFDocumentProxy, type RenderTask, TextLayer } from "pdfjs-dist";

import { pageBox, type PageSize } from "./geometry";
import { pdfWorker } from "./worker";

// The PDF viewer's use of pdf.js (FR-13.1, docs/decisions.md). A PDF can hold JavaScript, forms
// and actions; this viewer runs none of them. It draws each page on a canvas, with a text layer
// over it so text can be selected and found:
// - no annotation layer, so no links, form fields, JavaScript actions or annotation scripts
//   (AnnotationMode.DISABLE also keeps annotation appearances off the canvas);
// - the text layer is pdf.js's TextLayer: transparent spans holding the page's text, set as
//   text, never as HTML; it runs nothing;
// - no PDFViewer and no scripting sandbox (pdf.sandbox.mjs is never loaded), so the document's
//   scripts have nothing to run in;
// - no XFA forms.
// pdf.js 6 no longer evaluates code from fonts or functions at all (the isEvalSupported option
// is gone with it; a test checks the shipped build has no eval).
// This module, and pdf.js with it, loads only when the workspace shows a PDF (dynamic import).

export const pdfOptions = { enableXfa: false, stopAtErrors: false } as const;
export const renderOptions = { annotationMode: AnnotationMode.DISABLE } as const;

export type PdfDocument = PDFDocumentProxy;

let worker: Worker | null = null;

export async function openPdf(data: ArrayBuffer): Promise<PdfDocument> {
  if (!worker) {
    worker = pdfWorker();
    GlobalWorkerOptions.workerPort = worker;
  }
  return getDocument({ data: new Uint8Array(data), ...pdfOptions }).promise;
}

// pageSizes is the size of every page at scale 1, in CSS pixels, so the viewer can lay out pages
// it has not drawn and fit the widest to the panel.
export async function pageSizes(doc: PdfDocument): Promise<PageSize[]> {
  const sizes: PageSize[] = [];
  for (let n = 1; n <= doc.numPages; n++) {
    const { width, height } = (await doc.getPage(n)).getViewport({ scale: 1 });
    sizes.push({ width, height });
  }
  return sizes;
}

export type Task = { done: Promise<void>; cancel: () => void };

// renderPage draws one page on a new canvas at scale on a screen of devicePixelRatio ratio: the
// bitmap in device pixels, the CSS size in CSS pixels (pdf/geometry.ts). The caller shows the
// canvas once done resolves, so the page on screen is replaced, never stretched. cancel() stops a
// render that is no longer wanted.
export function renderPage(doc: PdfDocument, pageNumber: number, scale: number, ratio: number): Task & { canvas: HTMLCanvasElement } {
  const canvas = document.createElement("canvas");
  let task: RenderTask | null = null;
  let cancelled = false;
  const done = (async () => {
    const page = await doc.getPage(pageNumber);
    if (cancelled) return;
    const base = page.getViewport({ scale: 1 });
    const viewport = page.getViewport({ scale });
    const box = pageBox(base, scale, ratio);
    canvas.width = box.pixelWidth;
    canvas.height = box.pixelHeight;
    canvas.style.width = `${box.cssWidth}px`;
    canvas.style.height = `${box.cssHeight}px`;
    const transform = [box.pixelWidth / viewport.width, 0, 0, box.pixelHeight / viewport.height, 0, 0];
    task = page.render({ canvas, viewport, transform, ...renderOptions });
    try {
      await task.promise;
    } catch (err) {
      if (!cancelled) throw err;
    }
  })();
  return {
    canvas,
    done,
    cancel: () => {
      cancelled = true;
      task?.cancel();
    },
  };
}

// renderText fills container with the page's text layer. It is laid out at scale 1 and sized by
// the CSS variable --total-scale-factor of an enclosing element, so a zoom changes that variable
// and never rebuilds the layer.
export function renderText(doc: PdfDocument, pageNumber: number, container: HTMLElement): Task {
  let layer: TextLayer | null = null;
  let cancelled = false;
  const done = (async () => {
    const page = await doc.getPage(pageNumber);
    if (cancelled) return;
    layer = new TextLayer({ textContentSource: page.streamTextContent(), container, viewport: page.getViewport({ scale: 1 }) });
    try {
      await layer.render();
    } catch (err) {
      if (!cancelled) throw err;
    }
  })();
  return {
    done,
    cancel: () => {
      cancelled = true;
      layer?.cancel();
    },
  };
}
