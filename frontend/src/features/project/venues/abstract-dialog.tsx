"use client";

import { type FormEvent, useId, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { FormError, TextAreaField } from "@/components/ui/field";
import { length } from "@/features/profile/validation";

import { type Project, updateProject } from "../api";
import { MAX_ABSTRACT_LENGTH, projectMessages } from "../validation";

// AbstractDialog lets an editor, who may not change the project's other details, add or edit
// its abstract (SRS 2.3 "edit the abstract", FR-10.2). It sends only { abstract }; an emptied
// abstract is cleared.
export function AbstractDialog({
  projectId,
  initial,
  onClose,
  onSaved,
  returnFocusId,
}: {
  projectId: string;
  initial: string | null;
  onClose: () => void;
  onSaved: (project: Project) => void;
  returnFocusId?: string;
}) {
  const id = useId();
  const [value, setValue] = useState(initial ?? "");
  const [error, setError] = useState<string | null>(null);
  const [formError, setFormError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const dirty = value !== (initial ?? "");

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    const text = value.trim();
    setFormError(null);
    if (length(text) > MAX_ABSTRACT_LENGTH) {
      setError(projectMessages.abstractLong);
      return;
    }
    setBusy(true);
    const res = await updateProject(projectId, { abstract: text === "" ? null : text });
    setBusy(false);
    if (res.ok) onSaved(res.data);
    else if (res.code === "bad_request" && res.field === "abstract") setError(projectMessages.abstractLong);
    else if (res.code === "forbidden") setFormError("You can no longer edit this project’s abstract.");
    else setFormError("We couldn’t save the abstract. Try again.");
  }

  return (
    <Dialog title="Add abstract" dirty={dirty} onClose={onClose} returnFocusId={returnFocusId}>
      <form noValidate onSubmit={submit} className="flex flex-col gap-3">
        {formError && <FormError look="rs">{formError}</FormError>}
        <TextAreaField
          id={`${id}-abstract`}
          label="Paper abstract"
          look="rs"
          rows={6}
          placeholder="Paste your abstract"
          error={error ?? undefined}
          counter={{ count: length(value.trim()), max: MAX_ABSTRACT_LENGTH }}
          value={value}
          onChange={(e) => {
            setValue(e.target.value);
            setError(null);
          }}
        />
        <div className="flex flex-wrap justify-end gap-2 pt-1">
          <button type="button" onClick={onClose} className={rsButtonClass({ variant: "secondary", size: "form" })}>
            Cancel
          </button>
          <button type="submit" disabled={busy} className={rsButtonClass({ size: "form" })}>
            Save
          </button>
        </div>
      </form>
    </Dialog>
  );
}
