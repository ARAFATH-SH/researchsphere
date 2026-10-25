"use client";

import { type FormEvent, useMemo, useState } from "react";

import { buttonClass } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Field, FormError, SelectField } from "@/components/ui/field";
import { Spinner } from "@/components/ui/spinner";

import {
  addLanguage,
  type Language,
  type LanguageProficiency,
  removeLanguage,
  updateLanguage,
} from "./api";
import { proficiencyLabels } from "./format";
import { SaveButton } from "./save-button";
import {
  fieldForLanguageCode,
  genericError,
  type LanguageErrors,
  type LanguageField,
  languageMessages,
  type LanguageValues,
  validateLanguage,
} from "./validation";

type LanguageDialogProps = {
  // entry is the language to edit; without it the dialog adds a new one.
  entry?: Language;
  onClose: () => void;
  // onSaved closes the dialog and reloads the profile; so does onDeleted.
  onSaved: () => void;
  onDeleted: () => void;
  returnFocusId?: string;
};

// LanguageDialog adds or edits one language, and deletes it after a confirm step inside
// the dialog (FR-4.9).
export function LanguageDialog({
  entry,
  onClose,
  onSaved,
  onDeleted,
  returnFocusId,
}: LanguageDialogProps) {
  const initial = useMemo<LanguageValues>(
    () => ({ name: entry?.name ?? "", proficiency: entry?.proficiency ?? "" }),
    [entry],
  );
  const [values, setValues] = useState(initial);
  const [errors, setErrors] = useState<LanguageErrors>({});
  const [formError, setFormError] = useState<string>();
  const [saving, setSaving] = useState(false);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const dirty =
    values.name !== initial.name || values.proficiency !== initial.proficiency;

  function set(field: LanguageField, value: string) {
    setValues({ ...values, [field]: value });
  }

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError(undefined);
    const found = validateLanguage(values);
    setErrors(found);
    if (Object.keys(found).length > 0) return;

    const body = {
      name: values.name.trim(),
      proficiency: values.proficiency as LanguageProficiency,
    };
    setSaving(true);
    const res = entry
      ? await updateLanguage(entry.id, body)
      : await addLanguage(body);
    setSaving(false);
    if (res.ok) return onSaved();
    const field = fieldForLanguageCode(res.code);
    if (field)
      setErrors({
        [field]:
          res.code === "language_exists"
            ? languageMessages.exists
            : res.message,
      });
    else
      setFormError(
        res.code === "language_limit_reached"
          ? languageMessages.limitReached
          : genericError,
      );
  }

  async function onDelete() {
    if (!entry) return;
    setFormError(undefined);
    setDeleting(true);
    const res = await removeLanguage(entry.id);
    setDeleting(false);
    if (res.ok || res.code === "not_found") return onDeleted();
    setConfirmingDelete(false);
    setFormError(genericError);
  }

  return (
    <Dialog
      title={entry ? "Edit language" : "Add language"}
      dirty={dirty}
      onClose={onClose}
      returnFocusId={returnFocusId}
    >
      <form noValidate onSubmit={onSubmit} className="space-y-4">
        {formError ? <FormError>{formError}</FormError> : null}
        <Field
          id="language-name"
          name="name"
          label="Language"
          required
          error={errors.name}
          value={values.name}
          onChange={(e) => set("name", e.target.value)}
        />
        <SelectField
          id="language-proficiency"
          name="proficiency"
          label="Proficiency"
          required
          error={errors.proficiency}
          value={values.proficiency}
          onChange={(e) => set("proficiency", e.target.value)}
        >
          <option value="">Please select</option>
          {Object.entries(proficiencyLabels).map(([value, label]) => (
            <option key={value} value={value}>
              {label}
            </option>
          ))}
        </SelectField>
        <SaveButton saving={saving} />
        {entry &&
          (confirmingDelete ? (
            <div
              role="group"
              aria-label="Confirm delete"
              className="space-y-3 rounded-lg border border-danger p-4"
            >
              <p className="text-sm font-semibold text-text">
                Delete this language? This cannot be undone.
              </p>
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
              className={buttonClass({
                variant: "text",
                size: "md",
                full: true,
              })}
            >
              Delete language
            </button>
          ))}
      </form>
    </Dialog>
  );
}
