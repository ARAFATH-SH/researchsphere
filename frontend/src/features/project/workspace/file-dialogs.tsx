"use client";

import { type FormEvent, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Field, FormError, SelectField } from "@/components/ui/field";
import type { ApiError, Result } from "@/lib/api/call";

import { fileErrorText, type ProjectFile } from "../files/api";
import { baseName, isInside, joinPath, parentPath, pathError } from "../files/tree";
import { ConfirmDialog } from "../membership/confirm-dialog";

// The dialogs of the file tree (FR-12.1): new file, new folder, rename, move and delete. Each
// checks the path rules first, then sends the change and stays open with the server's reason.

const topLevel = "";

// Errors about the name go under the name field; the rest above the buttons.
const nameErrors = new Set(["invalid_path", "path_taken", "parent_not_folder"]);

function folderLabel(path: string) {
  return path === topLevel ? "Top level of the project" : `${path}/`;
}

function Buttons({ busy, action, onClose }: { busy: boolean; action: string; onClose: () => void }) {
  return (
    <div className="flex flex-wrap justify-end gap-2">
      <button type="button" onClick={onClose} className={rsButtonClass({ variant: "secondary", size: "form" })}>
        Cancel
      </button>
      <button type="submit" disabled={busy} className={rsButtonClass({ size: "form" })}>
        {action}
      </button>
    </div>
  );
}

// useSubmit runs a change and keeps its error, split into the name field's and the form's.
function useSubmit<T>(onDone: (data: T) => void) {
  const [busy, setBusy] = useState(false);
  const [nameError, setNameError] = useState<string | null>(null);
  const [formError, setFormError] = useState<string | null>(null);
  async function submit(run: () => Promise<Result<T>>) {
    setBusy(true);
    setNameError(null);
    setFormError(null);
    const res = await run();
    if (res.ok) {
      onDone(res.data);
      return;
    }
    setBusy(false);
    const text = fileErrorText(res as ApiError);
    if (nameErrors.has(res.code)) setNameError(text);
    else setFormError(text);
  }
  return { busy, nameError, setNameError, formError, submit };
}

// CreateDialog makes a new empty text file or folder, in folder (which the user may change).
export function CreateDialog({
  kind,
  folders,
  folder,
  create,
  onDone,
  onClose,
  returnFocusId,
}: {
  kind: "text" | "folder";
  folders: ProjectFile[];
  folder: string;
  create: (path: string) => Promise<Result<ProjectFile>>;
  onDone: (file: ProjectFile) => void;
  onClose: () => void;
  returnFocusId?: string;
}) {
  const [name, setName] = useState("");
  const [where, setWhere] = useState(folder);
  const { busy, nameError, setNameError, formError, submit } = useSubmit(onDone);

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    const err = pathError(where, name.trim(), kind);
    if (err) {
      setNameError(err);
      return;
    }
    void submit(() => create(joinPath(where, name.trim())));
  }

  return (
    <Dialog
      title={kind === "text" ? "New file" : "New folder"}
      dirty={name !== ""}
      onClose={onClose}
      returnFocusId={returnFocusId}
    >
      <form noValidate onSubmit={onSubmit} className="flex flex-col gap-4">
        <Field
          id="new-file-name"
          look="rs"
          label={kind === "text" ? "File name" : "Folder name"}
          hint={kind === "text" ? "For example introduction.tex or references.bib. No spaces." : "No spaces."}
          value={name}
          error={nameError ?? undefined}
          onChange={(e) => setName(e.target.value)}
          autoComplete="off"
          spellCheck={false}
          required
        />
        <SelectField id="new-file-folder" look="rs" label="In folder" value={where} onChange={(e) => setWhere(e.target.value)}>
          <option value={topLevel}>{folderLabel(topLevel)}</option>
          {folders.map((f) => (
            <option key={f.id} value={f.path}>
              {folderLabel(f.path)}
            </option>
          ))}
        </SelectField>
        {formError && <FormError look="rs">{formError}</FormError>}
        <Buttons busy={busy} action="Create" onClose={onClose} />
      </form>
    </Dialog>
  );
}

