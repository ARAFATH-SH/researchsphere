"use client";

import { type FormEvent, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Field, FormError } from "@/components/ui/field";

import { deleteProject } from "./api";

export const mismatchMessage = "Type the project’s title exactly as it is written.";

// DeleteProjectDialog deletes a project once the owner types its title (FR-10.6). The
// title must match letter case included, after trimming both, as the server checks it.
export function DeleteProjectDialog({
  projectId,
  title,
  onClose,
  onDeleted,
  returnFocusId,
}: {
  projectId: string;
  title: string;
  onClose: () => void;
  onDeleted: () => void;
  returnFocusId?: string;
}) {
  const [typed, setTyped] = useState("");
  const [fieldError, setFieldError] = useState<string>();
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const matches = typed.trim() === title.trim();

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!matches || busy) return;
    setError(null);
    setFieldError(undefined);
    setBusy(true);
    const res = await deleteProject(projectId, typed.trim());
    setBusy(false);
    if (res.ok) onDeleted();
    else if (res.code === "confirmation_mismatch") setFieldError(mismatchMessage);
    else if (res.code === "not_found") onDeleted();
    else setError("We couldn’t delete the project. Try again.");
  }

  return (
    <Dialog title="Delete project" dirty={typed !== ""} onClose={onClose} returnFocusId={returnFocusId}>
      <form noValidate onSubmit={submit} className="flex flex-col gap-3.5">
        {error && <FormError look="rs">{error}</FormError>}
        <p className="text-rs-body text-rs-text">
          This deletes <strong className="font-bold">{title}</strong> with its members, files and join requests. It
          can’t be undone.
        </p>
        <Field
          id="delete-project-confirm"
          label="Type the project title to confirm"
          look="rs"
          autoComplete="off"
          error={fieldError}
          value={typed}
          onChange={(e) => {
            setTyped(e.target.value);
            setFieldError(undefined);
          }}
        />
        <div className="flex flex-wrap justify-end gap-2 pt-1">
          <button type="button" onClick={onClose} className={rsButtonClass({ variant: "secondary", size: "form" })}>
            Cancel
          </button>
          <button
            type="submit"
            disabled={!matches || busy}
            className={rsButtonClass({ variant: "destructive", size: "form" })}
          >
            Delete project
          </button>
        </div>
      </form>
    </Dialog>
  );
}
