import { readFileSync } from "node:fs";
import { join } from "node:path";

import { openPdf, pageSizes, pdfOptions, renderOptions, renderPage, renderText } from "./load-pdf";

// The viewer runs nothing a PDF holds (FR-13.1, docs/decisions.md): pdf.js draws pages on a
// canvas, with a text layer, without annotations, forms, XFA or the scripting sandbox.

const mockGetDocument = jest.fn();
const mockWorkerOptions: { workerPort?: unknown } = {};
jest.mock("pdfjs-dist", () => ({
  AnnotationMode: { DISABLE: 0, ENABLE: 1, ENABLE_FORMS: 2, ENABLE_STORAGE: 3 },
  get GlobalWorkerOptions() {
    return mockWorkerOptions;
  },
  getDocument: (...args: unknown[]) => mockGetDocument(...args),
  TextLayer: jest.fn(function (this: { render: () => Promise<void>; cancel: () => void }) {
    this.render = async () => {};
    this.cancel = () => {};
  }),
}));
const mockWorker = { kind: "worker" };
jest.mock("./worker", () => ({ pdfWorker: () => mockWorker }));

describe("openPdf", () => {
  it("opens the bytes without XFA, with pdf.js's own worker", async () => {
    const doc = { numPages: 2 };
    mockGetDocument.mockReturnValue({ promise: Promise.resolve(doc) });
    expect(await openPdf(new Uint8Array([37, 80, 68, 70]).buffer)).toBe(doc);
    const [params] = mockGetDocument.mock.calls[0] as [Record<string, unknown>];
    expect(params.enableXfa).toBe(false);
    expect(params).toEqual({ data: new Uint8Array([37, 80, 68, 70]), ...pdfOptions });
    // Nothing turns scripting on: no scripting option exists at this level, and none is passed.
    expect(Object.keys(params).filter((k) => /script|eval|sandbox/i.test(k))).toEqual([]);
    expect(mockWorkerOptions.workerPort).toBe(mockWorker);
  });
});

function fakePage() {
  const render = jest.fn(() => ({ promise: Promise.resolve(), cancel: jest.fn() }));
  const page = {
    getViewport: ({ scale }: { scale: number }) => ({ width: 595.28 * scale, height: 841.89 * scale, scale }),
    render,
    streamTextContent: jest.fn(() => "text stream"),
  };
  return { page, render, doc: { numPages: 2, getPage: jest.fn(async () => page) } };
}

describe("renderPage", () => {
  const dpr = window.devicePixelRatio;
  afterEach(() => {
    window.devicePixelRatio = dpr;
  });

  it("draws the page on a new canvas with every annotation disabled", async () => {
    const { doc, render } = fakePage();
    const task = renderPage(doc as never, 3, 1.5, 1);
    await task.done;
    expect(doc.getPage).toHaveBeenCalledWith(3);
    expect(render).toHaveBeenCalledWith(expect.objectContaining({ canvas: task.canvas, annotationMode: 0 }));
    expect(renderOptions).toEqual({ annotationMode: 0 });
  });

  // The fix for a blurry viewer: one canvas pixel per screen pixel, at 100%, 150% and fit width
  // (an odd scale), on screens of several densities.
  it.each([
    [1, 1],
    [1.5, 1],
    [0.8731, 1],
    [1, 1.5],
    [1.5, 1.5],
    [0.8731, 1.5],
    [1, 2],
    [1.25, 1.25],
  ])("at scale %s and devicePixelRatio %s the canvas is its CSS size x devicePixelRatio", async (scale, ratio) => {
    window.devicePixelRatio = ratio;
    const { doc, render } = fakePage();
    const { canvas, done } = renderPage(doc as never, 1, scale, window.devicePixelRatio);
    await done;
    const cssWidth = parseFloat(canvas.style.width);
    const cssHeight = parseFloat(canvas.style.height);
    expect(canvas.width).toBeCloseTo(cssWidth * ratio, 6);
    expect(canvas.height).toBeCloseTo(cssHeight * ratio, 6);
    // Whole CSS pixels, at most a couple of device pixels under the page's size at this scale.
    expect(Number.isInteger(cssWidth) && Number.isInteger(cssHeight)).toBe(true);
    expect(595.28 * scale - cssWidth).toBeGreaterThanOrEqual(0);
    expect(595.28 * scale - cssWidth).toBeLessThan(2);
    // pdf.js draws at the CSS scale and the transform takes it to device pixels.
    const [{ viewport, transform }] = render.mock.calls[0] as unknown as [{ viewport: { scale: number; width: number }; transform: number[] }];
    expect(viewport.scale).toBe(scale);
    expect(transform[0] * viewport.width).toBeCloseTo(canvas.width, 6);
  });

  it("does not draw a cancelled page", async () => {
    const { doc, render } = fakePage();
    const task = renderPage(doc as never, 1, 1, 1);
    task.cancel();
    await task.done;
    expect(render).not.toHaveBeenCalled();
  });
});

describe("renderText", () => {
  it("builds pdf.js's text layer from the page's text, at scale 1", async () => {
    const { TextLayer } = jest.requireMock<{ TextLayer: jest.Mock }>("pdfjs-dist");
    const { doc, page } = fakePage();
    const container = document.createElement("div");
    await renderText(doc as never, 2, container).done;
    expect(doc.getPage).toHaveBeenCalledWith(2);
    expect(page.streamTextContent).toHaveBeenCalled();
    expect(TextLayer).toHaveBeenCalledWith({ textContentSource: "text stream", container, viewport: expect.objectContaining({ scale: 1 }) });
  });
});

describe("pageSizes", () => {
  it("is every page's size at scale 1", async () => {
    const { doc } = fakePage();
    expect(await pageSizes(doc as never)).toEqual([
      { width: 595.28, height: 841.89 },
      { width: 595.28, height: 841.89 },
    ]);
  });
});

describe("the pdf.js build", () => {
  // pdf.js 6 dropped isEvalSupported because it no longer evaluates code: keep it that way.
  it.each(["build/pdf.mjs", "build/pdf.worker.min.mjs"])("%s has no eval or new Function", (file) => {
    const code = readFileSync(join(process.cwd(), "node_modules/pdfjs-dist", file), "utf8");
    expect(code).not.toMatch(/\beval\s*\(/);
    expect(code).not.toMatch(/new Function\s*\(/);
  });

  it("is never told to load its scripting sandbox", () => {
    const code = (f: string) => readFileSync(join(__dirname, f), "utf8").replace(/^\s*\/\/.*$/gm, "");
    const viewer = code("load-pdf.ts") + code("../pdf-panel.tsx");
    expect(viewer).not.toMatch(/pdf\.sandbox|enableScripting:\s*true|AnnotationLayer|PDFViewer|pdf_viewer/);
  });
});
