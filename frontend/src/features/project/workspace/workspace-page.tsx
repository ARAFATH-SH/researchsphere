"use client";

import Link from "next/link";
import { type ReactNode, useCallback, useEffect, useId, useRef, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { useSession } from "@/features/auth/session";
import { DevApiStatus } from "@/features/health/dev-api-status";
import { WarningBanner } from "@/features/moderation/warning-banner";

import { getProject, listProjectMembers, type Project, updateProject } from "../api";
import { createFile, deleteFile, getFile, listFiles, moveFile, type ProjectFile, type ProjectStorage } from "../files/api";
import { baseName, countInside, folderAncestors, isInside, parentPath } from "../files/tree";
import { CreateDialog, DeleteFileDialog, MoveDialog, RenameDialog } from "./file-dialogs";
import { type Opened, FilePane } from "./file-pane";
import { FileTree, treeItemId } from "./file-tree";
import { collabStatus } from "./collab/types";
import { useCollab, useCollabState } from "./collab/use-collab";
import { CompileMessages } from "./compile/compile-messages";
import { CompileButton, CompileStatus } from "./compile/compile-status";
import { DownloadButtons, DownloadError, useDownloads } from "./compile/downloads";
import { ErrorsPanel, type JumpTarget } from "./compile/errors-panel";
import { PdfPanel } from "./compile/pdf-panel";
import { Split } from "./compile/split";
import { useCompile } from "./compile/use-compile";
import { useWide } from "./compile/use-wide";
import type { EditorHandle } from "./editor/latex-editor";
import { DropZone, figureAccept, StorageUse, UploadList, useFigureUploads } from "./figure-upload";
import { type MemberFace, PresenceBar } from "./presence-bar";
import { statusText } from "./autosave";
import { useSaveState } from "./use-savers";
import { type BarStatus, WorkspaceBar } from "./workspace-bar";

type Load =
  | { status: "loading" }
  | { status: "notFound" }
  | { status: "error" }
  | { status: "ok"; project: Project; items: ProjectFile[]; mainFilePath: string; storage: ProjectStorage };

type Open = { kind: "create"; type: "text" | "folder" } | { kind: "rename" | "move" | "delete"; file: ProjectFile } | null;

// WorkspacePage is /projects/[projectId]/workspace (design/screens/Workspace.html): the file
// tree (FR-12.1, FR-12.2) and the LaTeX editor (FR-12.3), edited together in real time through
// the sync server (FR-12.4 to FR-12.9, docs/sync.md) with presence and cursors (FR-12.5), and
// REST autosave (FR-12.6) when the sync server cannot be reached. Members only; a viewer reads
// without changing anything. Editors and the owner compile (FR-13.1); every member sees the
// compile's progress, the PDF beside the editor, the errors with a jump to their line (FR-13.2,
// FR-13.3) and can download the PDF and the files (FR-13.6, FR-13.7). The assistant comes later.
export function WorkspacePage({ projectId }: { projectId: string }) {
  const [attempt, setAttempt] = useState(0);
  const key = `${projectId}#${attempt}`;
  const [result, setResult] = useState<{ key: string; value: Load } | null>(null);

  useEffect(() => {
    let current = true;
    void Promise.all([getProject(projectId), listFiles(projectId)]).then(([project, files]) => {
      if (!current) return;
      let value: Load;
      if ((!project.ok && project.code === "not_found") || (project.ok && project.data.myRole === null)) {
        value = { status: "notFound" };
      } else if (!files.ok && files.code === "not_found") {
        value = { status: "notFound" };
      } else if (project.ok && files.ok) {
        value = {
          status: "ok",
          project: project.data,
          items: files.data.items,
          mainFilePath: files.data.mainFilePath,
          storage: files.data.storage,
        };
      } else {
        value = { status: "error" };
      }
      setResult({ key, value });
    });
    return () => {
      current = false;
    };
  }, [projectId, key]);

  const load: Load = result?.key === key ? result.value : { status: "loading" };
  switch (load.status) {
    case "loading":
      return (
        <p role="status" className="p-6 text-rs-body text-rs-text-muted">
          Loading the workspace…
        </p>
      );
    case "notFound":
      return (
        <StateCard title="Workspace not found" text="This project does not exist, or you are not a member of it.">
          <Link href="/projects" className={rsButtonClass({ full: true })}>
            Go to My projects
          </Link>
        </StateCard>
      );
    case "error":
      return (
        <StateCard title="Something went wrong" text="We couldn’t load the workspace.">
          <button type="button" onClick={() => setAttempt((n) => n + 1)} className={rsButtonClass({ full: true })}>
            Try again
          </button>
        </StateCard>
      );
    case "ok":
      return (
        <Workspace
          key={key}
          project={load.project}
          initialItems={load.items}
          mainFilePath={load.mainFilePath}
          initialStorage={load.storage}
        />
      );
  }
}

function StateCard({ title, text, children }: { title: string; text: string; children: ReactNode }) {
  return (
    <main className="flex flex-1 items-start justify-center p-6">
      <div className="flex w-full max-w-[400px] flex-col gap-4 rounded-card border border-rs-border bg-rs-surface p-6 text-center shadow-card">
        <h1 className="text-rs-h3 leading-rs-tight font-bold text-rs-text">{title}</h1>
        <p className="text-rs-body text-rs-text-muted">{text}</p>
        {children}
      </div>
    </main>
  );
}

function Workspace({
  project,
  initialItems,
  mainFilePath,
  initialStorage,
}: {
  project: Project;
  initialItems: ProjectFile[];
  mainFilePath: string;
  initialStorage: ProjectStorage;
}) {
  const projectId = project.id;
  // A file or its project deleted while open (file_gone) reads the tree again.
  const onGone = useRef<() => void>(() => {});
  const collab = useCollab(projectId, onGone);
  const me = useSession();
  const selfId = me.status === "signedIn" ? me.user.id : null;
  const drawerId = useId();
  const fileInput = useRef<HTMLInputElement>(null);

  const [items, setItems] = useState(initialItems);
  const [mainPath, setMainPath] = useState(mainFilePath);
  const [storage, setStorage] = useState(initialStorage);
  const main = initialItems.find((f) => f.kind === "text" && f.path === mainFilePath) ?? initialItems.find((f) => f.kind === "text");
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(
    () => new Set(main ? folderAncestors(initialItems, main.path) : []),
  );
  const [selectedId, setSelectedId] = useState<string | null>(main?.id ?? null);
  const [opened, setOpened] = useState<Opened>({ kind: "none" });
  const [dialog, setDialog] = useState<Open>(null);
  const [wrap, setWrap] = useState(true);
  const [drawer, setDrawer] = useState(false);
  const [treeError, setTreeError] = useState<string | null>(null);

  const activeSession = opened.kind === "text" ? opened.session : null;
  const collabState = useCollabState(activeSession);
  const binding = collabState.binding;
  const saveState = useSaveState(binding?.kind === "rest" ? binding.saver : null);
  // The sync server says the member's role as it is now (FR-12.7): a member made viewer, or
  // removed, loses the editing actions at once.
  const ended = collabState.ended === "access_ended";
  const role = ended ? "viewer" : (collabState.role ?? project.myRole ?? "viewer");
  const canEdit = role !== "viewer";
  const faces = useMemberFaces(projectId);
  const wide = useWide();
  const [tab, setTab] = useState<"editor" | "pdf">("editor");
  const compiler = useCompile(projectId, collab.sent);
  const startCompile = compiler.start;
  const downloads = useDownloads(projectId);
  const [missing, setMissing] = useState<string | null>(null);
  const [mainError, setMainError] = useState<string | null>(null);
  const editors = useRef(new Map<string, EditorHandle>());
  const pendingJump = useRef<{ fileId: string; line: number | null } | null>(null);
  const nameOf = useCallback(
    (userId: string | null) => (userId === null ? "a member" : userId === selfId ? "you" : (faces.get(userId)?.name ?? "a member")),
    [faces, selfId],
  );
  const request = useRef(0);
  const openedRef = useRef(opened);
  useEffect(() => {
    openedRef.current = opened;
  });

  const openFile = useCallback(
    async (file: ProjectFile) => {
      if (file.kind === "folder") return;
      setDrawer(false);
      const prev = openedRef.current;
      if (prev.kind !== "none" && prev.kind !== "failed" && prev.file.id === file.id) return;
      // Leaving a text file sends what it holds and frees the member's editing slot (FR-12.8).
      if (prev.kind === "text") collab.leave(prev.file.id);
      const token = ++request.current;
      setOpened({ kind: "loading", file });
      if (file.kind === "text") {
        const session = await collab.open(file.id);
        if (token !== request.current) {
          session.release();
          return;
        }
        setOpened({ kind: "text", file, session });
        return;
      }
      const res = await getFile(projectId, file.id);
      if (token !== request.current) return;
      setOpened(res.ok ? { kind: "figure", file: res.data.file, detail: res.data } : { kind: "failed", file, error: res });
    },
    [projectId, collab],
  );

  // The main file opens first (main.tex, FR-10.4).
  const opening = useRef(false);
  useEffect(() => {
    if (opening.current || !main) return;
    opening.current = true;
    void openFile(main);
  }, [main, openFile]);

  // reload reads the tree again after a change, keeps the open file by its ID and shows path.
  const reload = useCallback(
    async (show?: ProjectFile) => {
      const res = await listFiles(projectId);
      if (!res.ok) {
        setTreeError("We couldn’t refresh the file list. Reload the page to see the current files.");
        return;
      }
      setTreeError(null);
      const next = res.data.items;
      setItems(next);
      // The main file follows a rename or move (slice 21).
      setMainPath(res.data.mainFilePath);
      setStorage(res.data.storage);
      const byId = new Map(next.map((f) => [f.id, f]));
      setOpened((prev) => {
        if (prev.kind === "none") return prev;
        const file = byId.get(prev.file.id);
        return file ? { ...prev, file } : { kind: "none" };
      });
      if (show) {
        setExpanded((set) => new Set([...set, ...folderAncestors(next, show.path)]));
        setSelectedId(show.id);
      }
    },
    [projectId],
  );

  const selected = items.find((f) => f.id === selectedId) ?? null;
  const folder = selected ? (selected.kind === "folder" ? selected.path : parentPath(selected.path)) : "";
  const folders = items.filter((f) => f.kind === "folder");
  const namesIn = useCallback(
    (dir: string) => new Set(items.filter((f) => parentPath(f.path).toLowerCase() === dir.toLowerCase()).map((f) => baseName(f.path).toLowerCase())),
    [items],
  );
  const uploads = useFigureUploads(projectId, storage.figureMaxBytes, namesIn, (file) => void reload(file));

  const toggle = useCallback((file: ProjectFile, open?: boolean) => {
    setExpanded((set) => {
      const next = new Set(set);
      if (open ?? !set.has(file.id)) next.add(file.id);
      else next.delete(file.id);
      return next;
    });
  }, []);

  useEffect(() => {
    onGone.current = () => void reload();
  });

  const saveNow = useCallback(() => void activeSession?.flush(), [activeSession]);

  // Ctrl/Cmd+S saves at once anywhere in the workspace, never the browser's save dialog;
  // Ctrl/Cmd+Enter compiles, for editors and the owner (FR-13.1).
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (!(event.ctrlKey || event.metaKey) || event.altKey) return;
      if (event.key.toLowerCase() === "s") {
        event.preventDefault();
        saveNow();
      } else if (event.key === "Enter" && canEdit && !event.repeat) {
        event.preventDefault();
        void startCompile();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [saveNow, canEdit, startCompile]);

  // An error's file opens with the cursor at its line (FR-13.3), now or when its editor is ready.
  const onEditorReady = useCallback((fileId: string, handle: EditorHandle | null) => {
    if (!handle) {
      editors.current.delete(fileId);
      return;
    }
    editors.current.set(fileId, handle);
    const jump = pendingJump.current;
    if (jump?.fileId === fileId) {
      pendingJump.current = null;
      if (jump.line !== null) handle.goToLine(jump.line);
      else handle.focus();
    }
  }, []);

  const jumpTo = useCallback(
    (target: JumpTarget) => {
      const file = items.find((f) => f.kind === "text" && f.path === target.file);
      if (!file) {
        setMissing(target.file);
        return;
      }
      setMissing(null);
      setTab("editor");
      setSelectedId(file.id);
      setExpanded((set) => new Set([...set, ...folderAncestors(items, file.path)]));
      const handle = openedRef.current.kind === "text" && openedRef.current.file.id === file.id ? editors.current.get(file.id) : undefined;
      if (handle) {
        if (target.line !== null) handle.goToLine(target.line);
        else handle.focus();
        return;
      }
      pendingJump.current = { fileId: file.id, line: target.line };
      void openFile(file);
    },
    [items, openFile],
  );

  // The owner chooses the file a compile starts from (FR-13.1).
  const chooseMain = useCallback(
    async (path: string) => {
      setMainError(null);
      const res = await updateProject(projectId, { mainFilePath: path });
      if (res.ok) setMainPath(path);
      else setMainError(res.code === "main_file_invalid" ? "That file can’t be the main file: choose a .tex file." : "We couldn’t change the main file. Try again.");
    },
    [projectId],
  );

  function closeDialog() {
    setDialog(null);
  }

  function afterChange(file: ProjectFile) {
    setDialog(null);
    void reload(file).then(() => {
      document.getElementById(treeItemId(file.id))?.focus();
    });
  }

  function afterDelete(file: ProjectFile) {
    setDialog(null);
    for (const f of items) if (isInside(f.path, file.path)) collab.remove(f.id);
    if (opened.kind !== "none" && isInside(opened.file.path, file.path)) setOpened({ kind: "none" });
    setSelectedId(null);
    void reload();
  }

  const filesButton = (
    <button
      type="button"
      aria-controls={drawerId}
      aria-expanded={drawer}
      onClick={() => setDrawer(true)}
      className={`${rsButtonClass({ variant: "secondary", size: "xs" })} min-[641px]:hidden`}
    >
      Files
    </button>
  );

  const filePane = (
    <FilePane
      opened={opened}
      role={role}
      wrap={wrap}
      onToggleWrap={() => setWrap((w) => !w)}
      onSave={saveNow}
      onRetry={(f) => void openFile(f)}
      filesButton={filesButton}
      onEditorReady={onEditorReady}
    />
  );
  const pdfPanel = (
    <PdfPanel projectId={projectId} compile={compiler.compile} onDownload={downloads.pdf} downloading={downloads.busy.has("pdf")} />
  );
  const errorsPanel = <ErrorsPanel projectId={projectId} compile={compiler.compile} missing={missing} onJump={jumpTo} />;

  return (
    <div className="flex h-dvh flex-col">
      <WorkspaceBar
        projectId={projectId}
        title={project.title}
        role={role}
        save={opened.kind === "text" && canEdit && collabState.readOnly === null ? barStatus(collabState, saveState) : null}
        presence={
          opened.kind === "text" && collabState.mode === "live" ? (
            <PresenceBar
              users={collab.presence?.[opened.file.id] ?? []}
              maxEditors={collabState.maxEditors}
              selfId={selfId}
              faces={faces}
            />
          ) : null
        }
        actions={
          <>
            <CompileStatus compile={compiler.compile} starting={compiler.starting} />
            {canEdit && <CompileButton onClick={() => void startCompile()} busy={compiler.starting} />}
            <DownloadButtons downloads={downloads} />
          </>
        }
      />
      <WarningBanner />
      <DownloadError downloads={downloads} />
      <CompileMessages
        compile={compiler.compile}
        notice={compiler.notice}
        nameOf={nameOf}
        onRetry={() => void startCompile()}
        onDismiss={compiler.dismiss}
      />
      <div className="relative flex min-h-0 flex-1 border-b border-rs-border">
        {drawer && (
          <button
            type="button"
            aria-label="Close files"
            tabIndex={-1}
            onClick={() => setDrawer(false)}
            className="fixed inset-0 z-40 bg-rs-text/40 min-[641px]:hidden"
          />
        )}
        <nav
          id={drawerId}
          aria-label="Files"
          onKeyDown={(e) => {
            if (e.key === "Escape" && drawer) setDrawer(false);
          }}
          className={`flex w-[250px] flex-none flex-col gap-2 overflow-y-auto border-r border-rs-border bg-rs-surface p-3 ${
            drawer ? "max-[640px]:fixed max-[640px]:inset-y-0 max-[640px]:left-0 max-[640px]:z-50 max-[640px]:w-[min(85vw,320px)] max-[640px]:shadow-menu" : "max-[640px]:hidden"
          }`}
        >
          <div className="flex items-center justify-between gap-1.5">
            <strong className="text-rs-body">Files</strong>
            {drawer && (
              <button type="button" onClick={() => setDrawer(false)} className={`${rsButtonClass({ variant: "secondary", size: "xs" })} min-[641px]:hidden`}>
                Close
              </button>
            )}
          </div>
          {role === "owner" && (
            <MainFileChoice items={items} mainPath={mainPath} error={mainError} onChoose={(p) => void chooseMain(p)} />
          )}
          {canEdit && (
            <div className="flex flex-wrap gap-1">
              <button id="new-file" type="button" aria-label="New file" onClick={() => setDialog({ kind: "create", type: "text" })} className={smallButton}>
                + File
              </button>
              <button id="new-folder" type="button" aria-label="New folder" onClick={() => setDialog({ kind: "create", type: "folder" })} className={smallButton}>
                + Folder
              </button>
              <button
                type="button"
                aria-label="Upload figure"
                onClick={() => fileInput.current?.click()}
                className={`${smallButton} border-rs-accent-strong text-rs-accent-strong`}
              >
                Upload
              </button>
              <input
                ref={fileInput}
                type="file"
                multiple
                accept={figureAccept}
                aria-label="Choose figures to upload"
                className="hidden"
                onChange={(e) => {
                  uploads.start([...(e.target.files ?? [])], folder);
                  e.target.value = "";
                }}
              />
            </div>
          )}
          <FileTree
            items={items}
            expanded={expanded}
            selectedId={selectedId}
            openId={opened.kind === "none" ? null : opened.file.id}
            canEdit={canEdit}
            presence={collab.presence}
            mainFilePath={mainPath}
            onSelect={(f) => setSelectedId(f.id)}
            onToggle={toggle}
            onOpen={(f) => void openFile(f)}
            onRename={(f) => setDialog({ kind: "rename", file: f })}
            onMove={(f) => setDialog({ kind: "move", file: f })}
            onDelete={(f) => setDialog({ kind: "delete", file: f })}
          />
          {treeError && (
            <p role="alert" className="text-rs-caption text-rs-danger">
              {treeError}
            </p>
          )}
          {canEdit && (
            <DropZone folder={folder} maxBytes={storage.figureMaxBytes} onFiles={(files) => uploads.start(files, folder)} />
          )}
          <UploadList uploads={uploads.uploads} onDismiss={uploads.dismiss} />
          <StorageUse usedBytes={storage.usedBytes} limitBytes={storage.limitBytes} />
          <DevApiStatus inline />
        </nav>
        {wide ? (
          <Split left={filePane} right={pdfPanel} />
        ) : (
          <div className="flex min-h-0 min-w-0 flex-1 flex-col">
            <ViewTabs tab={tab} onTab={setTab} />
            <div id="workspace-editor" role="tabpanel" aria-labelledby="workspace-tab-editor" hidden={tab !== "editor"} className="flex min-h-0 flex-1 flex-col">
              {filePane}
              <div className="border-t border-rs-border">{errorsPanel}</div>
            </div>
            <div id="workspace-pdf" role="tabpanel" aria-labelledby="workspace-tab-pdf" hidden={tab !== "pdf"} className="flex min-h-0 flex-1 flex-col">
              {pdfPanel}
            </div>
          </div>
        )}
      </div>
      {wide && errorsPanel}

      {dialog?.kind === "create" && (
        <CreateDialog
          kind={dialog.type}
          folders={folders}
          folder={folder}
          create={(path) => createFile(projectId, { path, kind: dialog.type })}
          onDone={(file) => {
            afterChange(file);
            if (file.kind === "text") void openFile(file);
          }}
          onClose={closeDialog}
          returnFocusId={dialog.type === "text" ? "new-file" : "new-folder"}
        />
      )}
      {dialog?.kind === "rename" && (
        <RenameDialog
          file={dialog.file}
          move={(path) => moveFile(projectId, dialog.file.id, path)}
          onDone={afterChange}
          onClose={closeDialog}
          returnFocusId={treeItemId(dialog.file.id)}
        />
      )}
      {dialog?.kind === "move" && (
        <MoveDialog
          file={dialog.file}
          folders={folders}
          move={(path) => moveFile(projectId, dialog.file.id, path)}
          onDone={afterChange}
          onClose={closeDialog}
          returnFocusId={treeItemId(dialog.file.id)}
        />
      )}
      {dialog?.kind === "delete" && (
        <DeleteFileDialog
          file={dialog.file}
          inside={dialog.file.kind === "folder" ? countInside(items, dialog.file) : 0}
          remove={() => deleteFile(projectId, dialog.file.id)}
          onDone={() => afterDelete(dialog.file)}
          onClose={closeDialog}
          returnFocusId={treeItemId(dialog.file.id)}
        />
      )}
    </div>
  );
}

// ViewTabs switch between the editor and the PDF below 1024 pixels (FR-13.1).
function ViewTabs({ tab, onTab }: { tab: "editor" | "pdf"; onTab: (tab: "editor" | "pdf") => void }) {
  const tabs = [
    ["editor", "Editor"],
    ["pdf", "PDF"],
  ] as const;
  return (
    <div
      role="tablist"
      aria-label="Workspace view"
      onKeyDown={(e) => {
        if (e.key !== "ArrowLeft" && e.key !== "ArrowRight" && e.key !== "Home" && e.key !== "End") return;
        e.preventDefault();
        const next = e.key === "Home" ? "editor" : e.key === "End" ? "pdf" : tab === "editor" ? "pdf" : "editor";
        onTab(next);
        document.getElementById(`workspace-tab-${next}`)?.focus();
      }}
      className="flex flex-none border-b border-rs-border bg-rs-surface"
    >
      {tabs.map(([id, label]) => (
        <button
          key={id}
          id={`workspace-tab-${id}`}
          type="button"
          role="tab"
          aria-selected={tab === id}
          aria-controls={`workspace-${id}`}
          tabIndex={tab === id ? 0 : -1}
          onClick={() => onTab(id)}
          className={`flex-1 border-b-2 py-2 text-rs-body font-medium focus-visible:shadow-focus focus-visible:outline-none ${
            tab === id ? "border-rs-accent-strong text-rs-heading" : "border-transparent text-rs-text-muted hover:text-rs-text"
          }`}
        >
          {label}
        </button>
      ))}
    </div>
  );
}

// MainFileChoice lets the owner choose the .tex file a compile starts from (FR-13.1).
function MainFileChoice({
  items,
  mainPath,
  error,
  onChoose,
}: {
  items: ProjectFile[];
  mainPath: string;
  error: string | null;
  onChoose: (path: string) => void;
}) {
  const id = useId();
  const tex = items.filter((f) => f.kind === "text" && f.path.toLowerCase().endsWith(".tex")).map((f) => f.path);
  if (tex.length === 0) return null;
  const options = tex.includes(mainPath) ? tex : [mainPath, ...tex];
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-rs-caption font-medium text-rs-text">
        Main file
      </label>
      <select
        id={id}
        value={mainPath}
        onChange={(e) => onChoose(e.target.value)}
        className="h-8 rounded-control border border-rs-control bg-rs-surface px-1.5 text-rs-caption text-rs-text focus-visible:shadow-focus focus-visible:outline-none"
      >
        {options.map((p) => (
          <option key={p} value={p} disabled={!tex.includes(p)}>
            {tex.includes(p) ? p : `${p} (missing)`}
          </option>
        ))}
      </select>
      {error && (
        <p role="alert" className="text-rs-caption text-rs-danger">
          {error}
        </p>
      )}
    </div>
  );
}

function barStatus(state: ReturnType<typeof useCollabState>, save: ReturnType<typeof useSaveState>): BarStatus {
  const status = collabStatus(state, save);
  if (state.mode !== "fallback") return status;
  // REST saves while live editing is unavailable: the notice, then the save's own status.
  return { ...status, detail: statusText(save), saved: save.status === "saved" };
}

// useMemberFaces reads the members' names and photos once, for the presence avatars.
function useMemberFaces(projectId: string): ReadonlyMap<string, MemberFace> {
  const [faces, setFaces] = useState<ReadonlyMap<string, MemberFace>>(new Map());
  useEffect(() => {
    let current = true;
    void listProjectMembers(projectId).then((res) => {
      if (!current || !res.ok) return;
      setFaces(new Map(res.data.items.map((m) => [m.user.userId, { name: m.user.fullName, photoUrl: m.user.photoUrl }])));
    });
    return () => {
      current = false;
    };
  }, [projectId]);
  return faces;
}

const smallButton =
  "h-8 rounded-control border border-rs-control bg-rs-surface px-2 text-rs-caption font-medium text-rs-text hover:bg-rs-subtle focus-visible:shadow-focus focus-visible:outline-none";
