"use client";

import dynamic from "next/dynamic";
import Link from "next/link";
import { type ReactNode, useMemo, useRef, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import type { ApiError } from "@/lib/api/call";

import { fileErrorText, type ProjectFile, type ProjectFileDetail } from "../files/api";
import { baseName } from "../files/tree";
import type { ProjectRole } from "../api";
import type { CollabSession } from "./collab/session";
import { canType, capacityText, type Conflict, type EndReason, fallbackNotice, fallbackReasonText } from "./collab/types";
import { useCollabState } from "./collab/use-collab";
import type { EditorHandle, EditorSource } from "./editor/latex-editor";
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
  | { kind: "text"; file: ProjectFile; session: CollabSession }
  | { kind: "figure"; file: ProjectFile; detail: ProjectFileDetail }
  | { kind: "failed"; file: ProjectFile; error: ApiError };

type Props = {
  opened: Opened;
  // role is the member's role in the project, as the workspace knows it now.
  role: ProjectRole;
  wrap: boolean;
  onToggleWrap: () => void;
  onSave: () => void;
  onRetry: (file: ProjectFile) => void;
  // filesButton opens the file drawer on a narrow screen.
  filesButton: ReactNode;
  // onEditorReady tells the workspace which file's editor is ready, to jump to a line (FR-13.3).
  onEditorReady?: (fileId: string, handle: EditorHandle | null) => void;
};

// FilePane is the centre of the workspace: the editor for a text file (FR-12.3), a preview of
// an image figure, a download link for a PDF figure.
export function FilePane({ opened, role, wrap, onToggleWrap, onSave, onRetry, filesButton, onEditorReady }: Props) {
  return (
    <section aria-label="Editor" className="flex min-h-0 min-w-0 flex-1 flex-col">
      {opened.kind === "text" ? (
        <TextFile key={opened.file.id} file={opened.file} session={opened.session} {...{ role, wrap, onToggleWrap, onSave, filesButton, onEditorReady }} />
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

const endText: Record<EndReason, string> = {
  access_ended: "You’re no longer a member of this project, so this file closed.",
  file_gone: "This file was deleted.",
  session_lost: "Your session ended. Sign in again to keep editing.",
};

function TextFile({
  file,
  session,
  role,
  wrap,
  onToggleWrap,
  onSave,
  filesButton,
  onEditorReady,
}: Omit<Props, "opened" | "onRetry"> & { file: ProjectFile; session: CollabSession }) {
  const handle = useRef<EditorHandle | null>(null);
  const state = useCollabState(session);
  const binding = state.binding;
  const saver = binding?.kind === "rest" ? binding.saver : null;
  const saveState = useSaveState(saver);
  // The conflict dialogs can be closed and opened again; the text waits meanwhile.
  const [dismissed, setDismissed] = useState<unknown>(null);
  const name = baseName(file.path);
  const editable = canType(state, role);
  const viewer = (state.role ?? role) === "viewer";

  const restSource = useMemo<EditorSource | null>(() => (saver ? { kind: "rest", source: saver } : null), [saver]);
  const source: EditorSource | null = binding?.kind === "yjs" ? binding : restSource;

  // A save over REST that met a newer version (21.2), or texts that could not be merged.
  const saveConflict = saveState.status === "conflict" ? saveState : null;
  const syncConflict = state.conflict;
  const conflictKey = syncConflict ?? saveConflict;
  const showDialog = conflictKey !== null && dismissed !== conflictKey;

  function resolveSave(choice: "theirs" | "mine") {
    const text = saver?.resolve(choice) ?? null;
    if (text !== null) handle.current?.replaceText(text);
  }

  function resolveSync(choice: "theirs" | "mine") {
    const text = session.resolveConflict(choice);
    if (text !== null) handle.current?.applyText(text);
  }

  if (state.ended) {
    return (
      <>
        <div className="flex min-h-12 flex-wrap items-center gap-2 border-b border-rs-border px-2.5 py-1.5">
          {filesButton}
          <span className="text-rs-caption text-rs-text-muted">{file.path}</span>
        </div>
        <div role="alert" className="flex flex-col items-start gap-3 p-4 text-rs-body">
          <p>{endText[state.ended]}</p>
          {state.ended === "access_ended" && (
            <Link href="/projects" className={rsButtonClass({ variant: "secondary", size: "xs" })}>
              Go to My projects
            </Link>
          )}
        </div>
      </>
    );
  }

  return (
    <>
      <div className="flex min-h-12 flex-wrap items-center gap-1 border-b border-rs-border px-2.5 py-1.5">
        {filesButton}
        <ToolButton label="Undo" disabled={!editable} onClick={() => handle.current?.undo()}>
          <path d="M9 14L4 9l5-5" />
          <path d="M4 9h11a5 5 0 0 1 0 10h-3" />
        </ToolButton>
        <ToolButton label="Redo" disabled={!editable} onClick={() => handle.current?.redo()}>
          <path d="M15 14l5-5-5-5" />
          <path d="M20 9H9a5 5 0 0 0 0 10h3" />
        </ToolButton>
        <ToolButton label={viewer ? "Find" : "Find and replace"} onClick={() => handle.current?.openFind()}>
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
        {viewer && (
          <span className="ml-auto rounded-rs-pill border border-rs-border bg-rs-subtle px-2.5 py-0.5 text-rs-caption font-medium text-rs-text">
            View only
          </span>
        )}
        {conflictKey !== null && !showDialog && (
          <button type="button" onClick={() => setDismissed(null)} className={`${rsButtonClass({ variant: "danger", size: "xs" })} ml-auto`}>
            Resolve the conflict
          </button>
        )}
      </div>
      {state.mode === "fallback" && (
        <p role="note" className="border-b border-rs-warning-line bg-rs-warning-soft px-3 py-2 text-rs-caption text-rs-warning">
          <strong>{fallbackNotice}.</strong> {viewer ? "You can read the file." : "Your changes are saved as you type."}
          {process.env.NODE_ENV === "development" && state.fallbackReason && (
            <span className="ml-1 font-mono">(dev: {fallbackReasonText(state.fallbackReason)})</span>
          )}
        </p>
      )}
      {!viewer && state.readOnly === "capacity" && (
        <p role="status" className="border-b border-rs-border bg-rs-subtle px-3 py-2 text-rs-caption text-rs-text">
          <strong>Opened read-only</strong> — {capacityText(state.maxEditors)}
        </p>
      )}
      {state.tooLarge && (
        <div role="alert" className="flex items-center gap-2 border-b border-rs-danger-line bg-rs-danger-soft px-3 py-2 text-rs-caption text-rs-danger-strong">
          <span>This edit would make the file larger than 1 MiB, so it was undone.</span>
          <button type="button" onClick={() => session.dismissTooLarge()} className="ml-auto font-medium underline">
            Dismiss
          </button>
        </div>
      )}
      <div className="min-h-0 flex-1">
        {source ? (
          <LatexEditor
            source={source}
            label={editable ? `Editing ${name}` : viewer ? `${name}, view only` : `${name}, read only`}
            readOnly={!editable}
            wrap={wrap}
            onSave={onSave}
            onReady={(h) => {
              handle.current = h;
              onEditorReady?.(file.id, h);
            }}
          />
        ) : (
          <p role="status" className="p-4 text-rs-body text-rs-text-muted">
            {state.waiting
              ? `We can’t reach the server right now. ${name} opens as soon as the connection returns.`
              : `Opening ${name}…`}
          </p>
        )}
      </div>
      {showDialog && saveConflict && !syncConflict && (
        <ConflictDialog
          title="This file changed in another tab"
          text={`${name} was saved somewhere else (another tab, or another member) after you opened it, so your changes weren’t saved. Your text is kept until you choose.`}
          theirs="Use theirs"
          theirsText="shows the saved version and drops your changes."
          mine="Keep mine (overwrite)"
          mineText="saves your text over theirs."
          onChoose={resolveSave}
          onClose={() => setDismissed(conflictKey)}
        />
      )}
      {showDialog && syncConflict && (
        <ConflictDialog
          title="Your changes couldn’t be merged"
          text={`${name} changed on the server while your edits were waiting to be sent, and the two can’t be combined. Your text is kept until you choose.`}
          theirs="Use the server’s"
          theirsText="shows the current version and drops your waiting changes."
          mine="Keep mine (overwrite)"
          mineText="replaces the current text with yours; you can undo it."
          conflict={syncConflict}
          onChoose={resolveSync}
          onClose={() => setDismissed(conflictKey)}
        />
      )}
    </>
  );
}

function ConflictDialog(props: {
  title: string;
  text: string;
  theirs: string;
  theirsText: string;
  mine: string;
  mineText: string;
  conflict?: Conflict;
  onChoose: (choice: "theirs" | "mine") => void;
  onClose: () => void;
}) {
  return (
    <Dialog title={props.title} onClose={props.onClose}>
      <div className="flex flex-col gap-4 text-rs-body">
        <p>{props.text}</p>
        <ul className="list-disc pl-5 text-rs-text-muted">
          <li>
            <strong className="text-rs-text">{props.theirs}</strong> {props.theirsText}
          </li>
          <li>
            <strong className="text-rs-text">{props.mine}</strong> {props.mineText}
          </li>
        </ul>
        {props.conflict && (
          <details className="text-rs-caption">
            <summary className="cursor-pointer text-rs-text">Show your text</summary>
            <pre className="mt-2 max-h-48 overflow-auto rounded-control bg-rs-subtle p-2 font-mono whitespace-pre-wrap">{props.conflict.mine}</pre>
          </details>
        )}
        <div className="flex flex-wrap justify-end gap-2">
          <button type="button" onClick={() => props.onChoose("theirs")} className={rsButtonClass({ variant: "secondary", size: "form" })}>
            {props.theirs}
          </button>
          <button type="button" onClick={() => props.onChoose("mine")} className={rsButtonClass({ size: "form" })}>
            {props.mine}
          </button>
        </div>
      </div>
    </Dialog>
  );
}
