import "@/test/dialog-polyfill";
import "@/test/codemirror";

import { EditorView } from "@codemirror/view";
import { act, fireEvent, render, screen, within } from "@testing-library/react";
import * as Y from "yjs";

import type { RealtimeEvent } from "@/features/realtime/events";
import { saveBlob } from "@/lib/download";

import { getProject, getRecommendedVenues, listProjectMembers, updateProject } from "../api";
import {
  createCollabTicket,
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
import { project, projectID, recommendedVenue, venueNotice, venueRecs } from "../test-projects";
import { autosaveDelayMs } from "./autosave";
import {
  type Compile,
  downloadArchive,
  downloadPdf,
  fetchPdf,
  getCompile,
  getLatestCompile,
  startCompile,
} from "./compile/api";
import { timeoutText } from "./compile/compile-messages";
import { compile, succeeded } from "./compile/test-compiles";
import { FakeHub } from "./collab/fake-hub";
import type { LinkFactory } from "./collab/link";
import { CollabSession } from "./collab/session";
import { WorkspacePage } from "./workspace-page";

jest.mock("../api", () => ({
  ...jest.requireActual("../api"),
  getProject: jest.fn(),
  listProjectMembers: jest.fn(),
  updateProject: jest.fn(),
  getRecommendedVenues: jest.fn(),
}));
jest.mock("./compile/api", () => ({
  ...jest.requireActual("./compile/api"),
  startCompile: jest.fn(),
  getLatestCompile: jest.fn(),
  getCompile: jest.fn(),
  getCompileLog: jest.fn(),
  fetchPdf: jest.fn(),
  downloadPdf: jest.fn(),
  downloadArchive: jest.fn(),
}));
// pdf.js does not run in jsdom; the viewer's use of it has its own test (compile/pdf/load-pdf.test.ts).
jest.mock("./compile/pdf/load-pdf", () => ({
  openPdf: jest.fn(async () => ({ numPages: 6, loadingTask: { destroy: async () => {} } })),
  pageSizes: async () => Array.from({ length: 6 }, () => ({ width: 600, height: 800 })),
  renderPage: () => ({ canvas: document.createElement("canvas"), done: Promise.resolve(), cancel: () => {} }),
  renderText: () => ({ done: Promise.resolve(), cancel: () => {} }),
}));
jest.mock("@/lib/download", () => ({ saveBlob: jest.fn() }));
// The tab's one WebSocket, as the realtime hooks see it.
const mockRealtime = { events: new Set<(e: RealtimeEvent) => void>(), connected: new Set<() => void>() };
jest.mock("@/features/realtime/socket", () => ({
  onRealtimeEvent: (l: (e: RealtimeEvent) => void) => {
    mockRealtime.events.add(l);
    return () => mockRealtime.events.delete(l);
  },
  onConnected: (l: () => void) => {
    mockRealtime.connected.add(l);
    return () => mockRealtime.connected.delete(l);
  },
}));
// The sync server is the in-memory FakeHub (collab/fake-hub.ts).
const mockLink: { current: LinkFactory } = {
  current: () => {
    throw new Error("no hub");
  },
};
jest.mock("./collab/link", () => ({ hocuspocusLink: (o: Parameters<LinkFactory>[0]) => mockLink.current(o) }));
jest.mock("@/features/auth/session", () => ({ useSession: () => ({ status: "signedIn", user: { id: "u-me" } }) }));
jest.mock("../files/api", () => ({
  ...jest.requireActual("../files/api"),
  createCollabTicket: jest.fn(),
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

const me = { id: "u-me", name: "Me Myself" };
const bob = { id: "u-bob", name: "Bob Kahn" };
const cat = { id: "u-cat", name: "Cat Doe" };
let hub: FakeHub;
let ticketCount = 0;
const others: CollabSession[] = [];

// openAs opens a file as another member, in their own browser.
async function openAs(member: { id: string; name: string }, fileId = main.id) {
  const s = new CollabSession({
    projectId: projectID,
    fileId,
    link: hub.link(member),
    ticket: async () => ({
      ok: true,
      data: { ticket: `t-${member.id}-${++ticketCount}`, expiresAt: "", role: hub.roles.get(member.id) ?? "viewer", displayName: member.name, epoch: null },
    }),
    getFile: async () => ({ ok: false, code: "internal_error", message: "unused" }),
    save: async () => ({ ok: false, status: 500, code: "internal_error", message: "unused" }),
    refresh: async () => "ok",
    sessionLost: () => {},
  });
  others.push(s);
  await act(async () => {
    s.start();
    await Promise.resolve();
  });
  const b = s.getState().binding;
  if (b?.kind !== "yjs") throw new Error("not live");
  return { session: s, binding: b };
}

beforeEach(() => {
  hub = new FakeHub();
  for (const f of items) if (f.kind === "text") hub.seed(f.id, texts[f.id] ?? "");
  hub.roles.set(me.id, "owner").set(bob.id, "editor").set(cat.id, "editor");
  mockLink.current = hub.link(me);
  jest.mocked(createCollabTicket).mockImplementation(async () => ({
    ok: true,
    data: { ticket: `t-me-${++ticketCount}`, expiresAt: "", role: hub.roles.get(me.id) ?? "viewer", displayName: me.name, epoch: null },
  }));
  jest.mocked(listProjectMembers).mockResolvedValue({
    ok: true,
    data: {
      items: [me, bob, cat].map((m) => ({
        user: { userId: m.id, fullName: m.name, headline: null, photoUrl: m === bob ? "/api/v1/media/bob" : null },
        role: "editor" as const,
        joinedAt: "2026-11-01T00:00:00Z",
      })),
      page: 1,
      pageSize: 20,
      total: 3,
    },
  });
  jest.mocked(getProject).mockResolvedValue({ ok: true, data: project({ myRole: "owner" }) });
  mockList.mockResolvedValue({ ok: true, data: tree(items) });
  mockGet.mockImplementation(async (_p, id) => {
    const file = items.find((f) => f.id === id)!;
    return { ok: true, data: detail(file) };
  });
  mockSave.mockResolvedValue(saved(4));
  jest.mocked(getLatestCompile).mockResolvedValue({ ok: false, code: "no_compile", message: "never compiled" });
  jest.mocked(fetchPdf).mockResolvedValue({ ok: false, code: "no_pdf", message: "no PDF" });
});
afterEach(() => {
  for (const s of others.splice(0)) s.dispose();
  jest.useRealTimers();
  jest.clearAllMocks();
});

async function renderWorkspace(role: "owner" | "editor" | "viewer" = "owner", expectLabel?: string) {
  jest.mocked(getProject).mockResolvedValue({ ok: true, data: project({ myRole: role }) });
  hub.roles.set(me.id, role);
  render(<WorkspacePage projectId={projectID} />);
  const label = expectLabel ?? (role === "viewer" ? "main.tex, view only" : "Editing main.tex");
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
const status = () => screen.getByRole("banner").querySelector("[role=status]")!;

describe("WorkspacePage", () => {
  it("opens main.tex by default in the LaTeX editor, with the tree and the owner's actions", async () => {
    const view = await renderWorkspace();
    expect(view.state.doc.toString()).toBe(texts[main.id]);
    // The text comes from the sync server, with a ticket (FR-12.7), not from a REST read.
    expect(createCollabTicket).toHaveBeenCalledWith(projectID, main.id);
    expect(mockGet).not.toHaveBeenCalled();
    expect(screen.getByRole("link", { name: "ResearchSphere home" }).getAttribute("href")).toBe("/home");
    expect(screen.getByRole("link", { name: /Back to project/ }).getAttribute("href")).toBe(`/projects/${projectID}`);
    expect(screen.getByText("Owner")).toBeTruthy();
    expect(status().textContent).toBe("Live");

    const tree = screen.getByRole("tree", { name: "Project files" });
    expect(within(tree).getAllByRole("treeitem").map((i) => i.getAttribute("aria-level") + " " + i.textContent?.split(/Rename/)[0])).toEqual([
      "1 figures/",
      "1 sections/",
      "1 main.tex, Main, editing: Me Myself",
    ]);
    expect(treeItem("main.tex").getAttribute("aria-selected")).toBe("true");
    for (const name of ["New file", "New folder", "Upload figure", "Rename main.tex", "Move main.tex", "Delete main.tex"]) {
      expect(screen.getByRole("button", { name })).toBeTruthy();
    }
    // Compile and the downloads (slice 23).
    expect(screen.getByRole("button", { name: "Compile" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Download PDF" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Download all files (ZIP)" })).toBeTruthy();
    expect(screen.getByRole("separator", { name: "Resize the editor and the PDF" })).toBeTruthy();
    const presence = screen.getByRole("group", { name: "1 person editing this file" });
    expect(presence.textContent).toContain("1 / 4 editing");
    expect(within(presence).getByRole("img", { name: "Me Myself (you)" })).toBeTruthy();
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
    // No save status for a reader, nobody listed as editing, and F2 and Delete do nothing.
    expect(screen.getByRole("banner").querySelector("[role=status]")).toBeNull();
    expect(screen.queryByRole("group", { name: /editing this file/ })).toBeNull();
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
      hub.seed(created.id, "");
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

  // While the sync server cannot be reached, the 21.2 REST autosave keeps working.
  describe("autosave while live editing is unavailable", () => {
    beforeEach(() => {
      hub.refuseAll = "unavailable";
    });

    it("says live editing is unavailable and saves normally", async () => {
      await renderWorkspace();
      expect(status().textContent).toBe("Live editing unavailable, saving normally· Saved");
      expect(mockGet).toHaveBeenCalledWith(projectID, main.id);
    });

    it("saves 1.5 s after the last change and says so", async () => {
      const view = await renderWorkspace();
      jest.useFakeTimers();
      typeInto(view, " world");
      expect(status().textContent).toContain("Saving…");
      await act(async () => jest.advanceTimersByTimeAsync(autosaveDelayMs - 100));
      expect(mockSave).not.toHaveBeenCalled();
      await act(async () => jest.advanceTimersByTimeAsync(100));
      expect(mockSave).toHaveBeenCalledWith(projectID, main.id, `${texts[main.id]} world`, 3, { keepalive: false });
      expect(status().textContent).toContain("Saved");
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
      expect(status().textContent).toContain("Offline, changes kept");
      const leave = new Event("beforeunload", { cancelable: true });
      window.dispatchEvent(leave);
      expect(leave.defaultPrevented).toBe(true);
      await act(async () => jest.advanceTimersByTimeAsync(1000));
      expect(mockSave).toHaveBeenCalledTimes(2);

      mockSave.mockResolvedValue(saved(4));
      await act(async () => window.dispatchEvent(new Event("online")));
      expect(mockSave).toHaveBeenCalledTimes(3);
      expect(mockSave).toHaveBeenLastCalledWith(projectID, main.id, `${texts[main.id]} kept`, 3, { keepalive: false });
      expect(status().textContent).toContain("Saved");
      const again = new Event("beforeunload", { cancelable: true });
      window.dispatchEvent(again);
      expect(again.defaultPrevented).toBe(false);
    });
  });

  describe("version conflict while live editing is unavailable", () => {
    beforeEach(() => {
      hub.refuseAll = "unavailable";
    });
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
      expect(status().textContent).toContain("Not saved: this file changed in another tab");
      return { view, dialog };
    }

    it("uses theirs: shows the saved text and saves nothing", async () => {
      const { view, dialog } = await conflicted();
      await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Use theirs" })));
      expect(view.state.doc.toString()).toBe("Their text");
      expect(screen.queryByRole("dialog")).toBeNull();
      expect(status().textContent).toContain("Saved");
      expect(mockSave).toHaveBeenCalledTimes(1);
    });

    it("keeps mine: saves this text over theirs with their version", async () => {
      const { view, dialog } = await conflicted();
      await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Keep mine (overwrite)" })));
      expect(mockSave).toHaveBeenLastCalledWith(projectID, main.id, `${texts[main.id]} mine`, 9, { keepalive: false });
      expect(view.state.doc.toString()).toBe(`${texts[main.id]} mine`);
      expect(status().textContent).toContain("Saved");
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

  describe("live editing", () => {
    it("shows another member's text and cursor with their name in their colour (FR-12.4, FR-12.5)", async () => {
      const view = await renderWorkspace();
      const bobs = await openAs(bob);
      act(() => bobs.binding.text.insert(0, "% Bob\n"));
      expect(view.state.doc.toString()).toBe(`% Bob\n${texts[main.id]}`);

      typeInto(view, " and me");
      expect(bobs.binding.text.toString()).toBe(`% Bob\n${texts[main.id]} and me`);
      expect(hub.text(main.id)).toBe(`% Bob\n${texts[main.id]} and me`);

      // Bob's cursor after "% Bob"; the server named and coloured him.
      await act(async () => {
        bobs.binding.awareness.setLocalStateField("cursor", {
          anchor: Y.createRelativePositionFromTypeIndex(bobs.binding.text, 5),
          head: Y.createRelativePositionFromTypeIndex(bobs.binding.text, 5),
        });
        await Promise.resolve();
      });
      const label = view.dom.querySelector(".cm-rs-caret-label") as HTMLElement;
      expect(label.textContent).toBe("Bob Kahn");
      expect(label.style.backgroundColor).toBe("rgb(31, 78, 140)");
    });

    it("lists who edits the file in the bar and who is in which file in the tree", async () => {
      await renderWorkspace();
      await openAs(bob);
      await openAs(cat, intro.id);
      const presence = screen.getByRole("group", { name: "2 people editing this file" });
      expect(presence.textContent).toContain("2 / 4 editing");
      expect(within(presence).getByRole("img", { name: "Bob Kahn" }).getAttribute("src")).toBe("/api/v1/media/bob");
      expect(within(presence).getByRole("img", { name: "Me Myself (you)" })).toBeTruthy();
      // Under 640px only the count shows.
      expect(presence.querySelector("ul")!.className).toContain("max-[640px]:hidden");

      fireEvent.click(treeItem("sections/"));
      expect(treeItem("intro.tex").textContent).toContain("editing: Cat Doe");
      expect(treeItem("main.tex").textContent).toContain("editing: Me Myself, Bob Kahn");
    });

    it("undoes only my own edits", async () => {
      const view = await renderWorkspace();
      const bobs = await openAs(bob);
      typeInto(view, "!");
      act(() => bobs.binding.text.insert(0, ">"));
      fireEvent.click(screen.getByRole("button", { name: "Undo" }));
      expect(view.state.doc.toString()).toBe(`>${texts[main.id]}`);
      expect(hub.text(main.id)).toBe(`>${texts[main.id]}`);
    });

    it("opens the file read-only for a member over the limit, says why, and lets them edit when one leaves (FR-12.8)", async () => {
      hub.maxEditors = 1;
      const bobs = await openAs(bob);
      await renderWorkspace("editor", "main.tex, read only");
      expect(
        screen.getByText(/This file already has 1 people editing\. You can read it and will be able to edit when someone leaves\./),
      ).toBeTruthy();
      expect(screen.getByRole("button", { name: "Undo" })).toHaveProperty("disabled", true);

      act(() => bobs.session.release());
      const view = await editorView("Editing main.tex");
      expect(view.state.readOnly).toBe(false);
      expect(screen.queryByText(/already has 1 people editing/)).toBeNull();
    });

    it("turns read-only at once when made viewer, and closes the file when removed (FR-12.7)", async () => {
      await renderWorkspace("editor");
      act(() => hub.setRole(me.id, "viewer"));
      const view = await editorView("main.tex, view only");
      expect(view.state.readOnly).toBe(true);
      expect(screen.getByText("View only")).toBeTruthy();
      expect(screen.getByText("Viewer")).toBeTruthy();
      expect(screen.queryByRole("button", { name: "New file" })).toBeNull();

      act(() => hub.setRole(me.id, null));
      expect(screen.getByRole("alert").textContent).toContain("You’re no longer a member of this project");
      expect(screen.queryByRole("textbox", { name: /main\.tex/ })).toBeNull();
    });

    it("keeps typing offline, says so, and merges on reconnect (FR-12.9)", async () => {
      const view = await renderWorkspace();
      const bobs = await openAs(bob);
      act(() => hub.offline());
      expect(status().textContent).toBe("Offline, changes will sync");
      typeInto(view, " offline");
      act(() => bobs.binding.text.insert(0, "B"));
      const leave = new Event("beforeunload", { cancelable: true });
      window.dispatchEvent(leave);
      expect(leave.defaultPrevented).toBe(true);

      await act(async () => {
        hub.reconnectAll();
        await new Promise((r) => setTimeout(r, 0));
      });
      expect(status().textContent).toBe("Live");
      expect(view.state.doc.toString()).toBe(`B${texts[main.id]} offline`);
      expect(hub.text(main.id)).toBe(`B${texts[main.id]} offline`);
      const again = new Event("beforeunload", { cancelable: true });
      window.dispatchEvent(again);
      expect(again.defaultPrevented).toBe(false);
    });

    it("says Saved once the server stored the edits", async () => {
      const view = await renderWorkspace();
      typeInto(view, "!");
      act(() => hub.stored(main.id, 4));
      expect(status().textContent).toBe("Saved");
    });

    it("asks which text to keep when offline edits meet a new history, and keeps mine as an undoable edit", async () => {
      const view = await renderWorkspace();
      act(() => hub.offline());
      typeInto(view, " mine");
      hub.rebuild(main.id, "Rewritten over REST");
      await act(async () => {
        hub.reconnectAll();
        await new Promise((r) => setTimeout(r, 0));
      });
      const dialog = await screen.findByRole("dialog", { name: "Your changes couldn’t be merged" });
      // The modal dialog hides the page from assistive technology; the editor shows theirs meanwhile.
      const behind = EditorView.findFromDOM(screen.getByRole("textbox", { name: "main.tex, read only", hidden: true }))!;
      expect(behind.state.doc.toString()).toBe("Rewritten over REST");
      await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Keep mine (overwrite)" })));
      const after = await editorView("Editing main.tex");
      expect(after.state.doc.toString()).toBe(`${texts[main.id]} mine`);
      expect(hub.text(main.id)).toBe(`${texts[main.id]} mine`);
      fireEvent.click(screen.getByRole("button", { name: "Undo" }));
      expect(hub.text(main.id)).toBe("Rewritten over REST");
      expect(view).not.toBe(after);
    });
  });

  describe("when live editing is refused", () => {
    it.each(["origin_not_allowed", "unavailable", "too_many_connections"])(
      "still shows the editor with REST autosave and the notice (%s)",
      async (reason) => {
        hub.refuseAll = reason;
        const view = await renderWorkspace();
        const note = screen.getByRole("note");
        expect(note.textContent).toContain("Live editing unavailable, saving normally.");
        expect(note.textContent).not.toContain("dev:");
        expect(screen.queryByText(/isn’t available from this address/)).toBeNull();
        expect(view.state.doc.toString()).toBe(texts[main.id]);
        typeInto(view, "!");
        fireEvent.keyDown(window, { key: "s", ctrlKey: true });
        await act(async () => {});
        expect(mockSave).toHaveBeenCalledWith(projectID, main.id, `${texts[main.id]}!`, 3, { keepalive: false });
      },
    );

    it("adds the reason in development", async () => {
      jest.replaceProperty(process.env, "NODE_ENV", "development");
      hub.refuseAll = "origin_not_allowed";
      await renderWorkspace();
      expect(screen.getByRole("note").textContent).toContain(
        "(dev: the sync server refused this page’s origin; add it to SYNC_ALLOWED_ORIGINS)",
      );
    });

    it("shows a viewer the file read-only with the notice when the ticket is refused", async () => {
      jest.mocked(createCollabTicket).mockResolvedValue({ ok: false, code: "rate_limited", message: "Slow down." });
      const view = await renderWorkspace("viewer");
      expect(view.state.readOnly).toBe(true);
      expect(view.state.doc.toString()).toBe(texts[main.id]);
      expect(screen.getByRole("note").textContent).toContain("Live editing unavailable, saving normally. You can read the file.");
    });

    it("says it is waiting, never an empty area, when nothing answers", async () => {
      hub.unreachable = true;
      mockGet.mockResolvedValue({ ok: false, code: "network_error", message: "offline" });
      jest.mocked(getProject).mockResolvedValue({ ok: true, data: project({ myRole: "owner" }) });
      render(<WorkspacePage projectId={projectID} />);
      expect(
        (await screen.findByText(/We can’t reach the server right now\. main\.tex opens as soon as the connection returns\./)).textContent,
      ).toBeTruthy();
    });
  });

  describe("compile (slice 23)", () => {
    const emit = (c: Compile) =>
      act(() => mockRealtime.events.forEach((l) => l({ type: "compile.updated", data: { projectId: projectID, compile: c } })));
    const compileStatus = () => screen.getByTestId("compile-status").textContent;
    const pdfBytes = (id: string) => ({ ok: true as const, data: { data: new ArrayBuffer(8), compileId: id } });

    afterEach(() => {
      mockRealtime.events.clear();
      mockRealtime.connected.clear();
    });

    it("compiles, shows Queued, Compiling with the seconds, then Done, and swaps in the new PDF (FR-13.1)", async () => {
      jest.mocked(getLatestCompile).mockResolvedValue({ ok: true, data: succeeded("c1") });
      jest.mocked(fetchPdf).mockResolvedValue(pdfBytes("c1"));
      await renderWorkspace();
      const canvas = await screen.findByTestId("pdf-document");
      expect(canvas.getAttribute("data-compile")).toBe("c1");
      expect(screen.getByTestId("pdf-pages").textContent).toBe("PDF · page 1 of 6");
      expect(compileStatus()).toBe("Compile: Done");

      jest.mocked(startCompile).mockResolvedValue({ ok: true, data: compile("c2") });
      await act(async () => fireEvent.click(screen.getByRole("button", { name: "Compile" })));
      expect(startCompile).toHaveBeenCalledWith(projectID);
      expect(compileStatus()).toBe("Compile: Queued");

      // The clock on a whole second, so the elapsed time is exact.
      const now = Math.floor(Date.now() / 1000) * 1000;
      const clock = jest.spyOn(Date, "now").mockReturnValue(now);
      emit(compile("c2", { status: "running", startedAt: new Date(now - 12_000).toISOString() }));
      expect(compileStatus()).toBe("Compile: Compiling");
      expect(screen.getByTestId("compile-elapsed").textContent).toBe("· 12 s");
      clock.mockRestore();
      // The old PDF stays while the new compile runs.
      expect(screen.getByTestId("pdf-document").getAttribute("data-compile")).toBe("c1");
      // A late event of an earlier state changes nothing.
      emit(compile("c2"));
      expect(compileStatus()).toBe("Compile: Compiling");

      jest.mocked(fetchPdf).mockResolvedValue(pdfBytes("c2"));
      await act(async () => emit(succeeded("c2")));
      expect(compileStatus()).toBe("Compile: Done");
      expect(screen.getByTestId("compile-elapsed").textContent).toBe("· 7.8 s");
      expect(screen.getByTestId("pdf-document").getAttribute("data-compile")).toBe("c2");
    });

    it("sends this tab's edits to the sync server before it compiles", async () => {
      const view = await renderWorkspace();
      let compiledText: string | null = null;
      jest.mocked(startCompile).mockImplementation(async () => {
        compiledText = hub.text(main.id);
        return { ok: true, data: compile("c2") };
      });
      act(() => hub.offline());
      typeInto(view, " world");
      fireEvent.click(screen.getByRole("button", { name: "Compile" }));
      await act(async () => {
        await new Promise((r) => setTimeout(r, 20));
      });
      expect(startCompile).not.toHaveBeenCalled();
      await act(async () => {
        hub.reconnectAll();
        await new Promise((r) => setTimeout(r, 20));
      });
      expect(startCompile).toHaveBeenCalledTimes(1);
      expect(compiledText).toBe(`${texts[main.id]} world`);
    });

    it("follows the compile already running and says who started it (FR-13.4)", async () => {
      await renderWorkspace();
      jest.mocked(startCompile).mockResolvedValue({ ok: false, code: "compile_running", message: "busy", compileId: "c9" });
      jest.mocked(getCompile).mockResolvedValue({ ok: true, data: compile("c9", { status: "running", requestedBy: bob.id, startedAt: new Date().toISOString() }) });
      await act(async () => fireEvent.click(screen.getByRole("button", { name: "Compile" })));
      expect(getCompile).toHaveBeenCalledWith(projectID, "c9");
      expect(screen.getByText("A compile is already running (started by Bob Kahn).")).toBeTruthy();
      expect(compileStatus()).toBe("Compile: Compiling");
      // Asking again during it does not post again.
      await act(async () => fireEvent.click(screen.getByRole("button", { name: "Compile" })));
      expect(startCompile).toHaveBeenCalledTimes(1);
      // The notice goes when that compile ends.
      await act(async () => emit(succeeded("c9", { requestedBy: bob.id })));
      expect(screen.queryByText(/already running/)).toBeNull();
    });

    it("says a compile timed out (FR-13.5)", async () => {
      await renderWorkspace();
      emit(compile("c3", { status: "timed_out", startedAt: "2026-11-23T10:00:01Z", finishedAt: "2026-11-23T10:01:01Z", durationMs: 60_000 }));
      expect(compileStatus()).toBe("Compile: Timed out");
      expect(screen.getByText(timeoutText)).toBeTruthy();
    });

    it("says a failed compile's first error", async () => {
      await renderWorkspace();
      emit(
        compile("c4", {
          status: "failed",
          failure: "no_pdf",
          errors: [
            { file: "main.tex", line: 2, message: "Undefined control sequence.", severity: "error" },
            { file: null, line: null, message: "Emergency stop.", severity: "error" },
          ],
        }),
      );
      expect(screen.getByText("main.tex:2: Undefined control sequence.")).toBeTruthy();
    });

    it("says the compile service is unavailable and retries", async () => {
      await renderWorkspace();
      jest.mocked(startCompile).mockResolvedValue({ ok: false, code: "compile_unavailable", message: "x" });
      await act(async () => fireEvent.click(screen.getByRole("button", { name: "Compile" })));
      expect(screen.getByRole("alert").textContent).toContain("Compile service unavailable.");
      jest.mocked(startCompile).mockResolvedValue({ ok: true, data: compile("c5") });
      await act(async () => fireEvent.click(screen.getByRole("button", { name: "Retry" })));
      expect(startCompile).toHaveBeenCalledTimes(2);
      expect(screen.queryByText("Compile service unavailable.")).toBeNull();
      expect(compileStatus()).toBe("Compile: Queued");
    });

    it("compiles with Ctrl+Enter or Cmd+Enter", async () => {
      await renderWorkspace();
      jest.mocked(startCompile).mockResolvedValue({ ok: true, data: compile("c6") });
      await act(async () => fireEvent.keyDown(window, { key: "Enter", ctrlKey: true }));
      expect(startCompile).toHaveBeenCalledTimes(1);
      expect(compileStatus()).toBe("Compile: Queued");
    });

    it("gives a viewer no Compile button nor shortcut, but the status, the PDF and the downloads", async () => {
      jest.mocked(getLatestCompile).mockResolvedValue({ ok: true, data: succeeded("c1") });
      jest.mocked(fetchPdf).mockResolvedValue(pdfBytes("c1"));
      await renderWorkspace("viewer");
      expect(await screen.findByTestId("pdf-document")).toBeTruthy();
      expect(screen.queryByRole("button", { name: "Compile" })).toBeNull();
      expect(screen.queryByRole("combobox", { name: "Main file" })).toBeNull();
      await act(async () => fireEvent.keyDown(window, { key: "Enter", metaKey: true }));
      expect(startCompile).not.toHaveBeenCalled();
      expect(compileStatus()).toBe("Compile: Done");
      expect(screen.getAllByRole("button", { name: "Download PDF" }).length).toBeGreaterThan(0);
      expect(screen.getByRole("button", { name: "Download all files (ZIP)" })).toBeTruthy();
    });

    it("opens an error's file at its line, and says when the file is gone (FR-13.2, FR-13.3)", async () => {
      jest.mocked(getLatestCompile).mockResolvedValue({
        ok: true,
        data: compile("c7", {
          status: "failed",
          errors: [
            { file: "main.tex", line: 2, message: "Undefined control sequence.", severity: "error" },
            { file: "gone.tex", line: 4, message: "Missing $ inserted.", severity: "error" },
            { file: "sections/intro.tex", line: 1, message: "Overfull \\hbox.", severity: "warning" },
          ],
        }),
      });
      await renderWorkspace();
      const panel = screen.getByRole("region", { name: /Compile errors/ });
      await within(panel).findByRole("button", { name: /main\.tex:2/ });
      expect(within(panel).getByLabelText("2 errors")).toBeTruthy();
      expect(within(panel).getByText("1 warning")).toBeTruthy();

      // Open another file, then jump back to main.tex line 2.
      fireEvent.click(treeItem("sections/"));
      fireEvent.click(treeItem("intro.tex"));
      await editorView("Editing intro.tex");
      await act(async () => fireEvent.click(within(panel).getByRole("button", { name: /main\.tex:2/ })));
      const view = await editorView("Editing main.tex");
      await act(async () => {});
      expect(view.state.selection.main.head).toBe(view.state.doc.line(2).from);

      fireEvent.click(within(panel).getByRole("button", { name: /gone\.tex:4/ }));
      expect(within(panel).getByRole("alert").textContent).toBe("gone.tex no longer exists in this project.");

      // The arrow keys move through the list.
      const first = within(panel).getByRole("button", { name: /main\.tex:2/ });
      first.focus();
      fireEvent.keyDown(first, { key: "ArrowDown" });
      expect(document.activeElement).toBe(within(panel).getByRole("button", { name: /gone\.tex:4/ }));
    });

    it("downloads the PDF and the ZIP through the API, and says why not (FR-13.6, FR-13.7)", async () => {
      await renderWorkspace();
      const pdf = new Blob(["%PDF-"]);
      const zip = new Blob(["PK"]);
      jest.mocked(downloadPdf).mockResolvedValue({ ok: true, data: { blob: pdf, name: "Explainable CVS detection.pdf" } });
      jest.mocked(downloadArchive).mockResolvedValue({ ok: true, data: { blob: zip, name: "Explainable CVS detection.zip" } });
      await act(async () => fireEvent.click(screen.getByRole("button", { name: "Download PDF" })));
      expect(downloadPdf).toHaveBeenCalledWith(projectID);
      expect(saveBlob).toHaveBeenCalledWith(pdf, "Explainable CVS detection.pdf");
      await act(async () => fireEvent.click(screen.getByRole("button", { name: "Download all files (ZIP)" })));
      expect(saveBlob).toHaveBeenCalledWith(zip, "Explainable CVS detection.zip");

      jest.mocked(downloadArchive).mockResolvedValue({ ok: false, code: "archive_too_large", message: "x" });
      await act(async () => fireEvent.click(screen.getByRole("button", { name: "Download all files (ZIP)" })));
      expect(screen.getByRole("alert").textContent).toContain("too large to download as one ZIP");
      expect(saveBlob).toHaveBeenCalledTimes(2);
    });

    it("lets the owner choose the main file", async () => {
      await renderWorkspace();
      jest.mocked(updateProject).mockResolvedValue({ ok: true, data: project({ myRole: "owner" }) });
      const select = screen.getByRole("combobox", { name: "Main file" });
      expect(within(select).getAllByRole("option").map((o) => o.textContent)).toEqual(["main.tex", "sections/intro.tex", "sections/method.tex"]);
      await act(async () => fireEvent.change(select, { target: { value: "sections/intro.tex" } }));
      expect(updateProject).toHaveBeenCalledWith(projectID, { mainFilePath: "sections/intro.tex" });
      expect((select as HTMLSelectElement).value).toBe("sections/intro.tex");
    });

    describe("below 1024 pixels", () => {
      const original = window.matchMedia;
      beforeEach(() => {
        window.matchMedia = jest.fn(() => ({ matches: false, addEventListener: jest.fn(), removeEventListener: jest.fn() })) as unknown as typeof window.matchMedia;
      });
      afterEach(() => {
        window.matchMedia = original;
      });

      it("switches between the editor and the PDF with tabs, and More holds the downloads", async () => {
        jest.mocked(fetchPdf).mockResolvedValue(pdfBytes("c1"));
        await renderWorkspace();
        const editorTab = screen.getByRole("tab", { name: "Editor" });
        const pdfTab = screen.getByRole("tab", { name: "PDF" });
        expect(editorTab.getAttribute("aria-selected")).toBe("true");
        expect(screen.getByRole("tabpanel", { name: "Editor" })).toBeTruthy();
        expect(screen.queryByRole("tabpanel", { name: "PDF" })).toBeNull();
        expect(screen.queryByRole("separator")).toBeNull();

        fireEvent.click(pdfTab);
        expect(pdfTab.getAttribute("aria-selected")).toBe("true");
        expect(within(screen.getByRole("tabpanel", { name: "PDF" })).getByTestId("pdf-document")).toBeTruthy();
        fireEvent.keyDown(pdfTab, { key: "ArrowLeft" });
        expect(editorTab.getAttribute("aria-selected")).toBe("true");
        expect(document.activeElement).toBe(editorTab);

        jest.mocked(downloadArchive).mockResolvedValue({ ok: true, data: { blob: new Blob(["PK"]), name: "p.zip" } });
        fireEvent.click(screen.getByRole("button", { name: "More" }));
        const menu = screen.getByRole("menu", { name: "Project" });
        expect(document.activeElement).toBe(within(menu).getByRole("menuitem", { name: "Download PDF" }));
        fireEvent.keyDown(menu, { key: "ArrowDown" });
        await act(async () => fireEvent.click(document.activeElement!));
        expect(downloadArchive).toHaveBeenCalledWith(projectID);
        expect(screen.queryByRole("menu")).toBeNull();
      });
    });
  });

  describe("venues (AI-6)", () => {
    it("opens the Where to submit card in a side sheet from the bar, a bottom sheet under 640px", async () => {
      jest.mocked(getRecommendedVenues).mockResolvedValue(venueRecs("ready", [recommendedVenue(1)]));
      await renderWorkspace("viewer");
      expect(getRecommendedVenues).not.toHaveBeenCalled();
      const button = within(screen.getByRole("banner")).getByRole("button", { name: "Venues" });
      fireEvent.click(button);
      const sheet = screen.getByRole("dialog", { name: "Venues" });
      expect(sheet.className).toContain("ml-auto");
      expect(sheet.className).toContain("h-dvh");
      expect(sheet.className).toContain("max-[640px]:mt-auto");
      const card = await within(sheet).findByRole("region", { name: "Where to submit", busy: false });
      expect(within(card).getByRole("link", { name: /Conference on Digital Health 1/ }).getAttribute("href")).toBe(
        `/events/${recommendedVenue(1).id}`,
      );
      expect(within(card).getByText(venueNotice)).toBeTruthy();
      expect(getRecommendedVenues).toHaveBeenCalledWith(projectID);
      fireEvent.click(within(sheet).getByRole("button", { name: "Close" }));
      expect(screen.queryByRole("dialog", { name: "Venues" })).toBeNull();
      expect(document.activeElement).toBe(button);
    });

    it("lets an editor add the abstract from the sheet, then reads the venues again", async () => {
      jest.mocked(getProject).mockResolvedValue({ ok: true, data: project({ myRole: "editor", abstract: null }) });
      jest
        .mocked(getRecommendedVenues)
        .mockResolvedValueOnce(venueRecs("needs_abstract"))
        .mockResolvedValueOnce(venueRecs("processing"));
      hub.roles.set(me.id, "editor");
      render(<WorkspacePage projectId={projectID} />);
      await editorView();
      fireEvent.click(within(screen.getByRole("banner")).getByRole("button", { name: "Venues" }));
      const sheet = screen.getByRole("dialog", { name: "Venues" });
      fireEvent.click(await within(sheet).findByRole("button", { name: "Add abstract" }));
      const dialog = screen.getByRole("dialog", { name: "Add abstract" });
      fireEvent.change(within(dialog).getByLabelText(/^Paper abstract/), { target: { value: "We study screen use." } });
      jest.mocked(updateProject).mockResolvedValue({ ok: true, data: project({ myRole: "editor", abstract: "We study screen use." }) });
      await act(async () => {
        fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
      });
      expect(updateProject).toHaveBeenCalledWith(projectID, { abstract: "We study screen use." });
      expect(screen.queryByRole("dialog", { name: "Add abstract" })).toBeNull();
      expect(await within(sheet).findByText("Finding venues for your abstract…")).toBeTruthy();
      expect(getRecommendedVenues).toHaveBeenCalledTimes(2);
    });

    it("shows a viewer the request for an abstract without the button", async () => {
      jest.mocked(getProject).mockResolvedValue({ ok: true, data: project({ myRole: "viewer", abstract: null }) });
      jest.mocked(getRecommendedVenues).mockResolvedValue(venueRecs("needs_abstract"));
      hub.roles.set(me.id, "viewer");
      render(<WorkspacePage projectId={projectID} />);
      await editorView("main.tex, view only");
      fireEvent.click(within(screen.getByRole("banner")).getByRole("button", { name: "Venues" }));
      const sheet = screen.getByRole("dialog", { name: "Venues" });
      expect(await within(sheet).findByRole("heading", { name: "Add an abstract to see venues" })).toBeTruthy();
      expect(within(sheet).queryByRole("button", { name: "Add abstract" })).toBeNull();
      expect(within(sheet).getByText(venueNotice)).toBeTruthy();
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
