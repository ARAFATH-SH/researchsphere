"use client";

import Link from "next/link";
import { type ReactNode, useCallback, useEffect, useId, useRef, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { WarningBanner } from "@/features/moderation/warning-banner";

import { getProject, type Project } from "../api";
import { createFile, deleteFile, getFile, listFiles, moveFile, type ProjectFile, type ProjectStorage } from "../files/api";
import { baseName, countInside, folderAncestors, isInside, parentPath } from "../files/tree";
import { CreateDialog, DeleteFileDialog, MoveDialog, RenameDialog } from "./file-dialogs";
import { type Opened, FilePane } from "./file-pane";
import { FileTree, treeItemId } from "./file-tree";
import { DropZone, figureAccept, StorageUse, UploadList, useFigureUploads } from "./figure-upload";
import { useSaveState, useSavers } from "./use-savers";
import { WorkspaceBar } from "./workspace-bar";

type Load =
  | { status: "loading" }
  | { status: "notFound" }
  | { status: "error" }
  | { status: "ok"; project: Project; items: ProjectFile[]; mainFilePath: string; storage: ProjectStorage };

type Open = { kind: "create"; type: "text" | "folder" } | { kind: "rename" | "move" | "delete"; file: ProjectFile } | null;

// WorkspacePage is /projects/[projectId]/workspace (design/screens/Workspace.html): the file
// tree (FR-12.1, FR-12.2) and the LaTeX editor (FR-12.3) with autosave (FR-12.6) for one user.
// Members only; a viewer reads without changing anything. Compile, the PDF, the assistant and
// other members' cursors come with slices 22 and 23.
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
  const role = project.myRole ?? "viewer";
  const canEdit = role !== "viewer";
  const savers = useSavers(projectId);
  const drawerId = useId();
  const fileInput = useRef<HTMLInputElement>(null);

  const [items, setItems] = useState(initialItems);
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

  const activeSaver = opened.kind === "text" ? opened.saver : null;
  const saveState = useSaveState(activeSaver);
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
      // Leaving a text file saves it at once (FR-12.6).
      if (prev.kind === "text" && prev.file.id !== file.id) void prev.saver.flush();
      if (prev.kind !== "none" && prev.kind !== "failed" && prev.file.id === file.id) return;
      const token = ++request.current;
      setOpened({ kind: "loading", file });
      const kept = file.kind === "text" ? savers.get(file.id) : undefined;
      if (kept) {
        setOpened({ kind: "text", file, saver: kept });
        return;
      }
      const res = await getFile(projectId, file.id);
      if (token !== request.current) return;
      if (!res.ok) {
        setOpened({ kind: "failed", file, error: res });
        return;
      }
      if (file.kind === "text") {
        const saver = savers.get(file.id) ?? savers.create(file.id, res.data.content ?? "", res.data.file.version);
        setOpened({ kind: "text", file: res.data.file, saver });
      } else {
        setOpened({ kind: "figure", file: res.data.file, detail: res.data });
      }
    },
    [projectId, savers],
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

  const saveNow = useCallback(() => void activeSaver?.flush(), [activeSaver]);

  // Ctrl/Cmd+S saves at once anywhere in the workspace, never the browser's save dialog.
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && !event.altKey && event.key.toLowerCase() === "s") {
        event.preventDefault();
        saveNow();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [saveNow]);

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
    for (const f of items) if (isInside(f.path, file.path)) savers.remove(f.id);
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

  return (
    <div className="flex h-dvh flex-col">
      <WorkspaceBar
        projectId={projectId}
        title={project.title}
        role={role}
        save={opened.kind === "text" && canEdit ? saveState : null}
      />
      <WarningBanner />
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
        </nav>
        <FilePane
          opened={opened}
          canEdit={canEdit}
          wrap={wrap}
          onToggleWrap={() => setWrap((w) => !w)}
          onSave={saveNow}
          onRetry={(f) => void openFile(f)}
          filesButton={filesButton}
        />
      </div>

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

const smallButton =
  "h-8 rounded-control border border-rs-control bg-rs-surface px-2 text-rs-caption font-medium text-rs-text hover:bg-rs-subtle focus-visible:shadow-focus focus-visible:outline-none";
