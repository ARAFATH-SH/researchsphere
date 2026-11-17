"use client";

import { type DragEvent, useCallback, useEffect, useRef, useState } from "react";

import {
  type FigureUploadFailure,
  fileErrorText,
  formatBytes,
  type ProjectFile,
  uploadFigure,
} from "../files/api";
import { baseName, figureName, joinPath, parentPath, pathError, uniqueName } from "../files/tree";

// Figure uploads (FR-12.2): PNG, JPG or PDF of up to the server's figure limit each (50 MB by
// default), from the Upload button or dropped on the drop zone, into the selected folder. Each
// goes straight to storage (docs/decisions.md 2026-11-18). A name is never a reason to refuse
// a file: it gets the path rules' form ("WhatsApp Image 2026-11-17 at 9.10.25 PM.jpeg" becomes
// "whatsapp-image-2026-11-17-at-9.10.25-pm.jpeg") and "-2", "-3"... when the folder has it
// already; the final name is shown. Each shows its progress; each refused one says why, in
// words that name the cause.

export type Upload = {
  id: number;
  name: string; // the file's own name
  finalName: string; // the name it is stored under
  status: "uploading" | "done" | "failed";
  progress: number;
  error?: string;
};

export const figureAccept = ".png,.jpg,.jpeg,.pdf,image/png,image/jpeg,application/pdf";
const allowedName = /\.(png|jpe?g|pdf)$/i;
const allowedType = new Set(["image/png", "image/jpeg", "application/pdf"]);

// maxNameTries bounds the retries when the server finds the name taken meanwhile.
const maxNameTries = 5;

function lowerFirst(text: string) {
  return text.charAt(0).toLowerCase() + text.slice(1);
}

// checkFigure says why a file cannot be uploaded before sending it, or null. maxBytes is the
// server's figure limit. The type counts by the name's extension or the browser's type, never
// both; the server reads the content anyway.
export function checkFigure(file: File, maxBytes: number): string | null {
  if (!allowedName.test(file.name) && !allowedType.has(file.type)) return "only PNG, JPG and PDF files can be figures.";
  if (file.size === 0) return "the file is empty.";
  if (file.size > maxBytes) return `it’s over ${formatBytes(maxBytes)}, the largest a figure may be.`;
  return null;
}

// uploadFailureText says, after "<name> wasn’t uploaded: ", what went wrong at which step.
export function uploadFailureText(f: FigureUploadFailure): string {
  if (f.step === "storage") {
    if (f.code === "network_error" || f.status === 0) {
      return "the file couldn’t reach storage: the connection failed, or storage doesn’t accept uploads from this site. Check your connection and try again.";
    }
    if (f.status === 400) return "storage refused the file because its size didn’t match what was announced. Try again.";
    if (f.status === 403) return "storage refused the upload: its permission expired or was denied. Try again.";
    return `storage refused the file (HTTP ${f.status}). Try again.`;
  }
  const start = f.step === "start";
  switch (f.code) {
    case "network_error":
      return start
        ? "the server couldn’t be reached to start the upload. Check your connection and try again."
        : "the server couldn’t be reached to finish the upload. Check your connection and upload it again.";
    case "internal_error":
      return start
        ? "the server failed to start the upload (a server error). Try again; if it keeps failing, the API log names the cause."
        : "the server failed while checking the file (a server error). Try again.";
    case "unsupported_type":
      return "it isn’t a PNG, JPG or PDF. The type is read from the file’s content, not its name.";
    case "invalid_image":
      return "the image is damaged or incomplete, so it couldn’t be read.";
    case "not_found":
      return start
        ? "the project no longer exists, or you are no longer a member."
        : "the upload expired or was already finished. Upload it again.";
    case "rate_limited":
      return "you have uploaded 30 figures this hour. Try again later.";
  }
  return lowerFirst(fileErrorText(f));
}

// useFigureUploads runs the uploads of the workspace. taken lists the names in a folder now,
// lower case.
export function useFigureUploads(
  projectId: string,
  maxBytes: number,
  taken: (folder: string) => ReadonlySet<string>,
  onUploaded: (file: ProjectFile) => void,
) {
  const [uploads, setUploads] = useState<Upload[]>([]);
  const next = useRef(1);
  const done = useRef(onUploaded);
  const names = useRef(taken);
  const controllers = useRef(new Set<AbortController>());
  // claimed are the paths of uploads under way (lower case), so two files of one batch never
  // get the same name.
  const claimed = useRef(new Set<string>());
  useEffect(() => {
    done.current = onUploaded;
    names.current = taken;
  });
  useEffect(() => {
    const all = controllers.current;
    return () => all.forEach((c) => c.abort());
  }, []);

  const update = useCallback((id: number, change: Partial<Upload>) => {
    setUploads((list) => list.map((u) => (u.id === id ? { ...u, ...change } : u)));
  }, []);

  // pick gives the file a free path in folder: the path rules' form of its name, numbered when
  // the folder or another upload has it, or more when skip lists names refused meanwhile.
  const pick = useCallback((file: File, folder: string, skip: ReadonlySet<string>) => {
    const inFolder = new Set([...names.current(folder), ...skip]);
    for (const p of claimed.current) if (parentPath(p) === folder.toLowerCase()) inFolder.add(baseName(p));
    const room = Math.min(100, 255 - (folder ? folder.length + 1 : 0));
    return uniqueName(figureName(file.name, file.type, Math.max(5, room - 3)), inFolder);
  }, []);

  // send uploads the file under a free name; when the server finds the name taken meanwhile,
  // it tries the next number.
  const send = useCallback(
    async (id: number, file: File, folder: string, controller: AbortController) => {
      const skip = new Set<string>();
      for (;;) {
        const name = pick(file, folder, skip);
        const path = joinPath(folder, name);
        claimed.current.add(path.toLowerCase());
        update(id, { finalName: name, status: "uploading", progress: 0 });
        const res = await uploadFigure(projectId, path, file, {
          signal: controller.signal,
          onProgress: (share) => update(id, { progress: share }),
        });
        claimed.current.delete(path.toLowerCase());
        if (res.ok) {
          controllers.current.delete(controller);
          update(id, { status: "done", finalName: baseName(res.data.path), progress: 1 });
          done.current(res.data);
          return;
        }
        if (res.code === "aborted") return;
        if (res.code === "path_taken" && skip.size < maxNameTries) {
          skip.add(name.toLowerCase());
          continue;
        }
        controllers.current.delete(controller);
        update(id, { status: "failed", error: uploadFailureText(res) });
        return;
      }
    },
    [pick, projectId, update],
  );

  const start = useCallback(
    (files: File[], folder: string) => {
      for (const file of files) {
        const id = next.current++;
        const refusal = checkFigure(file, maxBytes) ?? folderRefusal(folder);
        if (refusal) {
          setUploads((list) => [...list, { id, name: file.name, finalName: file.name, status: "failed", progress: 0, error: refusal }]);
          continue;
        }
        setUploads((list) => [...list, { id, name: file.name, finalName: file.name, status: "uploading", progress: 0 }]);
        const controller = new AbortController();
        controllers.current.add(controller);
        void send(id, file, folder, controller);
      }
    },
    [maxBytes, send],
  );

  const dismiss = useCallback((id: number) => setUploads((list) => list.filter((u) => u.id !== id)), []);

  return { uploads, start, dismiss };
}

