import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";

import { projectID } from "../../test-projects";
import { fetchPdf, getCompileLog } from "./api";
import { ErrorsPanel } from "./errors-panel";
import { ago, PdfPanel } from "./pdf-panel";
import { openPdf } from "./pdf/load-pdf";
import { Split } from "./split";
import { compile, succeeded } from "./test-compiles";

jest.mock("./api", () => ({ ...jest.requireActual("./api"), fetchPdf: jest.fn(), getCompileLog: jest.fn() }));
type Drawn = { canvas: HTMLCanvasElement; done: Promise<void>; cancel: () => void };
const mockRender = jest.fn<Drawn, [unknown, number, number, number]>((_doc, page, scale, ratio) => {
  const canvas = document.createElement("canvas");
  canvas.dataset.drawn = `${page}@${scale}x${ratio}`;
  return { canvas, done: Promise.resolve(), cancel: () => {} };
});
const mockRenderText = jest.fn<{ done: Promise<void>; cancel: () => void }, [unknown, number, HTMLElement]>(() => ({ done: Promise.resolve(), cancel: () => {} }));
jest.mock("./pdf/load-pdf", () => ({
  openPdf: jest.fn(async () => ({ numPages: 3, loadingTask: { destroy: async () => {} } })),
  pageSizes: async () => [1, 2, 3].map(() => ({ width: 600, height: 800 })),
  renderPage: (...args: [unknown, number, number, number]) => mockRender(...args),
  renderText: (...args: [unknown, number, HTMLElement]) => mockRenderText(...args),
}));

afterEach(() => jest.clearAllMocks());

