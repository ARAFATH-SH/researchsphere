import "@/test/dialog-polyfill";
import "@/test/codemirror";

import { EditorView } from "@codemirror/view";
import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { getProject } from "../api";
import {
  createFile,
  deleteFile,
  getFile,
  listFiles,
  moveFile,
  type ProjectFile,
  type ProjectFileDetail,
  type SaveResult,
  saveContent,
  uploadFigure,
} from "../files/api";
import { figure, folder, storage, textFile, tree } from "../files/test-files";
import { project, projectID } from "../test-projects";
import { autosaveDelayMs } from "./autosave";
import { WorkspacePage } from "./workspace-page";

jest.mock("../api", () => ({ ...jest.requireActual("../api"), getProject: jest.fn() }));
jest.mock("../files/api", () => ({
  ...jest.requireActual("../files/api"),
  listFiles: jest.fn(),
  getFile: jest.fn(),
  createFile: jest.fn(),
  moveFile: jest.fn(),
  deleteFile: jest.fn(),
  saveContent: jest.fn(),
  uploadFigure: jest.fn(),
}));
jest.mock("@/features/moderation/warning-banner", () => ({ WarningBanner: () => null }));

const mockList = jest.mocked(listFiles);
const mockGet = jest.mocked(getFile);
const mockSave = jest.mocked(saveContent);

const main = textFile("t-main", "main.tex", { version: 3 });
const sections = folder("d-sec", "sections");
const intro = textFile("t-intro", "sections/intro.tex");
const method = textFile("t-method", "sections/method.tex");
const figures = folder("d-fig", "figures");
const plot = figure("f-plot", "figures/plot.png");
const diagram = figure("f-diag", "figures/diagram.pdf");
const items = [figures, plot, diagram, main, sections, intro, method];

const texts: Record<string, string> = { [main.id]: "\\section{Intro}\nHello", [intro.id]: "Intro text", [method.id]: "" };

function detail(file: ProjectFile): ProjectFileDetail {
  if (file.kind === "text") return { file, content: texts[file.id] ?? "", download: null };
  return { file, content: null, download: { url: `https://storage.test/${file.id}`, expiresAt: "2026-11-17T10:05:00Z" } };
}

const saved = (version: number): SaveResult => ({ ok: true, data: { ...main, version } });

beforeEach(() => {
  jest.mocked(getProject).mockResolvedValue({ ok: true, data: project({ myRole: "owner" }) });
  mockList.mockResolvedValue({ ok: true, data: tree(items) });
  mockGet.mockImplementation(async (_p, id) => {
    const file = items.find((f) => f.id === id)!;
    return { ok: true, data: detail(file) };
  });
  mockSave.mockResolvedValue(saved(4));
});
afterEach(() => {
  jest.useRealTimers();
  jest.clearAllMocks();
});

async function renderWorkspace(role: "owner" | "editor" | "viewer" = "owner") {
  jest.mocked(getProject).mockResolvedValue({ ok: true, data: project({ myRole: role }) });
  render(<WorkspacePage projectId={projectID} />);
  const label = role === "viewer" ? "main.tex, view only" : "Editing main.tex";
  return editorView(label);
}

async function editorView(label = "Editing main.tex") {
  const box = await screen.findByRole("textbox", { name: label });
  return EditorView.findFromDOM(box)!;
}

function typeInto(view: EditorView, text: string) {
  act(() => view.dispatch({ changes: { from: view.state.doc.length, insert: text } }));
}

const treeItem = (name: string) => within(screen.getByRole("tree", { name: "Project files" })).getByRole("treeitem", { name: new RegExp(`^${name}`) });
const status = () => screen.getByRole("status");