// folderRefusal is the one path rule a name cannot fix: the folder is as deep as files go.
function folderRefusal(folder: string): string | null {
  const error = pathError(folder, "f.png", "binary");
  return error ? lowerFirst(error) : null;
}

// DropZone takes figures dropped on it.
export function DropZone({ folder, maxBytes, onFiles }: { folder: string; maxBytes: number; onFiles: (files: File[]) => void }) {
  const [over, setOver] = useState(false);
  function onDrop(event: DragEvent) {
    event.preventDefault();
    setOver(false);
    const files = [...event.dataTransfer.files];
    if (files.length > 0) onFiles(files);
  }
  return (
    <div
      data-testid="figure-drop-zone"
      onDragOver={(e) => {
        e.preventDefault();
        setOver(true);
      }}
      onDragLeave={() => setOver(false)}
      onDrop={onDrop}
      className={`rounded-rs-md border border-dashed border-rs-accent-strong p-2.5 text-center text-rs-caption text-rs-text ${
        over ? "bg-rs-accent-soft" : ""
      }`}
    >
      Drop PNG, JPG or PDF figures here · max {formatBytes(maxBytes)} each
      {folder && <span className="block text-rs-text-muted">into {folder}/</span>}
    </div>
  );
}

// UploadList shows the uploads under way, the finished ones with the name they were stored
// under, and the refused ones with the reason.
export function UploadList({ uploads, onDismiss }: { uploads: Upload[]; onDismiss: (id: number) => void }) {
  if (uploads.length === 0) return null;
  const as = (u: Upload) => (u.finalName !== u.name ? ` as ${u.finalName}` : "");
  return (
    <ul aria-label="Uploads" className="flex flex-col gap-1.5">
      {uploads.map((u) => {
        if (u.status === "uploading") {
          return (
            <li key={u.id} className="flex flex-col gap-1 text-rs-caption">
              <span className="truncate">
                Uploading {u.name}
                {as(u)}
              </span>
              <span
                role="progressbar"
                aria-label={`Uploading ${u.name}`}
                aria-valuemin={0}
                aria-valuemax={100}
                aria-valuenow={Math.round(u.progress * 100)}
                className="block h-1.5 overflow-hidden rounded-rs-pill bg-rs-neutral"
              >
                <span className="block h-full bg-rs-accent-strong" style={{ width: `${Math.round(u.progress * 100)}%` }} />
              </span>
            </li>
          );
        }
        const failed = u.status === "failed";
        return (
          <li
            key={u.id}
            role={failed ? "alert" : "status"}
            className={`flex items-start justify-between gap-2 rounded-control border p-2 text-rs-caption ${
              failed ? "border-rs-danger-line bg-rs-danger-soft text-rs-danger-strong" : "border-rs-border bg-rs-subtle text-rs-text"
            }`}
          >
            <span className="min-w-0 break-words">
              {failed
                ? `${u.name} wasn’t uploaded: ${u.error}`
                : `${u.name} was uploaded${as(u)}. It’s being checked for malware before anyone can download it.`}
            </span>
            <button
              type="button"
              aria-label={`Dismiss the message about ${u.name}`}
              onClick={() => onDismiss(u.id)}
              className="flex-none rounded-rs-xs px-1 hover:bg-rs-surface focus-visible:shadow-focus focus-visible:outline-none"
            >
              ×
            </button>
          </li>
        );
      })}
    </ul>
  );
}

// StorageUse is the tree's footer: the project's figures against its storage limit (FR-12.2),
// "120 MB of 500 MB", in the warning colour from 90 % on.
export function StorageUse({ usedBytes, limitBytes }: { usedBytes: number; limitBytes: number }) {
  const full = limitBytes > 0 && usedBytes >= limitBytes * 0.9;
  return (
    <p className={`mt-auto border-t border-rs-border pt-2 text-rs-caption ${full ? "text-rs-warning" : "text-rs-text-muted"}`}>
      <span className="sr-only">Figure storage used: </span>
      {formatBytes(usedBytes)} of {formatBytes(limitBytes)}
    </p>
  );
}