describe("PdfPanel", () => {
  async function show(c = succeeded("c1")) {
    jest.mocked(fetchPdf).mockResolvedValue({ ok: true, data: { data: new ArrayBuffer(4), compileId: c.id } });
    const onDownload = jest.fn();
    const view = render(<PdfPanel projectId={projectID} compile={c} onDownload={onDownload} downloading={false} />);
    await screen.findByTestId("pdf-document");
    await act(async () => {});
    return { ...view, onDownload };
  }

  it("moves between pages and zooms", async () => {
    const { onDownload } = await show();
    expect(screen.getByTestId("pdf-pages").textContent).toBe("PDF · page 1 of 3");
    expect(screen.getByRole("button", { name: "Previous page" })).toHaveProperty("disabled", true);
    fireEvent.click(screen.getByRole("button", { name: "Next page" }));
    fireEvent.click(screen.getByRole("button", { name: "Next page" }));
    expect(screen.getByTestId("pdf-pages").textContent).toBe("PDF · page 3 of 3");
    expect(screen.getByRole("button", { name: "Next page" })).toHaveProperty("disabled", true);
    const box = screen.getByRole("textbox", { name: "Page number" });
    fireEvent.change(box, { target: { value: "9" } });
    fireEvent.keyDown(box, { key: "Enter" });
    expect(screen.getByTestId("pdf-pages").textContent).toBe("PDF · page 3 of 3");
    fireEvent.change(box, { target: { value: "2" } });
    fireEvent.blur(box);
    expect(screen.getByTestId("pdf-pages").textContent).toBe("PDF · page 2 of 3");

    // jsdom has no layout, so fit width keeps 100%; zoom steps from there.
    expect(screen.getByRole("button", { name: "Fit width" }).getAttribute("aria-pressed")).toBe("true");
    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    expect(screen.getByTestId("pdf-zoom").textContent).toBe("125%");
    expect(screen.getByRole("button", { name: "Fit width" }).getAttribute("aria-pressed")).toBe("false");
    fireEvent.click(screen.getByRole("button", { name: "Zoom out" }));
    fireEvent.click(screen.getByRole("button", { name: "Zoom out" }));
    expect(screen.getByTestId("pdf-zoom").textContent).toBe("75%");
    await act(async () => {});
    expect(mockRender).toHaveBeenCalledWith(expect.anything(), 2, 0.75, 1);
    fireEvent.click(screen.getByRole("button", { name: "Fit width" }));
    expect(screen.getByTestId("pdf-zoom").textContent).toBe("100%");

    fireEvent.click(screen.getByRole("button", { name: "Download PDF" }));
    expect(onDownload).toHaveBeenCalled();
  });

  it("draws the pages near the view at the zoom and density, and again when either changes", async () => {
    const dpr = window.devicePixelRatio;
    try {
      window.devicePixelRatio = 1;
      await show();
      // Pages 1 and 2 are near page 1 (jsdom has no IntersectionObserver); page 3 is not drawn.
      const drawn = (n: number) => screen.getByTestId(`pdf-page-${n}`).querySelector("canvas")?.dataset.drawn;
      await waitFor(() => expect(drawn(1)).toBe("1@1x1"));
      expect(drawn(2)).toBe("2@1x1");
      expect(drawn(3)).toBeUndefined();
      expect(screen.getByTestId("pdf-page-1").style.width).toBe("600px");

      // A zoom draws again at the new scale: the page box grows, nothing is stretched by CSS.
      fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
      await waitFor(() => expect(drawn(1)).toBe("1@1.25x1"));
      expect(screen.getByTestId("pdf-page-1").style.width).toBe("750px");
      expect(screen.getByTestId("pdf-page-1").style.transform).toBe("");

      // Moving to a denser screen (or a browser zoom) draws again at the new density.
      window.devicePixelRatio = 2;
      act(() => {
        window.dispatchEvent(new Event("resize"));
      });
      await waitFor(() => expect(drawn(1)).toBe("1@1.25x2"));

      // Going to page 3 draws it and frees page 1.
      fireEvent.click(screen.getByRole("button", { name: "Next page" }));
      fireEvent.click(screen.getByRole("button", { name: "Next page" }));
      await waitFor(() => expect(drawn(3)).toBe("3@1.25x2"));
      expect(drawn(1)).toBeUndefined();
    } finally {
      window.devicePixelRatio = dpr;
    }
  });

  it("builds every page's text layer, the near ones first", async () => {
    await show();
    await waitFor(() => expect(mockRenderText.mock.calls.map((c) => c[1])).toEqual([1, 2, 3]));
    expect(mockRenderText.mock.calls[0][2].className).toBe("rs-pdf-text");
  });

  it("keeps the PDF while a compile runs or fails, and swaps when a new one is ready", async () => {
    const { rerender } = await show();
    rerender(<PdfPanel projectId={projectID} compile={compile("c2", { status: "running" })} onDownload={() => {}} downloading={false} />);
    rerender(<PdfPanel projectId={projectID} compile={compile("c2", { status: "failed" })} onDownload={() => {}} downloading={false} />);
    await act(async () => {});
    expect(fetchPdf).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("pdf-document").getAttribute("data-compile")).toBe("c1");

    jest.mocked(fetchPdf).mockResolvedValue({ ok: true, data: { data: new ArrayBuffer(4), compileId: "c3" } });
    rerender(<PdfPanel projectId={projectID} compile={succeeded("c3")} onDownload={() => {}} downloading={false} />);
    expect(screen.getByTestId("pdf-document").getAttribute("data-compile")).toBe("c1");
    await act(async () => {});
    expect(screen.getByTestId("pdf-document").getAttribute("data-compile")).toBe("c3");
    expect(openPdf).toHaveBeenCalledTimes(2);
  });

  it("says there is no PDF yet, and when it can't show one", async () => {
    jest.mocked(fetchPdf).mockResolvedValue({ ok: false, code: "no_pdf", message: "x" });
    const { unmount } = render(<PdfPanel projectId={projectID} compile={null} onDownload={() => {}} downloading={false} />);
    expect(await screen.findByText("No PDF yet. Compile the project to see it here.")).toBeTruthy();
    unmount();
    jest.mocked(fetchPdf).mockResolvedValue({ ok: false, code: "internal_error", message: "x" });
    render(<PdfPanel projectId={projectID} compile={null} onDownload={() => {}} downloading={false} />);
    expect((await screen.findByRole("alert")).textContent).toContain("We couldn’t show the PDF.");
  });

  it("says when it was compiled", () => {
    const now = Date.parse("2026-11-23T10:10:00Z");
    expect(ago("2026-11-23T10:09:40Z", now)).toBe("just now");
    expect(ago("2026-11-23T10:08:00Z", now)).toBe("2 min ago");
    expect(ago("2026-11-23T07:00:00Z", now)).toBe("3 h ago");
  });
});