// RenameDialog changes the name of a file or folder and leaves it in its folder.
export function RenameDialog({
  file,
  move,
  onDone,
  onClose,
  returnFocusId,
}: {
  file: ProjectFile;
  move: (path: string) => Promise<Result<ProjectFile>>;
  onDone: (file: ProjectFile) => void;
  onClose: () => void;
  returnFocusId?: string;
}) {
  const [name, setName] = useState(baseName(file.path));
  const { busy, nameError, setNameError, formError, submit } = useSubmit(onDone);
  const folder = parentPath(file.path);

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    const err = pathError(folder, name.trim(), file.kind);
    if (err) {
      setNameError(err);
      return;
    }
    void submit(() => move(joinPath(folder, name.trim())));
  }

  return (
    <Dialog title={`Rename ${baseName(file.path)}`} dirty={name !== baseName(file.path)} onClose={onClose} returnFocusId={returnFocusId}>
      <form noValidate onSubmit={onSubmit} className="flex flex-col gap-4">
        <Field
          id="rename-name"
          look="rs"
          label="New name"
          hint={file.kind === "binary" ? "Keep the extension of the figure’s type. No spaces." : "No spaces."}
          value={name}
          error={nameError ?? undefined}
          onChange={(e) => setName(e.target.value)}
          autoComplete="off"
          spellCheck={false}
          required
        />
        {formError && <FormError look="rs">{formError}</FormError>}
        <Buttons busy={busy} action="Rename" onClose={onClose} />
      </form>
    </Dialog>
  );
}

// MoveDialog moves a file or folder, with everything in it, to another folder. A folder cannot
// go into itself or a folder inside it, so those are not offered.
export function MoveDialog({
  file,
  folders,
  move,
  onDone,
  onClose,
  returnFocusId,
}: {
  file: ProjectFile;
  folders: ProjectFile[];
  move: (path: string) => Promise<Result<ProjectFile>>;
  onDone: (file: ProjectFile) => void;
  onClose: () => void;
  returnFocusId?: string;
}) {
  const from = parentPath(file.path);
  const [where, setWhere] = useState(from);
  const { busy, nameError, formError, submit } = useSubmit(onDone);
  const targets = folders.filter((f) => !(file.kind === "folder" && isInside(f.path, file.path)));
  const name = baseName(file.path);

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (where === from) {
      onClose();
      return;
    }
    void submit(() => move(joinPath(where, name)));
  }

  return (
    <Dialog title={`Move ${name}`} onClose={onClose} returnFocusId={returnFocusId}>
      <form noValidate onSubmit={onSubmit} className="flex flex-col gap-4">
        <SelectField
          id="move-folder"
          look="rs"
          label="Move to folder"
          value={where}
          error={nameError ?? undefined}
          onChange={(e) => setWhere(e.target.value)}
        >
          <option value={topLevel}>{folderLabel(topLevel)}</option>
          {targets.map((f) => (
            <option key={f.id} value={f.path}>
              {folderLabel(f.path)}
            </option>
          ))}
        </SelectField>
        {formError && <FormError look="rs">{formError}</FormError>}
        <Buttons busy={busy} action="Move" onClose={onClose} />
      </form>
    </Dialog>
  );
}

// DeleteFileDialog asks before deleting; a folder says how much goes with it.
export function DeleteFileDialog({
  file,
  inside,
  remove,
  onDone,
  onClose,
  returnFocusId,
}: {
  file: ProjectFile;
  inside: number;
  remove: () => Promise<Result<undefined>>;
  onDone: () => void;
  onClose: () => void;
  returnFocusId?: string;
}) {
  const name = baseName(file.path);
  const folder = file.kind === "folder";
  return (
    <ConfirmDialog
      title={folder ? `Delete folder ${name}?` : `Delete ${name}?`}
      action="Delete"
      destructive
      run={remove}
      onDone={onDone}
      onClose={onClose}
      returnFocusId={returnFocusId}
      errorText={fileErrorText}
    >
      {folder ? (
        inside > 0 ? (
          <p>
            <strong>
              The {inside === 1 ? "1 file or folder" : `${inside} files and folders`} inside it will be deleted too.
            </strong>{" "}
            This can’t be undone.
          </p>
        ) : (
          <p>The folder is empty. This can’t be undone.</p>
        )
      ) : (
        <p>This deletes the file for every member of the project. This can’t be undone.</p>
      )}
    </ConfirmDialog>
  );
}