describe("WorkspacePage", () => {
  it("opens main.tex by default in the LaTeX editor, with the tree and the owner's actions", async () => {
    const view = await renderWorkspace();
    expect(view.state.doc.toString()).toBe(texts[main.id]);
    expect(mockGet).toHaveBeenCalledWith(projectID, main.id);
    expect(screen.getByRole("link", { name: "ResearchSphere home" }).getAttribute("href")).toBe("/home");
    expect(screen.getByRole("link", { name: /Back to project/ }).getAttribute("href")).toBe(`/projects/${projectID}`);
    expect(screen.getByText("Owner")).toBeTruthy();
    expect(status().textContent).toBe("Saved");

    const tree = screen.getByRole("tree", { name: "Project files" });
    expect(within(tree).getAllByRole("treeitem").map((i) => i.getAttribute("aria-level") + " " + i.textContent?.split(/Rename/)[0])).toEqual([
      "1 figures/",
      "1 sections/",
      "1 main.tex",
    ]);
    expect(treeItem("main.tex").getAttribute("aria-selected")).toBe("true");
    for (const name of ["New file", "New folder", "Upload figure", "Rename main.tex", "Move main.tex", "Delete main.tex"]) {
      expect(screen.getByRole("button", { name })).toBeTruthy();
    }
    // Compile, the PDF, the assistant and presence come with slices 22 and 23.
    expect(screen.queryByRole("button", { name: /Compile|Download PDF|Download ZIP/ })).toBeNull();
    expect(screen.queryByText(/editing$/)).toBeNull();
  });

  it("gives an editor the same actions", async () => {
    await renderWorkspace("editor");
    expect(screen.getByText("Editor")).toBeTruthy();
    expect(screen.getByRole("button", { name: "New file" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Delete main.tex" })).toBeTruthy();
  });

  it("shows a viewer the tree without actions and the editor read-only, View only", async () => {
    const view = await renderWorkspace("viewer");
    expect(screen.getByText("Viewer")).toBeTruthy();
    expect(screen.getByText("View only")).toBeTruthy();
    for (const name of ["New file", "New folder", "Upload figure", "Rename main.tex", "Delete main.tex"]) {
      expect(screen.queryByRole("button", { name })).toBeNull();
    }
    expect(screen.queryByTestId("figure-drop-zone")).toBeNull();
    expect(view.state.readOnly).toBe(true);
    expect(view.contentDOM.getAttribute("contenteditable")).toBe("false");
    expect(view.contentDOM.getAttribute("aria-readonly")).toBe("true");
    expect(screen.getByRole("button", { name: "Undo" })).toHaveProperty("disabled", true);
    // No save status for a reader, and F2 and Delete do nothing.
    expect(screen.queryByRole("status")).toBeNull();
    fireEvent.keyDown(treeItem("main.tex"), { key: "F2" });
    fireEvent.keyDown(treeItem("main.tex"), { key: "Delete" });
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("says when the project is not found or the caller is not a member", async () => {
    jest.mocked(getProject).mockResolvedValue({ ok: true, data: project({ myRole: null }) });
    render(<WorkspacePage projectId={projectID} />);
    expect(await screen.findByRole("heading", { name: "Workspace not found" })).toBeTruthy();
  });

  describe("tree keyboard", () => {
    it("moves with the arrows, opens and closes folders, and opens files with Enter", async () => {
      await renderWorkspace();
      const main = treeItem("main.tex");
      expect(main.tabIndex).toBe(0);
      fireEvent.keyDown(main, { key: "ArrowUp" });
      const sec = treeItem("sections/");
      expect(document.activeElement).toBe(sec);
      expect(sec.getAttribute("aria-expanded")).toBe("false");
      fireEvent.keyDown(sec, { key: "ArrowRight" });
      expect(sec.getAttribute("aria-expanded")).toBe("true");
      fireEvent.keyDown(sec, { key: "ArrowRight" });
      expect(document.activeElement).toBe(treeItem("intro.tex"));
      expect(treeItem("intro.tex").getAttribute("aria-level")).toBe("2");
      fireEvent.keyDown(document.activeElement!, { key: "ArrowLeft" });
      expect(document.activeElement).toBe(treeItem("sections/"));
      fireEvent.keyDown(document.activeElement!, { key: "ArrowLeft" });
      expect(treeItem("sections/").getAttribute("aria-expanded")).toBe("false");
      fireEvent.keyDown(document.activeElement!, { key: "Home" });
      expect(document.activeElement).toBe(treeItem("figures/"));
      fireEvent.keyDown(document.activeElement!, { key: "End" });
      expect(document.activeElement).toBe(treeItem("main.tex"));

      fireEvent.keyDown(treeItem("main.tex"), { key: "ArrowUp" });
      fireEvent.keyDown(document.activeElement!, { key: "Enter" });
      fireEvent.keyDown(document.activeElement!, { key: "ArrowDown" });
      fireEvent.keyDown(document.activeElement!, { key: "Enter" });
      await editorView("Editing intro.tex");
      expect(treeItem("intro.tex").getAttribute("aria-current")).toBe("page");
    });

    it("opens rename with F2, move with M and delete with Delete", async () => {
      await renderWorkspace();
      fireEvent.keyDown(treeItem("main.tex"), { key: "F2" });
      expect(screen.getByRole("dialog", { name: "Rename main.tex" })).toBeTruthy();
      fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
      fireEvent.keyDown(treeItem("main.tex"), { key: "m" });
      expect(screen.getByRole("dialog", { name: "Move main.tex" })).toBeTruthy();
      fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
      fireEvent.keyDown(treeItem("main.tex"), { key: "Delete" });
      expect(screen.getByRole("dialog", { name: "Delete main.tex?" })).toBeTruthy();
    });
  });

  describe("file actions", () => {
    it("creates a file in the selected folder, checking the name first, and opens it", async () => {
      await renderWorkspace();
      fireEvent.click(treeItem("sections/"));
      fireEvent.click(screen.getByRole("button", { name: "New file" }));
      const dialog = screen.getByRole("dialog", { name: "New file" });
      expect((within(dialog).getByRole("combobox", { name: "In folder" }) as HTMLSelectElement).value).toBe("sections");
      fireEvent.change(within(dialog).getByRole("textbox", { name: "File name" }), { target: { value: "my results.tex" } });
      fireEvent.click(within(dialog).getByRole("button", { name: "Create" }));
      expect(within(dialog).getByText(/can’t contain spaces/)).toBeTruthy();
      expect(createFile).not.toHaveBeenCalled();

      const created = textFile("t-res", "sections/results.tex");
      jest.mocked(createFile).mockResolvedValue({ ok: true, data: created });
      mockList.mockResolvedValue({ ok: true, data: tree([...items, created]) });
      texts[created.id] = "";
      items.push(created);
      fireEvent.change(within(dialog).getByRole("textbox", { name: "File name" }), { target: { value: "results.tex" } });
      await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Create" })));
      items.pop();
      expect(createFile).toHaveBeenCalledWith(projectID, { path: "sections/results.tex", kind: "text" });
      expect(screen.queryByRole("dialog")).toBeNull();
      await editorView("Editing results.tex");
      expect(treeItem("results.tex").getAttribute("aria-selected")).toBe("true");
    });

    it("creates a folder and shows the server's refusal under the name", async () => {
      await renderWorkspace();
      fireEvent.click(screen.getByRole("button", { name: "New folder" }));
      const dialog = screen.getByRole("dialog", { name: "New folder" });
      jest.mocked(createFile).mockResolvedValue({ ok: false, code: "path_taken", message: "taken" });
      fireEvent.change(within(dialog).getByRole("textbox", { name: "Folder name" }), { target: { value: "Figures" } });
      await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Create" })));
      expect(createFile).toHaveBeenCalledWith(projectID, { path: "Figures", kind: "folder" });
      expect(within(dialog).getByRole("textbox", { name: "Folder name" }).getAttribute("aria-invalid")).toBe("true");
      expect(within(dialog).getByText(/already there/)).toBeTruthy();
    });

    it("renames the selected file in its folder", async () => {
      await renderWorkspace();
      fireEvent.click(screen.getByRole("button", { name: "Rename main.tex" }));
      const dialog = screen.getByRole("dialog", { name: "Rename main.tex" });
      jest.mocked(moveFile).mockResolvedValue({ ok: true, data: { ...main, path: "paper.tex" } });
      mockList.mockResolvedValue({ ok: true, data: tree([...items.filter((f) => f !== main), { ...main, path: "paper.tex" }]) });
      fireEvent.change(within(dialog).getByRole("textbox", { name: "New name" }), { target: { value: "paper.tex" } });
      await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Rename" })));
      expect(moveFile).toHaveBeenCalledWith(projectID, main.id, "paper.tex");
      expect(treeItem("paper.tex")).toBeTruthy();
      // The open file follows its new name.
      await editorView("Editing paper.tex");
    });

    it("moves a file to another folder and never offers a folder inside itself", async () => {
      await renderWorkspace();
      fireEvent.click(screen.getByRole("button", { name: "Move main.tex" }));
      let dialog = screen.getByRole("dialog", { name: "Move main.tex" });
      const select = within(dialog).getByRole("combobox", { name: "Move to folder" });
      expect(within(select).getAllByRole("option").map((o) => o.textContent)).toEqual([
        "Top level of the project",
        "figures/",
        "sections/",
      ]);
      jest.mocked(moveFile).mockResolvedValue({ ok: true, data: { ...main, path: "sections/main.tex" } });
      fireEvent.change(select, { target: { value: "sections" } });
      await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Move" })));
      expect(moveFile).toHaveBeenCalledWith(projectID, main.id, "sections/main.tex");

      fireEvent.click(treeItem("sections/"));
      fireEvent.click(screen.getByRole("button", { name: "Move sections" }));
      dialog = screen.getByRole("dialog", { name: "Move sections" });
      const options = within(within(dialog).getByRole("combobox")).getAllByRole("option").map((o) => o.textContent);
      expect(options).toEqual(["Top level of the project", "figures/"]);
    });

    it("warns that a folder's contents go too, then deletes it", async () => {
      await renderWorkspace();
      fireEvent.click(treeItem("sections/"));
      fireEvent.click(screen.getByRole("button", { name: "Delete sections" }));
      const dialog = screen.getByRole("dialog", { name: "Delete folder sections?" });
      expect(within(dialog).getByText("The 2 files and folders inside it will be deleted too.")).toBeTruthy();
      jest.mocked(deleteFile).mockResolvedValue({ ok: false, code: "forbidden", message: "no" });
      await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Delete" })));
      expect(within(dialog).getByText("Only editors and the owner can change files.")).toBeTruthy();

      jest.mocked(deleteFile).mockResolvedValue({ ok: true, data: undefined });
      mockList.mockResolvedValue({ ok: true, data: tree([figures, plot, diagram, main]) });
      await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Delete" })));
      expect(deleteFile).toHaveBeenCalledWith(projectID, sections.id);
      expect(screen.queryByRole("dialog")).toBeNull();
      expect(screen.queryByRole("treeitem", { name: /^sections/ })).toBeNull();
    });

    it("closes the deleted open file", async () => {
      await renderWorkspace();
      fireEvent.click(screen.getByRole("button", { name: "Delete main.tex" }));
      jest.mocked(deleteFile).mockResolvedValue({ ok: true, data: undefined });
      mockList.mockResolvedValue({ ok: true, data: tree([figures, plot, diagram, sections, intro, method]) });
      await act(async () => fireEvent.click(screen.getByRole("button", { name: "Delete" })));
      expect(screen.getByText("Choose a file to open it.")).toBeTruthy();
      expect(screen.queryByRole("textbox", { name: /Editing/ })).toBeNull();
    });
  });

  describe("figure uploads", () => {
    const input = () => screen.getByLabelText("Choose figures to upload") as HTMLInputElement;
    function pick(...files: File[]) {
      fireEvent.change(input(), { target: { files } });
    }
    const messages = () => [...screen.getByRole("list", { name: "Uploads" }).querySelectorAll("li")].map((li) => li.textContent?.replace("×", ""));
    const uploaded = (path: string) => figure(`f-${path}`, path, { scanStatus: "pending" });

    it("refuses other types, empty files and files over 50 MB before sending, in words", async () => {
      await renderWorkspace();
      expect(input().accept).toBe(".png,.jpg,.jpeg,.pdf,image/png,image/jpeg,application/pdf");
      expect(screen.getByTestId("figure-drop-zone").textContent).toContain("max 50 MB each");
      const big = new File(["x"], "huge.png", { type: "image/png" });
      Object.defineProperty(big, "size", { value: 50 * 1024 * 1024 + 1 });
      const fits = new File(["x"], "fits.png", { type: "image/png" });
      Object.defineProperty(fits, "size", { value: 50 * 1024 * 1024 });
      jest.mocked(uploadFigure).mockReturnValue(new Promise(() => {}));
      pick(new File(["x"], "scan_raw.tiff", { type: "image/tiff" }), new File([], "empty.pdf"), big, fits);
      expect(messages()).toEqual([
        "scan_raw.tiff wasn’t uploaded: only PNG, JPG and PDF files can be figures.",
        "empty.pdf wasn’t uploaded: the file is empty.",
        "huge.png wasn’t uploaded: it’s over 50 MB, the largest a figure may be.",
        "Uploading fits.png",
      ]);
      expect(uploadFigure).toHaveBeenCalledTimes(1);
      expect(uploadFigure).toHaveBeenCalledWith(projectID, "fits.png", fits, expect.anything());
      fireEvent.click(screen.getByRole("button", { name: "Dismiss the message about scan_raw.tiff" }));
      expect(screen.getAllByRole("alert")).toHaveLength(2);
    });

    it("follows the server's figure limit", async () => {
      mockList.mockResolvedValue({ ok: true, data: tree(items, "main.tex", storage(0, 500 << 20, 10 << 20)) });
      await renderWorkspace();
      expect(screen.getByTestId("figure-drop-zone").textContent).toContain("max 10 MB each");
      const big = new File(["x"], "eleven.pdf");
      Object.defineProperty(big, "size", { value: 11 * 1024 * 1024 });
      pick(big);
      expect(screen.getByRole("alert").textContent).toContain("eleven.pdf wasn’t uploaded: it’s over 10 MB");
      expect(uploadFigure).not.toHaveBeenCalled();
    });

    it("shows the project's storage in the tree's footer, to viewers too", async () => {
      mockList.mockResolvedValue({ ok: true, data: tree(items, "main.tex", storage(120 << 20)) });
      await renderWorkspace("viewer");
      expect(screen.getByText("120 MB of 500 MB")).toBeTruthy();
      expect(screen.getByRole("navigation").textContent).toContain("Figure storage used: 120 MB of 500 MB");
    });

    it("updates the storage footer when the tree is read again", async () => {
      await renderWorkspace();
      expect(screen.getByText("0 MB of 500 MB")).toBeTruthy();
      const big = figure("f-new", "big.png", { scanStatus: "pending", size: 12.5 * 1024 * 1024 });
      jest.mocked(uploadFigure).mockResolvedValue({ ok: true, data: big });
      mockList.mockResolvedValue({ ok: true, data: tree([...items, big], "main.tex", storage(462.5 * 1024 * 1024)) });
      await act(async () => pick(new File(["png"], "big.png")));
      expect(screen.getByText("462.5 MB of 500 MB")).toBeTruthy();
    });

    it("makes the name safe, uploads into the selected folder with progress, and shows the final name", async () => {
      await renderWorkspace();
      fireEvent.click(treeItem("figures/"));
      let finish: (value: Awaited<ReturnType<typeof uploadFigure>>) => void = () => {};
      let progress: (share: number) => void = () => {};
      jest.mocked(uploadFigure).mockImplementation((_p, _path, _f, options) => {
        progress = options!.onProgress!;
        return new Promise((resolve) => (finish = resolve));
      });
      const file = new File(["jpeg"], "WhatsApp Image 2026-11-17 at 9.10.25 PM.jpeg", { type: "image/jpeg" });
      pick(file);
      const final = "whatsapp-image-2026-11-17-at-9.10.25-pm.jpeg";
      expect(uploadFigure).toHaveBeenCalledWith(projectID, `figures/${final}`, file, expect.anything());
      expect(messages()).toEqual([`Uploading ${file.name} as ${final}`]);
      act(() => progress(0.4));
      expect(screen.getByRole("progressbar", { name: `Uploading ${file.name}` }).getAttribute("aria-valuenow")).toBe("40");

      const stored = uploaded(`figures/${final}`);
      mockList.mockResolvedValue({ ok: true, data: tree([...items, stored]) });
      await act(async () => finish({ ok: true, data: stored }));
      expect(screen.queryByRole("progressbar")).toBeNull();
      expect(messages()).toEqual([
        `${file.name} was uploaded as ${final}. It’s being checked for malware before anyone can download it.`,
      ]);
      expect(treeItem(final).textContent).toContain("(checking)");
    });

    it("adds -2, -3 when the folder has the name, also within one batch, and never refuses for the name", async () => {
      await renderWorkspace();
      fireEvent.click(treeItem("figures/"));
      jest.mocked(uploadFigure).mockReturnValue(new Promise(() => {}));
      // figures/plot.png exists; two more files want the same name.
      pick(new File(["a"], "Plot.png", { type: "image/png" }), new File(["b"], "plot.PNG", { type: "image/png" }));
      expect(jest.mocked(uploadFigure).mock.calls.map((c) => c[1])).toEqual(["figures/plot-2.png", "figures/plot-3.png"]);
    });

    it("takes the next number when the server finds the name taken meanwhile", async () => {
      await renderWorkspace();
      jest
        .mocked(uploadFigure)
        .mockResolvedValueOnce({ ok: false, step: "start", code: "path_taken", message: "taken" })
        .mockResolvedValueOnce({ ok: true, data: uploaded("diagram-2.pdf") });
      await act(async () => pick(new File(["%PDF"], "Diagram.pdf", { type: "application/pdf" })));
      expect(jest.mocked(uploadFigure).mock.calls.map((c) => c[1])).toEqual(["diagram.pdf", "diagram-2.pdf"]);
      expect(messages()[0]).toContain("Diagram.pdf was uploaded as diagram-2.pdf.");
    });

    it.each([
      [{ step: "start", code: "internal_error", message: "x" }, "the server failed to start the upload (a server error)"],
      [{ step: "start", code: "network_error", message: "x" }, "the server couldn’t be reached to start the upload"],
      [
        { step: "start", code: "storage_limit_reached", message: "This project uses 480 MB of 500 MB, so a figure of 30 MB does not fit." },
        "this project uses 480 MB of 500 MB, so a figure of 30 MB does not fit.",
      ],
      [{ step: "start", code: "file_too_large", message: "A figure may be at most 50 MB." }, "a figure may be at most 50 MB."],
      [{ step: "start", code: "rate_limited", message: "x" }, "you have uploaded 30 figures this hour"],
      [{ step: "storage", status: 0, code: "network_error", message: "x" }, "the file couldn’t reach storage: the connection failed, or storage doesn’t accept uploads from this site"],
      [{ step: "storage", status: 403, code: "storage_failed", message: "x" }, "storage refused the upload: its permission expired or was denied"],
      [{ step: "storage", status: 400, code: "storage_failed", message: "x" }, "storage refused the file because its size didn’t match"],
      [{ step: "complete", code: "unsupported_type", message: "x" }, "it isn’t a PNG, JPG or PDF. The type is read from the file’s content"],
      [{ step: "complete", code: "type_mismatch", message: "x" }, "the file’s content doesn’t match its extension"],
      [{ step: "complete", code: "invalid_image", message: "x" }, "the image is damaged or incomplete"],
      [{ step: "complete", code: "upload_incomplete", message: "x" }, "the file didn’t reach storage. Upload it again."],
      [{ step: "complete", code: "not_found", message: "x" }, "the upload expired or was already finished"],
      [{ step: "complete", code: "internal_error", message: "x" }, "the server failed while checking the file"],
    ] as const)("says what went wrong: %j", async (failure, text) => {
      await renderWorkspace();
      jest.mocked(uploadFigure).mockResolvedValue({ ok: false, ...failure });
      await act(async () => {
        fireEvent.drop(screen.getByTestId("figure-drop-zone"), { dataTransfer: { files: [new File(["x"], "fig.png", { type: "image/png" })] } });
      });
      const alert = screen.getByRole("alert").textContent ?? "";
      expect(alert).toContain(`fig.png wasn’t uploaded: ${text}`);
      expect(alert).not.toMatch(/unexpected/i);
    });
  });

  describe("figures", () => {
    it("previews an image and links a PDF for download", async () => {
      await renderWorkspace();
      fireEvent.click(treeItem("figures/"));
      fireEvent.click(treeItem("plot.png"));
      const img = (await screen.findByRole("img", { name: "Preview of plot.png" })) as HTMLImageElement;
      expect(img.src).toBe(`https://storage.test/${plot.id}`);
      fireEvent.click(treeItem("diagram.pdf"));
      const link = await screen.findByRole("link", { name: "Download diagram.pdf" });
      expect(link.getAttribute("href")).toBe(`https://storage.test/${diagram.id}`);
    });

    it("says a figure is still being checked and checks again", async () => {
      await renderWorkspace();
      mockGet.mockResolvedValueOnce({ ok: false, code: "scan_pending", message: "pending" });
      fireEvent.click(treeItem("figures/"));
      fireEvent.click(treeItem("plot.png"));
      expect((await screen.findByRole("alert")).textContent).toContain("still being checked for malware");
      await act(async () => fireEvent.click(screen.getByRole("button", { name: "Check again" })));
      expect(screen.getByRole("img", { name: "Preview of plot.png" })).toBeTruthy();
    });
  });

  describe("autosave", () => {
    it("saves 1.5 s after the last change and says so", async () => {
      const view = await renderWorkspace();
      jest.useFakeTimers();
      typeInto(view, " world");
      expect(status().textContent).toBe("Saving…");
      await act(async () => jest.advanceTimersByTimeAsync(autosaveDelayMs - 100));
      expect(mockSave).not.toHaveBeenCalled();
      await act(async () => jest.advanceTimersByTimeAsync(100));
      expect(mockSave).toHaveBeenCalledWith(projectID, main.id, `${texts[main.id]} world`, 3, { keepalive: false });
      expect(status().textContent).toBe("Saved");
    });

    it("saves at once on Ctrl+S and Cmd+S without the browser's dialog", async () => {
      const view = await renderWorkspace();
      typeInto(view, "!");
      const notPrevented = fireEvent.keyDown(view.contentDOM, { key: "s", ctrlKey: true });
      expect(notPrevented).toBe(false);
      await act(async () => {});
      expect(mockSave).toHaveBeenCalledTimes(1);
      typeInto(view, "?");
      expect(fireEvent.keyDown(window, { key: "s", metaKey: true })).toBe(false);
      await act(async () => {});
      expect(mockSave).toHaveBeenCalledTimes(2);
      expect(mockSave).toHaveBeenLastCalledWith(projectID, main.id, `${texts[main.id]}!?`, 4, { keepalive: false });
    });

    it("saves when switching files and when the tab is hidden", async () => {
      const view = await renderWorkspace();
      typeInto(view, "1");
      fireEvent.click(treeItem("sections/"));
      fireEvent.click(treeItem("intro.tex"));
      expect(mockSave).toHaveBeenCalledWith(projectID, main.id, `${texts[main.id]}1`, 3, { keepalive: false });
      const introView = await editorView("Editing intro.tex");
      typeInto(introView, "2");
      Object.defineProperty(document, "visibilityState", { configurable: true, value: "hidden" });
      await act(async () => document.dispatchEvent(new Event("visibilitychange")));
      Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
      expect(mockSave).toHaveBeenLastCalledWith(projectID, intro.id, "Intro text2", 1, { keepalive: true });
    });

    it("keeps the text while offline, retries, and asks before the tab closes", async () => {
      const view = await renderWorkspace();
      jest.useFakeTimers();
      mockSave.mockResolvedValue({ ok: false, status: 0, code: "network_error", message: "offline" });
      typeInto(view, " kept");
      await act(async () => jest.advanceTimersByTimeAsync(autosaveDelayMs));
      expect(status().textContent).toBe("Offline, changes kept");
      const leave = new Event("beforeunload", { cancelable: true });
      window.dispatchEvent(leave);
      expect(leave.defaultPrevented).toBe(true);
      await act(async () => jest.advanceTimersByTimeAsync(1000));
      expect(mockSave).toHaveBeenCalledTimes(2);

      mockSave.mockResolvedValue(saved(4));
      await act(async () => window.dispatchEvent(new Event("online")));
      expect(mockSave).toHaveBeenCalledTimes(3);
      expect(mockSave).toHaveBeenLastCalledWith(projectID, main.id, `${texts[main.id]} kept`, 3, { keepalive: false });
      expect(status().textContent).toBe("Saved");
      const again = new Event("beforeunload", { cancelable: true });
      window.dispatchEvent(again);
      expect(again.defaultPrevented).toBe(false);
    });
  });

  describe("version conflict", () => {
    const conflict: SaveResult = {
      ok: false,
      status: 409,
      code: "version_conflict",
      message: "changed",
      current: { version: 9, content: "Their text", updatedAt: "2026-11-17T10:00:00Z" },
    };

    async function conflicted() {
      const view = await renderWorkspace();
      mockSave.mockResolvedValueOnce(conflict);
      typeInto(view, " mine");
      fireEvent.keyDown(window, { key: "s", ctrlKey: true });
      const dialog = await screen.findByRole("dialog", { name: "This file changed in another tab" });
      expect(status().textContent).toBe("Not saved: this file changed in another tab");
      return { view, dialog };
    }

    it("uses theirs: shows the saved text and saves nothing", async () => {
      const { view, dialog } = await conflicted();
      await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Use theirs" })));
      expect(view.state.doc.toString()).toBe("Their text");
      expect(screen.queryByRole("dialog")).toBeNull();
      expect(status().textContent).toBe("Saved");
      expect(mockSave).toHaveBeenCalledTimes(1);
    });

    it("keeps mine: saves this text over theirs with their version", async () => {
      const { view, dialog } = await conflicted();
      await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Keep mine (overwrite)" })));
      expect(mockSave).toHaveBeenLastCalledWith(projectID, main.id, `${texts[main.id]} mine`, 9, { keepalive: false });
      expect(view.state.doc.toString()).toBe(`${texts[main.id]} mine`);
      expect(status().textContent).toBe("Saved");
    });

    it("keeps the text when the dialog is closed and lets the user resolve later", async () => {
      const { view, dialog } = await conflicted();
      fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));
      expect(screen.queryByRole("dialog")).toBeNull();
      expect(view.state.doc.toString()).toBe(`${texts[main.id]} mine`);
      fireEvent.click(screen.getByRole("button", { name: "Resolve the conflict" }));
      expect(screen.getByRole("dialog", { name: "This file changed in another tab" })).toBeTruthy();
    });
  });

  it("puts the tree in a drawer on a narrow screen", async () => {
    await renderWorkspace();
    const files = screen.getByRole("button", { name: "Files" });
    const nav = screen.getByRole("navigation", { name: "Files" });
    expect(files.getAttribute("aria-controls")).toBe(nav.id);
    expect(nav.className).toContain("max-[640px]:hidden");
    fireEvent.click(files);
    expect(files.getAttribute("aria-expanded")).toBe("true");
    expect(nav.className).toContain("max-[640px]:fixed");
    fireEvent.keyDown(nav, { key: "Escape" });
    expect(nav.className).toContain("max-[640px]:hidden");
  });
});
