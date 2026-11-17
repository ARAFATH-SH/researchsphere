"use client";

import dynamic from "next/dynamic";
import { type ReactNode, useRef, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import type { ApiError } from "@/lib/api/call";

import { fileErrorText, type ProjectFile, type ProjectFileDetail } from "../files/api";
import { baseName } from "../files/tree";
import type { Autosaver } from "./autosave";
import type { EditorHandle } from "./editor/latex-editor";
import { useSaveState } from "./use-savers";

// CodeMirror loads only here, on the workspace route, once a text file is open.
const LatexEditor = dynamic(() => import("./editor/latex-editor"), {
  ssr: false,
  loading: () => (
    <p role="status" className="p-4 text-rs-body text-rs-text-muted">
      Loading the editor…
    </p>
  ),
});

export type Opened =
  | { kind: "none" }
  | { kind: "loading"; file: ProjectFile }
  | { kind: "text"; file: ProjectFile; saver: Autosaver }
  | { kind: "figure"; file: ProjectFile; detail: ProjectFileDetail }
  | { kind: "failed"; file: ProjectFile; error: ApiError };

type Props = {
  opened: Opened;
  canEdit: boolean;
  wrap: boolean;
  onToggleWrap: () => void;
  onSave: () => void;
  onRetry: (file: ProjectFile) => void;
  // filesButton opens the file drawer on a narrow screen.
  filesButton: ReactNode;
};

// FilePane is the centre of the workspace: the editor for a text file (FR-12.3), a preview of
// an image figure, a download link for a PDF figure.
export function FilePane({ opened, canEdit, wrap, onToggleWrap, onSave, onRetry, filesButton }: Props) {
  return (
    <section aria-label="Editor" className="flex min-h-0 min-w-0 flex-1 flex-col">
      {opened.kind === "text" ? (
        <TextFile key={opened.file.id} file={opened.file} saver={opened.saver} {...{ canEdit, wrap, onToggleWrap, onSave, filesButton }} />
      ) : (
        <>
          <div className="flex min-h-12 flex-wrap items-center gap-2 border-b border-rs-border px-2.5 py-1.5">
            {filesButton}
            {opened.kind !== "none" && <span className="text-rs-caption text-rs-text-muted">{opened.file.path}</span>}
          </div>
          <div className="min-h-0 flex-1 overflow-auto p-4">
            <OtherContent opened={opened} onRetry={onRetry} />
          </div>
        </>
      )}
    </section>
  );
}

function OtherContent({ opened, onRetry }: { opened: Exclude<Opened, { kind: "text" }>; onRetry: (file: ProjectFile) => void }) {
  switch (opened.kind) {
    case "none":
      return <p className="text-rs-body text-rs-text-muted">Choose a file to open it.</p>;
    case "loading":
      return (
        <p role="status" className="text-rs-body text-rs-text-muted">
          Opening {baseName(opened.file.path)}…
        </p>
      );
    case "figure":
      return <Figure file={opened.file} detail={opened.detail} />;
    case "failed": {
      const { file, error } = opened;
      const again = (
        <button type="button" onClick={() => onRetry(file)} className={rsButtonClass({ variant: "secondary", size: "xs" })}>
          Check again
        </button>
      );
      const text: Record<string, string> = {
        scan_pending: "This figure is still being checked for malware. It opens once the check is done.",
        scan_failed: "This figure couldn’t be checked for malware, so it can’t be opened.",
        file_removed: "This figure was removed because malware was found in it.",
      };
      return (
        <div role="alert" className="flex flex-col items-start gap-3 text-rs-body">
          <p>{text[error.code] ?? fileErrorText(error)}</p>
          {(error.code === "scan_pending" || !(error.code in text)) && again}
        </div>
      );
    }
  }
}

function Figure({ file, detail }: { file: ProjectFile; detail: ProjectFileDetail }) {
  const name = baseName(file.path);
  if (!detail.download) return <p className="text-rs-body text-rs-text-muted">This figure can’t be shown.</p>;
  if (file.contentType === "application/pdf") {
    return (
      <div className="flex flex-col items-start gap-3 text-rs-body">
        <p>PDF figures can’t be previewed here yet.</p>
        <a href={detail.download.url} className={rsButtonClass({ variant: "outline", size: "sm" })}>
          Download {name}
        </a>
      </div>
    );
  }
  return (
    // The presigned URL is on the storage host and expires in 5 minutes; next/image would
    // cache and resize it through the app server, so a plain img shows it.
    // eslint-disable-next-line @next/next/no-img-element
    <img src={detail.download.url} alt={`Preview of ${name}`} className="mx-auto block max-h-full max-w-full object-contain" />
  );
}

function ToolButton({ label, onClick, disabled, children }: { label: string; onClick: () => void; disabled?: boolean; children: ReactNode }) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={onClick}
      className="flex size-9 items-center justify-center rounded-control text-rs-text hover:bg-rs-subtle focus-visible:shadow-focus focus-visible:outline-none disabled:opacity-40"
    >
      <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" className="size-[18px]">
        {children}
      </svg>
    </button>
  );
}

