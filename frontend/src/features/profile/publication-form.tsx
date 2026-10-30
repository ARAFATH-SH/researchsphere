"use client";

import { type FormEvent, useState } from "react";

import { Dialog } from "@/components/ui/dialog";
import { Field, FieldRow, FormError, TextAreaField } from "@/components/ui/field";

import {
  abstractMax,
  type ManualErrors,
  type ManualValues,
  manualBody,
  publicationError,
  validateManual,
} from "./publication-errors";
import { type Publication, updatePublication } from "./publications-api";
import { SaveButton } from "./save-button";

type ManualFieldsProps = {
  // idPrefix keeps the ids apart when two forms could be on the page.
  idPrefix: string;
  values: ManualValues;
  errors: ManualErrors;
  onChange: (values: ManualValues) => void;
};

// ManualFields are the fields of a publication added by hand (FR-5.1), laid out as the
// "Add manually" part of design/screens/ProfileEdit.html: Title* and Venue, Year* and Link,
// then the optional DOI and the abstract with its counter.
export function ManualFields({ idPrefix, values, errors, onChange }: ManualFieldsProps) {
  const set = (field: keyof ManualValues) => (value: string) =>
    onChange({ ...values, [field]: value });
  const id = (field: string) => `${idPrefix}-${field}`;
  return (
    <>
      <FieldRow>
        <Field
          id={id("title")}
          label="Title"
          look="rs"
          required
          maxLength={500}
          error={errors.title}
          value={values.title}
          onChange={(e) => set("title")(e.target.value)}
        />
        <Field
          id={id("venue")}
          label="Venue"
          look="rs"
          maxLength={300}
          error={errors.venue}
          value={values.venue}
          onChange={(e) => set("venue")(e.target.value)}
        />
      </FieldRow>
      <FieldRow>
        <Field
          id={id("year")}
          label="Year"
          look="rs"
          required
          inputMode="numeric"
          maxLength={4}
          error={errors.year}
          value={values.year}
          onChange={(e) => set("year")(e.target.value)}
        />
        <Field
          id={id("url")}
          label="Link"
          look="rs"
          type="url"
          maxLength={500}
          error={errors.url}
          value={values.url}
          onChange={(e) => set("url")(e.target.value)}
        />
      </FieldRow>
      <Field
        id={id("doi")}
        label="DOI (optional)"
        look="rs"
        error={errors.doi}
        value={values.doi}
        onChange={(e) => set("doi")(e.target.value)}
      />
      <TextAreaField
        id={id("abstract")}
        label="Abstract"
        look="rs"
        rows={4}
        error={errors.abstract}
        counter={{ count: [...values.abstract].length, max: abstractMax }}
        value={values.abstract}
        onChange={(e) => set("abstract")(e.target.value)}
      />
    </>
  );
}

// splitErrors puts a refused request's message under its field, or returns it for the banner.
export function splitErrors(field: keyof ManualErrors, message: string): {
  errors: ManualErrors;
  formError?: string;
} {
  return field === "form" || field === "orcid"
    ? { errors: {}, formError: message }
    : { errors: { [field]: message } };
}

function valuesOf(p: Publication): ManualValues {
  return {
    title: p.title,
    venue: p.venue ?? "",
    year: p.year === null ? "" : String(p.year),
    url: p.url ?? "",
    doi: p.doi ?? "",
    abstract: p.abstract ?? "",
  };
}

// EditPublicationDialog changes a publication the user added by hand (FR-5.1). The API
// refuses one that is shared with another user (publication_shared_readonly); the dialog
// then says so and keeps the user's text.
export function EditPublicationDialog({
  publication,
  onClose,
  onSaved,
  returnFocusId,
}: {
  publication: Publication;
  onClose: () => void;
  onSaved: () => void;
  returnFocusId?: string;
}) {
  const [initial] = useState(() => valuesOf(publication));
  const [values, setValues] = useState(initial);
  const [errors, setErrors] = useState<ManualErrors>({});
  const [formError, setFormError] = useState<string>();
  const [saving, setSaving] = useState(false);
  const dirty = (Object.keys(initial) as (keyof ManualValues)[]).some(
    (k) => initial[k] !== values[k],
  );

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError(undefined);
    const found = validateManual(values);
    setErrors(found);
    if (Object.keys(found).length > 0) return;
    setSaving(true);
    const res = await updatePublication(publication.id, manualBody(values));
    setSaving(false);
    if (res.ok) return onSaved();
    const { field, message } = publicationError(res);
    const split = splitErrors(field, message);
    setErrors(split.errors);
    setFormError(split.formError);
  }

  return (
    <Dialog title="Edit publication" dirty={dirty} onClose={onClose} returnFocusId={returnFocusId}>
      <form noValidate onSubmit={onSubmit} className="space-y-3">
        {formError ? <FormError look="rs">{formError}</FormError> : null}
        <ManualFields idPrefix="edit-publication" values={values} errors={errors} onChange={setValues} />
        <div className="flex justify-end pt-1">
          <SaveButton saving={saving} />
        </div>
      </form>
    </Dialog>
  );
}
