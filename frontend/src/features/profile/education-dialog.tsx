"use client";

import { type FormEvent, useMemo, useState } from "react";

import { buttonClass } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Field, FormError } from "@/components/ui/field";
import { Spinner } from "@/components/ui/spinner";

import { addEducation, type Education, type Profile, removeEducation, updateEducation } from "./api";
import { EditButton } from "./edit-button";
import { yearRange } from "./format";
import { orNull } from "./profile-update";
import { SaveButton } from "./save-button";
import {
  type EducationErrors,
  type EducationField,
  type EducationValues,
  fieldForEducationCode,
  genericError,
  latestYear,
  MIN_EDUCATION_YEAR,
  validateEducation,
} from "./validation";

// MAX_EDUCATION_ENTRIES is how many entries a user may have (FR-4.4).
export const MAX_EDUCATION_ENTRIES = 20;

type EducationDialogProps = {
  profile: Profile;
  onClose: () => void;
  // onSaved closes the dialog and reloads the profile; message is the toast to show.
  onSaved: (message?: string) => void;
  returnFocusId?: string;
};

// The dialog shows the list of entries first; Add education or an entry's pencil opens
// the form. "new" is the form for a new entry.
type View = { kind: "list" } | { kind: "form"; entry: Education | "new" };

// EducationDialog adds, edits and removes education entries (FR-4.4).
export function EducationDialog({ profile, onClose, onSaved, returnFocusId }: EducationDialogProps) {
  const [view, setView] = useState<View>({ kind: "list" });
  const [dirty, setDirty] = useState(false);

  if (view.kind === "form") {
    const entry = view.entry === "new" ? undefined : view.entry;
    return (
      // A key per view mounts a fresh dialog, so the focus moves into the form.
      <Dialog
        key="form"
        title={entry ? "Edit education entry" : "Add education"}
        dirty={dirty}
        onClose={onClose}
        returnFocusId={returnFocusId}
      >
        <EducationForm entry={entry} onDirtyChange={setDirty} onSaved={onSaved} />
      </Dialog>
    );
  }

  const full = profile.education.length >= MAX_EDUCATION_ENTRIES;
  return (
    <Dialog key="list" title="Edit education" onClose={onClose} returnFocusId={returnFocusId}>
      <div className="space-y-4">
        {profile.education.length > 0 ? (
          <ul aria-label="Education entries" className="divide-y divide-border">
            {profile.education.map((e) => (
              <li key={e.id} className="flex items-start justify-between gap-4 py-3 first:pt-0">
                <div>
                  <p className="font-semibold text-text">{e.institution}</p>
                  {(e.degree || e.fieldOfStudy) && (
                    <p className="text-sm text-text">{[e.degree, e.fieldOfStudy].filter(Boolean).join(", ")}</p>
                  )}
                  <p className="text-sm text-text-muted">{yearRange(e.startYear, e.endYear)}</p>
                </div>
                <EditButton
                  label={`Edit ${e.institution}`}
                  onClick={() => setView({ kind: "form", entry: e })}
                />
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-sm text-text-muted">No education entries yet.</p>
        )}
        <button
          type="button"
          disabled={full}
          onClick={() => setView({ kind: "form", entry: "new" })}
          className={buttonClass({ variant: "outline", full: true })}
        >
          Add education
        </button>
        {full && (
          <p className="text-sm text-text-muted">
            You have reached the limit of {MAX_EDUCATION_ENTRIES} entries. Remove one to add another.
          </p>
        )}
      </div>
    </Dialog>
  );
}

function valuesOf(e?: Education): EducationValues {
  return {
    institution: e?.institution ?? "",
    degree: e?.degree ?? "",
    fieldOfStudy: e?.fieldOfStudy ?? "",
    startYear: e ? String(e.startYear) : "",
    endYear: e?.endYear != null ? String(e.endYear) : "",
    current: e ? e.endYear === null : false,
  };
}

type EducationFormProps = {
  entry?: Education;
  onDirtyChange: (dirty: boolean) => void;
  onSaved: (message?: string) => void;
};

function EducationForm({ entry, onDirtyChange, onSaved }: EducationFormProps) {
  const initial = useMemo(() => valuesOf(entry), [entry]);
  const [values, setValues] = useState(initial);
  const [errors, setErrors] = useState<EducationErrors>({});
  const [formError, setFormError] = useState<string>();
  const [saving, setSaving] = useState(false);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const latest = latestYear();

  function update(next: EducationValues) {
    setValues(next);
    onDirtyChange((Object.keys(initial) as (keyof EducationValues)[]).some((k) => next[k] !== initial[k]));
  }

  function set(field: EducationField, value: string) {
    update({ ...values, [field]: value });
  }

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError(undefined);
    const found = validateEducation(values);
    setErrors(found);
    if (Object.keys(found).length > 0) return;

    const body = {
      institution: values.institution.trim(),
      degree: orNull(values.degree),
      fieldOfStudy: orNull(values.fieldOfStudy),
      startYear: Number(values.startYear.trim()),
      // A current entry has no end year (FR-4.4).
      endYear: values.current ? null : Number(values.endYear.trim()),
    };
    setSaving(true);
    const res = entry ? await updateEducation(entry.id, body) : await addEducation(body);
    setSaving(false);
    if (res.ok) return onSaved();
    const field = fieldForEducationCode(res.code);
    if (field) setErrors({ [field]: res.message });
    else setFormError(res.code === "education_limit_reached" ? res.message : genericError);
  }

  async function onDelete() {
    if (!entry) return;
    setFormError(undefined);
    setDeleting(true);
    const res = await removeEducation(entry.id);
    setDeleting(false);
    if (res.ok || res.code === "not_found") return onSaved("Deleted");
    setConfirmingDelete(false);
    setFormError(genericError);
  }

  const years = `From ${MIN_EDUCATION_YEAR} to ${latest}.`;
  const text = (field: "institution" | "degree" | "fieldOfStudy", label: string, required = false) => (
    <Field
      id={`education-${field}`}
      name={field}
      label={label}
      required={required}
      error={errors[field]}
      value={values[field]}
      onChange={(e) => set(field, e.target.value)}
    />
  );
  const yearField = (field: "startYear" | "endYear", label: string, disabled = false) => (
    <Field
      id={`education-${field}`}
      name={field}
      label={label}
      inputMode="numeric"
      maxLength={4}
      hint={years}
      disabled={disabled}
      error={errors[field]}
      value={values[field]}
      onChange={(e) => set(field, e.target.value)}
      className="flex-1"
    />
  );

  return (
    <form noValidate onSubmit={onSubmit} className="space-y-4">
      {formError ? <FormError>{formError}</FormError> : null}
      {text("institution", "Institution", true)}
      {text("degree", "Degree (optional)")}
      {text("fieldOfStudy", "Field of study (optional)")}
      <div className="flex flex-col gap-4 sm:flex-row">
        {yearField("startYear", "Start year")}
        {yearField("endYear", "End year", values.current)}
      </div>
      <label className="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          name="current"
          checked={values.current}
          onChange={(e) => {
            // Studying here now means there is no end year yet.
            update({ ...values, current: e.target.checked, endYear: "" });
            if (e.target.checked) setErrors({ ...errors, endYear: undefined });
          }}
          className="size-4 accent-primary focus-visible:ring-2 focus-visible:ring-primary"
        />
        I currently study here
      </label>
      <SaveButton saving={saving} />
      {entry &&
        (confirmingDelete ? (
          <div role="group" aria-label="Confirm delete" className="space-y-3 rounded-lg border border-danger p-4">
            <p className="text-sm font-semibold text-text">Delete this entry? This cannot be undone.</p>
            <div className="flex flex-wrap justify-end gap-2">
              <button
                type="button"
                onClick={() => setConfirmingDelete(false)}
                className={buttonClass({ variant: "outline", size: "md" })}
              >
                Cancel
              </button>
              <button
                type="button"
                disabled={deleting}
                onClick={() => void onDelete()}
                className={buttonClass({ variant: "danger", size: "md" })}
              >
                {deleting && <Spinner />}
                {deleting ? "Deleting…" : "Delete"}
              </button>
            </div>
          </div>
        ) : (
          <button
            type="button"
            onClick={() => setConfirmingDelete(true)}
            className={buttonClass({ variant: "text", size: "md", full: true })}
          >
            Delete entry
          </button>
        ))}
    </form>
  );
}