function TextFile({
  file,
  saver,
  canEdit,
  wrap,
  onToggleWrap,
  onSave,
  filesButton,
}: Omit<Props, "opened" | "onRetry"> & { file: ProjectFile; saver: Autosaver }) {
  const handle = useRef<EditorHandle | null>(null);
  const state = useSaveState(saver);
  // The conflict dialog can be closed and opened again; the text waits meanwhile.
  const [dismissed, setDismissed] = useState<typeof state | null>(null);
  const conflict = state.status === "conflict" && dismissed !== state;
  const name = baseName(file.path);

  function resolve(choice: "theirs" | "mine") {
    const text = saver.resolve(choice);
    if (text !== null) handle.current?.replaceText(text);
  }

  return (
    <>
      <div className="flex min-h-12 flex-wrap items-center gap-1 border-b border-rs-border px-2.5 py-1.5">
        {filesButton}
        <ToolButton label="Undo" disabled={!canEdit} onClick={() => handle.current?.undo()}>
          <path d="M9 14L4 9l5-5" />
          <path d="M4 9h11a5 5 0 0 1 0 10h-3" />
        </ToolButton>
        <ToolButton label="Redo" disabled={!canEdit} onClick={() => handle.current?.redo()}>
          <path d="M15 14l5-5-5-5" />
          <path d="M20 9H9a5 5 0 0 0 0 10h3" />
        </ToolButton>
        <ToolButton label={canEdit ? "Find and replace" : "Find"} onClick={() => handle.current?.openFind()}>
          <circle cx="11" cy="11" r="7" />
          <path d="M20 20l-4-4" />
        </ToolButton>
        <button
          type="button"
          aria-pressed={wrap}
          onClick={onToggleWrap}
          className={`ml-1 h-8 rounded-control border px-2 text-rs-caption focus-visible:shadow-focus focus-visible:outline-none ${
            wrap ? "border-rs-accent-strong bg-rs-accent-soft text-rs-heading" : "border-rs-control bg-rs-surface text-rs-text"
          }`}
        >
          Wrap lines
        </button>
        <span className="ml-1.5 truncate text-rs-caption text-rs-text-muted">{file.path}</span>
        {!canEdit && (
          <span className="ml-auto rounded-rs-pill border border-rs-border bg-rs-subtle px-2.5 py-0.5 text-rs-caption font-medium text-rs-text">
            View only
          </span>
        )}
        {state.status === "conflict" && !conflict && (
          <button type="button" onClick={() => setDismissed(null)} className={`${rsButtonClass({ variant: "danger", size: "xs" })} ml-auto`}>
            Resolve the conflict
          </button>
        )}
      </div>
      <div className="min-h-0 flex-1">
        <LatexEditor
          source={saver}
          label={canEdit ? `Editing ${name}` : `${name}, view only`}
          readOnly={!canEdit}
          wrap={wrap}
          onSave={onSave}
          onReady={(h) => (handle.current = h)}
        />
      </div>
      {conflict && (
        <Dialog title="This file changed in another tab" onClose={() => setDismissed(state)}>
          <div className="flex flex-col gap-4 text-rs-body">
            <p>
              {name} was saved somewhere else (another tab, or another member) after you opened it, so your changes
              weren’t saved. Your text is kept until you choose.
            </p>
            <ul className="list-disc pl-5 text-rs-text-muted">
              <li>
                <strong className="text-rs-text">Use theirs</strong> shows the saved version and drops your changes.
              </li>
              <li>
                <strong className="text-rs-text">Keep mine</strong> saves your text over theirs.
              </li>
            </ul>
            <div className="flex flex-wrap justify-end gap-2">
              <button type="button" onClick={() => resolve("theirs")} className={rsButtonClass({ variant: "secondary", size: "form" })}>
                Use theirs
              </button>
              <button type="button" onClick={() => resolve("mine")} className={rsButtonClass({ size: "form" })}>
                Keep mine (overwrite)
              </button>
            </div>
          </div>
        </Dialog>
      )}
    </>
  );
}