describe("ErrorsPanel", () => {
  const failed = compile("c1", {
    status: "failed",
    hasLog: true,
    errors: [
      { file: "main.tex", line: 14, message: "Undefined control sequence.", severity: "error" },
      { file: null, line: null, message: "Emergency stop.", severity: "error" },
      { file: "method.tex", line: 31, message: "Citation ‘lundberg2017’ undefined.", severity: "warning" },
    ],
  });

  it("lists errors first and folds the warnings, with counts, and jumps on a click", () => {
    const onJump = jest.fn();
    render(<ErrorsPanel projectId={projectID} compile={failed} missing={null} onJump={onJump} />);
    expect(screen.getByRole("heading", { name: /Compile errors/ })).toBeTruthy();
    expect(screen.getByLabelText("2 errors").textContent).toBe("2");
    const warnings = screen.getByText("1 warning").closest("details")!;
    expect(warnings.open).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: /main\.tex:14/ }));
    expect(onJump).toHaveBeenCalledWith({ file: "main.tex", line: 14 });
    // An error outside the project's files goes nowhere.
    fireEvent.click(screen.getByRole("button", { name: /Emergency stop/ }));
    expect(onJump).toHaveBeenCalledTimes(1);
  });

  it("moves with the arrow keys, into the warnings once they are open", () => {
    render(<ErrorsPanel projectId={projectID} compile={failed} missing={null} onJump={() => {}} />);
    const first = screen.getByRole("button", { name: /main\.tex:14/ });
    expect(first.tabIndex).toBe(0);
    first.focus();
    fireEvent.keyDown(first, { key: "ArrowDown" });
    expect(document.activeElement).toBe(screen.getByRole("button", { name: /Emergency stop/ }));
    fireEvent.keyDown(document.activeElement!, { key: "ArrowDown" });
    expect(document.activeElement).toBe(screen.getByRole("button", { name: /Emergency stop/ }));
    (screen.getByText("1 warning").closest("details") as HTMLDetailsElement).open = true;
    fireEvent.keyDown(document.activeElement!, { key: "End" });
    expect(document.activeElement).toBe(screen.getByRole("button", { name: /method\.tex:31/ }));
    fireEvent.keyDown(document.activeElement!, { key: "Home" });
    expect(document.activeElement).toBe(first);
  });

  it("says a file is gone, and waits for a running compile", () => {
    const { rerender } = render(<ErrorsPanel projectId={projectID} compile={failed} missing="old.tex" onJump={() => {}} />);
    expect(screen.getByRole("alert").textContent).toBe("old.tex no longer exists in this project.");
    rerender(<ErrorsPanel projectId={projectID} compile={compile("c2", { status: "running" })} missing={null} onJump={() => {}} />);
    expect(screen.getByText("Errors appear here when the compile finishes.")).toBeTruthy();
  });

  it("loads the raw log only when asked, and searches it", async () => {
    jest.mocked(getCompileLog).mockResolvedValue({ ok: true, data: "This is pdfTeX\n! Undefined control sequence.\nl.14 \\shapplot\nOutput written" });
    render(<ErrorsPanel projectId={projectID} compile={failed} missing={null} onJump={() => {}} />);
    expect(getCompileLog).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "View full log" }));
    const log = await screen.findByLabelText("Compile log");
    expect(getCompileLog).toHaveBeenCalledWith(projectID, "c1");
    expect(log.textContent).toContain("Output written");
    fireEvent.change(screen.getByRole("searchbox", { name: "Search the log" }), { target: { value: "undefined" } });
    expect(screen.getByText("1 matching line")).toBeTruthy();
    expect(within(log).queryByText("Output written")).toBeNull();
  });

  it("says the log is gone when the API no longer keeps it", async () => {
    jest.mocked(getCompileLog).mockResolvedValue({ ok: false, code: "no_log", message: "x" });
    render(<ErrorsPanel projectId={projectID} compile={failed} missing={null} onJump={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: "View full log" }));
    expect(await screen.findByText("The log is kept only for the newest finished compile.")).toBeTruthy();
  });
});

describe("Split", () => {
  it("resizes with the keyboard within its limits", () => {
    render(<Split left={<p>left</p>} right={<p>right</p>} initial={70} />);
    const sep = screen.getByRole("separator", { name: "Resize the editor and the PDF" });
    expect(sep.getAttribute("aria-valuenow")).toBe("70");
    fireEvent.keyDown(sep, { key: "ArrowRight" });
    fireEvent.keyDown(sep, { key: "ArrowRight" });
    expect(sep.getAttribute("aria-valuenow")).toBe("75");
    fireEvent.keyDown(sep, { key: "Home" });
    expect(sep.getAttribute("aria-valuenow")).toBe("30");
    fireEvent.keyDown(sep, { key: "ArrowLeft" });
    expect(sep.getAttribute("aria-valuenow")).toBe("30");
  });
});
