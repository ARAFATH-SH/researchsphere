// pdfWorker starts pdf.js's worker from the package, bundled as its own file. Kept apart from
// load-pdf.ts because only the bundler understands `new URL(..., import.meta.url)`.
export function pdfWorker(): Worker {
  return new Worker(new URL("pdfjs-dist/build/pdf.worker.min.mjs", import.meta.url), { type: "module" });
}
